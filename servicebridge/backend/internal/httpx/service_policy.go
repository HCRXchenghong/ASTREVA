package httpx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/store"
)

// ===== 批次 4 第一波：客服策略 / 排队 / 黑名单 / 提醒渠道 / SMTP =====
//
// 本期只提供"存储 + 管理/客服接口 + 通知通道发送"，不接线 SLA 计时器、自动关闭、
// 优先级切换、接管/围观状态机（这些在下一波实现）。

// registerServicePolicyRoutes 注册批次 4 的全部新接口。
func registerServicePolicyRoutes(mux *http.ServeMux, service *app.Service, proxies trustedProxySet) {
	// ---------- 管理员：客服策略 ----------
	mux.HandleFunc("GET /api/admin/service-policy", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": service.Store().ServicePolicy()})
	})

	mux.HandleFunc("PATCH /api/admin/service-policy", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req servicePolicyPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		next := service.Store().ServicePolicy()
		if req.QueueEnabled != nil {
			next.QueueEnabled = *req.QueueEnabled
		}
		if req.MaxWaitSeconds != nil {
			next.MaxWaitSeconds = *req.MaxWaitSeconds
		}
		if req.SLAFirstResponseSeconds != nil {
			next.SLAFirstResponseSeconds = *req.SLAFirstResponseSeconds
		}
		if req.SLAIdleMinutes != nil {
			next.SLAIdleMinutes = *req.SLAIdleMinutes
		}
		if req.SLAIdleAction != nil {
			// 枚举写错时显式 400，避免"保存成功但实际被改回默认值"的困惑；
			// 存储层 Normalize 仍是最后一道防线（历史脏数据读取时回落）。
			if !domain.ValidSLAIdleAction(*req.SLAIdleAction) {
				writeError(w, fmt.Errorf("%w: sla_idle_action 只允许 ai 或 reassign", store.ErrInvalidInput))
				return
			}
			next.SLAIdleAction = domain.NormalizeSLAIdleAction(*req.SLAIdleAction)
		}
		if req.AutoCloseIdleMinutes != nil {
			next.AutoCloseIdleMinutes = *req.AutoCloseIdleMinutes
		}
		if req.AIKeywordPriority != nil {
			if !domain.ValidAIKeywordPriority(*req.AIKeywordPriority) {
				writeError(w, fmt.Errorf("%w: ai_keyword_priority 只允许 keyword_first、ai_first、keyword_only、ai_only", store.ErrInvalidInput))
				return
			}
			next.AIKeywordPriority = domain.NormalizeAIKeywordPriority(*req.AIKeywordPriority)
		}
		if req.UnattendedEnabled != nil {
			next.UnattendedEnabled = *req.UnattendedEnabled
		}
		if req.UnattendedReply != nil {
			next.UnattendedReply = *req.UnattendedReply
		}
		if req.TakeoverPopupEnabled != nil {
			next.TakeoverPopupEnabled = *req.TakeoverPopupEnabled
		}
		updated, err := service.Store().UpdateServicePolicy(next)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, proxies, "service_policy.update", "service_policy", "1", "更新客服策略")
		writeJSON(w, http.StatusOK, map[string]any{"policy": updated})
	})

	// ---------- 管理员：排队列表 ----------
	mux.HandleFunc("GET /api/admin/service-queue", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		entries := service.Store().QueueList()
		now := time.Now().UTC()
		views := make([]queuedConversationView, 0, len(entries))
		for _, entry := range entries {
			waitSeconds := int(now.Sub(entry.EnqueuedAt).Seconds())
			if waitSeconds < 0 {
				waitSeconds = 0
			}
			views = append(views, queuedConversationView{QueuedConversation: entry, WaitSeconds: waitSeconds})
		}
		writeJSON(w, http.StatusOK, map[string]any{"queue": views, "total": len(views)})
	})

	// ---------- 管理员：黑名单 ----------
	mux.HandleFunc("GET /api/admin/blacklist", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		if kind != "" && !domain.ValidBlacklistKind(kind) {
			writeError(w, fmt.Errorf("%w: kind 只允许 visitor、ip、account", store.ErrInvalidInput))
			return
		}
		entries := service.Store().BlacklistList(kind)
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": len(entries)})
	})

	mux.HandleFunc("POST /api/admin/blacklist", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req blacklistCreateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().UTC()) {
			writeError(w, fmt.Errorf("%w: expires_at 必须晚于当前时间", store.ErrInvalidInput))
			return
		}
		session, err := service.AuthFromRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		entry, err := service.Store().AddBlacklist(domain.BlacklistEntry{
			Kind:      req.Kind,
			Value:     req.Value,
			Reason:    req.Reason,
			CreatedBy: session.AccountID,
			ExpiresAt: req.ExpiresAt,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, proxies, "blacklist.add", "blacklist", entry.ID,
			"新增黑名单："+entry.Kind+"="+entry.Value)
		writeJSON(w, http.StatusCreated, map[string]any{"entry": entry})
	})

	mux.HandleFunc("DELETE /api/admin/blacklist/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		id := r.PathValue("id")
		if err := service.Store().DeleteBlacklist(id); err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, proxies, "blacklist.delete", "blacklist", id, "删除黑名单项")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// ---------- 管理员：客服提醒渠道 ----------
	mux.HandleFunc("GET /api/admin/agents/{id}/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		agentID := r.PathValue("id")
		if _, ok := findAgentByID(service.Store().AllAgents(), agentID); !ok {
			writeError(w, store.ErrNotFound)
			return
		}
		writeJSON(w, http.StatusOK, alertChannelsResponse(service.Store().AgentAlertChannels(agentID)))
	})

	mux.HandleFunc("POST /api/admin/agents/{id}/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		handleCreateAlertChannel(service, w, r, proxies, r.PathValue("id"))
	})

	mux.HandleFunc("PATCH /api/admin/agents/{id}/alert-channels/{channelId}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		handleUpdateAlertChannel(service, w, r, proxies, r.PathValue("id"), r.PathValue("channelId"))
	})

	mux.HandleFunc("DELETE /api/admin/agents/{id}/alert-channels/{channelId}", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		handleDeleteAlertChannel(service, w, r, proxies, r.PathValue("id"), r.PathValue("channelId"))
	})

	// ---------- 管理员：全局 SMTP 设置 ----------
	mux.HandleFunc("GET /api/admin/alert-smtp", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": service.Store().AlertSMTPSettings()})
	})

	mux.HandleFunc("PATCH /api/admin/alert-smtp", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		var req alertSMTPPatchRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		next := service.Store().AlertSMTPSettings()
		// 响应里的 PasswordMasked 绝不回写；密码只由 req.Password 控制。
		next.PasswordMasked = ""
		if req.Host != nil {
			next.Host = *req.Host
		}
		if req.Port != nil {
			next.Port = *req.Port
		}
		if req.Username != nil {
			next.Username = *req.Username
		}
		if req.FromAddress != nil {
			next.FromAddress = *req.FromAddress
		}
		if req.UseTLS != nil {
			next.UseTLS = *req.UseTLS
		}
		updated, err := service.Store().UpdateAlertSMTPSettings(next, req.Password)
		if err != nil {
			writeError(w, err)
			return
		}
		recordAuditFromRequest(service, r, proxies, "alert_smtp.update", "alert_smtp_settings", "1", "更新提醒邮件 SMTP 配置")
		writeJSON(w, http.StatusOK, map[string]any{"settings": updated})
	})

	// ---------- 管理员：提醒渠道测试发送（支持 {kind,config} 或 {id}）----------
	mux.HandleFunc("POST /api/admin/alert-channels/test", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(service, r) {
			writeError(w, store.ErrForbidden)
			return
		}
		handleAlertChannelTest(service, w, r, proxies, "")
	})

	// ---------- 客服：自己的提醒渠道（≤3，密钥只回掩码）----------
	mux.HandleFunc("GET /api/agent/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		session, ok := requireAgent(service, w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, alertChannelsResponse(service.Store().AgentAlertChannels(session.AccountID)))
	})

	mux.HandleFunc("POST /api/agent/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		session, ok := requireAgent(service, w, r)
		if !ok {
			return
		}
		handleCreateAlertChannel(service, w, r, proxies, session.AccountID)
	})

	mux.HandleFunc("PATCH /api/agent/alert-channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := requireAgent(service, w, r)
		if !ok {
			return
		}
		handleUpdateAlertChannel(service, w, r, proxies, session.AccountID, r.PathValue("id"))
	})

	mux.HandleFunc("DELETE /api/agent/alert-channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := requireAgent(service, w, r)
		if !ok {
			return
		}
		handleDeleteAlertChannel(service, w, r, proxies, session.AccountID, r.PathValue("id"))
	})

	mux.HandleFunc("POST /api/agent/alert-channels/test", func(w http.ResponseWriter, r *http.Request) {
		session, ok := requireAgent(service, w, r)
		if !ok {
			return
		}
		handleAlertChannelTest(service, w, r, proxies, session.AccountID)
	})
}

