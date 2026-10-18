package openapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

// adminSession 校验统一后台管理员身份，失败时已写好错误响应。
func (s *Service) adminSession(w http.ResponseWriter, r *http.Request) (domain.AuthSession, bool) {
	session, err := s.app.AuthFromRequest(r)
	if err != nil || session.Kind != domain.AccountAdmin {
		writeOpenError(w, http.StatusForbidden, store.ErrForbidden.Error())
		return domain.AuthSession{}, false
	}
	return session, true
}

func (s *Service) recordAdminAudit(r *http.Request, session domain.AuthSession, action, resourceID, description string) {
	ip, _ := s.resolveIP(r)
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorKind:   session.Kind,
		ActorID:     session.AccountID,
		Action:      action,
		Resource:    "api_app",
		ResourceID:  resourceID,
		IPAddress:   ip,
		UserAgent:   r.UserAgent(),
		Description: description,
		CreatedAt:   time.Now().UTC(),
	})
}

// adminAppRequest 是创建（POST）请求体：未提供的字段按默认值处理。
type adminAppRequest struct {
	Name      string `json:"name"`
	Note      string `json:"note"`
	RateLimit int    `json:"rate_limit"`
	Enabled   *bool  `json:"enabled"`
	// Scopes 权限范围；nil 表示未传（创建时不限制），空数组表示「不受限」（等同全部范围）。
	Scopes *[]string `json:"scopes"`
}

// adminAppPatchRequest 是更新（PATCH）请求体：全部字段用指针，
// **未提供的字段保持原值**（避免控制台只发 {enabled} 就把限速/备注清零）。
type adminAppPatchRequest struct {
	Name      *string   `json:"name"`
	Note      *string   `json:"note"`
	RateLimit *int      `json:"rate_limit"`
	Enabled   *bool     `json:"enabled"`
	Scopes    *[]string `json:"scopes"`
}

// resolveAdminScopes 处理管理端传入的 scopes：
//   - nil（未传）返回 fallback；
//   - 空数组返回空切片（不受限，等同全部范围）；
//   - 其它情况校验合法性、去重并自动补齐 me:read；非法范围返回中文说明。
func resolveAdminScopes(req *[]string, fallback []string) ([]string, error) {
	if req == nil {
		return fallback, nil
	}
	return domain.NormalizeAPIScopes(*req)
}

// HandleAdminApps 管理 API 授权应用。
// GET  /api/admin/api-apps 列表
// POST /api/admin/api-apps 创建（明文 Key 仅返回一次）
func (s *Service) HandleAdminApps(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminSession(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeOpenJSON(w, http.StatusOK, map[string]any{"apps": s.store.APIApps()})
	case http.MethodPost:
		var req adminAppRequest
		if !decodeAdminBody(w, r, &req) {
			return
		}
		// 未传 scopes = 默认全功能（存空数组表示不受限）；显式传入则按校验结果保存。
		// 先校验范围再生成 Key，避免非法请求产生无意义的密钥。
		scopes, err := resolveAdminScopes(req.Scopes, nil)
		if err != nil {
			writeOpenErrorCode(w, http.StatusBadRequest, "invalid_scope", err.Error())
			return
		}
		plaintext, prefix, hash, err := GenerateAPIKey()
		if err != nil {
			writeOpenError(w, http.StatusInternalServerError, "generate api key failed")
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		created, err := s.store.CreateAPIApp(domain.APIApp{
			Name:      req.Name,
			KeyPrefix: prefix,
			KeyHash:   hash,
			Enabled:   enabled,
			RateLimit: req.RateLimit,
			Scopes:    scopes,
			Note:      req.Note,
		})
		if err != nil {
			mapStoreError(w, err)
			return
		}
		s.recordAdminAudit(r, session, "api_app.create", created.ID, "创建 API 授权："+created.Name)
		writeOpenJSON(w, http.StatusCreated, map[string]any{"app": created, "api_key": plaintext})
	default:
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// HandleAdminApp 更新/删除单个 API 授权应用。
// PATCH  /api/admin/api-apps/{id}
// DELETE /api/admin/api-apps/{id}
func (s *Service) HandleAdminApp(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminSession(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodPatch:
		// 指针语义：请求未提供的字段保持原值（控制台只发 {enabled} 时不得清空限速/备注/范围）。
		var req adminAppPatchRequest
		if !decodeAdminBody(w, r, &req) {
			return
		}
		current, ok := s.findAPIApp(id)
		if !ok {
			writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
			return
		}
		enabled := current.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		name := current.Name
		if req.Name != nil {
			if trimmed := strings.TrimSpace(*req.Name); trimmed != "" {
				name = trimmed
			}
		}
		rateLimit := current.RateLimit
		if req.RateLimit != nil {
			rateLimit = *req.RateLimit
		}
		note := current.Note
		if req.Note != nil {
			note = *req.Note
		}
		scopes, err := resolveAdminScopes(req.Scopes, current.Scopes)
		if err != nil {
			writeOpenErrorCode(w, http.StatusBadRequest, "invalid_scope", err.Error())
			return
		}
		updated, err := s.store.UpdateAPIApp(id, domain.APIApp{
			Name:      name,
			Enabled:   enabled,
			RateLimit: rateLimit,
			Scopes:    scopes,
			Note:      note,
		})
		if err != nil {
			mapStoreError(w, err)
			return
		}
		s.recordAdminAudit(r, session, "api_app.update", id, "更新 API 授权："+updated.Name)
		writeOpenJSON(w, http.StatusOK, map[string]any{"app": updated})
	case http.MethodDelete:
		current, ok := s.findAPIApp(id)
		if !ok {
			writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
			return
		}
		if err := s.store.DeleteAPIApp(id); err != nil {
			mapStoreError(w, err)
			return
		}
		s.recordAdminAudit(r, session, "api_app.delete", id, "删除 API 授权："+current.Name)
		writeOpenJSON(w, http.StatusOK, map[string]any{"deleted": true})
	default:
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// HandleAdminAppResetKey 重置 API Key，明文仅返回一次。
// POST /api/admin/api-apps/{id}/reset-key
func (s *Service) HandleAdminAppResetKey(w http.ResponseWriter, r *http.Request) {
	session, ok := s.adminSession(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	current, ok := s.findAPIApp(id)
	if !ok {
		writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
		return
	}
	plaintext, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		writeOpenError(w, http.StatusInternalServerError, "generate api key failed")
		return
	}
	updated, err := s.store.UpdateAPIApp(id, domain.APIApp{
		Name:      current.Name,
		Enabled:   current.Enabled,
		RateLimit: current.RateLimit,
		Scopes:    current.Scopes,
		Note:      current.Note,
		KeyHash:   hash,
		KeyPrefix: prefix,
	})
	if err != nil {
		mapStoreError(w, err)
		return
	}
	s.recordAdminAudit(r, session, "api_app.reset_key", id, "重置 API Key："+updated.Name)
	writeOpenJSON(w, http.StatusOK, map[string]any{"app": updated, "api_key": plaintext})
}

func (s *Service) findAPIApp(id string) (domain.APIApp, bool) {
	for _, apiApp := range s.store.APIApps() {
		if apiApp.ID == id {
			return apiApp, true
		}
	}
	return domain.APIApp{}, false
}

func decodeAdminBody(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decoder.Decode(target); err != nil {
		writeOpenError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}
