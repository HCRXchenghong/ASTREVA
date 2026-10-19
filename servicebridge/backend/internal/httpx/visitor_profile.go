package httpx

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

// ===== 访客画像（P5）HTTP 接口 =====
//
// 契约（控制台与 App 已按此实现）：
//
//	GET/PATCH /api/admin/visitor-profile/settings -> {"settings":{...}}
//	GET       /api/admin/visitor-profiles?q=&country=&tag=&limit=&offset= -> {"profiles":[...],"total":n}
//	GET       /api/admin/visitor-profiles/{visitor_id} -> {"profile":{...,"evidence":[],"conversations":[]}}
//	POST      /api/admin/visitor-profiles/{visitor_id}/refresh -> {"profile":{...}}
//	GET       /api/agent/conversations/{id}/profile -> {"profile":{...}}（越权 403、画像关闭/无画像 404）

// visitorProfileSettingsPatchRequest 是画像设置的局部更新请求。
// geoip_db_path 与 geoip_dir 是同一个字段：控制台用 geoip_db_path，历史/内部调用可能用 geoip_dir。
type visitorProfileSettingsPatchRequest struct {
	Enabled                *bool   `json:"enabled"`
	GeoIPDBPath            *string `json:"geoip_db_path"`
	GeoIPDir               *string `json:"geoip_dir"`
	ThirdPartyURL          *string `json:"third_party_url"`
	ThirdPartyKey          *string `json:"third_party_key"`
	ThirdPartyTimeoutMS    *int    `json:"third_party_timeout_ms"`
	CacheTTLHours          *int    `json:"cache_ttl_hours"`
	AISummaryEnabled       *bool   `json:"ai_summary_enabled"`
	AISummaryEveryMessages *int    `json:"ai_summary_every_messages"`
	ShowFullIP             *bool   `json:"show_full_ip"`
	// RetentionDays：0=永久保留；30/90/180 是控制台给的可选项，服务端接受 7..3650。
	RetentionDays *int `json:"retention_days"`
}

// registerVisitorProfileRoutes 注册画像相关的全部路由。
func registerVisitorProfileRoutes(mux *http.ServeMux, service *app.Service, trustedProxies trustedProxySet) {
	mux.HandleFunc("GET /api/admin/visitor-profile/settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": service.VisitorProfileSettings()})
	})

	mux.HandleFunc("PATCH /api/admin/visitor-profile/settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req visitorProfileSettingsPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		patch := domain.VisitorProfileSettingsPatch{
			Enabled:                req.Enabled,
			ThirdPartyURL:          req.ThirdPartyURL,
			ThirdPartyKey:          req.ThirdPartyKey,
			ThirdPartyTimeoutMS:    req.ThirdPartyTimeoutMS,
			CacheTTLHours:          req.CacheTTLHours,
			AISummaryEnabled:       req.AISummaryEnabled,
			AISummaryEveryMessages: req.AISummaryEveryMessages,
			ShowFullIP:             req.ShowFullIP,
			RetentionDays:          req.RetentionDays,
		}
		if req.RetentionDays != nil {
			if *req.RetentionDays != 0 && (*req.RetentionDays < domain.MinVisitorProfileRetentionDays || *req.RetentionDays > domain.MaxVisitorProfileRetentionDays) {
				writeError(w, fmt.Errorf("%w: retention_days 只能是 0（永久保留）或 %d-%d 天",
					store.ErrInvalidInput, domain.MinVisitorProfileRetentionDays, domain.MaxVisitorProfileRetentionDays))
				return
			}
		}
		// 两个键都接受；同时给出时以控制台契约键 geoip_db_path 为准。
		switch {
		case req.GeoIPDBPath != nil:
			patch.GeoIPDir = req.GeoIPDBPath
		case req.GeoIPDir != nil:
			patch.GeoIPDir = req.GeoIPDir
		}
		if req.ThirdPartyTimeoutMS != nil {
			if *req.ThirdPartyTimeoutMS < domain.MinThirdPartyTimeoutMS || *req.ThirdPartyTimeoutMS > domain.MaxThirdPartyTimeoutMS {
				writeError(w, fmt.Errorf("%w: third_party_timeout_ms 必须在 %d 与 %d 之间",
					store.ErrInvalidInput, domain.MinThirdPartyTimeoutMS, domain.MaxThirdPartyTimeoutMS))
				return
			}
		}
		if req.CacheTTLHours != nil {
			if *req.CacheTTLHours < 1 || *req.CacheTTLHours > domain.MaxCacheTTLHours {
				writeError(w, fmt.Errorf("%w: cache_ttl_hours 必须在 1 与 %d 之间",
					store.ErrInvalidInput, domain.MaxCacheTTLHours))
				return
			}
		}
		if req.AISummaryEveryMessages != nil {
			if *req.AISummaryEveryMessages < 1 || *req.AISummaryEveryMessages > domain.MaxAISummaryEveryMessages {
				writeError(w, fmt.Errorf("%w: ai_summary_every_messages 必须在 1 与 %d 之间",
					store.ErrInvalidInput, domain.MaxAISummaryEveryMessages))
				return
			}
		}
		updated, err := service.UpdateVisitorProfileSettings(patch)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "visitor_profile_settings.update",
			"visitor_profile_settings", "1", "更新访客画像配置")
		writeJSON(w, http.StatusOK, map[string]any{"settings": updated})
	})

	mux.HandleFunc("GET /api/admin/visitor-profiles", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		profiles, total, err := service.VisitorProfiles(domain.VisitorProfileQuery{
			Query:   r.URL.Query().Get("q"),
			Country: r.URL.Query().Get("country"),
			Tag:     r.URL.Query().Get("tag"),
			Limit:   limit,
			Offset:  offset,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles, "total": total})
	})

	mux.HandleFunc("GET /api/admin/visitor-profiles/{visitor_id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		profile, err := service.VisitorProfileDetail(r.PathValue("visitor_id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	})

	mux.HandleFunc("POST /api/admin/visitor-profiles/{visitor_id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		profile, err := service.RefreshVisitorProfile(r.Context(), r.PathValue("visitor_id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "visitor_profile.refresh",
			"visitor_profile", r.PathValue("visitor_id"), "立即重算访客画像")
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	})

	// 客服会话侧画像卡：只允许客服/管理员访问（访客 token 也按越权处理），
	// 非本会话负责客服/围观客服 403；画像关闭或该访客无画像 404。
	mux.HandleFunc("GET /api/agent/conversations/{id}/profile", func(w http.ResponseWriter, r *http.Request) {
		conversationID := r.PathValue("id")
		session, err := service.AuthFromRequest(r)
		if err != nil || (session.Kind != domain.AccountAdmin && session.Kind != domain.AccountAgent) {
			writeError(w, store.ErrForbidden)
			return
		}
		if !canAccessConversation(service, r, conversationID) {
			writeError(w, store.ErrForbidden)
			return
		}
		profile, err := service.VisitorProfileForConversation(conversationID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profile})
	})
}
