package httpx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/openapi"
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
	// 提醒渠道正文里的会话链接前缀（可为空；为空时不下发链接）。
	service.SetAlertLinkHint(cfg.PublicAdminURL)
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
		// 带上请求头：CDN 注入的国家码（CF-IPCountry 等）是画像最优先的地理来源。
		session, err := service.CreateVisitorConversationWithGeo(ipInfo.IP, req.Source, req.Lang, ipInfo.Source, "", req.VisitorRef, r.Header)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, session)
	})

	mux.HandleFunc("GET /api/contact-settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, buildVisitorWidgetSettings(service))
	})

	// 访客切换网站语言后，前端把当前语言同步到会话，后续 AI 回复与翻译按新语言处理。
	mux.HandleFunc("POST /api/visitor/lang", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthVisitorFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		var req struct {
			Lang string `json:"lang"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		conversation, err := service.UpdateVisitorLang(session.AccountID, req.Lang)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, conversation)
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
			Platform    string `json:"platform"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		agent, err := service.Store().RegisterAgentDeviceToken(session.AccountID, req.DeviceToken, req.Platform)
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

	mux.HandleFunc("GET /api/agent/preferences", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		preferences, err := service.Store().AgentPreferences(session.AccountID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": preferences.WithDefaults()})
	})

	mux.HandleFunc("PATCH /api/agent/preferences", func(w http.ResponseWriter, r *http.Request) {
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if session.Kind != domain.AccountAgent {
			writeError(w, store.ErrForbidden)
			return
		}
		// 指针字段表示"本次是否提供"：未提供的字段保持原值。
		var req struct {
			AutoTranslateIncoming   *bool     `json:"auto_translate_incoming"`
			AutoTranslateOutgoing   *bool     `json:"auto_translate_outgoing"`
			ReviewTranslation       *bool     `json:"review_translation_before_send"`
			DialectMode             *string   `json:"translation_dialect_mode"`
			AISuggestReplies        *bool     `json:"ai_suggest_replies"`
			KnowledgeSuggestEnabled *bool     `json:"knowledge_suggest_enabled"`
			KnowledgeBaseIDs        *[]string `json:"knowledge_base_ids"`
			// 批次 4：接管弹窗与只读围观开关（默认 true / false）。
			TakeoverPopupEnabled *bool `json:"takeover_popup_enabled"`
			ReadOnlyWatch        *bool `json:"read_only_watch"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		current, err := service.Store().AgentPreferences(session.AccountID)
		if err != nil {
			writeError(w, err)
			return
		}
		next := current.WithDefaults()
		if req.AutoTranslateIncoming != nil {
			next.AutoTranslateIncoming = req.AutoTranslateIncoming
		}
		if req.AutoTranslateOutgoing != nil {
			next.AutoTranslateOutgoing = req.AutoTranslateOutgoing
		}
		if req.ReviewTranslation != nil {
			next.ReviewTranslation = req.ReviewTranslation
		}
		if req.AISuggestReplies != nil {
			next.AISuggestReplies = req.AISuggestReplies
		}
		if req.KnowledgeSuggestEnabled != nil {
			next.KnowledgeSuggestEnabled = req.KnowledgeSuggestEnabled
		}
		if req.TakeoverPopupEnabled != nil {
			next.TakeoverPopupEnabled = req.TakeoverPopupEnabled
		}
		if req.ReadOnlyWatch != nil {
			next.ReadOnlyWatch = req.ReadOnlyWatch
		}
		if req.KnowledgeBaseIDs != nil {
			baseIDs, err := resolveAgentKnowledgeBaseIDs(service, *req.KnowledgeBaseIDs)
			if err != nil {
				writeError(w, err)
				return
			}
			next.KnowledgeBaseIDs = baseIDs
		}
		if req.DialectMode != nil {
			if !domain.ValidDialectMode(*req.DialectMode) {
				writeError(w, fmt.Errorf("%w: translation_dialect_mode 只允许 standard 或 dialect", store.ErrInvalidInput))
				return
			}
			next.DialectMode = domain.NormalizeDialectMode(*req.DialectMode)
		}
		updated, err := service.Store().UpdateAgentPreferences(session.AccountID, next)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": updated.WithDefaults()})
	})

	// 知识库辅助推荐发送：只生成 1-3 条候选回复，绝不落库任何消息，AI 不直接发送。
	mux.HandleFunc("POST /api/agent/suggest-reply", func(w http.ResponseWriter, r *http.Request) {
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
			ConversationID string `json:"conversation_id"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := service.SuggestReply(r.Context(), session.AccountID, req.ConversationID)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidInput), errors.Is(err, store.ErrForbidden):
				writeError(w, err)
			case errors.Is(err, app.ErrAIProviderHostNotAllowed):
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ai_provider_host_not_allowed", "message": err.Error()})
			default:
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": "suggest_reply_failed", "message": err.Error()})
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	// 发送前译文预览：只翻译，不创建任何消息。
	mux.HandleFunc("POST /api/agent/translate-preview", func(w http.ResponseWriter, r *http.Request) {
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
			ConversationID string `json:"conversation_id"`
			Content        string `json:"content"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		preview, err := service.TranslatePreview(r.Context(), session.AccountID, req.ConversationID, req.Content)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidInput) || errors.Is(err, store.ErrForbidden) {
				writeError(w, err)
				return
			}
			// 翻译失败：只报错，不写库、不产生消息。
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "translation_failed", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, preview)
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
			ForceUpdate:  formValueTruthy(r.FormValue("force_update")),
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
		conversation, err := service.MarkConversationRead(session, r.PathValue("id"))
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
		// 请求体未带 api_key 时回退到已保存的 Key，与翻译模块行为一致；两者都为空才报错。
		if strings.TrimSpace(preview.APIKey) == "" {
			preview.APIKey = strings.TrimSpace(service.Store().AISettings().APIKey)
		}
		if strings.TrimSpace(preview.APIKey) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "ai_api_key_missing",
				"message": "AI API Key 未配置，无法获取模型列表",
			})
			return
		}
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
		// 保存后自动逐个实测模型缓存支持：先取上游模型列表（最多 20 个），
		// 支持的模型后续自动走最高效缓存方式，不支持的保持原样。
		go func() {
			probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			var models []string
			if list, listErr := service.AIModels(probeCtx, updated); listErr == nil {
				models = list
			}
			service.ProbeModelCaches(probeCtx, models)
		}()
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("GET /api/admin/ai-settings/cache-probe", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().AICacheProbe())
	})

	mux.HandleFunc("POST /api/admin/ai-settings/cache-probe", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			Models []string `json:"models"`
		}
		_ = decodeOptionalJSON(w, r, &req)
		if service.Store().AICacheProbe().Status == "running" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "probe_running", "message": "缓存探测正在进行中"})
			return
		}
		go func() {
			probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			models := req.Models
			if len(models) == 0 {
				if settings := service.Store().AISettings(); strings.TrimSpace(settings.Model) != "" {
					models = []string{settings.Model}
				}
			}
			service.ProbeModelCaches(probeCtx, models)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
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

	mux.HandleFunc("GET /api/admin/ai-providers", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		providers := service.Store().ListAIProviders()
		if providers == nil {
			providers = []domain.AIProvider{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	})

	// 供应商熔断健康快照：只读、仅管理员，返回状态与脱敏后的最后错误，绝不含密钥。
	mux.HandleFunc("GET /api/admin/ai-providers/health", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		providers := service.AIProviderHealth()
		if providers == nil {
			providers = []domain.AIProviderHealth{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
	})

	mux.HandleFunc("POST /api/admin/ai-providers", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req aiProviderRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		next := domain.AIProvider{
			Name:         req.Name,
			Kind:         req.Kind,
			APIType:      req.APIType,
			BaseURL:      req.BaseURL,
			Models:       req.Models,
			DefaultModel: req.DefaultModel,
			TimeoutMS:    req.TimeoutMS,
			AllowedHosts: req.AllowedHosts,
			Enabled:      enabled,
			SortOrder:    req.SortOrder,
		}
		provider, err := service.Store().CreateAIProvider(next, req.APIKey)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_provider.create", "ai_provider", provider.ID, "新增 AI 供应商 "+provider.Name)
		writeJSON(w, http.StatusCreated, map[string]any{"provider": provider})
	})

	mux.HandleFunc("PATCH /api/admin/ai-providers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req aiProviderPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		id := r.PathValue("id")
		current, ok := service.Store().AIProvider(id)
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		next := current
		if req.Name != nil {
			next.Name = *req.Name
		}
		if req.Kind != nil {
			next.Kind = *req.Kind
		}
		if req.APIType != nil {
			next.APIType = *req.APIType
		}
		if req.BaseURL != nil {
			next.BaseURL = *req.BaseURL
		}
		if req.Models != nil {
			next.Models = *req.Models
		}
		if req.DefaultModel != nil {
			next.DefaultModel = *req.DefaultModel
		}
		if req.TimeoutMS != nil {
			next.TimeoutMS = *req.TimeoutMS
		}
		if req.AllowedHosts != nil {
			next.AllowedHosts = *req.AllowedHosts
		}
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		if req.SortOrder != nil {
			next.SortOrder = *req.SortOrder
		}
		updated, err := service.Store().UpdateAIProvider(id, next, req.APIKey)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_provider.update", "ai_provider", updated.ID, "更新 AI 供应商 "+updated.Name)
		writeJSON(w, http.StatusOK, map[string]any{"provider": updated})
	})

	mux.HandleFunc("DELETE /api/admin/ai-providers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		id := r.PathValue("id")
		if err := service.Store().DeleteAIProvider(id); err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_provider.delete", "ai_provider", id, "删除 AI 供应商")
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
	})

	mux.HandleFunc("POST /api/admin/ai-providers/{id}/models", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		provider, models, err := service.FetchProviderModels(r.Context(), r.PathValue("id"))
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidInput):
				writeError(w, err)
			case errors.Is(err, app.ErrAIProviderHostNotAllowed):
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ai_provider_host_not_allowed", "message": err.Error()})
			default:
				// 上游失败统一返回 502，并附带具体原因，方便管理员排查。
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": "ai_models_unavailable", "message": err.Error()})
			}
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_provider.models_sync", "ai_provider", provider.ID, "一键获取 AI 供应商模型列表")
		writeJSON(w, http.StatusOK, map[string]any{"provider": provider, "models": models})
	})

	mux.HandleFunc("POST /api/admin/ai-providers/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		result, err := service.TestProvider(r.Context(), r.PathValue("id"))
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidInput):
				writeError(w, err)
			case errors.Is(err, app.ErrAIProviderHostNotAllowed):
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ai_provider_host_not_allowed", "message": err.Error()})
			default:
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"error":      "ai_provider_test_failed",
					"message":    err.Error(),
					"ok":         false,
					"latency_ms": result.LatencyMS,
				})
			}
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_provider.test", "ai_provider", r.PathValue("id"), "测试 AI 供应商连通性")
		writeJSON(w, http.StatusOK, result)
	})

	// 【服务间接口】仅限管理员会话调用，供 admin-server 读取供应商明文配置（下轮接入）。
	// 普通后台列表/详情永远不会返回 api_key 明文；此接口必须限制在内网调用。
	// /resolve 与 /secret 指向同一实现，兼容服务间既有叫法。
	mux.HandleFunc("GET /api/admin/ai-providers/{id}/secret", handleAIProviderSecret(service, trustedProxies))
	mux.HandleFunc("GET /api/admin/ai-providers/{id}/resolve", handleAIProviderSecret(service, trustedProxies))

	mux.HandleFunc("GET /api/admin/ai-bindings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": aiBindingList(service.Store().AIBindings())})
	})

	mux.HandleFunc("PUT /api/admin/ai-bindings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			Bindings []domain.AIBinding `json:"bindings"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		// 先整体校验再落库，避免部分成功导致能力指向半配置状态。
		if err := validateAIBindingRequest(service, req.Bindings); err != nil {
			writeError(w, err)
			return
		}
		for _, binding := range req.Bindings {
			if _, err := service.Store().UpsertAIBinding(binding); err != nil {
				writeError(w, err)
				return
			}
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_bindings.update", "ai_bindings", "", "更新 AI 能力绑定")
		writeJSON(w, http.StatusOK, map[string]any{"bindings": aiBindingList(service.Store().AIBindings())})
	})

	// ===== 能力候选供应商（同能力多供应商 + 故障转移）=====
	// GET 返回全部候选；PUT 为整体替换：请求里未出现的候选会被删除（可传空数组清空）。
	mux.HandleFunc("GET /api/admin/ai-binding-targets", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"targets": aiBindingTargetList(service.Store().AIBindingTargets())})
	})

	mux.HandleFunc("PUT /api/admin/ai-binding-targets", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			Targets []aiBindingTargetRequest `json:"targets"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		targets := make([]domain.AIBindingTarget, 0, len(req.Targets))
		for _, item := range req.Targets {
			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			targets = append(targets, domain.AIBindingTarget{
				Capability: item.Capability,
				ProviderID: item.ProviderID,
				Model:      item.Model,
				Priority:   item.Priority,
				Enabled:    enabled,
			})
		}
		// 先整体校验再落库，避免部分成功导致能力指向半配置状态。
		if err := validateAIBindingTargetRequest(service, targets); err != nil {
			writeError(w, err)
			return
		}
		keep := make(map[string]bool, len(targets))
		for _, target := range targets {
			saved, err := service.Store().UpsertAIBindingTarget(target)
			if err != nil {
				writeError(w, err)
				return
			}
			keep[saved.Capability+"\x00"+saved.ProviderID] = true
		}
		// 整体替换语义：删除本次请求未包含的旧候选。
		for _, existing := range service.Store().AIBindingTargets() {
			key := existing.Capability + "\x00" + existing.ProviderID
			if keep[key] {
				continue
			}
			if err := service.Store().DeleteAIBindingTarget(existing.Capability, existing.ProviderID); err != nil && !errors.Is(err, store.ErrNotFound) {
				writeError(w, err)
				return
			}
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_binding_targets.update", "ai_binding_targets", "", "更新 AI 能力候选供应商")
		writeJSON(w, http.StatusOK, map[string]any{"targets": aiBindingTargetList(service.Store().AIBindingTargets())})
	})

	// ===== AI 知识库（后台管理）=====
	mux.HandleFunc("GET /api/admin/knowledge/bases", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		bases := service.Store().KnowledgeBases()
		if bases == nil {
			bases = []domain.KnowledgeBase{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"bases": bases})
	})

	mux.HandleFunc("POST /api/admin/knowledge/bases", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeBaseCreateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		base, err := service.Store().CreateKnowledgeBase(domain.KnowledgeBase{
			Name:        req.Name,
			Description: req.Description,
			Enabled:     enabled,
			SortOrder:   req.SortOrder,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_base.create", "knowledge_base", base.ID, "新增知识库 "+base.Name)
		writeJSON(w, http.StatusCreated, map[string]any{"base": base})
	})

	mux.HandleFunc("PATCH /api/admin/knowledge/bases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeBasePatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		id := r.PathValue("id")
		current, ok := service.Store().KnowledgeBase(id)
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		next := current
		if req.Name != nil {
			next.Name = *req.Name
		}
		if req.Description != nil {
			next.Description = *req.Description
		}
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		if req.SortOrder != nil {
			next.SortOrder = *req.SortOrder
		}
		updated, err := service.Store().UpdateKnowledgeBase(id, next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_base.update", "knowledge_base", updated.ID, "更新知识库 "+updated.Name)
		writeJSON(w, http.StatusOK, map[string]any{"base": updated})
	})

	mux.HandleFunc("DELETE /api/admin/knowledge/bases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		id := r.PathValue("id")
		deleted, err := service.Store().DeleteKnowledgeBase(id)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_base.delete", "knowledge_base", id, "删除知识库 "+deleted.Name)
		writeJSON(w, http.StatusOK, map[string]any{"base": deleted})
	})

	mux.HandleFunc("GET /api/admin/knowledge/entries", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		entries := service.Store().KnowledgeEntries(
			r.URL.Query().Get("base_id"),
			r.URL.Query().Get("q"),
			limit,
			offset,
		)
		if entries == nil {
			entries = []domain.KnowledgeEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})

	mux.HandleFunc("POST /api/admin/knowledge/entries", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeEntryCreateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		entry, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
			BaseID:   req.BaseID,
			Title:    req.Title,
			Content:  req.Content,
			Keywords: req.Keywords,
			Category: req.Category,
			Priority: req.Priority,
			Enabled:  enabled,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_entry.create", "knowledge_entry", entry.ID, "新增知识条目")
		writeJSON(w, http.StatusCreated, map[string]any{"entry": entry})
	})

	mux.HandleFunc("PATCH /api/admin/knowledge/entries/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeEntryPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		id := r.PathValue("id")
		current, ok := service.Store().KnowledgeEntry(id)
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		next := current
		if req.BaseID != nil {
			next.BaseID = *req.BaseID
		}
		if req.Title != nil {
			next.Title = *req.Title
		}
		if req.Content != nil {
			next.Content = *req.Content
		}
		if req.Keywords != nil {
			next.Keywords = *req.Keywords
		}
		if req.Category != nil {
			next.Category = *req.Category
		}
		if req.Priority != nil {
			next.Priority = *req.Priority
		}
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		updated, err := service.Store().UpdateKnowledgeEntry(id, next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_entry.update", "knowledge_entry", updated.ID, "更新知识条目")
		writeJSON(w, http.StatusOK, map[string]any{"entry": updated})
	})

	mux.HandleFunc("DELETE /api/admin/knowledge/entries/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		id := r.PathValue("id")
		deleted, err := service.Store().DeleteKnowledgeEntry(id)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_entry.delete", "knowledge_entry", id, "删除知识条目")
		writeJSON(w, http.StatusOK, map[string]any{"entry": deleted})
	})

	// 后台检索预览：返回排序后的命中与分数，不做阈值过滤，便于管理员对照阈值调参。
	mux.HandleFunc("POST /api/admin/knowledge/search", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeSearchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			writeError(w, fmt.Errorf("%w: query 不能为空", store.ErrInvalidInput))
			return
		}
		limit := req.Limit
		if limit <= 0 {
			limit = service.Store().KnowledgeSettings().MaxEntries
		}
		if limit > 20 {
			limit = 20
		}
		hits := service.PreviewKnowledgeSearch(query, req.BaseIDs, limit)
		if hits == nil {
			hits = []domain.KnowledgeHit{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})

	// 批量导入知识条目：base_id 必须存在；单次最多 1000 条。
	mux.HandleFunc("POST /api/admin/knowledge/import", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeImportRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		baseID := strings.TrimSpace(req.BaseID)
		if baseID == "" || len(req.Entries) == 0 {
			writeError(w, fmt.Errorf("%w: base_id 与 entries 不能为空", store.ErrInvalidInput))
			return
		}
		if len(req.Entries) > 1000 {
			writeError(w, fmt.Errorf("%w: 单次最多导入 1000 条", store.ErrInvalidInput))
			return
		}
		if _, ok := service.Store().KnowledgeBase(baseID); !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		imported := 0
		for _, item := range req.Entries {
			entry, err := service.Store().CreateKnowledgeEntry(domain.KnowledgeEntry{
				BaseID:   baseID,
				Title:    item.Title,
				Content:  item.Content,
				Keywords: item.Keywords,
				Category: item.Category,
				Priority: item.Priority,
				Enabled:  true,
			})
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":    "invalid_input",
					"message":  err.Error(),
					"imported": imported,
				})
				return
			}
			_ = entry
			imported++
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_entry.import", "knowledge_base", baseID, "批量导入知识条目")
		writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
	})

	mux.HandleFunc("GET /api/admin/knowledge/settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().KnowledgeSettings())
	})

	mux.HandleFunc("PATCH /api/admin/knowledge/settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req knowledgeSettingsPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		next := service.Store().KnowledgeSettings()
		if req.UnknownAction != nil {
			if !domain.ValidKnowledgeUnknownAction(*req.UnknownAction) {
				writeError(w, fmt.Errorf("%w: unknown_action 只允许 refuse 或 handoff", store.ErrInvalidInput))
				return
			}
			next.UnknownAction = domain.NormalizeKnowledgeUnknownAction(*req.UnknownAction)
		}
		if req.RefuseReply != nil {
			next.RefuseReply = *req.RefuseReply
		}
		if req.ScoreThreshold != nil {
			if *req.ScoreThreshold < 0 || *req.ScoreThreshold > 1 {
				writeError(w, fmt.Errorf("%w: score_threshold 必须在 0 与 1 之间", store.ErrInvalidInput))
				return
			}
			next.ScoreThreshold = *req.ScoreThreshold
		}
		if req.MaxEntries != nil {
			if *req.MaxEntries < 0 || *req.MaxEntries > 20 {
				writeError(w, fmt.Errorf("%w: max_entries 必须在 0 与 20 之间", store.ErrInvalidInput))
				return
			}
			next.MaxEntries = *req.MaxEntries
		}
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		updated, err := service.Store().UpdateKnowledgeSettings(next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "knowledge_settings.update", "knowledge_settings", "1", "更新知识库检索配置")
		writeJSON(w, http.StatusOK, updated)
	})

	// AI 频率/次数/单次字数限额：GET 返回单例配置，PATCH 支持指针语义的局部更新。
	// 越界值（负数或超过上限）返回 400，由管理员显式修正，避免"看起来保存成功但被静默收敛"。
	mux.HandleFunc("GET /api/admin/ai-limits", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"limits": service.Store().AILimits()})
	})

	mux.HandleFunc("PATCH /api/admin/ai-limits", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req aiLimitsPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		next := service.Store().AILimits()
		if req.Enabled != nil {
			next.Enabled = *req.Enabled
		}
		if req.VisitorMessagesPerMinute != nil {
			if err := validateAILimitRange("visitor_messages_per_minute", *req.VisitorMessagesPerMinute, domain.AILimitMaxPerMinute); err != nil {
				writeError(w, err)
				return
			}
			next.VisitorMessagesPerMinute = *req.VisitorMessagesPerMinute
		}
		if req.VisitorMessagesPerDay != nil {
			if err := validateAILimitRange("visitor_messages_per_day", *req.VisitorMessagesPerDay, domain.AILimitMaxPerDay); err != nil {
				writeError(w, err)
				return
			}
			next.VisitorMessagesPerDay = *req.VisitorMessagesPerDay
		}
		if req.AICallsPerConversationDay != nil {
			if err := validateAILimitRange("ai_calls_per_conversation_day", *req.AICallsPerConversationDay, domain.AILimitMaxAICalls); err != nil {
				writeError(w, err)
				return
			}
			next.AICallsPerConversationDay = *req.AICallsPerConversationDay
		}
		if req.AIReplyMaxChars != nil {
			if err := validateAILimitRange("ai_reply_max_chars", *req.AIReplyMaxChars, domain.AILimitMaxReplyChars); err != nil {
				writeError(w, err)
				return
			}
			next.AIReplyMaxChars = *req.AIReplyMaxChars
		}
		if req.ExceedMessage != nil {
			next.ExceedMessage = *req.ExceedMessage
		}
		updated, err := service.Store().UpdateAILimits(next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "ai_limits.update", "ai_limits", "1", "更新 AI 限额配置")
		writeJSON(w, http.StatusOK, map[string]any{"limits": updated})
	})

	// 管理员查看/重置某客服的偏好，复用客服自助接口的存储方法。
	mux.HandleFunc("GET /api/admin/agents/{id}/preferences", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		preferences, err := service.Store().AgentPreferences(r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preferences": preferences.WithDefaults()})
	})

	mux.HandleFunc("PATCH /api/admin/agents/{id}/preferences", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req agentPreferencesPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		agentID := r.PathValue("id")
		current, err := service.Store().AgentPreferences(agentID)
		if err != nil {
			writeError(w, err)
			return
		}
		next := current.WithDefaults()
		if req.AutoTranslateIncoming != nil {
			next.AutoTranslateIncoming = req.AutoTranslateIncoming
		}
		if req.AutoTranslateOutgoing != nil {
			next.AutoTranslateOutgoing = req.AutoTranslateOutgoing
		}
		if req.ReviewTranslation != nil {
			next.ReviewTranslation = req.ReviewTranslation
		}
		if req.AISuggestReplies != nil {
			next.AISuggestReplies = req.AISuggestReplies
		}
		if req.KnowledgeSuggestEnabled != nil {
			next.KnowledgeSuggestEnabled = req.KnowledgeSuggestEnabled
		}
		if req.TakeoverPopupEnabled != nil {
			next.TakeoverPopupEnabled = req.TakeoverPopupEnabled
		}
		if req.ReadOnlyWatch != nil {
			next.ReadOnlyWatch = req.ReadOnlyWatch
		}
		if req.KnowledgeBaseIDs != nil {
			baseIDs, err := resolveAgentKnowledgeBaseIDs(service, *req.KnowledgeBaseIDs)
			if err != nil {
				writeError(w, err)
				return
			}
			next.KnowledgeBaseIDs = baseIDs
		}
		if req.DialectMode != nil {
			if !domain.ValidDialectMode(*req.DialectMode) {
				writeError(w, fmt.Errorf("%w: translation_dialect_mode 只允许 standard 或 dialect", store.ErrInvalidInput))
				return
			}
			next.DialectMode = domain.NormalizeDialectMode(*req.DialectMode)
		}
		updated, err := service.Store().UpdateAgentPreferences(agentID, next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "agent.preferences_update", "agent", agentID, "管理员更新客服偏好")
		writeJSON(w, http.StatusOK, map[string]any{"preferences": updated.WithDefaults()})
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

	// 推送配置：客户端 Firebase 参数与 APNs/FCM 密钥都在后台维护，改完立即生效。
	mux.HandleFunc("GET /api/admin/push-settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, service.Store().PushSettings())
	})

	mux.HandleFunc("PATCH /api/admin/push-settings", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req struct {
			FirebaseAPIKey            *string `json:"firebase_api_key"`
			FirebaseAppID             *string `json:"firebase_app_id"`
			FirebaseMessagingSenderID *string `json:"firebase_messaging_sender_id"`
			FirebaseProjectID         *string `json:"firebase_project_id"`
			FirebaseStorageBucket     *string `json:"firebase_storage_bucket"`
			FirebaseVapidKey          *string `json:"firebase_vapid_key"`
			FCMServiceAccountJSON     *string `json:"fcm_service_account_json"`
			FCMAccessToken            *string `json:"fcm_access_token"`
			APNsKeyP8                 *string `json:"apns_key_p8"`
			APNsKeyID                 *string `json:"apns_key_id"`
			APNsTeamID                *string `json:"apns_team_id"`
			APNsTopic                 *string `json:"apns_topic"`
			APNsEnvironment           *string `json:"apns_environment"`
			ClearFCM                  *bool   `json:"clear_fcm"`
			ClearAPNs                 *bool   `json:"clear_apns"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		next := service.Store().PushSettings()
		apply := func(target *string, value *string) {
			if value != nil {
				*target = strings.TrimSpace(*value)
			}
		}
		apply(&next.FirebaseAPIKey, req.FirebaseAPIKey)
		apply(&next.FirebaseAppID, req.FirebaseAppID)
		apply(&next.FirebaseMessagingSenderID, req.FirebaseMessagingSenderID)
		apply(&next.FirebaseProjectID, req.FirebaseProjectID)
		apply(&next.FirebaseStorageBucket, req.FirebaseStorageBucket)
		apply(&next.FirebaseVapidKey, req.FirebaseVapidKey)
		apply(&next.FCMServiceAccountJSON, req.FCMServiceAccountJSON)
		apply(&next.FCMAccessToken, req.FCMAccessToken)
		apply(&next.APNsKeyP8, req.APNsKeyP8)
		apply(&next.APNsKeyID, req.APNsKeyID)
		apply(&next.APNsTeamID, req.APNsTeamID)
		apply(&next.APNsTopic, req.APNsTopic)
		apply(&next.APNsEnvironment, req.APNsEnvironment)
		if req.ClearFCM != nil && *req.ClearFCM {
			next.FCMServiceAccountJSON = ""
			next.FCMAccessToken = ""
		}
		if req.ClearAPNs != nil && *req.ClearAPNs {
			next.APNsKeyP8 = ""
		}
		updated, err := service.Store().UpdatePushSettings(next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, trustedProxies, "push_settings.update", "push_settings", "1", fmt.Sprintf(
			"更新推送配置 fcm=%t apns=%t", updated.FCMConfigured, updated.APNsConfigured))
		writeJSON(w, http.StatusOK, updated)
	})

	mux.HandleFunc("POST /api/admin/push-settings/test", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		diagnostics := notify.NewProvider(logger).Validate(ctx, service.Store().PushSettings())
		recordAuditFromRequest(service, r, trustedProxies, "push_settings.tested", "push_settings", "1", "验证推送凭证")
		writeJSON(w, http.StatusOK, map[string]any{"diagnostics": diagnostics})
	})

	// 客服 App 启动时拉取公开 Firebase 参数，不再依赖构建期 dart-define。
	mux.HandleFunc("GET /api/app-config", func(w http.ResponseWriter, r *http.Request) {
		settings := service.Store().PushSettings()
		writeJSON(w, http.StatusOK, map[string]any{
			"firebase":     settings.ClientConfig(),
			"push_enabled": settings.PushEnabled(),
			"apns_enabled": settings.APNsConfigured,
		})
	})

	// 开放接口（第三方平台接入）与 API 授权管理。
	openService := openapi.NewService(openapi.Options{
		Store:            service.Store(),
		App:              service,
		Logger:           logger,
		Enabled:          cfg.OpenAPIEnabled,
		DefaultRateLimit: cfg.OpenAPIDefaultRateLimit,
		ResolveIP: func(r *http.Request) (string, string) {
			info := resolveClientIP(r, trustedProxies)
			return info.IP, info.Source
		},
	})

	mux.HandleFunc("GET /api/admin/api-apps", openService.HandleAdminApps)
	mux.HandleFunc("POST /api/admin/api-apps", openService.HandleAdminApps)
	mux.HandleFunc("PATCH /api/admin/api-apps/{id}", openService.HandleAdminApp)
	mux.HandleFunc("DELETE /api/admin/api-apps/{id}", openService.HandleAdminApp)
	mux.HandleFunc("POST /api/admin/api-apps/{id}/reset-key", openService.HandleAdminAppResetKey)

	mux.HandleFunc("OPTIONS /open/", openService.HandleOpenCORS)
	mux.HandleFunc("GET /open/v1/me", openService.HandleMe)
	mux.HandleFunc("GET /open/v1/conversations", openService.HandleConversations)
	mux.HandleFunc("POST /open/v1/conversations", openService.HandleConversations)
	mux.HandleFunc("GET /open/v1/conversations/{id}", openService.HandleConversation)
	mux.HandleFunc("GET /open/v1/conversations/{id}/messages", openService.HandleConversationMessages)
	mux.HandleFunc("POST /open/v1/conversations/{id}/messages", openService.HandleConversationMessages)
	mux.HandleFunc("POST /open/v1/conversations/{id}/close", openService.HandleConversationClose)
	mux.HandleFunc("GET /open/v1/ws", openService.HandleWebSocket)

	mux.HandleFunc("GET /ws", service.ServeWebSocket)

	// ===== 批次 4 第一波：客服策略 / 排队 / 黑名单 / 提醒渠道 / SMTP（只做配置与测试发送）=====
	registerServicePolicyRoutes(mux, service, trustedProxies)

	// ===== P5：访客画像（配置 / 列表 / 详情 / 重算 / 客服会话侧画像卡）=====
	registerVisitorProfileRoutes(mux, service, trustedProxies)

	// ===== 服务间接口（X-Internal-Token 鉴权；未配置令牌时 503）=====
	registerInternalRoutes(mux, service, cfg)
	// 数据看板：只读询盘聚合（口径见 analytics_dashboard.go）。
	registerAnalyticsDashboardRoutes(mux, service, cfg)

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
	// VisitorRef 是官网埋点写入的访客 ID（astreva_visitor_id），作为画像聚合键；
	// 缺失/非法/超长时由存储层清洗为空串，行为与不提供完全一致。
	VisitorRef string `json:"visitor_ref"`
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

