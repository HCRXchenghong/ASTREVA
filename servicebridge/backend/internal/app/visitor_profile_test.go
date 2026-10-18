package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 访客画像（P5）测试 =====

// newVisitorProfileService 构建可访问底层存储的画像测试服务。
func newVisitorProfileService(t *testing.T) (*Service, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	return service, st
}

// newVisitorProfileConversation 建一个访客会话并返回会话与访客 ID。
func newVisitorProfileConversation(t *testing.T, st store.Store, ip, lang string) domain.Conversation {
	t.Helper()
	session, err := st.CreateVisitorConversation(ip, "web", lang, "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	return session.Conversation
}

// storeVisitorMessage 直接写入一条带语种标签的访客消息（用于确定性地构造语言画像）。
func storeVisitorMessage(t *testing.T, st store.Store, conversationID, clientMsgID, content, detectedTag string) domain.Message {
	t.Helper()
	result, err := st.AddVisitorMessage(conversationID, clientMsgID, content, domain.MessageText, nil)
	if err != nil {
		t.Fatalf("写入访客消息失败：%v", err)
	}
	updated, err := st.SetMessageLanguageMeta(conversationID, result.Input.ServerMsgID, detectedTag, detectedTag, "pending")
	if err != nil {
		t.Fatalf("写入语种元数据失败：%v", err)
	}
	return updated
}

// TestVisitorProfileLanguageStats 覆盖多语种计数、方言标记与 code-switching 判定。
func TestVisitorProfileLanguageStats(t *testing.T) {
	service, st := newVisitorProfileService(t)
	conversation := newVisitorProfileConversation(t, st, "1.2.3.4", "es")
	settings := st.VisitorProfileSettings()

	// 12 条西语 + 3 条英语 + 1 条海湾阿拉伯语（最后一条带方言标签）。
	type sample struct {
		content string
		tag     string
	}
	samples := make([]sample, 0, 16)
	for i := 0; i < 12; i++ {
		samples = append(samples, sample{fmt.Sprintf("Hola, necesito una cotización %d", i), "es"})
	}
	for i := 0; i < 3; i++ {
		samples = append(samples, sample{fmt.Sprintf("Hello, I need a quote %d", i), "en"})
	}
	samples = append(samples, sample{"شلون كم السعر ابغى مره", "ar-Gulf"})

	for index, item := range samples {
		message := storeVisitorMessage(t, st, conversation.ID, fmt.Sprintf("pf_%d", index), item.content, item.tag)
		if _, _, err := service.applyVisitorMessage(context.Background(), settings, conversation, message, item.tag); err != nil {
			t.Fatalf("第 %d 条画像更新失败：%v", index, err)
		}
	}

	profile, ok := st.VisitorProfile(conversation.VisitorID)
	if !ok {
		t.Fatal("画像未生成")
	}
	if profile.Languages["es"] != 12 || profile.Languages["en"] != 3 || profile.Languages["ar"] != 1 {
		t.Fatalf("语言计数不符：%+v", profile.Languages)
	}
	if profile.Dialect != "ar-Gulf" {
		t.Fatalf("方言标记 = %q，want ar-Gulf", profile.Dialect)
	}
	if !profile.CodeSwitching {
		t.Fatal("最近 10 条内出现 ≥2 种语言，应判定 code-switching")
	}

	percentages := domain.LanguagePercentages(profile.Languages)
	if percentages["es"] != 75 || percentages["en"] != 19 || percentages["ar"] != 6 {
		t.Fatalf("语言占比不符：%+v", percentages)
	}
	if langs := domain.SortedLanguages(profile.Languages); langs[0] != "es" || langs[2] != "ar" {
		t.Fatalf("语种排序不符：%+v", langs)
	}

	// code-switching 判定：单语窗口不算，混合窗口才算。
	if hasCodeSwitching([]string{"es", "es", "es"}) {
		t.Fatal("单语窗口不应判定 code-switching")
	}
	if !hasCodeSwitching([]string{"es", "en"}) {
		t.Fatal("两种语言应判定 code-switching")
	}
	if hasCodeSwitching([]string{"", "  "}) {
		t.Fatal("空语种不应判定 code-switching")
	}
	// 方言标签归一化后仍算同一语言（zh-Hant / zh-Hans -> zh）。
	if hasCodeSwitching([]string{"zh-Hant", "zh-Hans"}) {
		t.Fatal("简繁同属中文，不应判定 code-switching")
	}
	if !hasCodeSwitching([]string{"zh-Hant", "en"}) {
		t.Fatal("中文 + 英文应判定 code-switching")
	}
}

// TestVisitorProfileLanguageStatsThroughMessages 覆盖真实消息链路（异步钩子）的语言统计。
func TestVisitorProfileLanguageStatsThroughMessages(t *testing.T) {
	service, st := newVisitorProfileService(t)
	conversation := newVisitorProfileConversation(t, st, "198.51.100.8", "es")

	contents := []string{
		"Hola, necesito una cotización para el proyecto solar",
		"Hello, could you send me the price list?",
		"Hola de nuevo, ¿cuál es el plazo de entrega?",
	}
	for index, content := range contents {
		if _, err := service.AddVisitorMessage(conversation.ID, fmt.Sprintf("hook_%d", index), content, domain.MessageText); err != nil {
			t.Fatalf("发送第 %d 条消息失败：%v", index, err)
		}
	}

	profile := waitForVisitorProfile(t, st, conversation.VisitorID, func(profile domain.VisitorProfile) bool {
		total := 0
		for _, count := range profile.Languages {
			total += count
		}
		return total >= 3
	})
	if profile.Languages["es"] < 2 {
		t.Fatalf("西语消息应被计数：%+v", profile.Languages)
	}
	if profile.IPLast != "198.51.100.8" {
		t.Fatalf("ip_last = %q，want 198.51.100.8", profile.IPLast)
	}
}

// waitForVisitorProfile 轮询等待异步画像更新落地（上限 3 秒）。
func waitForVisitorProfile(t *testing.T, st store.Store, visitorID string, match func(domain.VisitorProfile) bool) domain.VisitorProfile {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if profile, ok := st.VisitorProfile(visitorID); ok && match(profile) {
			return profile
		}
		time.Sleep(10 * time.Millisecond)
	}
	profile, _ := st.VisitorProfile(visitorID)
	t.Fatalf("等待画像更新超时：%+v", profile)
	return domain.VisitorProfile{}
}

