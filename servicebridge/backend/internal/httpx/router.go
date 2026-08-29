package httpx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
	"customer-service/backend/internal/upload"
)

type agentRatingSummary struct {
	domain.RatingSummary
	ManualSatisfactionRate  *float64 `json:"manual_satisfaction_rate,omitempty"`
	DisplaySatisfactionRate float64  `json:"display_satisfaction_rate"`
}

func buildAgentRatingSummary(agent domain.Agent, summary domain.RatingSummary) agentRatingSummary {
	display := summary.SatisfactionRate
	if agent.ManualSatisfactionRate != nil {
		display = *agent.ManualSatisfactionRate
	}
	return agentRatingSummary{
		RatingSummary:           summary,
		ManualSatisfactionRate:  agent.ManualSatisfactionRate,
		DisplaySatisfactionRate: display,
	}
}

func findAgentByID(agents []domain.Agent, id string) (domain.Agent, bool) {
	for _, agent := range agents {
		if agent.ID == id {
			return agent, true
		}
	}
	return domain.Agent{}, false
}

func NewRouter(cfg config.Config, logger *slog.Logger, service *app.Service) http.Handler {
	mux := http.NewServeMux()
	trustedProxies := parseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	service.SetWebSocketAllowedOrigins(parseAllowedOrigins(cfg.CORSAllowedOrigins))
	if !strings.EqualFold(strings.TrimSpace(cfg.UploadDriver), "s3") {
		_ = os.MkdirAll(cfg.UploadDir, 0o755)
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"env":  cfg.Env,
			"time": time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := service.Store().Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ready": false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ready": true,
		})
	})

	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		if cfg.MetricsBearerToken != "" && bearerToken(r) != cfg.MetricsBearerToken {
			writeError(w, store.ErrForbidden)
			return
		}
		writeMetrics(w, service)
	})

	mux.HandleFunc("POST /api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, admin, err := service.LoginAdmin(req.Account, req.Password)
		if err != nil {
			recordLoginAudit(service, r, trustedProxies, domain.AccountAdmin, req.Account, false)
			writeError(w, err)
			return
		}
		recordLoginAudit(service, r, trustedProxies, domain.AccountAdmin, admin.ID, true)
		writeJSON(w, http.StatusOK, map[string]any{"token": session.Token, "admin": admin})
	})

	mux.HandleFunc("POST /api/agent/login", func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, agent, err := service.LoginAgent(req.Account, req.Password)
		if err != nil {
			recordLoginAudit(service, r, trustedProxies, domain.AccountAgent, req.Account, false)
			writeError(w, err)
			return
		}
		recordLoginAudit(service, r, trustedProxies, domain.AccountAgent, agent.ID, true)
		writeJSON(w, http.StatusOK, map[string]any{"token": session.Token, "agent": agent})
	})

	mux.HandleFunc("POST /api/visitor/conversations", func(w http.ResponseWriter, r *http.Request) {
		var req createConversationRequest
		if !decodeOptionalJSON(w, r, &req) {
			return
		}
		ipInfo := resolveClientIP(r, trustedProxies)
		session, err := service.CreateVisitorConversation(ipInfo.IP, req.Source, req.Lang, ipInfo.Source)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, session)
	})

	mux.HandleFunc("GET /api/contact-settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, buildVisitorWidgetSettings(service))
	})

	mux.Handle("GET /uploads/", upload.LocalFileHandler(cfg))

	mux.HandleFunc("POST /api/uploads", func(w http.ResponseWriter, r *http.Request) {
		if !canUpload(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		uploaded, err := upload.SaveHTTP(w, r, cfg)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, uploaded)
	})

	mux.HandleFunc("POST /api/visitor/conversations/{id}/rating", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthVisitorFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		conversationID := r.PathValue("id")
		if session.AccountID != conversationID {
			writeError(w, store.ErrForbidden)
			return
		}
		var req ratingRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		rating, err := service.SubmitRating(conversationID, req.Score, req.Tags, req.Comment)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"rating": rating})
	})

	mux.HandleFunc("PATCH /api/admin/contact-settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req domain.ContactSettings
		if !decodeJSON(w, r, &req) {
			return
		}
		settings, err := service.Store().UpdateContactSettings(req)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "contact_settings.update", "contact_settings", "1", "更新对外联系方式配置")
		writeJSON(w, http.StatusOK, settings)
	})

	mux.HandleFunc("POST /api/agent/device-token", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			DeviceToken string `json:"device_token"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.Store().RegisterAgentDeviceToken(session.AccountID, req.DeviceToken)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("GET /api/agent/conversations", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		conversations, err := service.Store().ConversationsForAgent(session.AccountID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
	})

	mux.HandleFunc("POST /api/agent/status", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		var req setStatusRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.SetAgentStatus(session.AccountID, req.Status)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("POST /api/agent/avatar", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			AvatarURL string `json:"avatar_url"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.Store().UpdateAgentAvatar(session.AccountID, req.AvatarURL)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("GET /api/agent/ratings/summary", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		agent, ok := findAgentByID(service.Store().AllAgents(), session.AccountID)
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		summary := service.Store().RatingSummaryForAgent(session.AccountID)
		writeJSON(w, http.StatusOK, buildAgentRatingSummary(agent, summary))
	})

	mux.HandleFunc("POST /api/account/password", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAdmin && session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		var req changePasswordRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := service.ChangePassword(session, req.CurrentPassword, req.NewPassword); err != nil {
			writeError(w, err)
			return
		}
		recordAudit(service, session, r, trustedProxies, "account.password_change", string(session.Kind), session.AccountID, "修改登录密码")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/admin/conversations", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversations": service.Store().AllConversations()})
	})

	mux.HandleFunc("GET /api/admin/conversations/export.csv", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "conversation.export", "conversation", "all", "导出会话监控 CSV")
		writeConversationCSV(w, service.Store().AllConversations())
	})

	mux.HandleFunc("GET /api/admin/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().DashboardStats())
	})

	mux.HandleFunc("GET /api/admin/system-status", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		database := map[string]any{"ok": true}
		if err := service.Store().Ping(ctx); err != nil {
			database = map[string]any{
				"ok":      false,
				"message": err.Error(),
			}
		}
		aiSettings := service.Store().AISettings()
		aiConfigured := strings.TrimSpace(aiSettings.BaseURL) != "" &&
			strings.TrimSpace(aiSettings.Model) != "" &&
			strings.TrimSpace(aiSettings.APIKeyMasked) != ""
		writeJSON(w, http.StatusOK, map[string]any{
			"database": database,
			"ai": map[string]any{
				"enabled":        aiSettings.Enabled,
				"configured":     aiConfigured,
				"mode":           aiSettings.Mode,
				"base_url":       aiSettings.BaseURL,
				"model":          aiSettings.Model,
				"api_type":       aiSettings.APIType,
				"api_key_masked": aiSettings.APIKeyMasked,
			},
			"websocket": service.Hub().Stats(),
			"time":      time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /api/admin/agents", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"agents": service.Store().AllAgents()})
	})

	mux.HandleFunc("GET /api/admin/ratings/summaries", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agents := service.Store().AllAgents()
		byAgent := service.Store().RatingSummariesByAgent()
		items := make([]map[string]any, 0, len(agents))
		for _, agent := range agents {
			summary := byAgent[agent.ID]
			item := buildAgentRatingSummary(agent, summary)
			items = append(items, map[string]any{
				"agent_id":                  agent.ID,
				"agent_name":                agent.Name,
				"agent_account":             agent.Account,
				"summary":                   item,
				"manual_satisfaction_rate":  item.ManualSatisfactionRate,
				"display_satisfaction_rate": item.DisplaySatisfactionRate,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.HandleFunc("GET /api/admin/agents/{id}/ratings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agentID := r.PathValue("id")
		agent, ok := findAgentByID(service.Store().AllAgents(), agentID)
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		summary := service.Store().RatingSummaryForAgent(agentID)
		writeJSON(w, http.StatusOK, map[string]any{
			"agent":   agent,
			"summary": buildAgentRatingSummary(agent, summary),
			"ratings": service.Store().RatingsForAgent(agentID),
		})
	})

	mux.HandleFunc("POST /api/admin/agents/{id}/rating", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			SatisfactionRate *float64 `json:"satisfaction_rate"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.SatisfactionRate == nil || *req.SatisfactionRate < 0 || *req.SatisfactionRate > 100 {
			writeError(w, store.ErrInvalidInput)
			return
		}
		agentID := r.PathValue("id")
		agent, err := service.Store().SetAgentManualRating(agentID, *req.SatisfactionRate/100)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.set_rating", "agent", agent.ID, "手动设置客服好评率 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("DELETE /api/admin/agents/{id}/rating", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agentID := r.PathValue("id")
		agent, err := service.Store().ClearAgentManualRating(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.clear_rating", "agent", agent.ID, "清除客服手动好评率 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("POST /api/admin/agents/{id}/ratings/reset", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agentID := r.PathValue("id")
		agent, err := service.Store().ResetAgentRatings(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.reset_ratings", "agent", agent.ID, "重置客服真实评价 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("GET /api/admin/ratings/summary", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().RatingSummary())
	})

	mux.HandleFunc("GET /api/admin/ratings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, map[string]any{"ratings": service.Store().RecentRatings(limit)})
	})

	mux.HandleFunc("GET /api/admin/audit-events", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		writeJSON(w, http.StatusOK, map[string]any{"events": service.Store().RecentAuditEvents(limit)})
	})

	mux.HandleFunc("GET /api/admin/audit-events/export.csv", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		recordAuditFromRequest(service, r, trustedProxies, "audit_event.export", "audit_event", "all", "导出管理审计 CSV")
		writeAuditEventCSV(w, service.Store().RecentAuditEvents(limit))
	})

	mux.HandleFunc("POST /api/admin/agents", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req agentRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.CreateAgent(domain.Agent{
			Account:          req.Account,
			Name:             req.Name,
			Group:            req.Group,
			Status:           domain.AgentOffline,
			MaxConversations: req.MaxConversations,
		}, req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.create", "agent", agent.ID, "创建客服账号 "+agent.Account)
		writeJSON(w, http.StatusCreated, map[string]any{"agent": agent})
	})

	mux.HandleFunc("PATCH /api/admin/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req agentRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.UpdateAgent(r.PathValue("id"), domain.Agent{
			Name:             req.Name,
			Group:            req.Group,
			Status:           req.Status,
			MaxConversations: req.MaxConversations,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.update", "agent", agent.ID, "更新客服账号 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("POST /api/admin/agents/{id}/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req resetPasswordRequest
		if !decodeOptionalJSON(w, r, &req) {
			return
		}
		temporaryPassword := ""
		if req.Password == "" {
			generated, err := generateTemporaryPassword()
			if err != nil {
				writeError(w, err)
				return
			}
			req.Password = generated
			temporaryPassword = generated
		}
		agent, err := service.ResetAgentPassword(r.PathValue("id"), req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.reset_password", "agent", agent.ID, "重置客服密码 "+agent.Account)
		payload := map[string]any{"agent": agent}
		if temporaryPassword != "" {
			payload["temporary_password"] = temporaryPassword
		}
		writeJSON(w, http.StatusOK, payload)
	})

	mux.HandleFunc("POST /api/admin/agents/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agent, err := service.DisableAgent(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.disable", "agent", agent.ID, "禁用客服账号 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("DELETE /api/admin/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agent, err := service.DeleteAgent(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.delete", "agent", agent.ID, "删除客服账号 "+agent.Account)
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
	})

	mux.HandleFunc("GET /api/admin/keyword-rules", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": service.Store().KeywordRules()})
	})

	mux.HandleFunc("POST /api/admin/keyword-rules", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req domain.KeywordRule
		if !decodeJSON(w, r, &req) {
			return
		}
		rule, err := service.Store().CreateKeywordRule(req)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "keyword_rule.create", "keyword_rule", rule.ID, "新增关键词规则 "+rule.Keyword)
		writeJSON(w, http.StatusCreated, map[string]any{"rule": rule})
	})

	mux.HandleFunc("PATCH /api/admin/keyword-rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req domain.KeywordRule
		if !decodeJSON(w, r, &req) {
			return
		}
		rule, err := service.Store().UpdateKeywordRule(r.PathValue("id"), req)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "keyword_rule.update", "keyword_rule", rule.ID, "更新关键词规则 "+rule.Keyword)
		writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
	})

	mux.HandleFunc("DELETE /api/admin/keyword-rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		rule, err := service.Store().DeleteKeywordRule(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "keyword_rule.delete", "keyword_rule", rule.ID, "删除关键词规则 "+rule.Keyword)
		writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
	})

	mux.HandleFunc("GET /api/app-update/latest", func(w http.ResponseWriter, r *http.Request) {
		platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
		versionCode, _ := strconv.ParseInt(r.URL.Query().Get("version_code"), 10, 64)
		releases := service.Store().AppReleases(platform)
		var latest *domain.AppRelease
		for i := range releases {
			if releases[i].Enabled {
				latest = &releases[i]
				break
			}
		}
		payload := map[string]any{
			"platform":              platform,
			"current_version_code":  versionCode,
			"latest":                nil,
			"update_available":      false,
			"force_update":          false,
			"required_version_code": int64(0),
		}
		if latest != nil {
			payload["latest"] = latest
			payload["update_available"] = latest.VersionCode > versionCode
			payload["force_update"] = latest.VersionCode > versionCode && latest.ForceUpdate
			if latest.ForceUpdate {
				payload["required_version_code"] = latest.VersionCode
			}
		}
		writeJSON(w, http.StatusOK, payload)
	})

	mux.HandleFunc("GET /api/app-update/download/{id}", func(w http.ResponseWriter, r *http.Request) {
		release, ok := service.Store().AppReleaseByID(r.PathValue("id"))
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		downloadURL := strings.TrimSpace(release.DownloadURL)
		filePath := strings.TrimPrefix(strings.TrimSpace(release.FilePath), "/")
		if filePath != "" {
			fullPath := filepath.Join(cfg.UploadDir, filepath.FromSlash(filePath))
			file, err := os.Open(fullPath)
			if err == nil {
				defer file.Close()
				stat, err := file.Stat()
				if err != nil {
					writeError(w, store.ErrNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/vnd.android.package-archive")
				w.Header().Set("Content-Disposition", "attachment; filename=\""+release.FileName+"\"")
				w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
				w.Header().Set("Cache-Control", "no-store")
				http.ServeContent(w, r, release.FileName, stat.ModTime(), file)
				return
			}
		}
		if downloadURL != "" && !strings.HasPrefix(downloadURL, "/uploads/") {
			http.Redirect(w, r, downloadURL, http.StatusFound)
			return
		}
		writeError(w, store.ErrNotFound)
	})

	mux.HandleFunc("GET /api/admin/app-releases", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
		writeJSON(w, http.StatusOK, map[string]any{"releases": service.Store().AppReleases(platform)})
	})

	mux.HandleFunc("POST /api/admin/app-releases", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		platform := strings.ToLower(strings.TrimSpace(r.FormValue("platform")))
		versionName := strings.TrimSpace(r.FormValue("version_name"))
		versionCode, versionErr := strconv.ParseInt(strings.TrimSpace(r.FormValue("version_code")), 10, 64)
		if platform == "" || versionName == "" || versionErr != nil || versionCode <= 0 {
			writeError(w, store.ErrInvalidInput)
			return
		}
		saved, err := upload.SaveAppReleaseHTTP(w, r, cfg, cfg.AppReleaseMaxBytes)
		if err != nil {
			writeError(w, err)
			return
		}
		release, err := service.Store().CreateAppRelease(domain.AppRelease{
			Platform:     platform,
			VersionName:  versionName,
			VersionCode:  versionCode,
			FileName:     sanitizeFilename(versionName),
			FilePath:     saved.Path,
			FileSize:     int64(saved.Size),
			Checksum:     saved.Checksum,
			ReleaseNotes: strings.TrimSpace(r.FormValue("release_notes")),
			ForceUpdate:  r.FormValue("force_update") == "true" || r.FormValue("force_update") == "1",
			Enabled:      true,
			DownloadURL:  saved.URL,
		})
		if err != nil {
			removeUploadedRelease(cfg, saved.Path)
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "app_release.create", "app_release", release.ID, "上传 App 版本 "+release.VersionName)
		writeJSON(w, http.StatusCreated, map[string]any{"release": release})
	})

	mux.HandleFunc("PATCH /api/admin/app-releases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req appReleaseUpdateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		current, found := service.Store().AppReleaseByID(r.PathValue("id"))
		if !found {
			writeError(w, store.ErrNotFound)
			return
		}
		if req.ForceUpdate != nil {
			current.ForceUpdate = *req.ForceUpdate
		}
		if req.Enabled != nil {
			current.Enabled = *req.Enabled
		}
		release, err := service.Store().UpdateAppRelease(current.ID, domain.AppRelease{
			VersionName:  req.VersionName,
			VersionCode:  req.VersionCode,
			ReleaseNotes: req.ReleaseNotes,
			ForceUpdate:  current.ForceUpdate,
			Enabled:      current.Enabled,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "app_release.update", "app_release", release.ID, "更新 App 版本配置 "+release.VersionName)
		writeJSON(w, http.StatusOK, map[string]any{"release": release})
	})

	mux.HandleFunc("DELETE /api/admin/app-releases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		release, err := service.Store().DeleteAppRelease(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		removeUploadedRelease(cfg, release.FilePath)
		recordAuditFromRequest(service, r, trustedProxies, "app_release.delete", "app_release", release.ID, "删除 App 版本 "+release.VersionName)
		writeJSON(w, http.StatusOK, map[string]any{"release": release})
	})

	mux.HandleFunc("GET /api/conversations/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		if !canAccessConversation(service, r, r.PathValue("id")) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		before := r.URL.Query().Get("before")
		page := service.Store().PagedMessages(r.PathValue("id"), limit, before)
		writeJSON(w, http.StatusOK, localizeMessagePageForRequest(service, r, page))
	})

	mux.HandleFunc("POST /api/conversations/{id}/messages/{message_id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent && session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		msg, conversation, err := service.RevokeMessage(session, r.PathValue("id"), r.PathValue("message_id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "message.revoke", "message", msg.ServerMsgID, "撤回消息")
		writeJSON(w, http.StatusOK, map[string]any{"message": msg, "conversation": conversation})
	})

	mux.HandleFunc("POST /api/conversations/{id}/read", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent && session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		conversation, err := service.Store().MarkConversationRead(session, r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversation": conversation})
	})

	mux.HandleFunc("PATCH /api/conversations/{id}/remark", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent && session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		var req updateRemarkRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		conversation, err := service.UpdateRemark(session, r.PathValue("id"), req.Remark)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "conversation.remark_update", "conversation", conversation.ID, "更新访客备注")
		writeJSON(w, http.StatusOK, map[string]any{"conversation": conversation})
	})

	mux.HandleFunc("POST /api/conversations/{id}/close", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent && session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		conversation, err := service.CloseConversation(session, r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "conversation.close", "conversation", conversation.ID, "关闭会话")
		writeJSON(w, http.StatusOK, map[string]any{"conversation": conversation})
	})

	mux.HandleFunc("DELETE /api/conversations/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent && session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		if err := service.Store().DeleteConversation(session, r.PathValue("id")); err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "conversation.delete", "conversation", r.PathValue("id"), "删除已结束会话")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/admin/conversations/{id}/transfer", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAdmin {
			writeError(w, store.ErrForbidden)
			return
		}
		var req transferRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		conversation, err := service.TransferConversation(session, r.PathValue("id"), req.AgentID, req.Group)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "conversation.transfer", "conversation", conversation.ID, "强制转接会话")
		writeJSON(w, http.StatusOK, map[string]any{"conversation": conversation})
	})

	mux.HandleFunc("GET /api/admin/ai-settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().AISettings())
	})

	mux.HandleFunc("POST /api/admin/ai-settings/models", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			BaseURL        string `json:"base_url"`
			APIKey         string `json:"api_key"`
			APIType        string `json:"api_type"`
			TimeoutSeconds int    `json:"timeout_seconds"`
		}
		if !decodeOptionalJSON(w, r, &req) {
			return
		}
		preview := domain.AISettings{BaseURL: req.BaseURL, APIKey: req.APIKey, APIType: req.APIType, TimeoutSeconds: req.TimeoutSeconds}
		if err := store.ValidateAISettingsForRequest(preview); err != nil {
			writeError(w, err)
			return
		}
		models, err := service.AIModels(r.Context(), preview)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "ai_models_unavailable", "message": err.Error()})
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_settings.models_sync", "ai_settings", "1", "从上游接口获取 AI 模型列表")
		writeJSON(w, http.StatusOK, map[string]any{"models": models})
	})

	mux.HandleFunc("PATCH /api/admin/ai-settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req aiSettingsRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		next := domain.AISettings{
			Enabled:         req.Enabled,
			Mode:            req.Mode,
			BaseURL:         req.BaseURL,
			APIKey:          req.APIKey,
			Model:           req.Model,
			APIType:         req.APIType,
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxOutputTokens,
			TimeoutSeconds:  req.TimeoutSeconds,
			SystemPrompt:    req.SystemPrompt,
		}
		updated, err := service.Store().UpdateAISettings(next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_settings.update", "ai_settings", "1", "更新 AI 配置")
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("POST /api/admin/ai-settings/test", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			Input string `json:"input"`
		}
		if !decodeOptionalJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(service.Store().AISettings().APIKey) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "ai_api_key_missing",
				"message": "AI API Key 未配置，无法进行真实接口联调",
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		reply, err := service.TestAIReply(ctx, req.Input)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"reply": reply})
	})

	mux.HandleFunc("GET /api/admin/business-hours", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().BusinessHours())
	})

	mux.HandleFunc("PATCH /api/admin/business-hours", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req domain.BusinessHours
		if !decodeJSON(w, r, &req) {
			return
		}
		updated, err := service.Store().UpdateBusinessHours(req)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "business_hours.update", "business_hours", "1", "更新营业时间")
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("GET /ws", service.ServeWebSocket)

	handler := logRequests(logger, mux)
	handler = rateLimit(cfg, trustedProxies, handler)
	handler = cors(cfg, handler)
	handler = securityHeaders(cfg, handler)
	return handler
}

func buildVisitorWidgetSettings(service *app.Service) domain.VisitorWidgetSettings {
	contacts := service.Store().ContactSettings()
	rules := service.Store().KeywordRules()
	quickReplies := make([]domain.QuickReply, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled || !rule.ShowInQuickReplies {
			continue
		}
		text := strings.TrimSpace(rule.QuickReplyText)
		if text == "" {
			text = rule.Keyword
		}
		quickReplies = append(quickReplies, domain.QuickReply{
			RuleID:   rule.ID,
			Text:     text,
			SendText: rule.Keyword,
		})
	}
	return domain.VisitorWidgetSettings{
		Telephone:    contacts.Telephone,
		WhatsApp:     contacts.WhatsApp,
		Telegram:     contacts.Telegram,
		EntryReply:   contacts.EntryReply,
		QuickReplies: quickReplies,
	}
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type appReleaseUpdateRequest struct {
	VersionName  string `json:"version_name"`
	VersionCode  int64  `json:"version_code"`
	ReleaseNotes string `json:"release_notes"`
	ForceUpdate  *bool  `json:"force_update"`
	Enabled      *bool  `json:"enabled"`
}

type createConversationRequest struct {
	Source string `json:"source"`
	Lang   string `json:"lang"`
}

type setStatusRequest struct {
	Status domain.AgentStatus `json:"status"`
}

type updateRemarkRequest struct {
	Remark string `json:"remark"`
}

type agentRequest struct {
	Account          string             `json:"account"`
	Password         string             `json:"password"`
	Name             string             `json:"name"`
	Group            string             `json:"group"`
	Status           domain.AgentStatus `json:"status"`
	MaxConversations int                `json:"max_conversations"`
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type transferRequest struct {
	AgentID string `json:"agent_id"`
	Group   string `json:"group"`
}

type ratingRequest struct {
	Score   int      `json:"score"`
	Tags    []string `json:"tags"`
	Comment string   `json:"comment"`
}

type uploadResponse struct {
	URL      string `json:"url"`
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
}

type aiSettingsRequest struct {
	Enabled         bool                  `json:"enabled"`
	Mode            domain.AICustomerMode `json:"mode"`
	BaseURL         string                `json:"base_url"`
	APIKey          string                `json:"api_key"`
	Model           string                `json:"model"`
	APIType         string                `json:"api_type"`
	Temperature     float64               `json:"temperature"`
	MaxOutputTokens int                   `json:"max_output_tokens"`
	TimeoutSeconds  int                   `json:"timeout_seconds"`
	SystemPrompt    string                `json:"system_prompt"`
}

const maxJSONBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func generateTemporaryPassword() (string, error) {
	var buf [18]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "Tmp-" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func writeConversationCSV(w http.ResponseWriter, conversations []domain.Conversation) {
	writeCSV(w, "conversations.csv", []string{
		"id", "visitor_id", "visitor_ip", "visitor_remark", "status", "assigned_agent_id",
		"source", "last_message", "last_message_at", "unread_for_agent", "unread_for_visitor", "created_at", "updated_at",
	}, func(writer *csv.Writer) {
		for _, item := range conversations {
			_ = writer.Write([]string{
				item.ID,
				item.VisitorID,
				item.VisitorIP,
				item.VisitorRemark,
				string(item.Status),
				item.AssignedAgentID,
				item.Source,
				item.LastMessage,
				formatOptionalTime(item.LastMessageAt),
				strconv.Itoa(item.UnreadForAgent),
				strconv.Itoa(item.UnreadForVisitor),
				formatTime(item.CreatedAt),
				formatTime(item.UpdatedAt),
			})
		}
	})
}

func writeAuditEventCSV(w http.ResponseWriter, events []domain.AuditEvent) {
	writeCSV(w, "audit-events.csv", []string{
		"id", "actor_kind", "actor_id", "action", "resource", "resource_id", "ip_address", "user_agent", "description", "created_at",
	}, func(writer *csv.Writer) {
		for _, item := range events {
			_ = writer.Write([]string{
				item.ID,
				string(item.ActorKind),
				item.ActorID,
				item.Action,
				item.Resource,
				item.ResourceID,
				item.IPAddress,
				item.UserAgent,
				item.Description,
				formatTime(item.CreatedAt),
			})
		}
	})
}

func writeCSV(w http.ResponseWriter, filename string, header []string, writeRows func(*csv.Writer)) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(w)
	_ = writer.Write(header)
	writeRows(writer)
	writer.Flush()
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func writeMetrics(w http.ResponseWriter, service *app.Service) {
	stats := service.Hub().Stats()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte("# HELP customer_service_ws_connections Current websocket connections by role.\n"))
	_, _ = w.Write([]byte("# TYPE customer_service_ws_connections gauge\n"))
	_, _ = w.Write([]byte("customer_service_ws_connections{role=\"visitor\"} " + strconv.Itoa(stats.Visitors) + "\n"))
	_, _ = w.Write([]byte("customer_service_ws_connections{role=\"agent\"} " + strconv.Itoa(stats.Agents) + "\n"))
	_, _ = w.Write([]byte("customer_service_ws_connections{role=\"admin\"} " + strconv.Itoa(stats.Admins) + "\n"))
	_, _ = w.Write([]byte("# HELP customer_service_go_goroutines Current Go goroutine count.\n"))
	_, _ = w.Write([]byte("# TYPE customer_service_go_goroutines gauge\n"))
	_, _ = w.Write([]byte("customer_service_go_goroutines " + strconv.Itoa(runtime.NumGoroutine()) + "\n"))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "message": err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "message": "request body must contain a single JSON object"})
		return false
	}
	return true
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "message": err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "message": "request body must contain a single JSON object"})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	switch {
	case errors.Is(err, store.ErrInvalidCredentials):
		status = http.StatusUnauthorized
		code = "invalid_credentials"
	case errors.Is(err, store.ErrUnauthorized):
		status = http.StatusUnauthorized
		code = "unauthorized"
	case errors.Is(err, store.ErrForbidden):
		status = http.StatusForbidden
		code = "forbidden"
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, store.ErrConflict):
		status = http.StatusConflict
		code = "conflict"
	case errors.Is(err, store.ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_input"
	}
	writeJSON(w, status, map[string]any{"error": code, "message": err.Error()})
}

func canAccessConversation(service *app.Service, r *http.Request, conversationID string) bool {
	if visitor, err := service.AuthVisitorFromRequest(r); err == nil {
		return visitor.AccountID == conversationID
	}
	session, err := service.AuthFromRequest(r)
	if err != nil {
		return false
	}
	if session.Kind == domain.AccountAdmin {
		return true
	}
	if session.Kind == domain.AccountAgent {
		conversation, ok := service.Store().Conversation(conversationID)
		return ok && conversation.AssignedAgentID == session.AccountID
	}
	return false
}

// localizeMessagePageForRequest 按调用方身份本地化历史消息：
// 访客看到自己语言的译文；客服/后台默认看到中文译文并可通过 original_content 查看原文。
func localizeMessagePageForRequest(service *app.Service, r *http.Request, page store.MessagePage) store.MessagePage {
	if session, err := service.AuthVisitorFromRequest(r); err == nil {
		lang := ai.LangZh
		if conversation, ok := service.Store().Conversation(session.AccountID); ok {
			lang = ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
		}
		page.Messages = app.VisitorFacingMessagesForLang(page.Messages, lang)
		return page
	}
	page.Messages = app.AgentFacingMessages(page.Messages)
	return page
}

func isAdmin(service *app.Service, r *http.Request) bool {
	session, err := service.AuthFromRequest(r)
	return err == nil && session.Kind == domain.AccountAdmin
}

func canUpload(service *app.Service, r *http.Request) bool {
	if _, err := service.AuthVisitorFromRequest(r); err == nil {
		return true
	}
	session, err := service.AuthFromRequest(r)
	return err == nil && (session.Kind == domain.AccountAgent || session.Kind == domain.AccountAdmin)
}

type trustedProxySet struct {
	trustAll bool
	nets     []*net.IPNet
	ips      []net.IP
}

func recordAuditFromRequest(service *app.Service, r *http.Request, proxies trustedProxySet, action, resource, resourceID, description string) {
	session, err := service.AuthFromRequest(r)
	if err != nil {
		return
	}
	recordAudit(service, session, r, proxies, action, resource, resourceID, description)
}

func recordAudit(service *app.Service, session domain.AuthSession, r *http.Request, proxies trustedProxySet, action, resource, resourceID, description string) {
	if session.Kind != domain.AccountAdmin && session.Kind != domain.AccountAgent {
		return
	}
	service.Store().RecordAuditEvent(domain.AuditEvent{
		ActorKind:   session.Kind,
		ActorID:     session.AccountID,
		Action:      action,
		Resource:    resource,
		ResourceID:  resourceID,
		IPAddress:   clientIP(r, proxies),
		UserAgent:   r.UserAgent(),
		Description: description,
		CreatedAt:   time.Now().UTC(),
	})
}

func recordLoginAudit(service *app.Service, r *http.Request, proxies trustedProxySet, kind domain.AccountKind, actorID string, success bool) {
	action := string(kind) + ".login.failed"
	description := "登录失败"
	if success {
		action = string(kind) + ".login.succeeded"
		description = "登录成功"
	}
	service.Store().RecordAuditEvent(domain.AuditEvent{
		ActorKind:   kind,
		ActorID:     strings.TrimSpace(actorID),
		Action:      action,
		Resource:    "auth_session",
		IPAddress:   clientIP(r, proxies),
		UserAgent:   r.UserAgent(),
		Description: description,
		CreatedAt:   time.Now().UTC(),
	})
}

// cloudflareCIDRs 来自 https://www.cloudflare.com/ips/ （Cloudflare 官方公布的边缘节点段）。
// 请求经 Cloudflare 进入时，X-Forwarded-For 链中会出现这些地址：扫描转发链时应跳过它们，
// 并优先采信 Cloudflare 在边缘注入、访客无法伪造的 CF-Connecting-IP（IPv4/IPv6 均支持）。
// 若 Cloudflare 调整了地址段，更新此列表即可。
var cloudflareCIDRs = mustParseCIDRs([]string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
})

func mustParseCIDRs(values []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic("invalid CIDR: " + value)
		}
		out = append(out, network)
	}
	return out
}

func isCloudflareIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, network := range cloudflareCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

type clientIPResult struct {
	IP     string
	Source string
}

// resolveClientIP 解析真实访客 IP：
//  1. 直连地址不在可信代理内时，直接返回直连地址；
//  2. 否则从右往左扫描 X-Forwarded-For，跳过可信代理与 Cloudflare 边缘，
//     第一个非可信条目即真实访客（最左侧条目可被访客伪造，绝不采用）；
//  3. 转发链经过 Cloudflare 时，优先采用 CF-Connecting-IP（Cloudflare 边缘注入，
//     访客无法伪造，IPv6 同样适用）；
//  4. 兜底使用 X-Real-IP / 直连地址。
func resolveClientIP(r *http.Request, proxies trustedProxySet) clientIPResult {
	remoteIP := normalizeIP(r.RemoteAddr)
	if !isTrustedProxy(remoteIP, proxies) {
		return clientIPResult{IP: remoteIP, Source: "direct"}
	}
	cfIP := normalizeIP(r.Header.Get("CF-Connecting-IP"))
	cfValid := cfIP != "" && net.ParseIP(cfIP) != nil
	sawCloudflareHop := false
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
		entries := strings.Split(value, ",")
		for i := len(entries) - 1; i >= 0; i-- {
			entry := normalizeIP(entries[i])
			if net.ParseIP(entry) == nil {
				continue
			}
			if isCloudflareIP(entry) {
				sawCloudflareHop = true
				continue
			}
			if isTrustedProxy(entry, proxies) {
				continue
			}
			if cfValid && sawCloudflareHop {
				return clientIPResult{IP: cfIP, Source: "cloudflare"}
			}
			return clientIPResult{IP: entry, Source: "xff"}
		}
	}
	if cfValid && sawCloudflareHop {
		return clientIPResult{IP: cfIP, Source: "cloudflare"}
	}
	if realIP := normalizeIP(r.Header.Get("X-Real-IP")); realIP != "" && net.ParseIP(realIP) != nil {
		if isCloudflareIP(realIP) {
			if cfValid {
				return clientIPResult{IP: cfIP, Source: "cloudflare"}
			}
			return clientIPResult{IP: remoteIP, Source: "direct"}
		}
		return clientIPResult{IP: realIP, Source: "x-real-ip"}
	}
	return clientIPResult{IP: remoteIP, Source: "direct"}
}

func clientIP(r *http.Request, proxies trustedProxySet) string {
	return resolveClientIP(r, proxies).IP
}

func parseTrustedProxyCIDRs(value string) trustedProxySet {
	value = strings.TrimSpace(value)
	if value == "" {
		value = config.DefaultTrustedProxyCIDRs
	}
	if value == "*" {
		return trustedProxySet{trustAll: true}
	}
	var out trustedProxySet
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if ip := net.ParseIP(item); ip != nil {
			out.ips = append(out.ips, ip)
			continue
		}
		if _, network, err := net.ParseCIDR(item); err == nil {
			out.nets = append(out.nets, network)
		}
	}
	return out
}

func isTrustedProxy(value string, proxies trustedProxySet) bool {
	if proxies.trustAll {
		return true
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, trusted := range proxies.ips {
		if trusted.Equal(ip) {
			return true
		}
	}
	for _, network := range proxies.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = strings.Trim(host, "[]")
	} else {
		value = strings.Trim(value, "[]")
	}
	// 统一为压缩形式（含 IPv6），保证入库与展示一致。
	if parsed := net.ParseIP(value); parsed != nil {
		return parsed.String()
	}
	return value
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	if r.URL.Path == "/ws" {
		return strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return ""
}

func securityHeaders(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.SecurityHeaders {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", frameAncestorsPolicy(cfg))
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		}
		next.ServeHTTP(w, r)
	})
}

func frameAncestorsPolicy(cfg config.Config) string {
	raw := strings.TrimSpace(os.Getenv("FRAME_ANCESTORS"))
	if raw == "" {
		raw = cfg.CORSAllowedOrigins
	}

	origins := []string{"'self'"}
	seen := map[string]bool{"'self'": true}
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || seen[origin] {
			continue
		}
		origins = append(origins, origin)
		seen[origin] = true
	}

	return "default-src 'self'; frame-ancestors " + strings.Join(origins, " ") + "; base-uri 'self'; object-src 'none'"
}

func cors(cfg config.Config, next http.Handler) http.Handler {
	allowedOrigins := parseAllowedOrigins(cfg.CORSAllowedOrigins)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if len(allowedOrigins) == 0 || allowedOrigins["*"] {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseAllowedOrigins(value string) map[string]bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" {
		return map[string]bool{"*": true}
	}
	result := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result[item] = true
		}
	}
	return result
}

type rateLimiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	clients map[string]*rateClient
}

type rateClient struct {
	tokens float64
	seenAt time.Time
}

func newRateLimiter(rate float64, burst int) *rateLimiter {
	if rate <= 0 {
		rate = 20
	}
	if burst <= 0 {
		burst = int(math.Ceil(rate * 3))
	}
	return &rateLimiter{
		rate:    rate,
		burst:   float64(burst),
		clients: map[string]*rateClient{},
	}
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	client := l.clients[key]
	if client == nil {
		l.clients[key] = &rateClient{tokens: l.burst - 1, seenAt: now}
		return true
	}
	elapsed := now.Sub(client.seenAt).Seconds()
	client.tokens = math.Min(l.burst, client.tokens+elapsed*l.rate)
	client.seenAt = now
	if client.tokens < 1 {
		return false
	}
	client.tokens--
	if len(l.clients) > 10000 {
		cutoff := now.Add(-10 * time.Minute)
		for key, item := range l.clients {
			if item.seenAt.Before(cutoff) {
				delete(l.clients, key)
			}
		}
	}
	return true
}

func rateLimit(cfg config.Config, proxies trustedProxySet, next http.Handler) http.Handler {
	if !cfg.RateLimitEnabled {
		return next
	}
	limiter := newRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)
	loginLimiter := newRateLimiter(0.5, 10)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		key := clientIP(r, proxies)
		if strings.HasPrefix(r.URL.Path, "/ws") {
			key += ":ws"
		}
		if !limiter.allow(key, time.Now()) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate_limited", "message": "too many requests"})
			return
		}
		if isLoginPath(r.URL.Path) && !loginLimiter.allow(clientIP(r, proxies)+":login", time.Now()) {
			w.Header().Set("Retry-After", "2")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate_limited", "message": "too many login attempts"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoginPath(path string) bool {
	return path == "/api/admin/login" || path == "/api/agent/login"
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func sanitizeFilename(versionName string) string {
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.TrimSpace(versionName))
	name = strings.Trim(name, ".-_")
	if name == "" {
		name = "app"
	}
	return "astreva-online-service-" + name + ".apk"
}

func removeUploadedRelease(cfg config.Config, filePath string) {
	filePath = strings.TrimPrefix(strings.TrimSpace(filePath), "/")
	if filePath == "" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(cfg.UploadDriver), "s3") {
		return
	}
	fullPath := filepath.Join(cfg.UploadDir, filepath.FromSlash(filePath))
	if !strings.HasPrefix(fullPath, filepath.Clean(cfg.UploadDir)) {
		return
	}
	_ = os.Remove(fullPath)
}
