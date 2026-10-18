package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// TestPostgresAgentPreferences 是真实的 PostgreSQL 集成测试：
// 验证二期新增的 agents.preferences JSONB 列在历史库上能自动补齐、能写入并读回。
//
// 默认跳过，避免无数据库环境失败；需要时用一次性容器运行：
//
//	docker run --rm -d --name astreva-pg-test -e POSTGRES_PASSWORD=test \
//	  -e POSTGRES_DB=astreva_test -p 55432:5432 postgres:16-alpine
//	TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/astreva_test?sslmode=disable' \
//	  go test ./internal/store/ -run TestPostgresAgentPreferences -count=1
func TestPostgresAgentPreferences(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	account := "pref_it_" + time.Now().Format("150405.000000")
	agent, err := st.CreateAgent(domain.Agent{
		Account: account,
		Name:    "集成测试客服",
		Group:   "default",
		Status:  domain.AgentOnline,
	}, "IntegrationTest-123")
	if err != nil {
		t.Fatalf("创建客服失败：%v", err)
	}

	// 1) 未设置偏好时读到默认值（全部 true + standard）
	prefs, err := st.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("读取默认偏好失败：%v", err)
	}
	defaults := prefs.WithDefaults()
	if defaults.AutoTranslateIncoming == nil || !*defaults.AutoTranslateIncoming {
		t.Fatalf("默认 AutoTranslateIncoming 应为 true，实际 %+v", defaults)
	}
	if defaults.DialectMode != domain.DialectModeStandard {
		t.Fatalf("默认方言模式应为 standard，实际 %q", defaults.DialectMode)
	}

	// 2) 写入偏好（关闭入向、开启方言、关闭 AI 推荐）
	disabled := false
	enabled := true
	updated, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{
		AutoTranslateIncoming: &disabled,
		AutoTranslateOutgoing: &enabled,
		DialectMode:           domain.DialectModeDialect,
		AISuggestReplies:      &disabled,
	})
	if err != nil {
		t.Fatalf("更新偏好失败：%v", err)
	}
	if updated.DialectMode != domain.DialectModeDialect {
		t.Fatalf("更新后方言模式 = %q，want dialect", updated.DialectMode)
	}

	// 3) 重新读取：JSONB 往返必须保持
	again, err := st.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("二次读取偏好失败：%v", err)
	}
	again = again.WithDefaults()
	if again.AutoTranslateIncoming == nil || *again.AutoTranslateIncoming {
		t.Fatalf("AutoTranslateIncoming 应为 false，实际 %+v", again.AutoTranslateIncoming)
	}
	if again.AISuggestReplies == nil || *again.AISuggestReplies {
		t.Fatalf("AISuggestReplies 应为 false，实际 %+v", again.AISuggestReplies)
	}
	if again.DialectMode != domain.DialectModeDialect {
		t.Fatalf("方言模式未持久化：%q", again.DialectMode)
	}

	// 4) 非法方言值写入时归一为 standard
	invalid := "klingon"
	normalized, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{
		AutoTranslateIncoming: &disabled,
		DialectMode:           invalid,
	})
	if err != nil {
		t.Fatalf("写入非法方言失败：%v", err)
	}
	if normalized.DialectMode != domain.DialectModeStandard {
		t.Fatalf("非法方言应归一为 standard，实际 %q", normalized.DialectMode)
	}

	// 5) 列表读取路径（同一 scanAgent 列序）不能因新列而错位
	agents := st.AllAgents()
	found := false
	for _, item := range agents {
		if item.ID != agent.ID {
			continue
		}
		found = true
		if item.Account != account || item.Name != "集成测试客服" {
			t.Fatalf("列表读取字段错位：%+v", item)
		}
	}
	if !found {
		t.Fatal("AllAgents 未返回刚创建的客服，可能列序不匹配")
	}

	// 6) 会话与消息的二期列：真实读写。
	// 这些列如果只在 migrations/001_init.sql 里、没有进入 ensureCoreTables，
	// 真实数据库上所有会话/消息查询都会报 "column ... does not exist"。
	session, err := st.CreateVisitorConversation("203.0.113.9", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("创建访客会话失败（会话新列缺失时必然失败）：%v", err)
	}
	conversationID := session.Conversation.ID
	messageResult, err := st.AddVisitorMessage(conversationID, "it_msg_1", "Hola, ¿cuál es el precio?", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("写入访客消息失败（消息新列缺失时必然失败）：%v", err)
	}
	if _, err := st.SetMessageLanguageMeta(conversationID, messageResult.Input.ServerMsgID, "es", "es", "pending"); err != nil {
		t.Fatalf("写入消息语种元数据失败：%v", err)
	}
	override := "en"
	if _, err := st.UpdateConversationLanguage(conversationID, ConversationLanguagePatch{
		DetectedLang:      strPtr("es"),
		ReplyLangOverride: &override,
		LanguageStats:     map[string]int{"es": 3, "en": 1},
	}); err != nil {
		t.Fatalf("更新会话语言状态失败：%v", err)
	}
	reloaded, ok := st.Conversation(conversationID)
	if !ok {
		t.Fatal("会话重新读取失败")
	}
	if reloaded.DetectedLang != "es" {
		t.Fatalf("会话 detected_lang = %q，want es", reloaded.DetectedLang)
	}
	if reloaded.ReplyLangOverride != "en" {
		t.Fatalf("会话 reply_lang_override = %q，want en", reloaded.ReplyLangOverride)
	}
	if reloaded.ReplyLangOverrideAt == nil {
		t.Fatal("显式指令生效时间未写入")
	}
	if reloaded.LanguageStats["es"] != 3 || reloaded.LanguageStats["en"] != 1 {
		t.Fatalf("会话语种统计 = %+v，want es:3 en:1", reloaded.LanguageStats)
	}
	messages := st.Messages(conversationID)
	if len(messages) == 0 {
		t.Fatal("消息列表为空")
	}
	last := messages[len(messages)-1]
	if last.DetectedLang != "es" || last.AuthorLang != "es" || last.TranslationStatus != "pending" {
		t.Fatalf("消息语种元数据未落库：%+v", last)
	}

	// 7) 分页读取路径（同一 messageColumns 列序）也必须匹配
	page := st.PagedMessages(conversationID, 10, "")
	if len(page.Messages) == 0 {
		t.Fatal("分页消息为空，可能列序不匹配")
	}

	// 清理测试数据
	if err := st.DeleteConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_it"}, conversationID); err != nil {
		t.Logf("清理测试会话失败（不影响结论）：%v", err)
	}
	if _, err := st.DeleteAgent(agent.ID); err != nil {
		t.Logf("清理测试客服失败（不影响结论）：%v", err)
	}
}

