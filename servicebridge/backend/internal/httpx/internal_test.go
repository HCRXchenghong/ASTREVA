package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newInternalRouter 构造带服务间令牌的路由，并返回底层存储用于准备画像数据。
func newInternalRouter(t *testing.T, internalToken string) (http.Handler, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test", InternalAPIToken: internalToken}, slog.Default(), service), st
}

// doInternalActivity 调用访客活动上报接口。
func doInternalActivity(t *testing.T, router http.Handler, token string, payload any, wantStatus int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode payload: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/visitor-activity", &body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
		}
	}
	return out
}

// TestInternalVisitorActivityTokenAndBody 覆盖令牌缺失/错误/未配置、参数缺失、
// 画像不存在与画像关闭的返回约定。
func TestInternalVisitorActivityTokenAndBody(t *testing.T) {
	router, st := newInternalRouter(t, "internal-secret")
	const visitorID = "vis_internal_http"
	if _, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.Visits = 1
		return nil
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	// 令牌缺失 / 错误 → 401。
	doInternalActivity(t, router, "", map[string]any{"visitor_id": visitorID, "path": "/products/"}, http.StatusUnauthorized)
	doInternalActivity(t, router, "wrong-token", map[string]any{"visitor_id": visitorID, "path": "/products/"}, http.StatusUnauthorized)

	// 参数缺失 → 400。
	out := doInternalActivity(t, router, "internal-secret", map[string]any{"path": "/products/"}, http.StatusBadRequest)
	if out["error"] != "invalid_input" {
		t.Fatalf("缺少 visitor_id 应返回 invalid_input：%#v", out)
	}
	doInternalActivity(t, router, "internal-secret", map[string]any{"visitor_id": visitorID}, http.StatusBadRequest)
	// occurred_at 非法 → 400。
	doInternalActivity(t, router, "internal-secret", map[string]any{
		"visitor_id": visitorID, "path": "/products/", "occurred_at": "not-a-time",
	}, http.StatusBadRequest)

	// 画像不存在 → 200 + no_profile，且不创建画像。
	out = doInternalActivity(t, router, "internal-secret", map[string]any{
		"visitor_id": "vis_internal_missing", "path": "/products/",
	}, http.StatusOK)
	if out["ok"] != false || out["reason"] != "no_profile" {
		t.Fatalf("画像不存在应返回 no_profile：%#v", out)
	}
	if _, ok := st.VisitorProfile("vis_internal_missing"); ok {
		t.Fatal("不存在画像不应被创建")
	}

	// 正常上报 → 200 {"ok":true}，字段写入正确。
	occurredAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	out = doInternalActivity(t, router, "internal-secret", map[string]any{
		"visitor_id": visitorID, "path": "/products/", "occurred_at": occurredAt.Format(time.RFC3339),
	}, http.StatusOK)
	if out["ok"] != true {
		t.Fatalf("正常上报应返回 ok=true：%#v", out)
	}
	profile, ok := st.VisitorProfile(visitorID)
	if !ok {
		t.Fatal("画像应存在")
	}
	if profile.LastPath != "/products/" || profile.Visits != 2 {
		t.Fatalf("画像字段更新不符：%+v", profile)
	}
	if profile.LastSeenAt == nil || !profile.LastSeenAt.Equal(occurredAt) {
		t.Fatalf("last_seen_at 应等于 occurred_at：%v want %v", profile.LastSeenAt, occurredAt)
	}

	// 画像关闭 → 200 + disabled。
	disabled := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("disable profile: %v", err)
	}
	out = doInternalActivity(t, router, "internal-secret", map[string]any{
		"visitor_id": visitorID, "path": "/blocked/",
	}, http.StatusOK)
	if out["ok"] != false || out["reason"] != "disabled" {
		t.Fatalf("画像关闭应返回 disabled：%#v", out)
	}
	profile, _ = st.VisitorProfile(visitorID)
	if profile.LastPath != "/products/" {
		t.Fatalf("画像关闭时不应写库：%+v", profile)
	}
}

// TestInternalVisitorActivityTokenUnconfigured：INTERNAL_API_TOKEN 未配置时接口 503。
func TestInternalVisitorActivityTokenUnconfigured(t *testing.T) {
	router, _ := newInternalRouter(t, "")
	out := doInternalActivity(t, router, "anything", map[string]any{
		"visitor_id": "vis_x", "path": "/products/",
	}, http.StatusServiceUnavailable)
	if out["error"] != "internal_token_unconfigured" {
		t.Fatalf("未配置令牌应返回 internal_token_unconfigured：%#v", out)
	}
}
