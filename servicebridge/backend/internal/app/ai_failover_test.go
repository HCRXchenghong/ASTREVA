package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// aiFailoverStub 是 OpenAI 兼容上游桩：统计 /chat/completions 命中次数，
// status != 0 时返回对应错误码（模拟 5xx / 401 / 403），否则返回固定回复。
type aiFailoverStub struct {
	server *httptest.Server
	calls  int32
	status int32
	reply  string
}

func newAIFailoverStub(t *testing.T, reply string) *aiFailoverStub {
	t.Helper()
	stub := &aiFailoverStub{reply: reply}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&stub.calls, 1)
		if status := atomic.LoadInt32(&stub.status); status != 0 {
			http.Error(w, "upstream boom", int(status))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": stub.reply}},
			},
		})
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *aiFailoverStub) url() string    { return s.server.URL }
func (s *aiFailoverStub) callCount() int { return int(atomic.LoadInt32(&s.calls)) }
func (s *aiFailoverStub) setStatus(status int) {
	atomic.StoreInt32(&s.status, int32(status))
}

// createFailoverProvider 创建一个文本供应商，base_url 指向给定桩。
func createFailoverProvider(t *testing.T, st store.Store, name, baseURL, apiKey string) domain.AIProvider {
	t.Helper()
	provider, err := st.CreateAIProvider(domain.AIProvider{
		Name:    name,
		Kind:    domain.AIProviderKindText,
		APIType: domain.AIAPITypeChatCompletions,
		BaseURL: baseURL,
		Models:  []string{name + "-model"},
		Enabled: true,
	}, apiKey)
	if err != nil {
		t.Fatalf("创建供应商 %s 失败：%v", name, err)
	}
	return provider
}