func strPtr(value string) *string { return &value }

// TestPostgresKnowledge 是 AI 知识库的真实 PostgreSQL 集成测试：
// 验证 ensureCoreTables 能建表（含 messages.knowledge_entry_ids 列）、
// keywords / knowledge_entry_ids 的 JSONB 往返、检索配置单例读写与级联删除。
func TestPostgresKnowledge(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	// 1) 建库 + 关键词 JSONB 往返
	base, err := st.CreateKnowledgeBase(domain.KnowledgeBase{
		Name:        "集成测试知识库 " + time.Now().Format("150405.000000"),
		Description: "集成测试",
		Enabled:     true,
		SortOrder:   3,
	})
	if err != nil {
		t.Fatalf("创建知识库失败（ai_knowledge_bases 表缺失时必然失败）：%v", err)
	}
	if base.ID == "" || base.CreatedAt.IsZero() {
		t.Fatalf("知识库字段不完整：%+v", base)
	}

	entry, err := st.CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:   base.ID,
		Title:    "排水沟槽",
		Content:  "不锈钢排水沟槽支持定制长度与颜色。",
		Keywords: []string{"排水沟槽", "不锈钢"},
		Category: "产品",
		Priority: 7,
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("创建知识条目失败（ai_knowledge_entries 表缺失时必然失败）：%v", err)
	}
	if len(entry.Keywords) != 2 || entry.Keywords[1] != "不锈钢" {
		t.Fatalf("keywords JSONB 往返失败：%#v", entry.Keywords)
	}
	if entry.Priority != 7 {
		t.Fatalf("priority = %d, want 7", entry.Priority)
	}

	// 2) 列表/过滤/候选
	listed := st.KnowledgeEntries(base.ID, "排水", 0, 0)
	if len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("条目列表不符：%#v", listed)
	}
	candidates := st.KnowledgeEntriesForBases([]string{base.ID})
	if len(candidates) != 1 || candidates[0].ID != entry.ID {
		t.Fatalf("限定库的候选集不符：%#v", candidates)
	}
	if got := st.KnowledgeEntriesForBases([]string{"kb_missing"}); len(got) != 0 {
		t.Fatalf("不存在的库不应返回候选：%#v", got)
	}

	// 3) 检索配置单例：默认值 -> 更新 -> 读回
	defaults := st.KnowledgeSettings()
	if !defaults.Enabled || defaults.MaxEntries <= 0 {
		t.Fatalf("默认检索配置不符：%+v", defaults)
	}
	updatedSettings, err := st.UpdateKnowledgeSettings(domain.KnowledgeSettings{
		UnknownAction:  domain.KnowledgeUnknownRefuse,
		RefuseReply:    "抱歉，我暂时无法回答。",
		ScoreThreshold: 0.5,
		MaxEntries:     5,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("写入检索配置失败（ai_knowledge_settings 表缺失时必然失败）：%v", err)
	}
	if updatedSettings.UnknownAction != domain.KnowledgeUnknownRefuse || updatedSettings.ScoreThreshold != 0.5 || updatedSettings.MaxEntries != 5 {
		t.Fatalf("检索配置读回不符：%+v", updatedSettings)
	}
	if updatedSettings.UpdatedAt == nil {
		t.Fatal("检索配置 updated_at 未写入")
	}
	if again := st.KnowledgeSettings(); again.RefuseReply != "抱歉，我暂时无法回答。" {
		t.Fatalf("检索配置未持久化：%+v", again)
	}

	// 4) 消息 knowledge_entry_ids JSONB 往返
	if _, err := st.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeAlwaysAI}); err != nil {
		t.Fatalf("切换 AI 模式失败：%v", err)
	}
	session, err := st.CreateVisitorConversation("203.0.113.11", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建访客会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	msg, _, err := st.AddAIMessageWithKnowledge(conversationID, "依据资料回答", nil, []string{entry.ID, "ke_other"})
	if err != nil {
		t.Fatalf("写入带回答依据的 AI 消息失败（knowledge_entry_ids 列缺失时必然失败）：%v", err)
	}
	if len(msg.KnowledgeEntryIDs) != 2 {
		t.Fatalf("回答依据未正确写入：%#v", msg.KnowledgeEntryIDs)
	}
	messages := st.Messages(conversationID)
	if len(messages) == 0 {
		t.Fatal("消息列表为空")
	}
	last := messages[len(messages)-1]
	if len(last.KnowledgeEntryIDs) != 2 || last.KnowledgeEntryIDs[0] != entry.ID || last.KnowledgeEntryIDs[1] != "ke_other" {
		t.Fatalf("knowledge_entry_ids JSONB 往返失败：%#v", last.KnowledgeEntryIDs)
	}
	// 分页读取路径（同一 messageColumns 列序）也必须匹配。
	page := st.PagedMessages(conversationID, 10, "")
	if len(page.Messages) == 0 || len(page.Messages[len(page.Messages)-1].KnowledgeEntryIDs) != 2 {
		t.Fatalf("分页消息的回答依据丢失：%#v", page.Messages)
	}

	// 5) 删除知识库级联删除条目
	if _, err := st.DeleteKnowledgeBase(base.ID); err != nil {
		t.Fatalf("删除知识库失败：%v", err)
	}
	if _, ok := st.KnowledgeEntry(entry.ID); ok {
		t.Fatal("删除知识库后条目应被级联删除")
	}
	if _, err := st.DeleteKnowledgeBase(base.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除知识库应返回 ErrNotFound，实际 %v", err)
	}

	if err := st.DeleteConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_it"}, conversationID); err != nil {
		t.Logf("清理测试会话失败（不影响结论）：%v", err)
	}
}