type aiProviderRequest struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	APIType      string   `json:"api_type"`
	BaseURL      string   `json:"base_url"`
	APIKey       string   `json:"api_key"`
	Models       []string `json:"models"`
	DefaultModel string   `json:"default_model"`
	TimeoutMS    int      `json:"timeout_ms"`
	AllowedHosts string   `json:"allowed_hosts"`
	Enabled      *bool    `json:"enabled"`
	SortOrder    int      `json:"sort_order"`
}

// apiProviderPatchRequest 用指针区分“未提供”与“显式更新为空值”：
// api_key 未传 = 不改，传空串 = 清除。
type aiProviderPatchRequest struct {
	Name         *string   `json:"name"`
	Kind         *string   `json:"kind"`
	APIType      *string   `json:"api_type"`
	BaseURL      *string   `json:"base_url"`
	APIKey       *string   `json:"api_key"`
	Models       *[]string `json:"models"`
	DefaultModel *string   `json:"default_model"`
	TimeoutMS    *int      `json:"timeout_ms"`
	AllowedHosts *string   `json:"allowed_hosts"`
	Enabled      *bool     `json:"enabled"`
	SortOrder    *int      `json:"sort_order"`
}

// aiBindingTargetRequest 是能力候选供应商的写入请求体。
// enabled 用指针区分“未提供”（默认启用）与“显式禁用”。
type aiBindingTargetRequest struct {
	Capability string `json:"capability"`
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	Priority   int    `json:"priority"`
	Enabled    *bool  `json:"enabled"`
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

// agentPreferencesPatchRequest 是管理员重置客服偏好的局部更新请求。
type agentPreferencesPatchRequest struct {
	AutoTranslateIncoming   *bool     `json:"auto_translate_incoming"`
	AutoTranslateOutgoing   *bool     `json:"auto_translate_outgoing"`
	ReviewTranslation       *bool     `json:"review_translation_before_send"`
	DialectMode             *string   `json:"translation_dialect_mode"`
	AISuggestReplies        *bool     `json:"ai_suggest_replies"`
	KnowledgeSuggestEnabled *bool     `json:"knowledge_suggest_enabled"`
	KnowledgeBaseIDs        *[]string `json:"knowledge_base_ids"`
	TakeoverPopupEnabled    *bool     `json:"takeover_popup_enabled"`
	ReadOnlyWatch           *bool     `json:"read_only_watch"`
}

// knowledgeBaseCreateRequest 是新建知识库请求；enabled 省略时默认 true。
type knowledgeBaseCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	SortOrder   int    `json:"sort_order"`
}

