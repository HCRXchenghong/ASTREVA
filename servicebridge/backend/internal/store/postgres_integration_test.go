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

// TestPostgresServicePolicy 是客服策略单例的真实 PostgreSQL 集成测试：
// 验证 ensureCoreTables 建表、未写入时回退默认值、单例写入/读回与归一化。
func TestPostgresServicePolicy(t *testing.T) {
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

	// DELETE 成功本身即证明 service_policy 表已被 ensureCoreTables 创建。
	if _, err := st.pool.Exec(ctx, `DELETE FROM service_policy`); err != nil {
		t.Fatalf("清空 service_policy 失败（表缺失时必然失败）：%v", err)
	}
	defaults := st.ServicePolicy()
	want := domain.DefaultServicePolicy()
	if defaults.MaxWaitSeconds != want.MaxWaitSeconds ||
		defaults.SLAFirstResponseSeconds != want.SLAFirstResponseSeconds ||
		defaults.SLAIdleAction != want.SLAIdleAction ||
		defaults.AIKeywordPriority != want.AIKeywordPriority ||
		defaults.TakeoverPopupEnabled != want.TakeoverPopupEnabled {
		t.Fatalf("未写入时的默认策略不符：got=%+v want=%+v", defaults, want)
	}

	// 单例写入 + 读回 + 归一化。
	updated, err := st.UpdateServicePolicy(domain.ServicePolicy{
		QueueEnabled:         false,
		MaxWaitSeconds:       123,
		SLAIdleAction:        "bogus",
		AIKeywordPriority:    domain.AIKeywordPriorityAIOnly,
		AutoCloseIdleMinutes: 99999999,
		UnattendedReply:      "  请稍候  ",
	})
	if err != nil {
		t.Fatalf("写入策略失败：%v", err)
	}
	if updated.QueueEnabled || updated.MaxWaitSeconds != 123 || updated.UnattendedReply != "请稍候" {
		t.Fatalf("策略读回不符：%+v", updated)
	}
	if updated.SLAIdleAction != domain.SLAIdleActionAI {
		t.Fatalf("非法枚举应回落 ai：%q", updated.SLAIdleAction)
	}
	if updated.AutoCloseIdleMinutes != domain.ServicePolicyMaxAutoCloseIdleMinutesLimit {
		t.Fatalf("超上限应收敛：%d", updated.AutoCloseIdleMinutes)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("策略 updated_at 未写入")
	}
	// 单例约束：再次写入仍只有一行。
	if _, err := st.UpdateServicePolicy(domain.DefaultServicePolicy()); err != nil {
		t.Fatalf("覆盖写入策略失败：%v", err)
	}
	var rows int
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM service_policy`).Scan(&rows); err != nil {
		t.Fatalf("统计策略行失败：%v", err)
	}
	if rows != 1 {
		t.Fatalf("service_policy 应为单例，实际 %d 行", rows)
	}
}

// TestPostgresQueueBlacklistAndChannels 覆盖排队、黑名单、提醒渠道与 SMTP 的真实读写与级联清理。
func TestPostgresQueueBlacklistAndChannels(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	suffix := time.Now().Format("150405.000000")
	agent, err := st.CreateAgent(domain.Agent{
		Account: "sp_it_" + suffix,
		Name:    "策略集成客服",
		Group:   "default",
		Status:  domain.AgentOnline,
	}, "IntegrationTest-123")
	if err != nil {
		t.Fatalf("创建客服失败：%v", err)
	}

	// ---------- 排队 ----------
	visitor, err := st.CreateVisitorConversation("203.0.113.201", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: "conv_missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的会话入队应返回 ErrNotFound，实际 %v", err)
	}
	first, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: conversationID, GroupName: "售前", Priority: 3, Reason: "capacity",
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if first.Reason != domain.QueueReasonCapacity || first.GroupName != "售前" {
		t.Fatalf("入队记录不符：%+v", first)
	}
	again, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: conversationID, GroupName: "VIP", Priority: 9, Reason: "manual",
	})
	if err != nil {
		t.Fatalf("重复入队失败：%v", err)
	}
	if !again.EnqueuedAt.Equal(first.EnqueuedAt) {
		t.Fatalf("重复入队应保留原入队时间：%v -> %v", first.EnqueuedAt, again.EnqueuedAt)
	}
	if again.Priority != 9 || again.GroupName != "VIP" {
		t.Fatalf("重复入队应更新优先级/分组：%+v", again)
	}
	queue := st.QueueList()
	if len(queue) == 0 || queue[0].ConversationID != conversationID {
		t.Fatalf("排队列表不符：%+v", queue)
	}
	if err := st.DequeueConversation(conversationID); err != nil {
		t.Fatalf("出队失败：%v", err)
	}
	if err := st.DequeueConversation(conversationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复出队应返回 ErrNotFound，实际 %v", err)
	}

	// 会话删除应级联清理排队记录。
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: conversationID, Reason: "offline"}); err != nil {
		t.Fatalf("再次入队失败：%v", err)
	}
	adminSession := domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_it"}
	if _, _, err := st.CloseConversation(adminSession, conversationID); err != nil {
		t.Fatalf("关闭会话失败：%v", err)
	}
	if err := st.DeleteConversation(adminSession, conversationID); err != nil {
		t.Fatalf("删除会话失败：%v", err)
	}
	var queued int
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM conversation_queue WHERE conversation_id=$1`, conversationID).Scan(&queued); err != nil {
		t.Fatalf("统计排队记录失败：%v", err)
	}
	if queued != 0 {
		t.Fatal("会话删除后排队记录应被级联清理")
	}

	// ---------- 会话新增列（SLA / 排队分组）读写 ----------
	second, err := st.CreateVisitorConversation("203.0.113.202", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	secondID := second.Conversation.ID
	if _, err := st.pool.Exec(ctx, `
		UPDATE conversations
		SET first_response_at=now(), last_agent_reply_at=now(), sla_state='idle', queue_group='售后'
		WHERE id=$1
	`, secondID); err != nil {
		t.Fatalf("写入会话新增列失败（列缺失时必然失败）：%v", err)
	}
	reloaded, ok := st.Conversation(secondID)
	if !ok {
		t.Fatal("读回会话失败")
	}
	if reloaded.FirstResponseAt == nil || reloaded.LastAgentReplyAt == nil ||
		reloaded.SLAState != "idle" || reloaded.QueueGroup != "售后" {
		t.Fatalf("会话新增列读写不符：%+v", reloaded)
	}

	// ---------- 黑名单 ----------
	value := "vis_it_" + suffix
	entry, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: value, Reason: "集成测试", CreatedBy: "admin_it"})
	if err != nil {
		t.Fatalf("新增黑名单失败：%v", err)
	}
	if _, ok := st.MatchBlacklist("visitor", value); !ok {
		t.Fatal("黑名单应命中")
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: value}); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复添加应返回 ErrConflict，实际 %v", err)
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "ip", Value: "203.0.113.250:53100"}); err != nil {
		t.Fatalf("新增 IP 黑名单失败：%v", err)
	}
	if _, ok := st.MatchBlacklist("ip", "203.0.113.250"); !ok {
		t.Fatal("IP 黑名单应命中（忽略端口）")
	}
	past := time.Now().UTC().Add(-time.Minute)
	expired, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "account", Value: "acc_it_" + suffix, ExpiresAt: &past})
	if err != nil {
		t.Fatalf("新增过期黑名单失败：%v", err)
	}
	if _, ok := st.MatchBlacklist("account", "acc_it_"+suffix); ok {
		t.Fatal("过期黑名单应视为未命中")
	}
	// 命中过期项时应顺手删除该行。
	var expiredRows int
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM service_blacklist WHERE id=$1`, expired.ID).Scan(&expiredRows); err != nil {
		t.Fatalf("统计过期黑名单失败：%v", err)
	}
	if expiredRows != 0 {
		t.Fatal("过期黑名单应在命中时被清理")
	}
	if list := st.BlacklistList("visitor"); len(list) == 0 {
		t.Fatal("按 kind 查询黑名单应有结果")
	}
	if err := st.DeleteBlacklist(entry.ID); err != nil {
		t.Fatalf("删除黑名单失败：%v", err)
	}
	if err := st.DeleteBlacklist(entry.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}

	// ---------- 提醒渠道（密钥加密入库 + 合并语义 + 上限 + 级联清理）----------
	token := "123456:IntegrationSecretToken"
	channel, err := st.CreateAgentAlertChannel(agent.ID, "telegram", map[string]string{"token": token, "chat_id": "10086"})
	if err != nil {
		t.Fatalf("创建提醒渠道失败：%v", err)
	}
	if channel.Config["token"] != token {
		t.Fatalf("读回的渠道配置应解密为明文：%+v", channel.Config)
	}
	// 入库必须是密文：config_ciphertext 不能包含明文 token。
	var ciphertext string
	if err := st.pool.QueryRow(ctx, `SELECT config_ciphertext FROM agent_alert_channels WHERE id=$1`, channel.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("读取渠道密文失败：%v", err)
	}
	if strings.Contains(ciphertext, token) || !strings.HasPrefix(ciphertext, "enc:v1:") {
		t.Fatalf("渠道配置必须以 AES-256-GCM 密文入库：%q", ciphertext)
	}
	if single, ok := st.AgentAlertChannel(channel.ID); !ok || single.Config["token"] != token {
		t.Fatalf("按 ID 读取渠道失败：%+v %v", single, ok)
	}

	// 合并语义：只传 chat_id 时 token 保留。
	merged, err := st.UpdateAgentAlertChannel(channel.ID, domain.AgentAlertChannelPatch{}, &map[string]string{"chat_id": "10010"})
	if err != nil {
		t.Fatalf("合并更新渠道失败：%v", err)
	}
	if merged.Config["token"] != token || merged.Config["chat_id"] != "10010" {
		t.Fatalf("合并更新结果不符：%+v", merged.Config)
	}
	disabled := false
	updatedChannel, err := st.UpdateAgentAlertChannel(channel.ID, domain.AgentAlertChannelPatch{Enabled: &disabled}, nil)
	if err != nil {
		t.Fatalf("更新渠道开关失败：%v", err)
	}
	if updatedChannel.Enabled || updatedChannel.Config["token"] != token {
		t.Fatalf("更新开关不应改密钥：%+v", updatedChannel)
	}

	// 上限 3 个：第 4 个 400/ErrInvalidInput。
	if _, err := st.CreateAgentAlertChannel(agent.ID, "bark", map[string]string{"key": "bark-it-1"}); err != nil {
		t.Fatalf("创建 bark 渠道失败：%v", err)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "email", map[string]string{"address": "it@example.com"}); err != nil {
		t.Fatalf("创建 email 渠道失败：%v", err)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "bark", map[string]string{"key": "bark-it-2"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("第 4 个渠道应返回 ErrInvalidInput，实际 %v", err)
	}
	if err := st.DeleteAgentAlertChannel(channel.ID); err != nil {
		t.Fatalf("删除渠道失败：%v", err)
	}
	if err := st.DeleteAgentAlertChannel(channel.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
	// 删除客服应级联清理其提醒渠道。
	if _, err := st.DeleteAgent(agent.ID); err != nil {
		t.Fatalf("删除客服失败：%v", err)
	}
	var channelRows int
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM agent_alert_channels WHERE agent_id=$1`, agent.ID).Scan(&channelRows); err != nil {
		t.Fatalf("统计渠道失败：%v", err)
	}
	if channelRows != 0 {
		t.Fatal("删除客服后提醒渠道应被级联清理")
	}

	// ---------- SMTP 设置（单例 + 密码密文 + password 语义）----------
	if _, err := st.pool.Exec(ctx, `DELETE FROM alert_smtp_settings`); err != nil {
		t.Fatalf("清空 alert_smtp_settings 失败（表缺失时必然失败）：%v", err)
	}
	if settings := st.AlertSMTPSettings(); settings.Port != domain.DefaultAlertSMTPPort {
		t.Fatalf("SMTP 默认端口应为 %d，实际 %d", domain.DefaultAlertSMTPPort, settings.Port)
	}
	password := "it-smtp-secret-1234"
	stored, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "bot@example.com",
		FromAddress: "bot@example.com",
		UseTLS:      true,
	}, &password)
	if err != nil {
		t.Fatalf("写入 SMTP 设置失败：%v", err)
	}
	if stored.PasswordMasked != domain.MaskSecretValue(password) || stored.Password != password {
		t.Fatalf("SMTP 读回不符：%+v", stored)
	}
	var smtpCiphertext string
	if err := st.pool.QueryRow(ctx, `SELECT password_ciphertext FROM alert_smtp_settings WHERE id=1`).Scan(&smtpCiphertext); err != nil {
		t.Fatalf("读取 SMTP 密文失败：%v", err)
	}
	if strings.Contains(smtpCiphertext, password) || !strings.HasPrefix(smtpCiphertext, "enc:v1:") {
		t.Fatalf("SMTP 密码必须以 AES-256-GCM 密文入库：%q", smtpCiphertext)
	}
	// password==nil 不改密码。
	if _, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host: "smtp2.example.com", Port: 587, FromAddress: "bot@example.com",
	}, nil); err != nil {
		t.Fatalf("更新 SMTP（不改密码）失败：%v", err)
	}
	if got := st.AlertSMTPSettings(); got.Password != password || got.Host != "smtp2.example.com" {
		t.Fatalf("nil 语义不符：%+v", got)
	}
	// password=="" 清除。
	empty := ""
	if _, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host: "smtp2.example.com", Port: 587, FromAddress: "bot@example.com",
	}, &empty); err != nil {
		t.Fatalf("清除 SMTP 密码失败：%v", err)
	}
	if got := st.AlertSMTPSettings(); got.Password != "" || got.PasswordMasked != "" {
		t.Fatalf("清除语义不符：%+v", got)
	}
	// 非法端口返回 ErrInvalidInput。
	if _, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{Port: 70000}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法端口应返回 ErrInvalidInput，实际 %v", err)
	}
}