// TestPostgresAIProvidersAndBindings 是 AI 多供应商与能力绑定的真实 PostgreSQL 集成测试：
// 验证 ensureCoreTables 能建表、默认值/加密密钥/掩码正确、绑定可读写，
// 以及删除供应商后绑定引用被清理。
func TestPostgresAIProvidersAndBindings(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	provider, err := st.CreateAIProvider(domain.AIProvider{
		Name:         "集成测试供应商",
		Kind:         domain.AIProviderKindText,
		APIType:      domain.AIAPITypeChatCompletions,
		BaseURL:      "https://api.openai.com/v1",
		Models:       []string{"gpt-a", "gpt-b"},
		AllowedHosts: "api.openai.com",
		Enabled:      true,
		SortOrder:    7,
	}, "sk-integration-1234")
	if err != nil {
		t.Fatalf("创建供应商失败（ai_providers 表缺失时必然失败）：%v", err)
	}
	if provider.ID == "" {
		t.Fatal("供应商 ID 为空")
	}
	if provider.TimeoutMS != 30000 {
		t.Fatalf("timeout_ms 默认值 = %d, want 30000", provider.TimeoutMS)
	}
	if len(provider.Models) != 2 {
		t.Fatalf("models 未正确落库：%#v", provider.Models)
	}
	if provider.APIKeyMasked != "sk-i****1234" {
		t.Fatalf("api_key_masked = %q, want sk-i****1234", provider.APIKeyMasked)
	}
	if provider.APIKey != "sk-integration-1234" {
		t.Fatalf("内部读取密钥 = %q", provider.APIKey)
	}

	// 列表必须脱敏，不返回明文。
	found := false
	for _, item := range st.ListAIProviders() {
		if item.ID != provider.ID {
			continue
		}
		found = true
		if item.APIKey != "" {
			t.Fatalf("列表返回了明文密钥：%q", item.APIKey)
		}
		if item.APIKeyMasked != "sk-i****1234" || item.Name != "集成测试供应商" || item.SortOrder != 7 {
			t.Fatalf("列表字段不符合预期：%+v", item)
		}
	}
	if !found {
		t.Fatal("ListAIProviders 未返回刚创建的供应商")
	}

	// apiKey==nil 不改密钥，其余字段覆盖更新。
	patch := provider
	patch.Name = "集成测试供应商-改名"
	patch.Models = []string{"gpt-a", "gpt-b", "gpt-c"}
	updated, err := st.UpdateAIProvider(provider.ID, patch, nil)
	if err != nil {
		t.Fatalf("更新供应商失败：%v", err)
	}
	if updated.Name != "集成测试供应商-改名" || len(updated.Models) != 3 {
		t.Fatalf("更新未生效：%+v", updated)
	}
	if updated.APIKey != "sk-integration-1234" {
		t.Fatalf("未传 api_key 时密钥被改动：%q", updated.APIKey)
	}

	// 显式更新密钥。
	newKey := "sk-updated-5678"
	updated, err = st.UpdateAIProvider(provider.ID, patch, &newKey)
	if err != nil {
		t.Fatalf("更新密钥失败：%v", err)
	}
	if updated.APIKey != newKey || updated.APIKeyMasked != "sk-u****5678" {
		t.Fatalf("密钥更新失败：key=%q mask=%q", updated.APIKey, updated.APIKeyMasked)
	}

	// 能力绑定：JSONB params 往返、同能力覆盖更新。
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "gpt-a",
		Params:     map[string]any{"temperature": 0.2},
	}); err != nil {
		t.Fatalf("写入能力绑定失败（ai_bindings 表缺失时必然失败）：%v", err)
	}
	bindings := st.AIBindings()
	binding, ok := bindings[domain.AICapabilityTranslation]
	if !ok || binding.ProviderID != provider.ID || binding.Model != "gpt-a" {
		t.Fatalf("绑定读回不一致：%#v", binding)
	}
	if temperature, _ := binding.Params["temperature"].(float64); temperature != 0.2 {
		t.Fatalf("绑定 params 未正确往返：%#v", binding.Params)
	}
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "gpt-b",
	}); err != nil {
		t.Fatalf("覆盖能力绑定失败：%v", err)
	}
	if got := st.AIBindings()[domain.AICapabilityTranslation]; got.Model != "gpt-b" {
		t.Fatalf("绑定覆盖更新失败：%+v", got)
	}

	// 删除供应商：绑定引用被一并删除。
	if err := st.DeleteAIProvider(provider.ID); err != nil {
		t.Fatalf("删除供应商失败：%v", err)
	}
	if _, ok := st.AIProvider(provider.ID); ok {
		t.Fatal("删除后仍能读到供应商")
	}
	if _, ok := st.AIBindings()[domain.AICapabilityTranslation]; ok {
		t.Fatal("删除供应商后不应残留能力绑定")
	}
	if err := st.DeleteAIProvider(provider.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
}