// newGeoThirdPartyStub 起一个第三方 IP 库桩（ip-api 风格）。
func newGeoThirdPartyStub(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success", "countryCode": "SA", "country": "Saudi Arabia",
			"regionName": "Riyadh", "city": "Riyadh", "timezone": "Asia/Riyadh",
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestVisitorProfileGeoAndNationality 覆盖三级地理回落、国籍推测权重与置信度上限。
func TestVisitorProfileGeoAndNationality(t *testing.T) {
	service, st := newVisitorProfileService(t)
	stub, calls := newGeoThirdPartyStub(t)
	url := stub.URL + "/ip/{ip}"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &url}); err != nil {
		t.Fatalf("配置第三方库失败：%v", err)
	}
	settings := st.VisitorProfileSettings()
	conversation := newVisitorProfileConversation(t, st, "203.0.113.9", "en")

	// 第一条英语消息：地理来自第三方，语言（en）与 SA 不一致，时区一致 -> 0.6 + 0.2 = 0.8。
	message := storeVisitorMessage(t, st, conversation.ID, "geo_1", "Hello, I need a quote", "en")
	profile, _, err := service.applyVisitorMessage(context.Background(), settings, conversation, message, "en")
	if err != nil {
		t.Fatalf("画像更新失败：%v", err)
	}
	if profile.Country != "Saudi Arabia" || profile.CountryCode != "SA" {
		t.Fatalf("地理字段 = %q/%q，want Saudi Arabia/SA", profile.Country, profile.CountryCode)
	}
	if profile.City != "Riyadh" || profile.Timezone != "Asia/Riyadh" {
		t.Fatalf("城市/时区 = %q/%q", profile.City, profile.Timezone)
	}
	if profile.NationalityGuess != "SA" {
		t.Fatalf("nationality_guess = %q，want SA（ISO alpha-2 大写）", profile.NationalityGuess)
	}
	if profile.NationalityConfidence != 0.8 {
		t.Fatalf("置信度 = %v，want 0.8", profile.NationalityConfidence)
	}
	if len(profile.NationalityBasis) != 2 || profile.NationalityBasis[0] != domain.GeoBasisThirdParty ||
		profile.NationalityBasis[1] != domain.GeoBasisTimezone {
		t.Fatalf("nationality_basis = %+v，want [third_party timezone]", profile.NationalityBasis)
	}
	// 地理来源键只允许契约允许的 5 个值。
	for _, basis := range profile.NationalityBasis {
		switch basis {
		case domain.GeoBasisCDN, domain.GeoBasisLocal, domain.GeoBasisThirdParty, domain.GeoBasisLanguage, domain.GeoBasisTimezone:
		default:
			t.Fatalf("nationality_basis 出现契约外取值：%q", basis)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("第三方调用次数 = %d，want 1", got)
	}

	// 第二条阿拉伯语消息：语言与 SA 一致 -> 0.6 + 0.2 + 0.2 = 1.0，上限收敛到 0.95。
	message = storeVisitorMessage(t, st, conversation.ID, "geo_2", "شلون كم السعر", "ar")
	profile, _, err = service.applyVisitorMessage(context.Background(), settings, conversation, message, "ar")
	if err != nil {
		t.Fatalf("画像更新失败：%v", err)
	}
	if profile.NationalityConfidence != 0.95 {
		t.Fatalf("置信度上限应为 0.95，实际 %v", profile.NationalityConfidence)
	}
	if len(profile.NationalityBasis) != 3 {
		t.Fatalf("依据应为 3 项：%+v", profile.NationalityBasis)
	}
	// 同一 IP 的第二次解析走缓存，不再请求第三方。
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("IP 缓存未生效：第三方调用次数 = %d，want 1", got)
	}
}