// ---------- 请求/响应结构 ----------

// servicePolicyPatchRequest 是客服策略局部更新请求：nil 表示不修改。
// 数值字段越界由 domain.ServicePolicy.Normalize 收敛（负数归零、超上限截断）。
type servicePolicyPatchRequest struct {
	QueueEnabled            *bool   `json:"queue_enabled"`
	MaxWaitSeconds          *int    `json:"max_wait_seconds"`
	SLAFirstResponseSeconds *int    `json:"sla_first_response_seconds"`
	SLAIdleMinutes          *int    `json:"sla_idle_minutes"`
	SLAIdleAction           *string `json:"sla_idle_action"`
	AutoCloseIdleMinutes    *int    `json:"auto_close_idle_minutes"`
	AIKeywordPriority       *string `json:"ai_keyword_priority"`
	UnattendedEnabled       *bool   `json:"unattended_enabled"`
	UnattendedReply         *string `json:"unattended_reply"`
	TakeoverPopupEnabled    *bool   `json:"takeover_popup_enabled"`
}

// queuedConversationView 在排队记录上补充"已等待秒数"，供后台展示。
type queuedConversationView struct {
	domain.QueuedConversation
	WaitSeconds int `json:"wait_seconds"`
}

// blacklistCreateRequest 是新增黑名单请求；expires_at 省略表示永不过期。
type blacklistCreateRequest struct {
	Kind      string     `json:"kind"`
	Value     string     `json:"value"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// alertChannelCreateRequest 是新增提醒渠道请求；enabled 省略时默认启用。
type alertChannelCreateRequest struct {
	Kind    string            `json:"kind"`
	Config  map[string]string `json:"config"`
	Enabled *bool             `json:"enabled"`
}

// alertChannelPatchRequest 是渠道局部更新请求：config 省略表示不改配置。
// config 为合并语义：未出现的键保留服务端已保存的值（编辑时前端不必回传密钥），
// 显式传空字符串表示清空该键，其余键覆盖。
type alertChannelPatchRequest struct {
	Enabled *bool              `json:"enabled"`
	Config  *map[string]string `json:"config"`
}

// alertChannelTestRequest 是测试发送请求，支持两种形式：
//   - {kind, config:{...}}：即时配置发送；
//   - {id:"<channel_id>"}：读取服务端已保存配置后发送（密钥不必回传前端）。
type alertChannelTestRequest struct {
	ID     string            `json:"id"`
	Kind   string            `json:"kind"`
	Config map[string]string `json:"config"`
}

// alertSMTPPatchRequest 是 SMTP 设置局部更新请求；password 省略=不改，""=清除。
type alertSMTPPatchRequest struct {
	Host        *string `json:"host"`
	Port        *int    `json:"port"`
	Username    *string `json:"username"`
	Password    *string `json:"password"`
	FromAddress *string `json:"from_address"`
	UseTLS      *bool   `json:"use_tls"`
}

// ---------- 处理函数 ----------

// requireAgent 要求当前会话是客服账号；管理员/访客访问返回 403。
func requireAgent(service *app.Service, w http.ResponseWriter, r *http.Request) (domain.AuthSession, bool) {
	session, err := service.AuthFromRequest(r)
	if err != nil {
		writeError(w, err)
		return domain.AuthSession{}, false
	}
	if session.Kind != domain.AccountAgent {
		writeError(w, store.ErrForbidden)
		return domain.AuthSession{}, false
	}
	return session, true
}

// alertChannelsResponse 统一渠道列表响应结构（ConfigMasked 已在存储层填充）。
func alertChannelsResponse(channels []domain.AgentAlertChannel) map[string]any {
	if channels == nil {
		channels = []domain.AgentAlertChannel{}
	}
	return map[string]any{
		"channels":     channels,
		"max_channels": domain.AlertChannelMaxPerAgent,
	}
}

// channelBelongsToAgent 校验渠道归属，防止客服/管理员跨账号操作。
func channelBelongsToAgent(service *app.Service, agentID, channelID string) bool {
	for _, channel := range service.Store().AgentAlertChannels(agentID) {
		if channel.ID == channelID {
			return true
		}
	}
	return false
}

// handleCreateAlertChannel 实现管理员与客服共用的"新增渠道"逻辑。
func handleCreateAlertChannel(service *app.Service, w http.ResponseWriter, r *http.Request, proxies trustedProxySet, agentID string) {
	var req alertChannelCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	channel, err := service.Store().CreateAgentAlertChannel(agentID, req.Kind, req.Config)
	if err != nil {
		writeError(w, err)
		return
	}
	if req.Enabled != nil && !*req.Enabled {
		channel, err = service.Store().UpdateAgentAlertChannel(channel.ID, domain.AgentAlertChannelPatch{Enabled: req.Enabled}, nil)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	recordAuditFromRequest(service, r, proxies, "alert_channel.create", "agent_alert_channel", channel.ID,
		"新增提醒渠道："+channel.Kind)
	writeJSON(w, http.StatusCreated, map[string]any{"channel": channel})
}

// handleUpdateAlertChannel 实现管理员与客服共用的"局部更新渠道"逻辑。
func handleUpdateAlertChannel(service *app.Service, w http.ResponseWriter, r *http.Request, proxies trustedProxySet, agentID, channelID string) {
	if !channelBelongsToAgent(service, agentID, channelID) {
		writeError(w, store.ErrNotFound)
		return
	}
	var req alertChannelPatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	channel, err := service.Store().UpdateAgentAlertChannel(channelID, domain.AgentAlertChannelPatch{Enabled: req.Enabled}, req.Config)
	if err != nil {
		writeError(w, err)
		return
	}
	recordAuditFromRequest(service, r, proxies, "alert_channel.update", "agent_alert_channel", channel.ID,
		"更新提醒渠道："+channel.Kind)
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel})
}

// handleDeleteAlertChannel 实现管理员与客服共用的"删除渠道"逻辑。
func handleDeleteAlertChannel(service *app.Service, w http.ResponseWriter, r *http.Request, proxies trustedProxySet, agentID, channelID string) {
	if !channelBelongsToAgent(service, agentID, channelID) {
		writeError(w, store.ErrNotFound)
		return
	}
	if err := service.Store().DeleteAgentAlertChannel(channelID); err != nil {
		writeError(w, err)
		return
	}
	recordAuditFromRequest(service, r, proxies, "alert_channel.delete", "agent_alert_channel", channelID, "删除提醒渠道")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAlertChannelTest 发送一条测试消息，支持两种入参：
//   - {kind, config:{...}}：用即时配置发送；
//   - {id:"<channel_id>"}：读取服务端已保存配置后发送（密钥不必回传前端）。
//
// agentID 非空时校验渠道归属（客服只能测试自己的渠道）；配置不全 400，
// 发送失败仍返回 200 + {ok:false,message}，便于前端展示失败原因。
func handleAlertChannelTest(service *app.Service, w http.ResponseWriter, r *http.Request, proxies trustedProxySet, agentID string) {
	var req alertChannelTestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var channel domain.AgentAlertChannel
	if channelID := strings.TrimSpace(req.ID); channelID != "" {
		found, ok := service.Store().AgentAlertChannel(channelID)
		if !ok || (agentID != "" && found.AgentID != agentID) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":   "not_found",
				"message": "提醒渠道不存在或不属于当前账号",
			})
			return
		}
		if len(found.Config) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_input",
				"message": "该提醒渠道的配置不可用，请重新保存配置后再试",
			})
			return
		}
		channel = found
	} else {
		kind := strings.ToLower(strings.TrimSpace(req.Kind))
		if !domain.ValidAlertChannelKind(kind) {
			writeError(w, fmt.Errorf("%w: kind 只允许 telegram、bark、email", store.ErrInvalidInput))
			return
		}
		normalized, err := domain.NormalizeAlertChannelConfig(kind, req.Config)
		if err != nil {
			writeError(w, fmt.Errorf("%w: %s", store.ErrInvalidInput, err))
			return
		}
		channel = domain.AgentAlertChannel{Kind: kind, Config: normalized, Enabled: true}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	payload := notify.AlertPayload{
		Title: "Astreva 提醒渠道测试",
		Body:  "这是一条测试消息，收到即表示渠道配置可用。",
	}
	if err := service.Notifier().SendAlertChannels(ctx, []domain.AgentAlertChannel{channel}, payload); err != nil {
		recordAuditFromRequest(service, r, proxies, "alert_channel.test_failed", "agent_alert_channel", channel.Kind,
			"提醒渠道测试发送失败")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	recordAuditFromRequest(service, r, proxies, "alert_channel.test", "agent_alert_channel", channel.Kind, "发送提醒渠道测试消息")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "测试消息已发送"})
}