// TestPostgresAILimits 是 AI 限额的真实 PostgreSQL 集成测试：
// 验证 ensureCoreTables 能建 ai_limits 表、未写入时回退默认值、
// 单例写入/读回，以及存储层对负数与超上限的收敛与超长文案的拒绝。
func TestPostgresAILimits(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	// 清空单例行，验证"从未写入 -> DefaultAILimits()"的回退路径；
	// DELETE 本身也证明 ensureCoreTables 确实创建了 ai_limits 表。
	if _, err := st.pool.Exec(ctx, `DELETE FROM ai_limits`); err != nil {
		t.Fatalf("清空 ai_limits 失败（表缺失时必然失败）：%v", err)
	}
	defaults := st.AILimits()
	want := domain.DefaultAILimits()
	if defaults.Enabled != want.Enabled ||
		defaults.VisitorMessagesPerMinute != want.VisitorMessagesPerMinute ||
		defaults.VisitorMessagesPerDay != want.VisitorMessagesPerDay ||
		defaults.AICallsPerConversationDay != want.AICallsPerConversationDay ||
		defaults.AIReplyMaxChars != want.AIReplyMaxChars ||
		defaults.ExceedMessage != want.ExceedMessage {
		t.Fatalf("未写入时的默认限额不符：got=%+v want=%+v", defaults, want)
	}

	// 单例写入 + 读回。
	custom := domain.AILimits{
		Enabled:                   false,
		VisitorMessagesPerMinute:  7,
		VisitorMessagesPerDay:     70,
		AICallsPerConversationDay: 9,
		AIReplyMaxChars:           80,
		ExceedMessage:             "请稍后再试。",
	}
	updated, err := st.UpdateAILimits(custom)
	if err != nil {
		t.Fatalf("写入限额失败：%v", err)
	}
	if updated.Enabled || updated.VisitorMessagesPerMinute != 7 || updated.VisitorMessagesPerDay != 70 ||
		updated.AICallsPerConversationDay != 9 || updated.AIReplyMaxChars != 80 ||
		updated.ExceedMessage != "请稍后再试。" {
		t.Fatalf("限额读回不符：%+v", updated)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("限额 updated_at 未写入")
	}
	again := st.AILimits()
	if again.Enabled || again.VisitorMessagesPerMinute != 7 || again.ExceedMessage != "请稍后再试。" {
		t.Fatalf("限额未持久化：%+v", again)
	}

	// 负数归零、超上限收敛（存储层保持健壮，HTTP 层另行返回 400）。
	normalized, err := st.UpdateAILimits(domain.AILimits{
		Enabled:                   true,
		VisitorMessagesPerMinute:  -5,
		VisitorMessagesPerDay:     999999,
		AICallsPerConversationDay: 999999,
		AIReplyMaxChars:           999999,
		ExceedMessage:             "  提示  ",
	})
	if err != nil {
		t.Fatalf("写入越界限额失败：%v", err)
	}
	if normalized.VisitorMessagesPerMinute != 0 ||
		normalized.VisitorMessagesPerDay != domain.AILimitMaxPerDay ||
		normalized.AICallsPerConversationDay != domain.AILimitMaxAICalls ||
		normalized.AIReplyMaxChars != domain.AILimitMaxReplyChars {
		t.Fatalf("越界值未按预期收敛：%+v", normalized)
	}
	if normalized.ExceedMessage != "提示" {
		t.Fatalf("提示文案未去空白：%q", normalized.ExceedMessage)
	}

	// 超长提示文案应返回 ErrInvalidInput。
	if _, err := st.UpdateAILimits(domain.AILimits{ExceedMessage: strings.Repeat("a", 1001)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("超长提示文案应返回 ErrInvalidInput，实际 %v", err)
	}

	// 恢复默认，避免复用同一个测试库时影响其它用例。
	if _, err := st.UpdateAILimits(domain.DefaultAILimits()); err != nil {
		t.Fatalf("恢复默认限额失败：%v", err)
	}
}
