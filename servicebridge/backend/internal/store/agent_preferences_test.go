package store

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// TestMemoryAgentPreferencesDefaults：未设置过的客服偏好按默认值返回。
func TestMemoryAgentPreferencesDefaults(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	agent, err := s.CreateAgent(domain.Agent{Account: "pref_default", Name: "偏好客服", Status: domain.AgentOffline}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	preferences, err := s.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("agent preferences: %v", err)
	}
	if preferences.AutoTranslateIncoming == nil || !*preferences.AutoTranslateIncoming {
		t.Fatalf("auto_translate_incoming 默认应为 true：%+v", preferences)
	}
	if preferences.AutoTranslateOutgoing == nil || !*preferences.AutoTranslateOutgoing {
		t.Fatalf("auto_translate_outgoing 默认应为 true：%+v", preferences)
	}
	if preferences.ReviewTranslation == nil || !*preferences.ReviewTranslation {
		t.Fatalf("review_translation_before_send 默认应为 true：%+v", preferences)
	}
	if preferences.AISuggestReplies == nil || !*preferences.AISuggestReplies {
		t.Fatalf("ai_suggest_replies 默认应为 true：%+v", preferences)
	}
	if preferences.DialectMode != domain.DialectModeStandard {
		t.Fatalf("translation_dialect_mode 默认应为 standard：%q", preferences.DialectMode)
	}
}

// TestMemoryAgentPreferencesUpdateAndNormalize：
// 更新后可读回；非法 DialectMode 归一为 standard。
func TestMemoryAgentPreferencesUpdateAndNormalize(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	agent, err := s.CreateAgent(domain.Agent{Account: "pref_update", Name: "偏好客服", Status: domain.AgentOffline}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	incoming := false
	updated, err := s.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{
		AutoTranslateIncoming: &incoming,
		DialectMode:           "klingon",
	})
	if err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}
	if updated.AutoTranslateIncoming == nil || *updated.AutoTranslateIncoming {
		t.Fatalf("auto_translate_incoming 应更新为 false：%+v", updated)
	}
	if updated.AutoTranslateOutgoing == nil || !*updated.AutoTranslateOutgoing {
		t.Fatalf("未提供的字段应保持默认 true：%+v", updated)
	}
	if updated.DialectMode != domain.DialectModeStandard {
		t.Fatalf("非法方言模式应归一为 standard，实际 %q", updated.DialectMode)
	}
	readBack, err := s.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("read back preferences: %v", err)
	}
	if readBack.AutoTranslateIncoming == nil || *readBack.AutoTranslateIncoming {
		t.Fatalf("读回偏好与写入不一致：%+v", readBack)
	}
	if readBack.DialectMode != domain.DialectModeStandard {
		t.Fatalf("读回方言模式 = %q, want standard", readBack.DialectMode)
	}
	// 方言模式显式设置为 dialect 时应保留。
	if _, err := s.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{DialectMode: "dialect"}); err != nil {
		t.Fatalf("update dialect mode: %v", err)
	}
	readBack, err = s.AgentPreferences(agent.ID)
	if err != nil {
		t.Fatalf("read back dialect preferences: %v", err)
	}
	if readBack.DialectMode != domain.DialectModeDialect {
		t.Fatalf("方言模式 = %q, want dialect", readBack.DialectMode)
	}
}

// TestMemoryAgentPreferencesUnknownAgent：不存在的客服返回 ErrNotFound。
func TestMemoryAgentPreferencesUnknownAgent(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	if _, err := s.AgentPreferences("missing"); err == nil {
		t.Fatal("expected error for missing agent")
	}
	if _, err := s.UpdateAgentPreferences("missing", domain.AgentPreferences{}); err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// fakeScanTarget 按 agentColumns 的顺序回填扫描目标，用于验证 postgres 读取路径
// 的列数与 preferences 解析（无需真实数据库）。
type fakeScanTarget struct {
	values []any
}

func (f fakeScanTarget) Scan(dest ...any) error {
	if len(dest) != len(f.values) {
		return fmt.Errorf("scan 目标数量 = %d, 值数量 = %d", len(dest), len(f.values))
	}
	for index, target := range dest {
		switch ptr := target.(type) {
		case *string:
			*ptr = f.values[index].(string)
		case *domain.AgentStatus:
			*ptr = f.values[index].(domain.AgentStatus)
		case *int:
			*ptr = f.values[index].(int)
		case *sql.NullTime:
			*ptr = f.values[index].(sql.NullTime)
		case *time.Time:
			*ptr = f.values[index].(time.Time)
		case *sql.NullFloat64:
			*ptr = f.values[index].(sql.NullFloat64)
		default:
			return fmt.Errorf("unsupported scan target %T", target)
		}
	}
	return nil
}

func agentScanValues(preferencesJSON string) []any {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []any{
		"agent_1",           // id
		"pref_pg",           // account
		"偏好客服",              // name
		"默认组",               // group_name
		"hash",              // password_hash
		domain.AgentOffline, // status
		"",                  // device_token
		"",                  // device_platform
		10,                  // max_conversations
		0,                   // current_conversations
		sql.NullTime{},      // disabled_at
		now,                 // created_at
		now,                 // updated_at
		"",                  // avatar_url
		sql.NullFloat64{},   // manual_satisfaction_rate
		preferencesJSON,     // preferences
	}
}

// TestScanAgentDecodesPreferences 验证 postgres 的 agent 扫描函数能把 JSONB 偏好解析出来。
func TestScanAgentDecodesPreferences(t *testing.T) {
	agent, err := scanAgent(fakeScanTarget{values: agentScanValues(`{"auto_translate_incoming": false, "translation_dialect_mode": "dialect"}`)})
	if err != nil {
		t.Fatalf("scan agent: %v", err)
	}
	preferences := agent.Preferences.WithDefaults()
	if preferences.AutoTranslateIncoming == nil || *preferences.AutoTranslateIncoming {
		t.Fatalf("scan 后 auto_translate_incoming 应为 false：%+v", preferences)
	}
	if preferences.DialectMode != domain.DialectModeDialect {
		t.Fatalf("scan 后方言模式 = %q, want dialect", preferences.DialectMode)
	}
	if preferences.AutoTranslateOutgoing == nil || !*preferences.AutoTranslateOutgoing {
		t.Fatalf("未设置字段应补默认 true：%+v", preferences)
	}
}

// TestScanAgentEmptyPreferencesUsesDefaults 验证空 JSON 对象走默认值。
func TestScanAgentEmptyPreferencesUsesDefaults(t *testing.T) {
	agent, err := scanAgent(fakeScanTarget{values: agentScanValues("{}")})
	if err != nil {
		t.Fatalf("scan agent: %v", err)
	}
	preferences := agent.Preferences.WithDefaults()
	if preferences.DialectMode != domain.DialectModeStandard {
		t.Fatalf("空偏好方言模式 = %q, want standard", preferences.DialectMode)
	}
	if preferences.ReviewTranslation == nil || !*preferences.ReviewTranslation {
		t.Fatalf("空偏好应补默认 true：%+v", preferences)
	}
}