// addFailoverTarget 为某能力追加一条候选供应商。
func addFailoverTarget(t *testing.T, st store.Store, capability string, provider domain.AIProvider, model string, priority int) {
	t.Helper()
	if _, err := st.UpsertAIBindingTarget(domain.AIBindingTarget{
		Capability: capability,
		ProviderID: provider.ID,
		Model:      model,
		Priority:   priority,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("写入候选供应商失败：%v", err)
	}
}

// setTestCircuit 用可注入时钟与自定义冷却替换熔断器，便于断言 open/half_open 行为。
func setTestCircuit(service *Service, cooldown, authCooldown time.Duration, now func() time.Time) {
	service.circuit = newAICircuitBreaker(cooldown, authCooldown, now)
}

// healthFor 从健康快照里取出指定供应商。
func healthFor(t *testing.T, service *Service, providerID string) domain.AIProviderHealth {
	t.Helper()
	for _, item := range service.AIProviderHealth() {
		if item.ID == providerID {
			return item
		}
	}
	t.Fatalf("健康快照缺少供应商 %s", providerID)
	return domain.AIProviderHealth{}
}

// TestAIFailoverTargetsPriority：targets 按 priority 升序尝试，优先级最高的正常时只打它。
func TestAIFailoverTargetsPriority(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	primaryStub := newAIFailoverStub(t, "primary-reply")
	secondaryStub := newAIFailoverStub(t, "secondary-reply")
	service, st := newAIProviderService(t, globalStub.url())

	primary := createFailoverProvider(t, st, "主供应商", primaryStub.url(), "key-primary")
	secondary := createFailoverProvider(t, st, "备供应商", secondaryStub.url(), "key-secondary")
	addFailoverTarget(t, st, domain.AICapabilityTranslation, primary, "primary-model", 0)
	addFailoverTarget(t, st, domain.AICapabilityTranslation, secondary, "secondary-model", 1)
	addFailoverTarget(t, st, domain.AICapabilityReception, primary, "primary-model", 0)
	addFailoverTarget(t, st, domain.AICapabilityReception, secondary, "secondary-model", 1)

	// 翻译能力：命中 priority=0 的主供应商。
	translated := service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola, necesito ayuda", "")
	if got := translated["zh"]; got != "primary-reply" {
		t.Fatalf("译文 = %q, want primary-reply", got)
	}
	if primaryStub.callCount() != 1 || secondaryStub.callCount() != 0 || globalStub.callCount() != 0 {
		t.Fatalf("优先级调用次数 primary=%d secondary=%d global=%d，期望 1/0/0",
			primaryStub.callCount(), secondaryStub.callCount(), globalStub.callCount())
	}

	// 接待能力：同样只打主供应商（访客语言用中文，避免触发回复译文再调一次翻译）。
	conversationID := seedAIServingConversation(t, st, "192.0.2.31")
	service.generateAIReply(conversationID, "", "你好，介绍一下产品", ai.LangZh)
	reply, ok := lastAIMessage(st, conversationID)
	if !ok || reply.Content != "primary-reply" {
		t.Fatalf("接待回复 = %#v, want primary-reply", reply)
	}
	if primaryStub.callCount() != 2 || secondaryStub.callCount() != 0 || globalStub.callCount() != 0 {
		t.Fatalf("接待调用次数 primary=%d secondary=%d global=%d，期望 2/0/0",
			primaryStub.callCount(), secondaryStub.callCount(), globalStub.callCount())
	}
}

// TestAIFailoverSwitchesToNextTargetOn5xx：主供应商 500 时自动切到下一个，
// 调用方拿到备供应商的结果（翻译与接待各验证一次）。
func TestAIFailoverSwitchesToNextTargetOn5xx(t *testing.T) {
	t.Run("translation", func(t *testing.T) {
		globalStub := newAIFailoverStub(t, "global-reply")
		brokenStub := newAIFailoverStub(t, "broken-reply")
		backupStub := newAIFailoverStub(t, "backup-reply")
		brokenStub.setStatus(http.StatusInternalServerError)
		service, st := newAIProviderService(t, globalStub.url())

		broken := createFailoverProvider(t, st, "故障供应商", brokenStub.url(), "key-broken")
		backup := createFailoverProvider(t, st, "备用供应商", backupStub.url(), "key-backup")
		addFailoverTarget(t, st, domain.AICapabilityTranslation, broken, "broken-model", 0)
		addFailoverTarget(t, st, domain.AICapabilityTranslation, backup, "backup-model", 1)

		translated := service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola, necesito ayuda", "")
		if got := translated["zh"]; got != "backup-reply" {
			t.Fatalf("故障转移后译文 = %q, want backup-reply", got)
		}
		if brokenStub.callCount() != 1 || backupStub.callCount() != 1 || globalStub.callCount() != 0 {
			t.Fatalf("调用次数 broken=%d backup=%d global=%d，期望 1/1/0",
				brokenStub.callCount(), backupStub.callCount(), globalStub.callCount())
		}
	})

	t.Run("reception", func(t *testing.T) {
		globalStub := newAIFailoverStub(t, "global-reply")
		brokenStub := newAIFailoverStub(t, "broken-reply")
		backupStub := newAIFailoverStub(t, "backup-reply")
		brokenStub.setStatus(http.StatusInternalServerError)
		service, st := newAIProviderService(t, globalStub.url())

		broken := createFailoverProvider(t, st, "故障供应商", brokenStub.url(), "key-broken")
		backup := createFailoverProvider(t, st, "备用供应商", backupStub.url(), "key-backup")
		addFailoverTarget(t, st, domain.AICapabilityReception, broken, "broken-model", 0)
		addFailoverTarget(t, st, domain.AICapabilityReception, backup, "backup-model", 1)

		conversationID := seedAIServingConversation(t, st, "192.0.2.32")
		service.generateAIReply(conversationID, "", "你好", ai.LangZh)
		reply, ok := lastAIMessage(st, conversationID)
		if !ok || reply.Content != "backup-reply" {
			t.Fatalf("接待回复 = %#v, want backup-reply", reply)
		}
		if brokenStub.callCount() != 1 || backupStub.callCount() != 1 || globalStub.callCount() != 0 {
			t.Fatalf("调用次数 broken=%d backup=%d global=%d，期望 1/1/0",
				brokenStub.callCount(), backupStub.callCount(), globalStub.callCount())
		}
	})
}

// TestAICircuitOpensAfterThreeFailuresAndHalfOpenProbe 覆盖熔断核心语义：
// 连续 3 次失败打开 → 后续请求完全跳过该供应商 → 冷却后 half_open 只放行一次：
// 成功关闭并清零；再次失败立即重新打开。
func TestAICircuitOpensAfterThreeFailuresAndHalfOpenProbe(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	brokenStub := newAIFailoverStub(t, "broken-reply")
	backupStub := newAIFailoverStub(t, "backup-reply")
	brokenStub.setStatus(http.StatusInternalServerError)
	service, st := newAIProviderService(t, globalStub.url())

	broken := createFailoverProvider(t, st, "故障供应商", brokenStub.url(), "key-broken")
	backup := createFailoverProvider(t, st, "备用供应商", backupStub.url(), "key-backup")
	addFailoverTarget(t, st, domain.AICapabilityTranslation, broken, "broken-model", 0)
	addFailoverTarget(t, st, domain.AICapabilityTranslation, backup, "backup-model", 1)

	clock := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	setTestCircuit(service, 100*time.Millisecond, 10*time.Minute, func() time.Time { return clock })

	// translate 触发一次翻译并返回译文；故障供应商正常时译文为 broken-reply，
	// 失败切换到备供应商时为 backup-reply。
	translate := func() string {
		t.Helper()
		translated := service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola, necesito ayuda", "")
		return translated["zh"]
	}
	expectTranslated := func(want string) {
		t.Helper()
		if got := translate(); got != want {
			t.Fatalf("译文 = %q, want %q", got, want)
		}
	}

	// 连续 3 次失败：每次都先打故障供应商再切备供应商。
	for index := 0; index < 3; index++ {
		expectTranslated("backup-reply")
	}
	if brokenStub.callCount() != 3 || backupStub.callCount() != 3 {
		t.Fatalf("3 次失败后调用次数 broken=%d backup=%d，期望 3/3",
			brokenStub.callCount(), backupStub.callCount())
	}
	health := healthFor(t, service, broken.ID)
	if health.State != domain.AICircuitStateOpen || health.ConsecutiveFailures != 3 {
		t.Fatalf("连续 3 次失败后应为 open/3，实际 %+v", health)
	}
	if health.OpenedUntil == nil || !health.OpenedUntil.Equal(clock.Add(100*time.Millisecond)) {
		t.Fatalf("打开时间 = %v, want %v", health.OpenedUntil, clock.Add(100*time.Millisecond))
	}
	if strings.Contains(health.LastError, "key-broken") {
		t.Fatalf("健康快照泄露密钥：%q", health.LastError)
	}

	// open 期间：完全跳过故障供应商，调用次数不再增长。
	expectTranslated("backup-reply")
	if brokenStub.callCount() != 3 || backupStub.callCount() != 4 {
		t.Fatalf("open 期间应跳过故障供应商：broken=%d backup=%d，期望 3/4",
			brokenStub.callCount(), backupStub.callCount())
	}

	// 冷却结束 + 上游恢复：half_open 放行一次探测并成功 → closed 且清零。
	clock = clock.Add(time.Second)
	brokenStub.setStatus(0)
	expectTranslated("broken-reply")
	if brokenStub.callCount() != 4 {
		t.Fatalf("half_open 应放行一次探测：broken=%d，期望 4", brokenStub.callCount())
	}
	health = healthFor(t, service, broken.ID)
	if health.State != domain.AICircuitStateClosed || health.ConsecutiveFailures != 0 || health.OpenedUntil != nil {
		t.Fatalf("探测成功后应为 closed/0，实际 %+v", health)
	}

	// 再次连续失败 3 次 → 重新打开；随后 half_open 探测失败 → 立即重新打开。
	brokenStub.setStatus(http.StatusInternalServerError)
	for index := 0; index < 3; index++ {
		expectTranslated("backup-reply")
	}
	if health = healthFor(t, service, broken.ID); health.State != domain.AICircuitStateOpen || health.ConsecutiveFailures != 3 {
		t.Fatalf("再次失败 3 次后应为 open/3，实际 %+v", health)
	}
	callsBeforeProbe := brokenStub.callCount()
	clock = clock.Add(time.Second)
	expectTranslated("backup-reply")
	if brokenStub.callCount() != callsBeforeProbe+1 {
		t.Fatalf("half_open 应只放行一次探测：before=%d after=%d", callsBeforeProbe, brokenStub.callCount())
	}
	health = healthFor(t, service, broken.ID)
	if health.State != domain.AICircuitStateOpen || health.ConsecutiveFailures != 4 {
		t.Fatalf("探测失败后应重新打开且计数递增，实际 %+v", health)
	}
	if health.OpenedUntil == nil || !health.OpenedUntil.Equal(clock.Add(100*time.Millisecond)) {
		t.Fatalf("重新打开时间 = %v, want %v", health.OpenedUntil, clock.Add(100*time.Millisecond))
	}

	// 重新打开后继续跳过，不再增加调用。
	expectTranslated("backup-reply")
	if brokenStub.callCount() != callsBeforeProbe+1 {
		t.Fatalf("重新打开后应继续跳过：broken=%d，期望 %d", brokenStub.callCount(), callsBeforeProbe+1)
	}
}

// TestAICircuitAuthFailureCooldownLonger：401/403 的冷却时间显著长于普通 5xx，避免反复打无效 Key。
func TestAICircuitAuthFailureCooldownLonger(t *testing.T) {
	const (
		baseCooldown = 30 * time.Second
		authCooldown = 5 * time.Minute
	)
	clock := time.Date(2026, 4, 1, 9, 30, 0, 0, time.UTC)

	openAfterFailures := func(t *testing.T, status int) domain.AIProviderHealth {
		t.Helper()
		globalStub := newAIFailoverStub(t, "global-reply")
		brokenStub := newAIFailoverStub(t, "broken-reply")
		backupStub := newAIFailoverStub(t, "backup-reply")
		brokenStub.setStatus(status)
		service, st := newAIProviderService(t, globalStub.url())
		broken := createFailoverProvider(t, st, "故障供应商", brokenStub.url(), "key-broken")
		backup := createFailoverProvider(t, st, "备用供应商", backupStub.url(), "key-backup")
		addFailoverTarget(t, st, domain.AICapabilityTranslation, broken, "broken-model", 0)
		addFailoverTarget(t, st, domain.AICapabilityTranslation, backup, "backup-model", 1)
		setTestCircuit(service, baseCooldown, authCooldown, func() time.Time { return clock })

		for index := 0; index < 3; index++ {
			service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola", "")
		}
		return healthFor(t, service, broken.ID)
	}

	auth := openAfterFailures(t, http.StatusUnauthorized)
	if auth.State != domain.AICircuitStateOpen || auth.OpenedUntil == nil {
		t.Fatalf("401 应打开熔断，实际 %+v", auth)
	}
	if !auth.OpenedUntil.Equal(clock.Add(authCooldown)) {
		t.Fatalf("401 冷却 = %v, want %v", auth.OpenedUntil, clock.Add(authCooldown))
	}

	transient := openAfterFailures(t, http.StatusInternalServerError)
	if transient.State != domain.AICircuitStateOpen || transient.OpenedUntil == nil {
		t.Fatalf("500 应打开熔断，实际 %+v", transient)
	}
	if !transient.OpenedUntil.Equal(clock.Add(baseCooldown)) {
		t.Fatalf("500 冷却 = %v, want %v", transient.OpenedUntil, clock.Add(baseCooldown))
	}
	if !auth.OpenedUntil.After(*transient.OpenedUntil) {
		t.Fatalf("401 冷却应长于 500：auth=%v transient=%v", auth.OpenedUntil, transient.OpenedUntil)
	}
}

// TestAICircuitCooldownEnvOverride：环境变量可覆盖冷却时间，且收敛到最小 5 秒。
func TestAICircuitCooldownEnvOverride(t *testing.T) {
	t.Setenv("AI_CIRCUIT_COOLDOWN_MS", "100")
	if got := envAICircuitCooldown("AI_CIRCUIT_COOLDOWN_MS", defaultAICircuitCooldown); got != minAICircuitCooldown {
		t.Fatalf("过小配置应收敛到最小值：got %v, want %v", got, minAICircuitCooldown)
	}
	t.Setenv("AI_CIRCUIT_COOLDOWN_MS", "45000")
	if got := envAICircuitCooldown("AI_CIRCUIT_COOLDOWN_MS", defaultAICircuitCooldown); got != 45*time.Second {
		t.Fatalf("配置未生效：got %v, want 45s", got)
	}
	t.Setenv("AI_CIRCUIT_COOLDOWN_MS", "not-a-number")
	if got := envAICircuitCooldown("AI_CIRCUIT_COOLDOWN_MS", defaultAICircuitCooldown); got != defaultAICircuitCooldown {
		t.Fatalf("非法配置应回落默认：got %v, want %v", got, defaultAICircuitCooldown)
	}
	t.Setenv("AI_CIRCUIT_COOLDOWN_MS", "")
	if got := envAICircuitCooldown("AI_CIRCUIT_COOLDOWN_MS", defaultAICircuitCooldown); got != defaultAICircuitCooldown {
		t.Fatalf("未配置应回落默认：got %v, want %v", got, defaultAICircuitCooldown)
	}
}

// TestAIFailoverAllCandidatesFail：全链失败返回带尝试次数的错误，且绝不泄露任何密钥。
func TestAIFailoverAllCandidatesFail(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	primaryStub := newAIFailoverStub(t, "primary-reply")
	backupStub := newAIFailoverStub(t, "backup-reply")
	primaryStub.setStatus(http.StatusInternalServerError)
	backupStub.setStatus(http.StatusBadGateway)
	globalStub.setStatus(http.StatusServiceUnavailable)
	service, st := newAIProviderService(t, globalStub.url())

	primary := createFailoverProvider(t, st, "主供应商", primaryStub.url(), "key-primary-secret")
	backup := createFailoverProvider(t, st, "备供应商", backupStub.url(), "key-backup-secret")
	addFailoverTarget(t, st, domain.AICapabilityTranslation, primary, "primary-model", 0)
	addFailoverTarget(t, st, domain.AICapabilityTranslation, backup, "backup-model", 1)

	visitor, err := st.CreateVisitorConversation("192.0.2.40", "web", "en", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	_, err = service.TranslatePreview(context.Background(), "agent_missing", visitor.Conversation.ID, "Hola, necesito ayuda")
	if err == nil {
		t.Fatal("全链失败应返回错误")
	}
	if !errors.Is(err, ErrAIUpstreamUnavailable) {
		t.Fatalf("错误应可用 errors.Is 匹配 ErrAIUpstreamUnavailable，实际 %v", err)
	}
	message := err.Error()
	if !strings.Contains(message, "已尝试 3 个候选") {
		t.Fatalf("错误信息应带尝试次数：%q", message)
	}
	for _, secret := range []string{"key-primary-secret", "key-backup-secret", "global-key"} {
		if strings.Contains(message, secret) {
			t.Fatalf("错误信息泄露密钥 %q：%q", secret, message)
		}
	}
	if primaryStub.callCount() != 1 || backupStub.callCount() != 1 || globalStub.callCount() != 1 {
		t.Fatalf("调用次数 primary=%d backup=%d global=%d，期望 1/1/1",
			primaryStub.callCount(), backupStub.callCount(), globalStub.callCount())
	}
}

// TestAISettingsChainWithoutTargetsPreservesLegacyBehavior：没有 targets 时，
// 能力链与改造前完全一致（旧 binding 优先，随后全局 ai_settings；禁用则回退全局）。
func TestAISettingsChainWithoutTargetsPreservesLegacyBehavior(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	boundStub := newAIFailoverStub(t, "bound-reply")
	service, st := newAIProviderService(t, globalStub.url())

	// 未绑定任何供应商：链只有全局配置，解析结果与改造前一致。
	chain := service.aiSettingsChain(domain.AICapabilityTranslation)
	if len(chain) != 1 || chain[0].BaseURL != globalStub.url() {
		t.Fatalf("未绑定时候选链 = %#v, want 仅全局配置", chain)
	}
	if got := service.aiSettingsFor(domain.AICapabilityTranslation); got.BaseURL != globalStub.url() {
		t.Fatalf("未绑定时应使用全局配置，实际 %q", got.BaseURL)
	}

	provider := createFailoverProvider(t, st, "绑定供应商", boundStub.url(), "bound-key")
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "bound-model",
	}); err != nil {
		t.Fatalf("写入绑定失败：%v", err)
	}
	chain = service.aiSettingsChain(domain.AICapabilityTranslation)
	if len(chain) != 2 || chain[0].ProviderID != provider.ID || chain[0].BaseURL != boundStub.url() || chain[1].BaseURL != globalStub.url() {
		t.Fatalf("绑定后的候选链 = %#v, want [绑定, 全局]", chain)
	}
	first, err := service.aiSettingsForCapability(domain.AICapabilityTranslation)
	if err != nil || first.ProviderID != provider.ID {
		t.Fatalf("严格解析应返回绑定供应商：%+v err=%v", first, err)
	}

	// 禁用供应商：与改造前一致，回退全局配置且链中不再包含该供应商。
	disabled := provider
	disabled.Enabled = false
	if _, err := st.UpdateAIProvider(provider.ID, disabled, nil); err != nil {
		t.Fatalf("禁用供应商失败：%v", err)
	}
	chain = service.aiSettingsChain(domain.AICapabilityTranslation)
	if len(chain) != 1 || chain[0].BaseURL != globalStub.url() {
		t.Fatalf("禁用后候选链 = %#v, want 仅全局配置", chain)
	}
	if got, err := service.aiSettingsForCapability(domain.AICapabilityTranslation); err != nil || got.BaseURL != globalStub.url() {
		t.Fatalf("禁用后应回退全局配置：%+v err=%v", got, err)
	}
}