// knowledgeBasePatchRequest 是知识库局部更新请求。
type knowledgeBasePatchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
	SortOrder   *int    `json:"sort_order"`
}

// knowledgeEntryCreateRequest 是新建知识条目请求；enabled 省略时默认 true。
type knowledgeEntryCreateRequest struct {
	BaseID   string   `json:"base_id"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Keywords []string `json:"keywords"`
	Category string   `json:"category"`
	Priority int      `json:"priority"`
	Enabled  *bool    `json:"enabled"`
}

// knowledgeEntryPatchRequest 是知识条目局部更新请求。
type knowledgeEntryPatchRequest struct {
	BaseID   *string   `json:"base_id"`
	Title    *string   `json:"title"`
	Content  *string   `json:"content"`
	Keywords *[]string `json:"keywords"`
	Category *string   `json:"category"`
	Priority *int      `json:"priority"`
	Enabled  *bool     `json:"enabled"`
}

// knowledgeSearchRequest 是后台检索预览请求。
type knowledgeSearchRequest struct {
	Query   string   `json:"query"`
	BaseIDs []string `json:"base_ids"`
	Limit   int      `json:"limit"`
}

// knowledgeImportRequest 是批量导入请求。
type knowledgeImportRequest struct {
	BaseID  string `json:"base_id"`
	Entries []struct {
		Title    string   `json:"title"`
		Content  string   `json:"content"`
		Keywords []string `json:"keywords"`
		Category string   `json:"category"`
		Priority int      `json:"priority"`
	} `json:"entries"`
}

// knowledgeSettingsPatchRequest 是知识库检索配置局部更新请求。
type knowledgeSettingsPatchRequest struct {
	UnknownAction  *string  `json:"unknown_action"`
	RefuseReply    *string  `json:"refuse_reply"`
	ScoreThreshold *float64 `json:"score_threshold"`
	MaxEntries     *int     `json:"max_entries"`
	Enabled        *bool    `json:"enabled"`
}

// aiLimitsPatchRequest 是 AI 限额局部更新请求：指针语义，nil 表示不修改。
type aiLimitsPatchRequest struct {
	Enabled                   *bool   `json:"enabled"`
	VisitorMessagesPerMinute  *int    `json:"visitor_messages_per_minute"`
	VisitorMessagesPerDay     *int    `json:"visitor_messages_per_day"`
	AICallsPerConversationDay *int    `json:"ai_calls_per_conversation_day"`
	AIReplyMaxChars           *int    `json:"ai_reply_max_chars"`
	ExceedMessage             *string `json:"exceed_message"`
}

// validateAILimitRange 校验单个限额字段：负数为非法，超过上限同样拒绝（HTTP 层选择 400 而非静默收敛）。
func validateAILimitRange(name string, value, max int) error {
	if value < 0 {
		return fmt.Errorf("%w: %s 不能为负数", store.ErrInvalidInput, name)
	}
	if value > max {
		return fmt.Errorf("%w: %s 不能超过 %d", store.ErrInvalidInput, name, max)
	}
	return nil
}

// resolveAgentKnowledgeBaseIDs 清洗并校验客服限定的知识库 ID：去空去重，且必须真实存在。
func resolveAgentKnowledgeBaseIDs(service *app.Service, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := service.Store().KnowledgeBase(id); !ok {
			return nil, fmt.Errorf("%w: 知识库 %s 不存在", store.ErrInvalidInput, id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// handleAIProviderSecret 实现服务间解密配置读取接口：
// 仅管理员会话可访问，返回明文 api_key，响应头标记 X-Astreva-Service-Only。
func handleAIProviderSecret(service *app.Service, proxies trustedProxySet) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		provider, ok := service.Store().AIProvider(r.PathValue("id"))
		if !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		model := strings.TrimSpace(provider.DefaultModel)
		if model == "" && len(provider.Models) > 0 {
			model = strings.TrimSpace(provider.Models[0])
		}
		w.Header().Set("X-Astreva-Service-Only", "true")
		w.Header().Set("Cache-Control", "no-store")
		recordAuditFromRequest(service, r, proxies, "ai_provider.secret_read", "ai_provider", provider.ID, "服务间读取 AI 供应商解密配置")
		writeJSON(w, http.StatusOK, map[string]any{
			"service_only":  true,
			"id":            provider.ID,
			"name":          provider.Name,
			"kind":          provider.Kind,
			"api_type":      provider.APIType,
			"base_url":      provider.BaseURL,
			"api_key":       provider.APIKey,
			"model":         model,
			"default_model": provider.DefaultModel,
			"models":        provider.Models,
			"allowed_hosts": provider.AllowedHosts,
			"timeout_ms":    provider.TimeoutMS,
			"enabled":       provider.Enabled,
		})
	}
}

// aiBindingList 把能力绑定 map 转为稳定顺序的列表，便于前端渲染与测试断言。
func aiBindingList(bindings map[string]domain.AIBinding) []domain.AIBinding {
	out := make([]domain.AIBinding, 0, len(bindings))
	seen := map[string]bool{}
	for _, capability := range domain.AICapabilities() {
		if binding, ok := bindings[capability]; ok {
			out = append(out, binding)
			seen[capability] = true
		}
	}
	extras := make([]string, 0)
	for capability := range bindings {
		if !seen[capability] {
			extras = append(extras, capability)
		}
	}
	sort.Strings(extras)
	for _, capability := range extras {
		out = append(out, bindings[capability])
	}
	return out
}

// validateAIBindingRequest 在落库前整体校验能力、供应商存在性、启用状态与类型匹配。
func validateAIBindingRequest(service *app.Service, bindings []domain.AIBinding) error {
	seen := map[string]bool{}
	for _, binding := range bindings {
		capability := strings.ToLower(strings.TrimSpace(binding.Capability))
		if !domain.ValidAICapability(capability) {
			return fmt.Errorf("%w: 能力 %q 不合法", store.ErrInvalidInput, binding.Capability)
		}
		if seen[capability] {
			return fmt.Errorf("%w: 能力 %q 重复提交", store.ErrInvalidInput, capability)
		}
		seen[capability] = true
		providerID := strings.TrimSpace(binding.ProviderID)
		if providerID == "" {
			// 空 provider_id 表示解除绑定，允许。
			continue
		}
		provider, ok := service.Store().AIProvider(providerID)
		if !ok {
			return fmt.Errorf("%w: 供应商 %q 不存在", store.ErrInvalidInput, providerID)
		}
		if !provider.Enabled {
			return fmt.Errorf("%w: 供应商 %q 已禁用", store.ErrInvalidInput, providerID)
		}
		if provider.Kind != domain.AICapabilityKind(capability) {
			return fmt.Errorf("%w: 能力 %q 需要 %s 类型供应商，供应商 %q 是 %s 类型",
				store.ErrInvalidInput, capability, domain.AICapabilityKind(capability), providerID, provider.Kind)
		}
	}
	return nil
}

// aiBindingTargetList 把候选列表归一为稳定顺序（capability、priority、provider_id），
// 存储层已排序，这里只保证空列表序列化为 []。
func aiBindingTargetList(targets []domain.AIBindingTarget) []domain.AIBindingTarget {
	if targets == nil {
		return []domain.AIBindingTarget{}
	}
	return targets
}

// validateAIBindingTargetRequest 在落库前整体校验候选：
// 能力合法、provider_id 非空且不重复、供应商存在且已启用、类型与能力匹配。
func validateAIBindingTargetRequest(service *app.Service, targets []domain.AIBindingTarget) error {
	seen := map[string]bool{}
	for _, target := range targets {
		capability := strings.ToLower(strings.TrimSpace(target.Capability))
		if !domain.ValidAICapability(capability) {
			return fmt.Errorf("%w: 能力 %q 不合法", store.ErrInvalidInput, target.Capability)
		}
		providerID := strings.TrimSpace(target.ProviderID)
		if providerID == "" {
			return fmt.Errorf("%w: 能力 %q 的 provider_id 不能为空", store.ErrInvalidInput, capability)
		}
		key := capability + "\x00" + providerID
		if seen[key] {
			return fmt.Errorf("%w: 能力 %q 的供应商 %q 重复提交", store.ErrInvalidInput, capability, providerID)
		}
		seen[key] = true
		provider, ok := service.Store().AIProvider(providerID)
		if !ok {
			return fmt.Errorf("%w: 供应商 %q 不存在", store.ErrInvalidInput, providerID)
		}
		if !provider.Enabled {
			return fmt.Errorf("%w: 供应商 %q 已禁用", store.ErrInvalidInput, providerID)
		}
		if provider.Kind != domain.AICapabilityKind(capability) {
			return fmt.Errorf("%w: 能力 %q 需要 %s 类型供应商，供应商 %q 是 %s 类型",
				store.ErrInvalidInput, capability, domain.AICapabilityKind(capability), providerID, provider.Kind)
		}
	}
	return nil
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
	status, code := errorStatusAndCode(err)
	message := err.Error()
	// 限额错误的内部文本只有维度/计数；面向访客的提示必须使用业务文案。
	if limitMessage := store.RateLimitMessage(err); limitMessage != "" {
		message = limitMessage
	}
	// 黑名单命中的错误文本只有 kind，不含被拉黑的值；统一使用配置文案。
	if errors.Is(err, store.ErrBlacklisted) {
		message = domain.DefaultBlacklistMessage
		if blacklistMessage := store.BlacklistMessage(err); blacklistMessage != "" {
			message = blacklistMessage
		}
	}
	writeJSON(w, status, map[string]any{"error": code, "message": message})
}

// errorStatusAndCode 把存储层错误映射为 HTTP 状态码与错误码，供 writeError 与需要
// 自定义 message 的接口（如接管冲突）共用。
func errorStatusAndCode(err error) (int, string) {
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
	case errors.Is(err, store.ErrRateLimited):
		status = http.StatusTooManyRequests
		code = "rate_limited"
	case errors.Is(err, store.ErrBlacklisted):
		// 黑名单命中按 403 拒绝（不复用 429：这是策略拒绝而非频率限制）。
		status = http.StatusForbidden
		code = "blacklisted"
	}
	return status, code
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
		if !ok {
			return false
		}
		// 只读围观允许查看消息（但不标记已读、不计入并发）。
		return conversation.AssignedAgentID == session.AccountID || service.IsWatching(session.AccountID, conversationID)
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

// formValueTruthy 解析 HTML 表单/布尔字段：on/true/1/yes 均视为真（大小写不敏感）。
// HTML checkbox 默认提交 "on"，不能只认 "true"/"1"。
func formValueTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes":
		return true
	default:
		return false
	}
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
