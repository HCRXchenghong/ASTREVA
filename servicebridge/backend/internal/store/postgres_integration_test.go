package store

import (
	"context"
	"os"
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