// TestAIBindingTargetsFallbackToLegacyBinding：targets 全部不可用（供应商被禁用）时，
// 旧 binding 仍然作为兜底候选参与尝试。
func TestAIBindingTargetsFallbackToLegacyBinding(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	boundStub := newAIFailoverStub(t, "bound-reply")
	targetStub := newAIFailoverStub(t, "target-reply")
	service, st := newAIProviderService(t, globalStub.url())

	targetProvider := createFailoverProvider(t, st, "被禁用的候选", targetStub.url(), "target-key")
	boundProvider := createFailoverProvider(t, st, "兜底绑定", boundStub.url(), "bound-key")
	addFailoverTarget(t, st, domain.AICapabilityTranslation, targetProvider, "target-model", 0)
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: boundProvider.ID,
		Model:      "bound-model",
	}); err != nil {
		t.Fatalf("写入绑定失败：%v", err)
	}

	// 候选供应商被禁用：跳过 target，落到旧 binding。
	disabled := targetProvider
	disabled.Enabled = false
	if _, err := st.UpdateAIProvider(targetProvider.ID, disabled, nil); err != nil {
		t.Fatalf("禁用候选失败：%v", err)
	}
	chain := service.aiSettingsChain(domain.AICapabilityTranslation)
	if len(chain) != 2 || chain[0].ProviderID != boundProvider.ID || chain[1].BaseURL != globalStub.url() {
		t.Fatalf("候选不可用时应回退旧 binding：%#v", chain)
	}
	translated := service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola", "")
	if got := translated["zh"]; got != "bound-reply" {
		t.Fatalf("译文 = %q, want bound-reply", got)
	}
	if targetStub.callCount() != 0 || boundStub.callCount() != 1 {
		t.Fatalf("调用次数 target=%d bound=%d，期望 0/1", targetStub.callCount(), boundStub.callCount())
	}
}