// TestPostgresTakeoverAssignAndUnattended 覆盖批次 4 第二波的真实 PostgreSQL 行为：
// conversations.unattended_notified_at 列、接管/静默分配与排队清理、无人值守话术只发一次。
func TestPostgresTakeoverAssignAndUnattended(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	// 关闭营业时间限制：自动分配不受运行时刻影响。
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("更新营业时间失败：%v", err)
	}
	suffix := time.Now().Format("150405.000000")
	agentA := createIntegrationAgent(t, st, "to_it_a_"+suffix)
	agentB := createIntegrationAgent(t, st, "to_it_b_"+suffix)

	// ---------- 接管：清空 SLA / 移出队列 / 拒绝跨客服 ----------
	visitor, err := st.CreateVisitorConversation("203.0.113.211", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := st.pool.Exec(ctx, `
		UPDATE conversations SET assigned_agent_id=$2, status='assigned', sla_state='idle', first_response_at=now()
		WHERE id=$1
	`, conversationID, agentA.ID); err != nil {
		t.Fatalf("准备会话状态失败：%v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: conversationID, Reason: "offline"}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := st.TakeoverConversation(agentB.ID, conversationID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("他人负责的会话接管应返回 ErrForbidden，实际 %v", err)
	}
	taken, err := st.TakeoverConversation(agentA.ID, conversationID)
	if err != nil {
		t.Fatalf("接管失败：%v", err)
	}
	if taken.Status != domain.ConversationAssigned || taken.SLAState != "" || taken.FirstResponseAt != nil {
		t.Fatalf("接管后会话状态不符：%+v", taken)
	}
	var queued int
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM conversation_queue WHERE conversation_id=$1`, conversationID).Scan(&queued); err != nil {
		t.Fatalf("统计排队记录失败：%v", err)
	}
	if queued != 0 {
		t.Fatal("接管后应移出排队队列")
	}
	if reloaded, ok := st.Conversation(conversationID); !ok || reloaded.FirstResponseAt != nil || reloaded.SLAState != "" {
		t.Fatalf("接管结果未落库：%+v", reloaded)
	}

	// ---------- 静默分配（排队出队）：不改状态、计并发、移出队列 ----------
	visitor2, err := st.CreateVisitorConversation("203.0.113.212", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	secondID := visitor2.Conversation.ID
	if _, err := st.pool.Exec(ctx, `UPDATE conversations SET assigned_agent_id=NULL, status='waiting' WHERE id=$1`, secondID); err != nil {
		t.Fatalf("重置会话失败：%v", err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE agents SET current_conversations=0 WHERE id=$1`, agentB.ID); err != nil {
		t.Fatalf("重置并发失败：%v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: secondID, GroupName: "it", Priority: 2}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	assigned, err := st.AssignConversationToAgent(secondID, agentB.ID)
	if err != nil {
		t.Fatalf("出队分配失败：%v", err)
	}
	if assigned.AssignedAgentID != agentB.ID || assigned.Status != domain.ConversationWaiting {
		t.Fatalf("静默分配结果不符：%+v", assigned)
	}
	if err := st.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM conversation_queue WHERE conversation_id=$1`, secondID).Scan(&queued); err != nil {
		t.Fatalf("统计排队记录失败：%v", err)
	}
	if queued != 0 {
		t.Fatal("分配成功后必须移出队列")
	}
	var current int
	if err := st.pool.QueryRow(ctx, `SELECT current_conversations FROM agents WHERE id=$1`, agentB.ID).Scan(&current); err != nil {
		t.Fatalf("读取并发失败：%v", err)
	}
	if current != 1 {
		t.Fatalf("分配后并发应为 1，实际 %d", current)
	}
	// 重复分配给同一客服幂等；分配给他人冲突。
	if _, err := st.AssignConversationToAgent(secondID, agentB.ID); err != nil {
		t.Fatalf("重复分配应幂等：%v", err)
	}
	if _, err := st.AssignConversationToAgent(secondID, agentA.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("已分配的会话再分配应返回 ErrConflict，实际 %v", err)
	}
	// 并发已满时拒绝分配。
	visitor3, err := st.CreateVisitorConversation("203.0.113.213", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	thirdID := visitor3.Conversation.ID
	if _, err := st.pool.Exec(ctx, `UPDATE conversations SET assigned_agent_id=NULL WHERE id=$1`, thirdID); err != nil {
		t.Fatalf("重置会话失败：%v", err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE agents SET current_conversations=max_conversations WHERE id=$1`, agentA.ID); err != nil {
		t.Fatalf("占满并发失败：%v", err)
	}
	if _, err := st.AssignConversationToAgent(thirdID, agentA.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("并发已满应返回 ErrConflict，实际 %v", err)
	}

	// ---------- 无人值守兜底：新列写入 + 每会话只发一次 ----------
	notice, noticeConversation, err := st.AddUnattendedNotice(secondID, "集成测试兜底话术", map[string]string{"en": "fallback message"})
	if err != nil {
		t.Fatalf("发送无人值守兜底失败：%v", err)
	}
	if notice.MessageType != domain.MessageSystem || notice.SenderType != domain.SenderSystem {
		t.Fatalf("兜底话术应按系统消息落库：%+v", notice)
	}
	if noticeConversation.UnattendedNotifiedAt == nil {
		t.Fatal("返回的会话应带 unattended_notified_at")
	}
	reloaded, ok := st.Conversation(secondID)
	if !ok || reloaded.UnattendedNotifiedAt == nil {
		t.Fatalf("unattended_notified_at 未落库：%+v", reloaded)
	}
	if reloaded.LastMessage != "集成测试兜底话术" {
		t.Fatalf("兜底话术应更新 last_message：%q", reloaded.LastMessage)
	}
	if _, _, err := st.AddUnattendedNotice(secondID, "重复发送", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("同一会话重复发送应返回 ErrConflict，实际 %v", err)
	}

	// 已结束的会话拒绝发送。
	adminSession := domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_it"}
	if _, _, err := st.CloseConversation(adminSession, secondID); err != nil {
		t.Fatalf("关闭会话失败：%v", err)
	}
	if _, _, err := st.AddUnattendedNotice(secondID, "关闭后发送", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("已结束会话应返回 ErrForbidden，实际 %v", err)
	}
}

// TestPostgresSLAStateAndReassign 覆盖 SLA 巡检用到的存储能力（真实 PostgreSQL）：
// sla_state 写入、交回 AI、转接其他客服与释放回队，以及客服回复写入 SLA 时间戳。
func TestPostgresSLAStateAndReassign(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过真实 PostgreSQL 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st, err := NewPostgresStore(ctx, databaseURL, Options{
		DataEncryptionKey:      "integration-test-encryption-key-32chars",
		BootstrapAdminPassword: "IntegrationTest-123",
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL 存储失败：%v", err)
	}
	defer st.Close()

	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("更新营业时间失败：%v", err)
	}
	suffix := time.Now().Format("150405.000000")
	agentA := createIntegrationAgent(t, st, "sla_it_a_"+suffix)
	agentB := createIntegrationAgent(t, st, "sla_it_b_"+suffix)

	currentConversations := func(agentID string) int {
		t.Helper()
		var current int
		if err := st.pool.QueryRow(ctx, `SELECT current_conversations FROM agents WHERE id=$1`, agentID).Scan(&current); err != nil {
			t.Fatalf("读取客服并发失败：%v", err)
		}
		return current
	}

	visitor, err := st.CreateVisitorConversation("203.0.113.231", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := st.TakeoverConversation(agentA.ID, conversationID); err != nil {
		t.Fatalf("接管失败：%v", err)
	}

	// sla_state 往返（巡检幂等标记）。
	state, err := st.SetConversationSLAState(conversationID, domain.ConversationSLAStateIdle)
	if err != nil {
		t.Fatalf("写入 sla_state 失败：%v", err)
	}
	if state.SLAState != domain.ConversationSLAStateIdle {
		t.Fatalf("sla_state 应为 idle，实际 %q", state.SLAState)
	}

	// 交回 AI：状态置 ai_serving，保留负责客服与 sla_state 幂等标记。
	aiServing, err := st.ReturnConversationToAI(conversationID)
	if err != nil {
		t.Fatalf("交回 AI 失败：%v", err)
	}
	if aiServing.Status != domain.ConversationAIServing {
		t.Fatalf("交回 AI 后状态应为 ai_serving，实际 %q", aiServing.Status)
	}
	if aiServing.AssignedAgentID != agentA.ID || aiServing.SLAState != domain.ConversationSLAStateIdle {
		t.Fatalf("交回 AI 应保留负责客服与 sla_state：%+v", aiServing)
	}

	// 客服真实回复：写入 first_response_at / last_agent_reply_at 并清空 sla_state。
	if _, err := st.AddAgentMessage(agentA.ID, conversationID, "sla-it-reply", "您好，为您跟进。", domain.MessageText, nil); err != nil {
		t.Fatalf("客服回复失败：%v", err)
	}
	replied, ok := st.Conversation(conversationID)
	if !ok {
		t.Fatal("会话应存在")
	}
	if replied.FirstResponseAt == nil || replied.LastAgentReplyAt == nil {
		t.Fatalf("客服回复后应写入 SLA 时间戳：%+v", replied)
	}
	if replied.SLAState != "" {
		t.Fatalf("客服回复后应清空 sla_state，实际 %q", replied.SLAState)
	}

	// 转接给其他客服：并发从 A 转移到 B。
	moved, err := st.ReassignConversation(conversationID, agentB.ID)
	if err != nil {
		t.Fatalf("SLA 转接失败：%v", err)
	}
	if moved.AssignedAgentID != agentB.ID {
		t.Fatalf("转接后应分配给 B，实际 %q", moved.AssignedAgentID)
	}
	if got := currentConversations(agentA.ID); got != 0 {
		t.Fatalf("转接后 A 的并发应为 0，实际 %d", got)
	}
	if got := currentConversations(agentB.ID); got != 1 {
		t.Fatalf("转接后 B 的并发应为 1，实际 %d", got)
	}

	// 释放回队：清空分配、状态回到 human_requested、并发归零。
	released, err := st.ReassignConversation(conversationID, "")
	if err != nil {
		t.Fatalf("SLA 释放失败：%v", err)
	}
	if released.AssignedAgentID != "" {
		t.Fatalf("释放后应无负责客服，实际 %q", released.AssignedAgentID)
	}
	if released.Status != domain.ConversationHumanRequested {
		t.Fatalf("释放后状态应为 human_requested，实际 %q", released.Status)
	}
	if got := currentConversations(agentB.ID); got != 0 {
		t.Fatalf("释放后 B 的并发应为 0，实际 %d", got)
	}
}

// createIntegrationAgent 建一个在线客服（集成测试辅助）。
func createIntegrationAgent(t *testing.T, st *PostgresStore, account string) domain.Agent {
	t.Helper()
	agent, err := st.CreateAgent(domain.Agent{
		Account: account,
		Name:    "接管集成客服",
		Group:   "it",
		Status:  domain.AgentOnline,
	}, "IntegrationTest-123")
	if err != nil {
		t.Fatalf("创建客服失败：%v", err)
	}
	return agent
}
