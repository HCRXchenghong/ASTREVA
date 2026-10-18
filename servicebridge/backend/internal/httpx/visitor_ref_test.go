package httpx

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// TestVisitorConversationVisitorRefFlow 覆盖"官网访问 → 画像 last_path"打通的完整 HTTP 链路：
// 带 visitor_ref 建会话 → 画像立即以该 ref 为键创建 → 内部活动上报命中 → 不带/非法 ref 回归。
func TestVisitorConversationVisitorRefFlow(t *testing.T) {
	router, st := newInternalRouter(t, "internal-secret")
	const ref = "av_site_http_0001"

	// ① 带 visitor_ref 建会话：画像立即存在、键为该 ref、visits=1。
	session := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{
		"source": "web", "lang": "zh", "visitor_ref": ref,
	}, http.StatusCreated)
	conversation, _ := session["conversation"].(map[string]any)
	if conversation == nil {
		t.Fatalf("建会话响应缺少 conversation：%#v", session)
	}
	if conversation["profile_visitor_id"] != ref {
		t.Fatalf("会话画像键 = %v，want %q", conversation["profile_visitor_id"], ref)
	}
	engineVisitorID, _ := conversation["visitor_id"].(string)
	profile, ok := st.VisitorProfile(ref)
	if !ok {
		t.Fatal("带 visitor_ref 建会话后画像应立即存在")
	}
	if profile.Visits != 1 {
		t.Fatalf("建会话后 visits = %d，want 1", profile.Visits)
	}

	// ② 内部活动上报（同 ref，官网埋点 id 即画像键）→ ok=true，last_path 更新、visits+1。
	out := doInternalActivity(t, router, "internal-secret", map[string]any{
		"visitor_id": ref, "path": "/products/solar-panel",
	}, http.StatusOK)
	if out["ok"] != true {
		t.Fatalf("内部活动上报应命中画像：%#v", out)
	}
	profile, _ = st.VisitorProfile(ref)
	if profile.LastPath != "/products/solar-panel" || profile.Visits != 2 {
		t.Fatalf("画像未更新 last_path/visits：%+v", profile)
	}

	// ③ 不带 visitor_ref：行为与历史一致，画像键回落引擎 visitor_id。
	plain := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{
		"source": "web", "lang": "zh",
	}, http.StatusCreated)
	plainConversation, _ := plain["conversation"].(map[string]any)
	if _, exists := plainConversation["profile_visitor_id"]; exists {
		t.Fatalf("未提供 visitor_ref 时不应下发 profile_visitor_id：%#v", plainConversation)
	}
	plainVisitorID, _ := plainConversation["visitor_id"].(string)
	waitHTTPVisitorProfile(t, st, plainVisitorID, func(profile domain.VisitorProfile) bool {
		return profile.Visits == 1
	})

	// ④ 非法/超长 visitor_ref：被忽略且不影响建会话，画像键同样回落引擎 id。
	for _, invalid := range []string{"av bad/ref!!", strings.Repeat("a", 65)} {
		payload := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{
			"source": "web", "lang": "zh", "visitor_ref": invalid,
		}, http.StatusCreated)
		item, _ := payload["conversation"].(map[string]any)
		if _, exists := item["profile_visitor_id"]; exists {
			t.Fatalf("非法 visitor_ref 应被忽略：%#v", item)
		}
		if _, exists := st.VisitorProfile(invalid); exists {
			t.Fatalf("非法 visitor_ref 不应创建画像：%q", invalid)
		}
		fallbackID, _ := item["visitor_id"].(string)
		waitHTTPVisitorProfile(t, st, fallbackID, func(profile domain.VisitorProfile) bool {
			return profile.Visits == 1
		})
	}

	// 画像键不该被引擎 id 覆盖：ref 画像仍在。
	if _, ok := st.VisitorProfile(engineVisitorID); ok {
		t.Fatalf("带 ref 的会话不应在引擎 visitor_id 上另建画像：%s", engineVisitorID)
	}
}

// waitHTTPVisitorProfile 轮询等待异步采集的画像落地（上限 3 秒）。
func waitHTTPVisitorProfile(t *testing.T, st interface {
	VisitorProfile(string) (domain.VisitorProfile, bool)
}, visitorID string, match func(domain.VisitorProfile) bool) domain.VisitorProfile {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if profile, ok := st.VisitorProfile(visitorID); ok && match(profile) {
			return profile
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待画像 %s 采集超时", visitorID)
	return domain.VisitorProfile{}
}
