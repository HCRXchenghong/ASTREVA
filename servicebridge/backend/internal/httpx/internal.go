package httpx

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/store"
)

// ===== 服务间接口（内网调用，共享令牌鉴权）=====
//
// 与管理员/客服接口不同，这里不做会话鉴权，而是校验 X-Internal-Token 与
// INTERNAL_API_TOKEN 是否一致；未配置令牌时直接 503，避免内网接口裸奔。

// internalTokenHeader 是服务间共享令牌的请求头名称。
const internalTokenHeader = "X-Internal-Token"

// registerInternalRoutes 注册 /api/internal/* 路由。
func registerInternalRoutes(mux *http.ServeMux, service *app.Service, cfg config.Config) {
	// 访客活动上报：admin-server 侧站点埋点把 {visitor_id, path, occurred_at} 转发过来。
	mux.HandleFunc("POST /api/internal/visitor-activity", func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(cfg.InternalAPIToken)
		if expected == "" {
			// 未配置令牌：拒绝所有请求并只记一次 warning（避免刷屏）。
			service.WarnInternalTokenUnconfigured()
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "internal_token_unconfigured",
				"message": "服务间接口未配置 INTERNAL_API_TOKEN，已拒绝请求",
			})
			return
		}
		provided := strings.TrimSpace(r.Header.Get(internalTokenHeader))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, store.ErrUnauthorized)
			return
		}
		var req struct {
			VisitorID  string `json:"visitor_id"`
			Path       string `json:"path"`
			OccurredAt string `json:"occurred_at"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var occurredAt *time.Time
		if raw := strings.TrimSpace(req.OccurredAt); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeError(w, fmt.Errorf("%w: occurred_at 必须是 RFC3339 时间", store.ErrInvalidInput))
				return
			}
			occurredAt = &parsed
		}
		result, err := service.RecordVisitorActivity(req.VisitorID, req.Path, occurredAt)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