// TestVisitorProfileCDNHeaderWins 覆盖 CDN 头优先于第三方库。
func TestVisitorProfileCDNHeaderWins(t *testing.T) {
	service, st := newVisitorProfileService(t)
	stub, calls := newGeoThirdPartyStub(t)
	url := stub.URL + "/ip/{ip}"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &url}); err != nil {
		t.Fatalf("配置第三方库失败：%v", err)
	}
	header := http.Header{}
	header.Set("CF-IPCountry", "CN")

	session, err := service.CreateVisitorConversationWithGeo("203.0.113.9", "web", "zh", "direct", "", header)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	profile := waitForVisitorProfile(t, st, session.Conversation.VisitorID, func(profile domain.VisitorProfile) bool {
		return profile.Visits > 0
	})
	if profile.CountryCode != "CN" || profile.Country != "中国" {
		t.Fatalf("CDN 国家码未生效：%+v", profile)
	}
	if profile.NationalityBasis[0] != domain.GeoBasisCDN {
		t.Fatalf("国籍依据应首选 ip_cdn：%+v", profile.NationalityBasis)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("CDN 命中时不应请求第三方，实际 %d 次", got)
	}
	if profile.Visits != 1 {
		t.Fatalf("visits = %d，want 1", profile.Visits)
	}
	if profile.Languages["zh"] != 1 {
		t.Fatalf("建会话应记录初始语言：%+v", profile.Languages)
	}
}

// TestVisitorProfilePrivacySwitches 覆盖 show_full_ip 脱敏与 enabled=false 不采集/不返回。
func TestVisitorProfilePrivacySwitches(t *testing.T) {
	service, st := newVisitorProfileService(t)
	stub, calls := newGeoThirdPartyStub(t)
	url := stub.URL + "/ip/{ip}"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &url}); err != nil {
		t.Fatalf("配置第三方库失败：%v", err)
	}
	conversation := newVisitorProfileConversation(t, st, "203.0.113.9", "en")
	message := storeVisitorMessage(t, st, conversation.ID, "privacy_1", "Hello", "en")
	if _, _, err := service.applyVisitorMessage(context.Background(), st.VisitorProfileSettings(), conversation, message, "en"); err != nil {
		t.Fatalf("画像更新失败：%v", err)
	}

	// 默认显示完整 IP。
	view, err := service.VisitorProfileDetail(conversation.VisitorID)
	if err != nil {
		t.Fatalf("读取画像详情失败：%v", err)
	}
	if view.IPLast != "203.0.113.9" {
		t.Fatalf("默认应显示完整 IP，实际 %q", view.IPLast)
	}

	// show_full_ip=false：保留前三段。
	showFull := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ShowFullIP: &showFull}); err != nil {
		t.Fatalf("关闭完整 IP 失败：%v", err)
	}
	view, err = service.VisitorProfileDetail(conversation.VisitorID)
	if err != nil {
		t.Fatalf("读取画像详情失败：%v", err)
	}
	if view.IPLast != "203.0.113.*" {
		t.Fatalf("脱敏 IP = %q，want 203.0.113.*", view.IPLast)
	}
	// 存储层仍保留完整 IP，仅接口脱敏。
	stored, _ := st.VisitorProfile(conversation.VisitorID)
	if stored.IPLast != "203.0.113.9" {
		t.Fatalf("存储层不应被脱敏：%q", stored.IPLast)
	}

	// enabled=false：不采集、不发起第三方请求、接口 404。
	enabled := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("关闭画像失败：%v", err)
	}
	before := atomic.LoadInt32(calls)
	disabledSettings := st.VisitorProfileSettings()
	disabledConversation := newVisitorProfileConversation(t, st, "198.51.100.30", "es")
	disabledMessage := storeVisitorMessage(t, st, disabledConversation.ID, "privacy_2", "Hola", "es")
	if _, _, err := service.applyVisitorMessage(context.Background(), disabledSettings, disabledConversation, disabledMessage, "es"); err == nil {
		t.Fatal("画像关闭时 applyVisitorMessage 应返回错误")
	}
	service.trackVisitorMessage(disabledConversation, disabledMessage, "es")
	service.trackVisitorVisit(domain.VisitorSession{Conversation: disabledConversation}, http.Header{})
	time.Sleep(50 * time.Millisecond)
	if after := atomic.LoadInt32(calls); after != before {
		t.Fatalf("画像关闭时不应发起第三方请求：before=%d after=%d", before, after)
	}
	if _, ok := st.VisitorProfile(disabledConversation.VisitorID); ok {
		t.Fatal("画像关闭时不应采集画像")
	}
	if _, _, err := service.VisitorProfiles(domain.VisitorProfileQuery{}); err == nil {
		t.Fatal("画像关闭时列表应返回 404 语义错误")
	}
	if _, err := service.VisitorProfileDetail(conversation.VisitorID); err == nil {
		t.Fatal("画像关闭时详情应返回 404 语义错误")
	}
	if _, err := service.VisitorProfileForConversation(conversation.ID); err == nil {
		t.Fatal("画像关闭时客服侧画像应返回 404 语义错误")
	}
}

