package app

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// TestRecordVisitorActivity 覆盖跨服务访客活动上报的全部行为：
// 参数校验、画像关闭、画像不存在、字段更新与 visits 递增、last_seen_at 只前进不后退。
func TestRecordVisitorActivity(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	service := NewService(st, realtime.NewHub(), slog.Default())

	// 1) 参数缺失：visitor_id / path 都必须提供。
	if _, err := service.RecordVisitorActivity("", "/products/", nil); !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("缺少 visitor_id 应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, err := service.RecordVisitorActivity("vis_x", "   ", nil); !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("缺少 path 应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, ok := st.VisitorProfile("vis_x"); ok {
		t.Fatal("参数校验失败时不应创建画像")
	}

	// 2) 画像不存在：返回 no_profile，且绝不凭空创建。
	result, err := service.RecordVisitorActivity("vis_missing", "/products/", nil)
	if err != nil {
		t.Fatalf("画像不存在不应返回错误：%v", err)
	}
	if result.OK || result.Reason != VisitorActivityReasonNoProfile {
		t.Fatalf("画像不存在应返回 no_profile：%+v", result)
	}
	if _, ok := st.VisitorProfile("vis_missing"); ok {
		t.Fatal("不存在的画像不应被创建")
	}

	// 3) 已有画像：更新 last_path、visits+1、last_seen_at。
	visitorID := "vis_activity"
	if _, err := st.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.Visits = 2
		profile.LastPath = "/old"
		return nil
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	occurredAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	result, err = service.RecordVisitorActivity(visitorID, "  /products/  ", &occurredAt)
	if err != nil || !result.OK {
		t.Fatalf("画像存在时应上报成功：result=%+v err=%v", result, err)
	}
	profile := mustProfile(t, st, visitorID)
	if profile.LastPath != "/products/" {
		t.Fatalf("last_path 应被更新（并去空格）：%q", profile.LastPath)
	}
	if profile.Visits != 3 {
		t.Fatalf("visits 应递增到 3，实际 %d", profile.Visits)
	}
	if profile.LastSeenAt == nil || !profile.LastSeenAt.Equal(occurredAt) {
		t.Fatalf("last_seen_at 应等于 occurred_at：got=%v want=%v", profile.LastSeenAt, occurredAt)
	}

	// 4) 多次上报都计数；last_seen_at 只前进不后退（乱序上报不回退）。
	newer := occurredAt.Add(30 * time.Minute)
	if _, err := service.RecordVisitorActivity(visitorID, "/pricing/", &newer); err != nil {
		t.Fatalf("second report: %v", err)
	}
	profile = mustProfile(t, st, visitorID)
	if profile.Visits != 4 || profile.LastPath != "/pricing/" {
		t.Fatalf("第二次上报应继续累加：%+v", profile)
	}
	if profile.LastSeenAt == nil || !profile.LastSeenAt.Equal(newer) {
		t.Fatalf("last_seen_at 应前进到 newer：%v", profile.LastSeenAt)
	}
	if _, err := service.RecordVisitorActivity(visitorID, "/legacy/", &occurredAt); err != nil {
		t.Fatalf("out-of-order report: %v", err)
	}
	profile = mustProfile(t, st, visitorID)
	if profile.Visits != 5 || profile.LastPath != "/legacy/" {
		t.Fatalf("乱序上报仍应计数并更新路径：%+v", profile)
	}
	if profile.LastSeenAt == nil || !profile.LastSeenAt.Equal(newer) {
		t.Fatalf("乱序上报不应让 last_seen_at 回退：%v", profile.LastSeenAt)
	}

	// 5) 画像关闭：返回 ok=false + reason=disabled，且不写库、不创建画像。
	disabled := false
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("disable profile: %v", err)
	}
	result, err = service.RecordVisitorActivity(visitorID, "/blocked/", nil)
	if err != nil {
		t.Fatalf("画像关闭不应返回错误：%v", err)
	}
	if result.OK || result.Reason != VisitorActivityReasonDisabled {
		t.Fatalf("画像关闭应返回 disabled：%+v", result)
	}
	profile = mustProfile(t, st, visitorID)
	if profile.Visits != 5 || profile.LastPath != "/legacy/" {
		t.Fatalf("画像关闭时不应写库：%+v", profile)
	}
	result, err = service.RecordVisitorActivity("vis_closed_missing", "/x/", nil)
	if err != nil || result.OK || result.Reason != VisitorActivityReasonDisabled {
		t.Fatalf("画像关闭时优先返回 disabled：result=%+v err=%v", result, err)
	}
	if _, ok := st.VisitorProfile("vis_closed_missing"); ok {
		t.Fatal("画像关闭时不应创建画像")
	}
}

func mustProfile(t *testing.T, st store.Store, visitorID string) domain.VisitorProfile {
	t.Helper()
	profile, ok := st.VisitorProfile(visitorID)
	if !ok {
		t.Fatalf("画像 %s 不存在", visitorID)
	}
	return profile
}