// TestAIProviderHealthIncludesDisabledProviders：健康接口覆盖全部供应商（含禁用），
// 未产生失败的供应商显示 closed/0。
func TestAIProviderHealthIncludesDisabledProviders(t *testing.T) {
	globalStub := newAIFailoverStub(t, "global-reply")
	service, st := newAIProviderService(t, globalStub.url())

	provider := createFailoverProvider(t, st, "健康供应商", globalStub.url(), "health-key")
	disabled := provider
	disabled.Enabled = false
	if _, err := st.UpdateAIProvider(provider.ID, disabled, nil); err != nil {
		t.Fatalf("禁用供应商失败：%v", err)
	}
	health := healthFor(t, service, provider.ID)
	if health.State != domain.AICircuitStateClosed || health.ConsecutiveFailures != 0 || health.Enabled {
		t.Fatalf("健康快照不符合预期：%+v", health)
	}
	if health.OpenedUntil != nil || health.LastError != "" {
		t.Fatalf("未失败时不应有打开时间/错误：%+v", health)
	}
}

// TestAIFailoverLoggerIsWired 保证故障转移日志限流器在 NewService 中已初始化，
// 避免上游持续故障时因 nil 指针 panic。
func TestAIFailoverLoggerIsWired(t *testing.T) {
	service := NewService(store.NewMemoryStore(store.Options{}), realtime.NewHub(), slog.Default())
	if service.circuit == nil || service.failoverLogs == nil {
		t.Fatal("NewService 必须初始化熔断器与故障转移日志限流器")
	}
	if !service.failoverLogs.allow("reception/1") {
		t.Fatal("首次日志应被放行")
	}
	if service.failoverLogs.allow("reception/1") {
		t.Fatal("同一 key 在窗口内不应重复放行")
	}
}