// newProfileSummaryStub 起一个返回固定内容的 OpenAI 兼容上游，统计调用次数。
func newProfileSummaryStub(t *testing.T, content string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// TestVisitorProfileAISummary 覆盖摘要触发频率、严格 JSON 解析、证据指向真实消息与失败保留旧值。
func TestVisitorProfileAISummary(t *testing.T) {
	service, st := newVisitorProfileService(t)
	conversation := newVisitorProfileConversation(t, st, "203.0.113.20", "es")

	// 先落三条消息，拿到真实 server_msg_id 作为证据。
	ids := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		message := storeVisitorMessage(t, st, conversation.ID, fmt.Sprintf("sum_%d", index),
			fmt.Sprintf("Hola, quiero una cotización del proyecto solar %d", index), "es")
		ids = append(ids, message.ServerMsgID)
	}

	stub, calls := newProfileSummaryStub(t, fmt.Sprintf(
		`{"summary":"客户咨询太阳能项目报价","tags":["高意向","询价"],"interests":["太阳能"],"evidence":["%s","不存在的消息"]}`, ids[2]))
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled: true, Mode: domain.AIModeHumanFirst, BaseURL: stub.URL, APIKey: "test-key",
		Model: "test-model", APIType: domain.AIAPITypeChatCompletions, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("配置 AI 上游失败：%v", err)
	}
	every := 3
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{AISummaryEveryMessages: &every}); err != nil {
		t.Fatalf("配置摘要频率失败：%v", err)
	}
	settings := st.VisitorProfileSettings()

	// 每 3 条触发一次：只有第 3 条返回 due=true。
	dueCount := 0
	for index := 0; index < 3; index++ {
		message := storeVisitorMessage(t, st, conversation.ID, fmt.Sprintf("due_%d", index), "Hola", "es")
		_, due, err := service.applyVisitorMessage(context.Background(), settings, conversation, message, "es")
		if err != nil {
			t.Fatalf("第 %d 条更新失败：%v", index, err)
		}
		if due {
			dueCount++
			if index != 2 {
				t.Fatalf("第 %d 条不应触发摘要（每 3 条一次）", index+1)
			}
		}
	}
	if dueCount != 1 {
		t.Fatalf("触发次数 = %d，want 1", dueCount)
	}

	if err := service.runVisitorProfileSummary(context.Background(), conversation.VisitorID); err != nil {
		t.Fatalf("生成摘要失败：%v", err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("上游调用次数 = %d，want 1", got)
	}
	profile, _ := st.VisitorProfile(conversation.VisitorID)
	if profile.Summary != "客户咨询太阳能项目报价" {
		t.Fatalf("摘要 = %q", profile.Summary)
	}
	if len(profile.Tags) != 2 || profile.Tags[0] != "高意向" {
		t.Fatalf("标签 = %+v", profile.Tags)
	}
	if len(profile.Interests) != 1 || profile.Interests[0] != "太阳能" {
		t.Fatalf("关注点 = %+v", profile.Interests)
	}
	if len(profile.Evidence) != 1 {
		t.Fatalf("证据应只保留真实消息：%+v", profile.Evidence)
	}
	if profile.Evidence[0].MessageID != ids[2] {
		t.Fatalf("证据消息 id = %q，want %q", profile.Evidence[0].MessageID, ids[2])
	}
	if strings.TrimSpace(profile.Evidence[0].Text) == "" {
		t.Fatal("证据应带原文")
	}
	if profile.AIUpdatedAt == nil {
		t.Fatal("ai_updated_at 应写入")
	}

	// 解析失败：不覆盖旧值。
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "这不是 JSON"}}},
		})
	}))
	t.Cleanup(broken.Close)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled: true, Mode: domain.AIModeHumanFirst, BaseURL: broken.URL, APIKey: "test-key",
		Model: "test-model", APIType: domain.AIAPITypeChatCompletions, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("切换 AI 上游失败：%v", err)
	}
	if err := service.runVisitorProfileSummary(context.Background(), conversation.VisitorID); err != nil {
		t.Fatalf("解析失败不应返回错误（只记日志）：%v", err)
	}
	again, _ := st.VisitorProfile(conversation.VisitorID)
	if again.Summary != profile.Summary || len(again.Tags) != len(profile.Tags) {
		t.Fatalf("解析失败不应覆盖旧值：%+v", again)
	}

	// 摘要关闭：不再调用上游。
	aiOff := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{AISummaryEnabled: &aiOff}); err != nil {
		t.Fatalf("关闭摘要失败：%v", err)
	}
	before := atomic.LoadInt32(calls)
	if err := service.runVisitorProfileSummary(context.Background(), conversation.VisitorID); err != nil {
		t.Fatalf("摘要关闭时不应报错：%v", err)
	}
	if after := atomic.LoadInt32(calls); after != before {
		t.Fatalf("摘要关闭时不应调用上游：before=%d after=%d", before, after)
	}
}

