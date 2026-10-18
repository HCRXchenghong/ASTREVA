package store

import (
	"testing"

	"customer-service/backend/internal/domain"
)

// TestResolveAutoReplyPath 覆盖四种 AI/关键词优先级模式的判定（关键词与 AI 的先后关系）。
func TestResolveAutoReplyPath(t *testing.T) {
	cases := []struct {
		name             string
		priority         string
		aiEnabled        bool
		aiConfigured     bool
		wantKeywordAllow bool
		wantTryAI        bool
	}{
		// keyword_first（默认、现行为）：先关键词；AI 可用时未命中关键词才请求 AI。
		{"keyword_first_ai_ready", domain.AIKeywordPriorityKeywordFirst, true, true, true, true},
		{"keyword_first_ai_enabled_unconfigured", domain.AIKeywordPriorityKeywordFirst, true, false, true, true},
		{"keyword_first_ai_disabled", domain.AIKeywordPriorityKeywordFirst, false, false, true, false},
		// ai_first：AI 可用（启用 + 上游已配置）时忽略关键词；否则回退关键词。
		{"ai_first_ai_ready", domain.AIKeywordPriorityAIFirst, true, true, false, true},
		{"ai_first_upstream_missing", domain.AIKeywordPriorityAIFirst, true, false, true, false},
		{"ai_first_ai_disabled", domain.AIKeywordPriorityAIFirst, false, false, true, false},
		// keyword_only：永不请求 AI。
		{"keyword_only_ai_ready", domain.AIKeywordPriorityKeywordOnly, true, true, true, false},
		{"keyword_only_ai_disabled", domain.AIKeywordPriorityKeywordOnly, false, false, true, false},
		// ai_only：忽略关键词，只走 AI（AI 不可用时也不回退关键词）。
		{"ai_only_ai_ready", domain.AIKeywordPriorityAIOnly, true, true, false, true},
		{"ai_only_upstream_missing", domain.AIKeywordPriorityAIOnly, true, false, false, false},
		{"ai_only_ai_disabled", domain.AIKeywordPriorityAIOnly, false, false, false, false},
		// 非法值一律回落到 keyword_first。
		{"invalid_falls_back", "unknown_mode", true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := resolveAutoReplyPath(tc.priority, tc.aiEnabled, tc.aiConfigured)
			if path.keywordAllowed != tc.wantKeywordAllow || path.tryAI != tc.wantTryAI {
				t.Fatalf("resolveAutoReplyPath(%q, enabled=%v, configured=%v) = %+v，want keywordAllowed=%v tryAI=%v",
					tc.priority, tc.aiEnabled, tc.aiConfigured, path, tc.wantKeywordAllow, tc.wantTryAI)
			}
		})
	}
}

// TestAIUpstreamConfigured 覆盖"上游配置缺失"判定：地址与密钥缺一不可。
func TestAIUpstreamConfigured(t *testing.T) {
	if aiUpstreamConfigured(domain.AISettings{BaseURL: "https://api.example.com/v1", APIKey: "sk-x"}) != true {
		t.Fatal("地址与密钥齐全时应视为已配置")
	}
	if aiUpstreamConfigured(domain.AISettings{BaseURL: "https://api.example.com/v1"}) {
		t.Fatal("缺少密钥时应视为未配置")
	}
	if aiUpstreamConfigured(domain.AISettings{APIKey: "sk-x"}) {
		t.Fatal("缺少地址时应视为未配置")
	}
}
