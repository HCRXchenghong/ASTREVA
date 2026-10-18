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

type adminAppRequest struct {
	Name      string `json:"name"`
	Note      string `json:"note"`
	RateLimit int    `json:"rate_limit"`
	Enabled   *bool  `json:"enabled"`
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
		var req adminAppRequest
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
		if strings.TrimSpace(req.Name) != "" {
			name = req.Name
		}
		updated, err := s.store.UpdateAPIApp(id, domain.APIApp{
			Name:      name,
			Enabled:   enabled,
			RateLimit: req.RateLimit,
			Note:      req.Note,
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
