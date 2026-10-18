package app

import (
	"strings"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

// ===== 跨服务访客活动上报（供 admin-server 调用）=====
//
// 与访客自身消息/到访的区别：本接口由可信的兄弟服务上报，只补充"浏览路径 + 到访次数"，
// 不做地理/AI 摘要等重活；不存在的画像绝不凭空创建（画像由访客建会话时产生）。

// VisitorActivityResult 是来访活动上报的业务结果。HTTP 始终 200，用 ok/reason 表达状态，
// 便于调用方区分"画像关闭"与"画像不存在"。
type VisitorActivityResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// 上报结果为 ok=false 时的原因取值。
const (
	VisitorActivityReasonDisabled  = "disabled"
	VisitorActivityReasonNoProfile = "no_profile"
)

// RecordVisitorActivity 把一次访客活动合并进画像：更新 last_path、visits+1、last_seen_at。
//
// 行为约定：
//   - visitor_id / path 为空返回 store.ErrInvalidInput（HTTP 400）；
//   - 画像关闭时返回 {ok:false, reason:"disabled"}，不写库；
//   - 画像不存在时返回 {ok:false, reason:"no_profile"}，不创建画像；
//   - last_seen_at 取 occurred_at（缺省为当前时间），并且只前进不后退（乱序上报不回退）。
//
// 注意：mutate 在存储锁内执行，必须是纯内存操作；画像存在性检查在锁外完成。
func (s *Service) RecordVisitorActivity(visitorID, path string, occurredAt *time.Time) (VisitorActivityResult, error) {
	visitorID = strings.TrimSpace(visitorID)
	path = strings.TrimSpace(path)
	if visitorID == "" || path == "" {
		return VisitorActivityResult{}, store.ErrInvalidInput
	}
	settings := s.store.VisitorProfileSettings()
	if !settings.Enabled {
		return VisitorActivityResult{OK: false, Reason: VisitorActivityReasonDisabled}, nil
	}
	// 不存在的画像不要凭空创建：先确认存在，再进 UpdateVisitorProfile 的写锁。
	if _, ok := s.store.VisitorProfile(visitorID); !ok {
		return VisitorActivityResult{OK: false, Reason: VisitorActivityReasonNoProfile}, nil
	}
	seen := time.Now().UTC()
	if occurredAt != nil {
		seen = occurredAt.UTC()
	}
	if _, err := s.store.UpdateVisitorProfile(visitorID, func(profile *domain.VisitorProfile) error {
		profile.LastPath = path
		profile.Visits++
		if profile.LastSeenAt == nil || seen.After(*profile.LastSeenAt) {
			stamp := seen
			profile.LastSeenAt = &stamp
		}
		return nil
	}); err != nil {
		return VisitorActivityResult{}, err
	}
	return VisitorActivityResult{OK: true}, nil
}

// WarnInternalTokenUnconfigured 只记一次 warning，避免 INTERNAL_API_TOKEN 未配置时每次请求都刷屏。
func (s *Service) WarnInternalTokenUnconfigured() {
	s.internalTokenWarnOnce.Do(func() {
		s.logger.Warn("INTERNAL_API_TOKEN 未配置，/api/internal/* 服务间接口已拒绝全部请求（503）")
	})
}