// TestVisitorProfileRefreshRecomputes 覆盖 refresh：地理重算 + 摘要立即生成。
func TestVisitorProfileRefreshRecomputes(t *testing.T) {
	service, st := newVisitorProfileService(t)
	stub, calls := newGeoThirdPartyStub(t)
	url := stub.URL + "/ip/{ip}"
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{ThirdPartyURL: &url}); err != nil {
		t.Fatalf("配置第三方库失败：%v", err)
	}
	summaryStub, summaryCalls := newProfileSummaryStub(t, `{"summary":"刷新后的摘要","tags":["刷新"],"interests":[],"evidence":[]}`)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled: true, Mode: domain.AIModeHumanFirst, BaseURL: summaryStub.URL, APIKey: "test-key",
		Model: "test-model", APIType: domain.AIAPITypeChatCompletions, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("配置 AI 上游失败：%v", err)
	}

	conversation := newVisitorProfileConversation(t, st, "203.0.113.9", "en")
	message := storeVisitorMessage(t, st, conversation.ID, "refresh_1", "Hello there", "en")
	if _, _, err := service.applyVisitorMessage(context.Background(), st.VisitorProfileSettings(), conversation, message, "en"); err != nil {
		t.Fatalf("画像更新失败：%v", err)
	}

	view, err := service.RefreshVisitorProfile(context.Background(), conversation.VisitorID)
	if err != nil {
		t.Fatalf("refresh 失败：%v", err)
	}
	if view.CountryCode != "SA" || view.NationalityGuess != "SA" {
		t.Fatalf("refresh 后地理/国籍不符：%+v", view)
	}
	if view.Summary != "刷新后的摘要" {
		t.Fatalf("refresh 应触发摘要：%q", view.Summary)
	}
	if got := atomic.LoadInt32(summaryCalls); got != 1 {
		t.Fatalf("摘要上游调用次数 = %d，want 1", got)
	}
	// 地理结果命中 IP 缓存，refresh 不应再次请求第三方。
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("refresh 应命中 IP 缓存：第三方调用次数 = %d，want 1", got)
	}

	// 画像不存在时 refresh 返回 ErrNotFound。
	if _, err := service.RefreshVisitorProfile(context.Background(), "vis_not_exists"); err == nil {
		t.Fatal("不存在的访客 refresh 应报错")
	}
}
