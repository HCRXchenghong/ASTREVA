package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
)

var _ Store = (*PostgresStore)(nil)

const (
	defaultDBTimeout = 5 * time.Second

	adminColumns        = `id, account, name, password_hash, created_at`
	agentColumns        = `id, account, name, group_name, password_hash, status, COALESCE(device_token, ''), COALESCE(device_platform, ''), max_conversations, current_conversations, disabled_at, created_at, updated_at, avatar_url, manual_satisfaction_rate, COALESCE(preferences::text, '{}')`
	conversationColumns = `id, visitor_id, visitor_ip::text, visitor_ip_source, visitor_remark, remark_updated_by, remark_updated_at, status, COALESCE(assigned_agent_id, ''), source, visitor_lang, COALESCE(detected_lang, ''), COALESCE(reply_lang_override, ''), reply_lang_override_at, COALESCE(language_stats::text, '{}'), last_message, last_message_at, unread_for_agent, unread_for_visitor, first_response_at, last_agent_reply_at, COALESCE(sla_state, ''), COALESCE(queue_group, ''), unattended_notified_at, created_at, updated_at`
	messageColumns      = `server_msg_id, client_msg_id, conversation_id, sender_type, sender_id, message_type, content, COALESCE(translations::text, '{}'), created_at, revoked_at, revoked_by_kind, revoked_by_id, COALESCE(detected_lang, ''), COALESCE(author_lang, ''), COALESCE(translation_status, ''), COALESCE(knowledge_entry_ids::text, '[]')`
	ratingColumns       = `id, conversation_id, visitor_id, COALESCE(assigned_agent_id, ''), score, tags, comment, created_at`
	auditColumns        = `id, actor_kind, actor_id, action, resource, resource_id, ip_address, user_agent, description, created_at`
	aiProviderColumns   = `id, name, kind, api_type, base_url, api_key_ciphertext, COALESCE(models::text, '[]'), default_model, timeout_ms, allowed_hosts, enabled, sort_order, created_at, updated_at`
	aiBindingColumns    = `capability, provider_id, COALESCE(model, ''), COALESCE(params::text, '{}'), updated_at`
)

type PostgresStore struct {
	pool    *pgxpool.Pool
	options Options
}

type postgresOptions struct {
	MaxConns int32
}

type dbTx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type scanTarget interface {
	Scan(...any) error
}

func NewPostgresStore(ctx context.Context, databaseURL string, options Options) (*PostgresStore, error) {
	return newPostgresStore(ctx, databaseURL, options, postgresOptions{MaxConns: 64})
}

func newPostgresStore(ctx context.Context, databaseURL string, options Options, opts postgresOptions) (*PostgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database url is empty")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if cfg.MinConns == 0 && cfg.MaxConns >= 8 {
		cfg.MinConns = 4
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &PostgresStore{pool: pool, options: options}
	if err := s.ensureCoreTables(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := s.ensureServiceRatingsTable(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := s.seedDefaults(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresStore) ensureCoreTables(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admin_users (
			id TEXT PRIMARY KEY, account TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			password_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY, account TEXT NOT NULL UNIQUE, name TEXT NOT NULL, group_name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'offline', max_conversations INTEGER NOT NULL DEFAULT 10,
			current_conversations INTEGER NOT NULL DEFAULT 0, disabled_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), CONSTRAINT agents_status_check CHECK (status IN ('online','busy','offline')),
			CONSTRAINT agents_max_conversations_check CHECK (max_conversations >= 0),
			CONSTRAINT agents_current_conversations_check CHECK (current_conversations >= 0)
		)`,
		`CREATE TABLE IF NOT EXISTS visitors (
			id TEXT PRIMARY KEY, ip INET NOT NULL, remark TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'web',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY, visitor_id TEXT NOT NULL REFERENCES visitors(id), visitor_ip INET NOT NULL, visitor_remark TEXT NOT NULL,
			remark_updated_by TEXT NOT NULL DEFAULT '', remark_updated_at TIMESTAMPTZ, status TEXT NOT NULL,
			assigned_agent_id TEXT REFERENCES agents(id), source TEXT NOT NULL DEFAULT 'web', last_message TEXT NOT NULL DEFAULT '',
			last_message_at TIMESTAMPTZ, unread_for_agent INTEGER NOT NULL DEFAULT 0, unread_for_visitor INTEGER NOT NULL DEFAULT 0,
			closed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT conversations_status_check CHECK (status IN ('waiting','assigned','ai_serving','human_requested','closed')),
			CONSTRAINT conversations_unread_agent_check CHECK (unread_for_agent >= 0),
			CONSTRAINT conversations_unread_visitor_check CHECK (unread_for_visitor >= 0)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_assigned_agent ON conversations(assigned_agent_id) WHERE assigned_agent_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_status_updated ON conversations(status, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_visitor ON conversations(visitor_id)`,
		`CREATE TABLE IF NOT EXISTS messages (
			server_msg_id TEXT PRIMARY KEY, client_msg_id TEXT NOT NULL DEFAULT '', conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
			sender_type TEXT NOT NULL, sender_id TEXT NOT NULL DEFAULT '', message_type TEXT NOT NULL, content TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), revoked_at TIMESTAMPTZ, revoked_by_kind TEXT NOT NULL DEFAULT '', revoked_by_id TEXT NOT NULL DEFAULT '',
			CONSTRAINT messages_sender_type_check CHECK (sender_type IN ('visitor','agent','ai','system')),
			CONSTRAINT messages_message_type_check CHECK (message_type IN ('text','emoji','image','audio','contact_phone','contact_wechat','contact_telephone','contact_whatsapp','contact_telegram','system','ai_text','handoff_request','revoked'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation_created ON messages(conversation_id, created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_client_dedup ON messages(conversation_id, sender_type, sender_id, client_msg_id) WHERE client_msg_id <> ''`,
		`CREATE TABLE IF NOT EXISTS keyword_rules (
			id TEXT PRIMARY KEY, keyword TEXT NOT NULL, match_type TEXT NOT NULL DEFAULT 'contains', reply TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true, priority INTEGER NOT NULL DEFAULT 0, action TEXT NOT NULL DEFAULT 'text',
			show_in_quick_replies BOOLEAN NOT NULL DEFAULT false, quick_reply_text TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT keyword_rules_match_type_check CHECK (match_type IN ('contains','exact','prefix')),
			CONSTRAINT keyword_rules_action_check CHECK (action IN ('text','telephone','whatsapp','telegram','handoff'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_keyword_rules_enabled_priority ON keyword_rules(enabled, priority DESC)`,
		`CREATE TABLE IF NOT EXISTS contact_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1, phone TEXT NOT NULL DEFAULT '', wechat TEXT NOT NULL DEFAULT '',
			wechat_reply_type TEXT NOT NULL DEFAULT 'text', wechat_image_url TEXT NOT NULL DEFAULT '', qq TEXT NOT NULL DEFAULT '',
			qq_reply_type TEXT NOT NULL DEFAULT 'text', qq_image_url TEXT NOT NULL DEFAULT '', entry_reply TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), CONSTRAINT contact_settings_singleton CHECK (id=1),
			CONSTRAINT contact_settings_wechat_reply_type_check CHECK (wechat_reply_type IN ('text','image')),
			CONSTRAINT contact_settings_qq_reply_type_check CHECK (qq_reply_type IN ('text','image'))
		)`,
		`CREATE TABLE IF NOT EXISTS ai_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1, enabled BOOLEAN NOT NULL DEFAULT true, mode TEXT NOT NULL DEFAULT 'human_first',
			base_url TEXT NOT NULL DEFAULT 'https://api.openai.com/v1', api_key_ciphertext TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT 'gpt-4o-mini',
			api_type TEXT NOT NULL DEFAULT 'chat_completions', temperature NUMERIC(3,2) NOT NULL DEFAULT 0.70,
			max_output_tokens INTEGER NOT NULL DEFAULT 512, timeout_seconds INTEGER NOT NULL DEFAULT 20, system_prompt TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_settings_singleton CHECK (id=1), CONSTRAINT ai_settings_mode_check CHECK (mode IN ('human_first','always_ai','manual_only')),
			CONSTRAINT ai_settings_api_type_check CHECK (api_type IN ('chat_completions','responses','anthropic_messages')),
			CONSTRAINT ai_settings_temperature_check CHECK (temperature >= 0 AND temperature <= 2)
		)`,
		`CREATE TABLE IF NOT EXISTS ai_providers (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'text',
			api_type TEXT NOT NULL DEFAULT 'chat_completions', base_url TEXT NOT NULL DEFAULT '',
			api_key_ciphertext TEXT NOT NULL DEFAULT '', models JSONB NOT NULL DEFAULT '[]'::jsonb,
			default_model TEXT NOT NULL DEFAULT '', timeout_ms INTEGER NOT NULL DEFAULT 30000,
			allowed_hosts TEXT NOT NULL DEFAULT '', enabled BOOLEAN NOT NULL DEFAULT true,
			sort_order INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_providers_kind_check CHECK (kind IN ('text','image')),
			CONSTRAINT ai_providers_api_type_check CHECK (api_type IN ('chat_completions','responses','anthropic_messages','images'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_providers_enabled_sort ON ai_providers(enabled, sort_order, created_at)`,
		// 能力绑定：provider_id 允许为空串（未绑定），因此不建外键；
		// 删除供应商时由 DeleteAIProvider 删除引用，索引用于清理与排查。
		`CREATE TABLE IF NOT EXISTS ai_bindings (
			capability TEXT PRIMARY KEY, provider_id TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
			params JSONB NOT NULL DEFAULT '{}'::jsonb, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_bindings_capability_check CHECK (capability IN ('reception','translation','detection','seo','image'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_bindings_provider ON ai_bindings(provider_id)`,
		// ===== AI 知识库（多库、两种用途、库外拒答）=====
		`CREATE TABLE IF NOT EXISTS ai_knowledge_bases (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true, sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_enabled_sort ON ai_knowledge_bases(enabled, sort_order, created_at)`,
		`CREATE TABLE IF NOT EXISTS ai_knowledge_entries (
			id TEXT PRIMARY KEY, base_id TEXT NOT NULL REFERENCES ai_knowledge_bases(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
			category TEXT NOT NULL DEFAULT '', priority INTEGER NOT NULL DEFAULT 0, enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_knowledge_entries_base ON ai_knowledge_entries(base_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_knowledge_entries_enabled ON ai_knowledge_entries(enabled)`,
		// 库外行为配置：单例表，未写入时由 KnowledgeSettings() 返回默认值。
		`CREATE TABLE IF NOT EXISTS ai_knowledge_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1, unknown_action TEXT NOT NULL DEFAULT 'handoff',
			refuse_reply TEXT NOT NULL DEFAULT '', score_threshold NUMERIC(4,3) NOT NULL DEFAULT 0.35,
			max_entries INTEGER NOT NULL DEFAULT 3, enabled BOOLEAN NOT NULL DEFAULT true,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_knowledge_settings_singleton CHECK (id=1),
			CONSTRAINT ai_knowledge_settings_unknown_action_check CHECK (unknown_action IN ('refuse','handoff'))
		)`,
		// AI 频率/次数/单次字数限额：单例表，未写入时由 AILimits() 返回 DefaultAILimits()。
		`CREATE TABLE IF NOT EXISTS ai_limits (
			id SMALLINT PRIMARY KEY DEFAULT 1, enabled BOOLEAN NOT NULL DEFAULT true,
			visitor_messages_per_minute INTEGER NOT NULL DEFAULT 20,
			visitor_messages_per_day INTEGER NOT NULL DEFAULT 300,
			ai_calls_per_conversation_day INTEGER NOT NULL DEFAULT 60,
			ai_reply_max_chars INTEGER NOT NULL DEFAULT 1200,
			exceed_message TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_limits_singleton CHECK (id=1)
		)`,
		// ===== 客服策略（批次 4）：本期只建表/补列 + 管理接口，不接线计时器 =====
		// 客服策略单例：未写入时由 ServicePolicy() 返回 DefaultServicePolicy()。
		`CREATE TABLE IF NOT EXISTS service_policy (
			id SMALLINT PRIMARY KEY DEFAULT 1, queue_enabled BOOLEAN NOT NULL DEFAULT true,
			max_wait_seconds INTEGER NOT NULL DEFAULT 600,
			sla_first_response_seconds INTEGER NOT NULL DEFAULT 60,
			sla_idle_minutes INTEGER NOT NULL DEFAULT 5,
			sla_idle_action TEXT NOT NULL DEFAULT 'ai',
			auto_close_idle_minutes INTEGER NOT NULL DEFAULT 30,
			ai_keyword_priority TEXT NOT NULL DEFAULT 'keyword_first',
			unattended_enabled BOOLEAN NOT NULL DEFAULT true,
			unattended_reply TEXT NOT NULL DEFAULT '',
			takeover_popup_enabled BOOLEAN NOT NULL DEFAULT true,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT service_policy_singleton CHECK (id=1),
			CONSTRAINT service_policy_sla_idle_action_check CHECK (sla_idle_action IN ('ai','reassign')),
			CONSTRAINT service_policy_ai_keyword_priority_check CHECK (ai_keyword_priority IN ('keyword_first','ai_first','keyword_only','ai_only'))
		)`,
		// 排队队列：会话删除时级联清理，避免悬挂排队记录。
		`CREATE TABLE IF NOT EXISTS conversation_queue (
			conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
			group_name TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			reason TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_queue_order ON conversation_queue(priority DESC, enqueued_at ASC)`,
		`CREATE TABLE IF NOT EXISTS service_blacklist (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, value TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '', created_by TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (kind, value),
			CONSTRAINT service_blacklist_kind_check CHECK (kind IN ('visitor','ip','account'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_blacklist_kind ON service_blacklist(kind, created_at DESC)`,
		// 提醒渠道：密钥（token/key/address）整体 JSON 后 AES-256-GCM 加密入库；客服删除时级联清理。
		`CREATE TABLE IF NOT EXISTS agent_alert_channels (
			id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			kind TEXT NOT NULL, config_ciphertext TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT agent_alert_channels_kind_check CHECK (kind IN ('telegram','bark','email'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_alert_channels_agent ON agent_alert_channels(agent_id)`,
		// 全局 SMTP 发信设置（email 渠道使用）：密码密文入库，接口只回掩码。
		`CREATE TABLE IF NOT EXISTS alert_smtp_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1, host TEXT NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 587, username TEXT NOT NULL DEFAULT '',
			password_ciphertext TEXT NOT NULL DEFAULT '', from_address TEXT NOT NULL DEFAULT '',
			use_tls BOOLEAN NOT NULL DEFAULT false, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT alert_smtp_settings_singleton CHECK (id=1),
			CONSTRAINT alert_smtp_settings_port_check CHECK (port BETWEEN 1 AND 65535)
		)`,
		`CREATE TABLE IF NOT EXISTS business_hours (
			id SMALLINT PRIMARY KEY DEFAULT 1, timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai', start_time TEXT NOT NULL DEFAULT '09:00',
			end_time TEXT NOT NULL DEFAULT '18:00', enabled BOOLEAN NOT NULL DEFAULT true,
			offline_reply TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT business_hours_singleton CHECK (id=1)
		)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			token TEXT PRIMARY KEY, kind TEXT NOT NULL, account_id TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at TIMESTAMPTZ NOT NULL, revoked_at TIMESTAMPTZ,
			CONSTRAINT auth_sessions_kind_check CHECK (kind IN ('admin','agent','visitor'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_account ON auth_sessions(kind, account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at)`,
		// ===== 二期列补齐：既有数据库在启动时自动升级，缺失列会导致会话/消息查询直接报错 =====
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS detected_lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS reply_lang_override TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS reply_lang_override_at TIMESTAMPTZ`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS language_stats JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS detected_lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS author_lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS translation_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS knowledge_entry_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}'::jsonb`,
		// 批次 4：SLA 计时器/排队分组所需列（本期只落库，不参与会话流转）。
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS first_response_at TIMESTAMPTZ`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS last_agent_reply_at TIMESTAMPTZ`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS sla_state TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS queue_group TEXT NOT NULL DEFAULT ''`,
		// 批次 4 第二波：无人值守兜底话术的发送时间（非空表示本会话已发过，保证只发一次）。
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS unattended_notified_at TIMESTAMPTZ`,
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) LoginAdmin(account, password string) (domain.AuthSession, domain.AdminUser, error) {
	ctx, cancel := dbContext()
	defer cancel()

	admin, err := s.adminByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuthSession{}, domain.AdminUser{}, ErrInvalidCredentials
		}
		return domain.AuthSession{}, domain.AdminUser{}, err
	}
	if !verifyPassword(admin.Password, password) {
		return domain.AuthSession{}, domain.AdminUser{}, ErrInvalidCredentials
	}
	session, err := s.createAuth(ctx, s.pool, domain.AccountAdmin, admin.ID, 24*time.Hour)
	if err != nil {
		return domain.AuthSession{}, domain.AdminUser{}, err
	}
	return session, admin, nil
}

func (s *PostgresStore) LoginAgent(account, password string) (domain.AuthSession, domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agent, err := s.agentByAccount(ctx, account)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuthSession{}, domain.Agent{}, ErrInvalidCredentials
		}
		return domain.AuthSession{}, domain.Agent{}, err
	}
	if agent.DisabledAt != nil {
		return domain.AuthSession{}, domain.Agent{}, ErrForbidden
	}
	if !verifyPassword(agent.Password, password) {
		return domain.AuthSession{}, domain.Agent{}, ErrInvalidCredentials
	}
	session, err := s.createAuth(ctx, s.pool, domain.AccountAgent, agent.ID, 24*time.Hour)
	if err != nil {
		return domain.AuthSession{}, domain.Agent{}, err
	}
	return session, agent, nil
}

func (s *PostgresStore) Auth(token string) (domain.AuthSession, bool) {
	session, ok := s.auth(token, false)
	return session, ok
}

func (s *PostgresStore) AuthVisitor(token string) (domain.AuthSession, bool) {
	session, ok := s.auth(token, true)
	return session, ok
}

func (s *PostgresStore) CreateVisitorConversation(ip, source, lang, ipSource, remark string) (domain.VisitorSession, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.VisitorSession
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		now := dbNow()
		visitorID := "vis_" + randomID(8)
		conversationID := "conv_" + randomID(10)
		ip = ipForDB(ip)
		source = strings.TrimSpace(source)
		if source == "" {
			source = "web"
		}
		if _, err := cleanRequiredText(source, maxSourceRunes); err != nil {
			return err
		}
		remark = strings.TrimSpace(remark)
		if remark == "" {
			remark = ip
		} else if _, err := cleanRequiredText(remark, maxRemarkRunes); err != nil {
			return err
		}

		visitor := domain.Visitor{
			ID:        visitorID,
			IP:        ip,
			Remark:    remark,
			Source:    source,
			CreatedAt: now,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO visitors (id, ip, remark, source, created_at)
			VALUES ($1, $2::inet, $3, $4, $5)
		`, visitor.ID, visitor.IP, visitor.Remark, visitor.Source, visitor.CreatedAt); err != nil {
			return err
		}

		conversation := domain.Conversation{
			ID:              conversationID,
			VisitorID:       visitorID,
			VisitorIP:       ip,
			VisitorIPSource: strings.TrimSpace(ipSource),
			VisitorRemark:   remark,
			Status:          domain.ConversationWaiting,
			Source:          source,
			VisitorLang:     ai.NormalizeLang(lang, ai.LangZh),
			LastMessage:     "新会话",
			LastMessageAt:   &now,
			UnreadForAgent:  0,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversations (
				id, visitor_id, visitor_ip, visitor_ip_source, visitor_remark, status, source, visitor_lang,
				last_message, last_message_at, unread_for_agent, unread_for_visitor, created_at, updated_at
			)
			VALUES ($1, $2, $3::inet, $4, $5, $6, $7, $8, $9, $10, 0, 0, $11, $12)
		`, conversation.ID, conversation.VisitorID, conversation.VisitorIP, conversation.VisitorIPSource, conversation.VisitorRemark, conversation.Status, conversation.Source, conversation.VisitorLang, conversation.LastMessage, conversation.LastMessageAt, conversation.CreatedAt, conversation.UpdatedAt); err != nil {
			return err
		}

		if err := s.assignConversationTx(ctx, tx, &conversation); err != nil {
			return err
		}

		initialMessages := []domain.Message{}
		contacts, err := s.contactSettings(ctx, tx)
		if err != nil {
			return err
		}
		if entryReply := strings.TrimSpace(contacts.EntryReply); entryReply != "" {
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderAI, "ai", domain.MessageAIText, entryReply, nil)
			if err != nil {
				return err
			}
			initialMessages = append(initialMessages, msg)
		}

		session, err := s.createAuth(ctx, tx, domain.AccountVisitor, conversation.ID, 30*24*time.Hour)
		if err != nil {
			return err
		}
		out = domain.VisitorSession{
			Token:           session.Token,
			Visitor:         visitor,
			Conversation:    conversation,
			InitialMessages: initialMessages,
		}
		return nil
	})
	return out, err
}

// UpdateVisitorLang 更新会话的访客语言（访客切换网站语言后由前端同步），
// 后续 AI 回复与双向翻译都会按新语言处理。
func (s *PostgresStore) UpdateVisitorLang(conversationID, lang string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()
	row := s.pool.QueryRow(ctx, `
		UPDATE conversations SET visitor_lang=$2, updated_at=now()
		WHERE id=$1
		RETURNING `+conversationColumns, conversationID, lang)
	return scanConversation(row)
}

func (s *PostgresStore) SetAgentStatus(agentID string, status domain.AgentStatus) (domain.Agent, []domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if err := validateAgentStatus(status); err != nil {
		return domain.Agent{}, nil, err
	}
	var out domain.Agent
	var assigned []domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, agentID)
		if err != nil {
			return mapNoRows(err)
		}
		if agent.DisabledAt != nil && status == domain.AgentOnline {
			return ErrForbidden
		}
		now := dbNow()
		agent.Status = status
		agent.Updated = now
		if _, err := tx.Exec(ctx, `UPDATE agents SET status=$2, updated_at=$3 WHERE id=$1`, agent.ID, agent.Status, agent.Updated); err != nil {
			return err
		}

		if status == domain.AgentOffline {
			agent.CurrentConversations = 0
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=0, updated_at=$2 WHERE id=$1`, agent.ID, now); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT `+conversationColumns+`
				FROM conversations
				WHERE assigned_agent_id=$1 AND status <> $2
				ORDER BY created_at
				FOR UPDATE
			`, agent.ID, domain.ConversationClosed)
			if err != nil {
				return err
			}
			conversations, err := scanConversations(rows)
			rows.Close()
			if err != nil {
				return err
			}
			for _, conversation := range conversations {
				conversation.AssignedAgentID = ""
				// 人工已实际接待过的会话才需要重新排队；AI 接待中的会话保持原状态。
				if conversation.Status == domain.ConversationAssigned {
					conversation.Status = domain.ConversationHumanRequested
				}
				conversation.UpdatedAt = now
				if _, err := tx.Exec(ctx, `
					UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
				`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
					return err
				}
				if err := s.assignHumanConversationTx(ctx, tx, &conversation); err != nil {
					return err
				}
				assigned = append(assigned, conversation)
			}
		}

		if status == domain.AgentOnline {
			business, err := s.businessHoursTx(ctx, tx)
			if err != nil {
				return err
			}
			// 非营业时间不自动接单：客服仍可正常上线，只是不默认领取等待中的会话。
			for businessOpen(business, now) && agent.CurrentConversations < agent.MaxConversations {
				conversation, err := s.nextWaitingConversationForUpdateTx(ctx, tx)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						break
					}
					return err
				}
				agent.CurrentConversations++
				conversation.AssignedAgentID = agent.ID
				conversation.AssignedAgentName = agent.Name
				conversation.AssignedAgentAvatar = agent.AvatarURL
				conversation.UpdatedAt = now
				// 静默分配：不改变会话状态，客服实际回复后才对外显示人工服务（无痕转人工）。
				if _, err := tx.Exec(ctx, `
					UPDATE conversations SET assigned_agent_id=$2, updated_at=$3 WHERE id=$1
				`, conversation.ID, conversation.AssignedAgentID, conversation.UpdatedAt); err != nil {
					return err
				}
				assigned = append(assigned, conversation)
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=$2, updated_at=$3 WHERE id=$1`, agent.ID, agent.CurrentConversations, now); err != nil {
				return err
			}
		}
		out = agent
		return nil
	})
	return out, assigned, err
}

func (s *PostgresStore) ConversationsForAgent(agentID string) ([]domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if _, err := s.agentByID(ctx, agentID); err != nil {
		return nil, mapNoRows(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE assigned_agent_id=$1 ORDER BY updated_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	conversations, err := scanConversations(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := s.fillConversationAgents(ctx, s.pool, conversations); err != nil {
		return nil, err
	}
	return conversations, nil
}

func (s *PostgresStore) AllConversations() []domain.Conversation {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	conversations, err := scanConversations(rows)
	rows.Close()
	if err != nil {
		return nil
	}
	if err := s.fillConversationAgents(ctx, s.pool, conversations); err != nil {
		return nil
	}
	return conversations
}

func (s *PostgresStore) AllAgents() []domain.Agent {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+agentColumns+` FROM agents ORDER BY group_name, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	agents, err := scanAgents(rows)
	if err != nil {
		return nil
	}
	return agents
}

func (s *PostgresStore) Conversation(id string) (domain.Conversation, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	conversation, err := s.conversationByID(ctx, id)
	return conversation, err == nil
}

func (s *PostgresStore) Messages(conversationID string) []domain.Message {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+messageColumns+` FROM messages WHERE conversation_id=$1 ORDER BY created_at`, conversationID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return nil
	}
	return messages
}

func (s *PostgresStore) PagedMessages(conversationID string, limit int, beforeServerMsgID string) MessagePage {
	ctx, cancel := dbContext()
	defer cancel()

	if limit <= 0 || limit > 100 {
		limit = 50
	}
	beforeServerMsgID = strings.TrimSpace(beforeServerMsgID)
	// 注意：外层必须用 page.*，不能重复列出 messageColumns ——
	// 子查询的输出列名是表达式名（如 coalesce），外层再引用 translations/detected_lang
	// 这类原始列名会报 "column does not exist"，而这里历史上会吞掉错误返回空页。
	rows, err := s.pool.Query(ctx, `
		SELECT page.*
		FROM (
			SELECT `+messageColumns+`
			FROM messages
			WHERE conversation_id=$1
				AND (
					$2 = ''
					OR (created_at, server_msg_id) < (
						SELECT created_at, server_msg_id
						FROM messages
						WHERE conversation_id=$1 AND server_msg_id=$2
					)
				)
			ORDER BY created_at DESC, server_msg_id DESC
			LIMIT $3
		) page
		ORDER BY page.created_at ASC, page.server_msg_id ASC
	`, conversationID, beforeServerMsgID, limit+1)
	if err != nil {
		return MessagePage{}
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return MessagePage{}
	}
	page := MessagePage{Messages: messages}
	if len(messages) > limit {
		page.HasMore = true
		page.Messages = messages[1:]
	}
	if page.HasMore && len(page.Messages) > 0 {
		page.NextBefore = page.Messages[0].ServerMsgID
	}
	return page
}

func (s *PostgresStore) MarkConversationRead(actor domain.AuthSession, conversationID string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		if conversation.UnreadForAgent == 0 {
			out = conversation
			return nil
		}
		row := tx.QueryRow(ctx, `UPDATE conversations SET unread_for_agent=0 WHERE id=$1 RETURNING `+conversationColumns, conversationID)
		out, err = scanConversation(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) UpdateRemark(actor domain.AuthSession, conversationID, remark string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		remark = strings.TrimSpace(remark)
		if remark == "" {
			remark = conversation.VisitorIP
		}
		now := dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET visitor_remark=$2, remark_updated_by=$3, remark_updated_at=$4, updated_at=$4
			WHERE id=$1
			RETURNING `+conversationColumns, conversationID, remark, actor.AccountID, now)
		updated, err := scanConversation(row)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE visitors SET remark=$2 WHERE id=$1`, updated.VisitorID, updated.VisitorRemark); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *PostgresStore) AddVisitorMessage(conversationID, clientMsgID, content string, messageType domain.MessageType, translations map[string]string) (MessageResult, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out MessageResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if err := validateClientMsgID(clientMsgID); err != nil {
			return err
		}
		if existing, ok, err := s.findMessageByClientIDTx(ctx, tx, conversationID, domain.SenderVisitor, conversation.VisitorID, clientMsgID); err != nil {
			return err
		} else if ok {
			out = MessageResult{Input: existing, Conversation: conversation}
			return nil
		}
		if err := validateUserMessage(messageType, content); err != nil {
			return err
		}

		input, err := s.createMessageTx(ctx, tx, conversationID, clientMsgID, domain.SenderVisitor, conversation.VisitorID, messageType, content, translations)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, agentPreviewText(messageType, content, translations), true, input.CreatedAt); err != nil {
			return err
		}
		generated, needAI, err := s.generateAutoRepliesTx(ctx, tx, &conversation, messageType, content)
		if err != nil {
			return err
		}
		latest, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		out = MessageResult{
			Input:                input,
			Generated:            generated,
			Conversation:         latest,
			NeedAI:               needAI,
			AIText:               content,
			VisitorMessageSentAt: input.CreatedAt,
		}
		return nil
	})
	return out, err
}

func (s *PostgresStore) AddAIMessage(conversationID, content string, translations map[string]string) (domain.Message, domain.Conversation, error) {
	return s.AddAIMessageWithKnowledge(conversationID, content, translations, nil)
}

// AddAIMessageWithKnowledge 落库一条 AI 消息，并记录本次回答依据的知识条目 ID。
func (s *PostgresStore) AddAIMessageWithKnowledge(conversationID, content string, translations map[string]string, knowledgeEntryIDs []string) (domain.Message, domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var msg domain.Message
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		// 人工客服实际回复后（状态 assigned）AI 静默撤出；排队等待人工期间 AI 继续服务。
		if conversation.Status != domain.ConversationAIServing && conversation.Status != domain.ConversationHumanRequested {
			return ErrConflict
		}
		if conversation.Status == domain.ConversationWaiting {
			conversation.Status = domain.ConversationAIServing
			if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
				return err
			}
		}
		msg, err = s.createMessageWithKnowledgeTx(ctx, tx, conversationID, "", domain.SenderAI, "ai", domain.MessageAIText, content, translations, knowledgeEntryIDs)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, agentPreviewText(domain.MessageAIText, content, translations), false, msg.CreatedAt); err != nil {
			return err
		}
		out, err = s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		return err
	})
	return msg, out, err
}

func (s *PostgresStore) AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType, translations map[string]string) (MessageResult, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out MessageResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.AssignedAgentID != agentID {
			return ErrForbidden
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if err := validateClientMsgID(clientMsgID); err != nil {
			return err
		}
		if existing, ok, err := s.findMessageByClientIDTx(ctx, tx, conversationID, domain.SenderAgent, agentID, clientMsgID); err != nil {
			return err
		} else if ok {
			out = MessageResult{Input: existing, Conversation: conversation}
			return nil
		}
		if err := validateUserMessage(messageType, content); err != nil {
			return err
		}
		// 客服首次真实回复即视为接管：状态变为 assigned，AI 随后静默撤出（无痕转人工）。
		if conversation.Status == domain.ConversationAIServing || conversation.Status == domain.ConversationHumanRequested || conversation.Status == domain.ConversationWaiting {
			conversation.Status = domain.ConversationAssigned
			if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
				return err
			}
		}
		input, err := s.createMessageTx(ctx, tx, conversationID, clientMsgID, domain.SenderAgent, agentID, messageType, content, translations)
		if err != nil {
			return err
		}
		if err := s.touchConversationTx(ctx, tx, &conversation, conversationPreviewText(messageType, content), false, input.CreatedAt); err != nil {
			return err
		}
		// SLA 计时：首次真实回复写入 first_response_at，每次回复更新 last_agent_reply_at，
		// 并清空 sla_state（该会话已发生的超时告警重新计时）。
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET first_response_at=COALESCE(first_response_at, $2), last_agent_reply_at=$2, sla_state=''
			WHERE id=$1
		`, conversationID, input.CreatedAt); err != nil {
			return err
		}
		latest, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return err
		}
		out = MessageResult{Input: input, Conversation: latest}
		return nil
	})
	return out, err
}

// SetMessageLanguageMeta 记录消息语种与翻译状态。
func (s *PostgresStore) SetMessageLanguageMeta(conversationID, serverMsgID, detectedLang, authorLang, translationStatus string) (domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx,
		`UPDATE messages SET
		   detected_lang = COALESCE(NULLIF($3, ''), detected_lang),
		   author_lang = COALESCE(NULLIF($4, ''), author_lang),
		   translation_status = COALESCE(NULLIF($5, ''), translation_status)
		 WHERE server_msg_id=$1 AND conversation_id=$2
		 RETURNING `+messageColumns,
		serverMsgID, conversationID, detectedLang, authorLang, translationStatus)
	msg, err := scanMessage(row)
	if err != nil {
		return domain.Message{}, mapNoRows(err)
	}
	return msg, nil
}

// UpdateConversationLanguage 更新会话语言状态。
func (s *PostgresStore) UpdateConversationLanguage(conversationID string, patch ConversationLanguagePatch) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	sets := []string{}
	args := []any{conversationID}
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	if patch.VisitorLang != nil {
		add("visitor_lang", *patch.VisitorLang)
	}
	if patch.DetectedLang != nil {
		add("detected_lang", *patch.DetectedLang)
	}
	if patch.ReplyLangOverride != nil {
		add("reply_lang_override", *patch.ReplyLangOverride)
		if *patch.ReplyLangOverride == "" {
			sets = append(sets, "reply_lang_override_at=NULL")
		} else {
			sets = append(sets, "reply_lang_override_at=now()")
		}
	}
	if patch.LanguageStats != nil {
		encoded, err := json.Marshal(patch.LanguageStats)
		if err != nil {
			return domain.Conversation{}, err
		}
		add("language_stats", string(encoded))
	}
	if len(sets) == 0 {
		conversation, ok := s.Conversation(conversationID)
		if !ok {
			return domain.Conversation{}, ErrNotFound
		}
		return conversation, nil
	}
	sets = append(sets, "updated_at=now()")
	row := s.pool.QueryRow(ctx,
		`UPDATE conversations SET `+strings.Join(sets, ", ")+` WHERE id=$1 RETURNING `+conversationColumns, args...)
	conversation, err := scanConversation(row)
	if err != nil {
		return domain.Conversation{}, mapNoRows(err)
	}
	return conversation, nil
}

func (s *PostgresStore) UpdateMessageTranslations(conversationID, serverMsgID string, translations map[string]string) (domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	merged := cleanTranslations(translations)
	var out domain.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE conversation_id=$1 AND server_msg_id=$2
			FOR UPDATE
		`, conversationID, strings.TrimSpace(serverMsgID))
		msg, err := scanMessage(row)
		if err != nil {
			return mapNoRows(err)
		}
		if len(merged) > 0 {
			current := map[string]string{}
			for lang, text := range msg.Translations {
				current[lang] = text
			}
			for lang, text := range merged {
				current[lang] = text
			}
			msg.Translations = current
		}
		if _, err := tx.Exec(ctx, `UPDATE messages SET translations=$3::jsonb WHERE server_msg_id=$1 AND conversation_id=$2`, serverMsgID, conversationID, marshalTranslationsJSON(msg.Translations)); err != nil {
			return err
		}
		out = msg
		return nil
	})
	return out, err
}

func (s *PostgresStore) RevokeMessage(actor domain.AuthSession, conversationID, serverMsgID string) (domain.Message, domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var msg domain.Message
	var conversation domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		current, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && current.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind != domain.AccountAgent && actor.Kind != domain.AccountAdmin {
			return ErrForbidden
		}

		row := tx.QueryRow(ctx, `
			SELECT `+messageColumns+`
			FROM messages
			WHERE conversation_id=$1 AND server_msg_id=$2
			FOR UPDATE
		`, conversationID, strings.TrimSpace(serverMsgID))
		msg, err = scanMessage(row)
		if err != nil {
			return mapNoRows(err)
		}
		if msg.SenderType != domain.SenderAgent {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountAgent && msg.SenderID != actor.AccountID {
			return ErrForbidden
		}
		if msg.RevokedAt == nil {
			now := dbNow()
			row = tx.QueryRow(ctx, `
				UPDATE messages
				SET revoked_at=$3, revoked_by_kind=$4, revoked_by_id=$5
				WHERE conversation_id=$1 AND server_msg_id=$2
				RETURNING `+messageColumns,
				conversationID,
				msg.ServerMsgID,
				now,
				actor.Kind,
				actor.AccountID,
			)
			msg, err = scanMessage(row)
			if err != nil {
				return err
			}
			if err := s.touchConversationRevokeTx(ctx, tx, &current, msg, now); err != nil {
				return err
			}
		}
		conversation, err = s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		return err
	})
	return msg, conversation, err
}

func (s *PostgresStore) CloseConversation(actor domain.AuthSession, conversationID string) (domain.Conversation, domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	var msg domain.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		now := dbNow()
		if conversation.AssignedAgentID != "" {
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=GREATEST(current_conversations-1, 0), updated_at=$2 WHERE id=$1`, conversation.AssignedAgentID, now); err != nil {
				return err
			}
		}
		row := tx.QueryRow(ctx, `UPDATE conversations SET status=$2, updated_at=$3, closed_at=$3 WHERE id=$1 RETURNING `+conversationColumns, conversationID, domain.ConversationClosed, now)
		out, err = scanConversation(row)
		if err != nil {
			return err
		}
		msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageSystem, "会话已结束", nil)
		return err
	})
	return out, msg, err
}

func (s *PostgresStore) DeleteConversation(actor domain.AuthSession, conversationID string) error {
	ctx, cancel := dbContext()
	defer cancel()

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if actor.Kind == domain.AccountAgent && conversation.AssignedAgentID != actor.AccountID {
			return ErrForbidden
		}
		if actor.Kind == domain.AccountVisitor && conversation.ID != actor.AccountID {
			return ErrForbidden
		}
		if conversation.Status != domain.ConversationClosed {
			return ErrForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, conversationID)
		return err
	})
}

func (s *PostgresStore) TransferConversation(actor domain.AuthSession, conversationID, agentID, group string) (domain.Conversation, domain.Message, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	var msg domain.Message
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if actor.Kind != domain.AccountAdmin {
			return ErrForbidden
		}
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		target, err := s.selectTransferTargetTx(ctx, tx, agentID, group)
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.AssignedAgentID == target.ID {
			msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageHandoff, "会话已在目标客服名下。", nil)
			out = conversation
			return err
		}
		now := dbNow()
		if conversation.AssignedAgentID != "" {
			if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=GREATEST(current_conversations-1, 0), updated_at=$2 WHERE id=$1`, conversation.AssignedAgentID, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2 WHERE id=$1`, target.ID, now); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET assigned_agent_id=$2, updated_at=$3
			WHERE id=$1
			RETURNING `+conversationColumns, conversationID, target.ID, now)
		out, err = scanConversation(row)
		if err != nil {
			return err
		}
		out.AssignedAgentName = target.Name
		out.AssignedAgentAvatar = target.AvatarURL
		// 转接只更换客服，不改变会话状态：客服实际回复后才对外显示人工服务（无痕转人工）。
		msg, err = s.createMessageTx(ctx, tx, conversationID, "", domain.SenderSystem, "system", domain.MessageHandoff, "会话已转接至 "+target.Name+"（仅客服端可见）。", nil)
		return err
	})
	return out, msg, err
}

func (s *PostgresStore) SubmitRating(conversationID string, score int, tags []string, comment string) (domain.ServiceRating, error) {
	ctx, cancel := dbContext()
	defer cancel()
	if score < 1 || score > 5 {
		return domain.ServiceRating{}, ErrInvalidInput
	}
	if err := validateRatingInput(tags, comment); err != nil {
		return domain.ServiceRating{}, err
	}
	var out domain.ServiceRating
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, conversationID)
		if err != nil {
			return mapNoRows(err)
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO service_ratings (id, conversation_id, visitor_id, assigned_agent_id, score, tags, comment, created_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8)
			RETURNING `+ratingColumns,
			"rate_"+randomID(8),
			conversation.ID,
			conversation.VisitorID,
			conversation.AssignedAgentID,
			score,
			cleanTags(tags),
			strings.TrimSpace(comment),
			dbNow(),
		)
		out, err = scanRating(row)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	})
	return out, err
}

func (s *PostgresStore) RatingSummary() domain.RatingSummary {
	ctx, cancel := dbContext()
	defer cancel()

	var total int
	var average float64
	var satisfied int
	var neutral int
	var unsatisfied int
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COALESCE(AVG(score), 0)::float8,
			COUNT(*) FILTER (WHERE score >= 4)::int,
			COUNT(*) FILTER (WHERE score = 3)::int,
			COUNT(*) FILTER (WHERE score <= 2)::int
		FROM service_ratings
	`).Scan(&total, &average, &satisfied, &neutral, &unsatisfied)
	if err != nil || total == 0 {
		return domain.RatingSummary{}
	}
	return domain.RatingSummary{
		Total:            total,
		Average:          average,
		Satisfied:        satisfied,
		Neutral:          neutral,
		Unsatisfied:      unsatisfied,
		SatisfactionRate: float64(satisfied) / float64(total),
	}
}

func (s *PostgresStore) RatingSummaryForAgent(agentID string) domain.RatingSummary {
	ctx, cancel := dbContext()
	defer cancel()

	var total int
	var average float64
	var satisfied int
	var neutral int
	var unsatisfied int
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COALESCE(AVG(score), 0)::float8,
			COUNT(*) FILTER (WHERE score >= 4)::int,
			COUNT(*) FILTER (WHERE score = 3)::int,
			COUNT(*) FILTER (WHERE score <= 2)::int
		FROM service_ratings
		WHERE assigned_agent_id = $1
	`, agentID).Scan(&total, &average, &satisfied, &neutral, &unsatisfied)
	if err != nil || total == 0 {
		return domain.RatingSummary{}
	}
	return domain.RatingSummary{
		Total:            total,
		Average:          average,
		Satisfied:        satisfied,
		Neutral:          neutral,
		Unsatisfied:      unsatisfied,
		SatisfactionRate: float64(satisfied) / float64(total),
	}
}

func (s *PostgresStore) RatingSummariesByAgent() map[string]domain.RatingSummary {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+ratingColumns+` FROM service_ratings`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	grouped := make(map[string][]domain.ServiceRating)
	for rows.Next() {
		rating, err := scanRating(rows)
		if err != nil || rating.AssignedAgentID == "" {
			continue
		}
		grouped[rating.AssignedAgentID] = append(grouped[rating.AssignedAgentID], rating)
	}
	result := make(map[string]domain.RatingSummary, len(grouped))
	for agentID, ratings := range grouped {
		result[agentID] = buildRatingSummary(ratings)
	}
	return result
}

func (s *PostgresStore) RatingsForAgent(agentID string) []domain.ServiceRating {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT `+ratingColumns+` FROM service_ratings
		WHERE assigned_agent_id = $1
		ORDER BY created_at DESC
	`, agentID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ratings, err := scanRatings(rows)
	if err != nil {
		return nil
	}
	return ratings
}

func (s *PostgresStore) RecentRatings(limit int) []domain.ServiceRating {
	ctx, cancel := dbContext()
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT `+ratingColumns+` FROM service_ratings ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	ratings, err := scanRatings(rows)
	if err != nil {
		return nil
	}
	return ratings
}

func (s *PostgresStore) DashboardStats() domain.DashboardStats {
	ctx, cancel := dbContext()
	defer cancel()

	stats := domain.DashboardStats{Rating: s.RatingSummary()}
	rows, err := s.pool.Query(ctx, `SELECT status, COUNT(*)::int FROM conversations GROUP BY status`)
	if err == nil {
		for rows.Next() {
			var status domain.ConversationStatus
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				continue
			}
			stats.TotalConversations += count
			switch status {
			case domain.ConversationAIServing:
				stats.AIServing = count
				stats.ActiveConversations += count
			case domain.ConversationWaiting:
				stats.Waiting = count
				stats.ActiveConversations += count
			case domain.ConversationHumanRequested:
				stats.HumanRequested = count
				stats.ActiveConversations += count
			case domain.ConversationAssigned:
				stats.Assigned = count
				stats.ActiveConversations += count
			case domain.ConversationClosed:
				stats.Closed = count
			}
		}
		rows.Close()
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status=$1 AND disabled_at IS NULL)::int
		FROM agents
	`, domain.AgentOnline).Scan(&stats.TotalAgents, &stats.OnlineAgents)
	return stats
}

func (s *PostgresStore) RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent {
	ctx, cancel := dbContext()
	defer cancel()

	if event.ID == "" {
		event.ID = "audit_" + randomID(8)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = dbNow()
	}
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	event.Description = strings.TrimSpace(event.Description)
	row := s.pool.QueryRow(ctx, `
		INSERT INTO audit_events (id, actor_kind, actor_id, action, resource, resource_id, ip_address, user_agent, description, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+auditColumns,
		event.ID, event.ActorKind, event.ActorID, event.Action, event.Resource, event.ResourceID, event.IPAddress, event.UserAgent, event.Description, event.CreatedAt)
	out, err := scanAuditEvent(row)
	if err != nil {
		return event
	}
	return out
}

func (s *PostgresStore) RecentAuditEvents(limit int) []domain.AuditEvent {
	ctx, cancel := dbContext()
	defer cancel()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+auditColumns+` FROM audit_events ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	events, err := scanAuditEvents(rows)
	if err != nil {
		return nil
	}
	return events
}

func (s *PostgresStore) ContactSettings() domain.ContactSettings {
	ctx, cancel := dbContext()
	defer cancel()
	settings, err := s.contactSettings(ctx, s.pool)
	if err != nil {
		return domain.ContactSettings{}
	}
	return settings
}

func (s *PostgresStore) UpdateContactSettings(next domain.ContactSettings) (domain.ContactSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.contactSettings(ctx, s.pool)
	if err := validateContactSettings(next); err != nil {
		return current, err
	}
	// PATCH 为整体替换语义：允许管理员把联系方式显式清空，未传/传空的字段即视为清空。
	current = domain.ContactSettings{
		Telephone:  strings.TrimSpace(next.Telephone),
		WhatsApp:   strings.TrimSpace(next.WhatsApp),
		Telegram:   strings.TrimSpace(next.Telegram),
		EntryReply: strings.TrimSpace(next.EntryReply),
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO contact_settings (id, phone, wechat, wechat_reply_type, wechat_image_url, qq, qq_reply_type, qq_image_url, entry_reply, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			phone=EXCLUDED.phone,
			wechat=EXCLUDED.wechat,
			wechat_reply_type=EXCLUDED.wechat_reply_type,
			wechat_image_url=EXCLUDED.wechat_image_url,
			qq=EXCLUDED.qq,
			qq_reply_type=EXCLUDED.qq_reply_type,
			qq_image_url=EXCLUDED.qq_image_url,
			entry_reply=EXCLUDED.entry_reply,
			updated_at=EXCLUDED.updated_at
	`, current.Telephone, current.WhatsApp, "text", "", current.Telegram, "text", "", current.EntryReply, dbNow())
	return current, nil
}

func (s *PostgresStore) KeywordRules() []domain.KeywordRule {
	ctx, cancel := dbContext()
	defer cancel()
	rules, err := s.keywordRules(ctx, s.pool)
	if err != nil {
		return nil
	}
	return rules
}

func (s *PostgresStore) CreateKeywordRule(next domain.KeywordRule) (domain.KeywordRule, error) {
	ctx, cancel := dbContext()
	defer cancel()
	next.Keyword = strings.TrimSpace(next.Keyword)
	next.Reply = strings.TrimSpace(next.Reply)
	if err := validateKeywordRule(next); err != nil {
		return domain.KeywordRule{}, err
	}
	if strings.TrimSpace(next.MatchType) == "" {
		next.MatchType = "contains"
	}
	if strings.TrimSpace(next.Action) == "" {
		next.Action = "text"
	}
	if next.ID == "" {
		next.ID = "kw_" + randomID(8)
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO keyword_rules (id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		RETURNING id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
	`, next.ID, next.Keyword, next.MatchType, next.Reply, next.Enabled, next.Priority, next.Action, next.ShowInQuickReplies, strings.TrimSpace(next.QuickReplyText), dbNow()).Scan(&next.ID, &next.Keyword, &next.MatchType, &next.Reply, &next.Enabled, &next.Priority, &next.Action, &next.ShowInQuickReplies, &next.QuickReplyText)
	if isUniqueViolation(err) {
		return domain.KeywordRule{}, ErrConflict
	}
	return next, err
}

func (s *PostgresStore) DeleteKeywordRule(id string) (domain.KeywordRule, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, err := s.keywordRuleByID(ctx, s.pool, id)
	if err != nil {
		return domain.KeywordRule{}, mapNoRows(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM keyword_rules WHERE id=$1`, id); err != nil {
		return domain.KeywordRule{}, err
	}
	return current, nil
}

func (s *PostgresStore) UpdateKeywordRule(id string, next domain.KeywordRule) (domain.KeywordRule, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, err := s.keywordRuleByID(ctx, s.pool, id)
	if err != nil {
		return domain.KeywordRule{}, mapNoRows(err)
	}
	if strings.TrimSpace(next.Keyword) != "" {
		current.Keyword = strings.TrimSpace(next.Keyword)
	}
	if strings.TrimSpace(next.MatchType) != "" {
		current.MatchType = strings.TrimSpace(next.MatchType)
	}
	if strings.TrimSpace(next.Reply) != "" {
		current.Reply = strings.TrimSpace(next.Reply)
	}
	if strings.TrimSpace(next.Action) != "" {
		current.Action = strings.TrimSpace(next.Action)
	}
	current.ShowInQuickReplies = next.ShowInQuickReplies
	current.QuickReplyText = strings.TrimSpace(next.QuickReplyText)
	current.Enabled = next.Enabled
	current.Priority = next.Priority
	if err := validateKeywordRule(current); err != nil {
		return domain.KeywordRule{}, err
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE keyword_rules
		SET keyword=$2, match_type=$3, reply=$4, enabled=$5, priority=$6, action=$7, show_in_quick_replies=$8, quick_reply_text=$9, updated_at=$10
		WHERE id=$1
		RETURNING id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
	`, id, current.Keyword, current.MatchType, current.Reply, current.Enabled, current.Priority, current.Action, current.ShowInQuickReplies, current.QuickReplyText, dbNow()).Scan(&current.ID, &current.Keyword, &current.MatchType, &current.Reply, &current.Enabled, &current.Priority, &current.Action, &current.ShowInQuickReplies, &current.QuickReplyText)
	return current, err
}

func (s *PostgresStore) AISettings() domain.AISettings {
	ctx, cancel := dbContext()
	defer cancel()
	ai, err := s.aiSettingsTx(ctx, s.pool)
	if err != nil {
		return domain.AISettings{}
	}
	return ai
}

func (s *PostgresStore) UpdateAISettings(next domain.AISettings) (domain.AISettings, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.aiSettingsTx(ctx, s.pool)
	if err := validateAISettings(next); err != nil {
		return current, err
	}
	if strings.TrimSpace(next.BaseURL) != "" {
		current.BaseURL = strings.TrimSpace(next.BaseURL)
	}
	if strings.TrimSpace(next.APIKey) != "" {
		current.APIKey = strings.TrimSpace(next.APIKey)
		current.APIKeyMasked = maskAPIKey(next.APIKey)
	}
	if strings.TrimSpace(next.Model) != "" {
		current.Model = strings.TrimSpace(next.Model)
	}
	if strings.TrimSpace(next.APIType) != "" {
		current.APIType = strings.TrimSpace(next.APIType)
	}
	if next.Mode != "" {
		current.Mode = next.Mode
	}
	current.Enabled = next.Enabled
	if next.Temperature >= 0 {
		current.Temperature = next.Temperature
	}
	if next.MaxOutputTokens > 0 {
		current.MaxOutputTokens = next.MaxOutputTokens
	}
	if next.TimeoutSeconds > 0 {
		current.TimeoutSeconds = next.TimeoutSeconds
	}
	if strings.TrimSpace(next.SystemPrompt) != "" {
		current.SystemPrompt = strings.TrimSpace(next.SystemPrompt)
	}
	protectedKey, err := protectSecret(s.options.DataEncryptionKey, current.APIKey)
	if err != nil {
		return current, err
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO ai_settings (
			id, enabled, mode, base_url, api_key_ciphertext, model, api_type,
			temperature, max_output_tokens, timeout_seconds, system_prompt, updated_at
		)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			mode=EXCLUDED.mode,
			base_url=EXCLUDED.base_url,
			api_key_ciphertext=EXCLUDED.api_key_ciphertext,
			model=EXCLUDED.model,
			api_type=EXCLUDED.api_type,
			temperature=EXCLUDED.temperature,
			max_output_tokens=EXCLUDED.max_output_tokens,
			timeout_seconds=EXCLUDED.timeout_seconds,
			system_prompt=EXCLUDED.system_prompt,
			updated_at=EXCLUDED.updated_at
	`, current.Enabled, current.Mode, current.BaseURL, protectedKey, current.Model, current.APIType, current.Temperature, current.MaxOutputTokens, current.TimeoutSeconds, current.SystemPrompt, dbNow())
	return current, nil
}

// ===== AI 多供应商与能力绑定 =====

func scanAIProvider(row scanTarget) (domain.AIProvider, error) {
	var out domain.AIProvider
	var storedKey, modelsJSON string
	err := row.Scan(
		&out.ID, &out.Name, &out.Kind, &out.APIType, &out.BaseURL, &storedKey,
		&modelsJSON, &out.DefaultModel, &out.TimeoutMS, &out.AllowedHosts, &out.Enabled,
		&out.SortOrder, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return domain.AIProvider{}, err
	}
	out.Models = parseAIModelsJSON(modelsJSON)
	// APIKey 字段先暂存密文，由 revealAIProvider 换成明文并计算末四位掩码。
	out.APIKey = storedKey
	return out, nil
}

func parseAIModelsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

// revealAIProvider 把密文换成明文并刷新掩码；密文损坏时返回错误。
func (s *PostgresStore) revealAIProvider(provider *domain.AIProvider) error {
	key, err := revealSecret(s.options.DataEncryptionKey, provider.APIKey)
	if err != nil {
		return err
	}
	provider.APIKey = key
	provider.APIKeyMasked = maskAPIKey(key)
	return nil
}

// ListAIProviders 返回全部供应商；为避免明文外泄，列表只暴露末四位掩码。
func (s *PostgresStore) ListAIProviders() []domain.AIProvider {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+aiProviderColumns+` FROM ai_providers ORDER BY sort_order, created_at, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]domain.AIProvider, 0)
	for rows.Next() {
		provider, err := scanAIProvider(rows)
		if err != nil {
			return nil
		}
		if err := s.revealAIProvider(&provider); err != nil {
			provider.APIKey = ""
			provider.APIKeyMasked = ""
		} else {
			provider.APIKey = ""
		}
		out = append(out, provider)
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	return out
}

// AIProvider 读取单个供应商并解密密钥，仅供服务端内部与服务间 /secret 使用。
func (s *PostgresStore) AIProvider(id string) (domain.AIProvider, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	provider, err := scanAIProvider(s.pool.QueryRow(ctx, `
		SELECT `+aiProviderColumns+` FROM ai_providers WHERE id=$1
	`, strings.TrimSpace(id)))
	if err != nil {
		return domain.AIProvider{}, false
	}
	if err := s.revealAIProvider(&provider); err != nil {
		return domain.AIProvider{}, false
	}
	return provider, true
}

func (s *PostgresStore) CreateAIProvider(next domain.AIProvider, apiKey string) (domain.AIProvider, error) {
	ctx, cancel := dbContext()
	defer cancel()

	next, err := normalizeAIProvider(next)
	if err != nil {
		return domain.AIProvider{}, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if err := validateAIProviderAPIKey(apiKey); err != nil {
		return domain.AIProvider{}, err
	}
	if next.ID == "" {
		next.ID = "aip_" + randomID(8)
	}
	protectedKey, err := protectSecret(s.options.DataEncryptionKey, apiKey)
	if err != nil {
		return domain.AIProvider{}, err
	}
	modelsJSON, err := json.Marshal(next.Models)
	if err != nil {
		return domain.AIProvider{}, err
	}
	now := dbNow()
	provider, err := scanAIProvider(s.pool.QueryRow(ctx, `
		INSERT INTO ai_providers (
			id, name, kind, api_type, base_url, api_key_ciphertext, models, default_model,
			timeout_ms, allowed_hosts, enabled, sort_order, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+aiProviderColumns,
		next.ID, next.Name, next.Kind, next.APIType, next.BaseURL, protectedKey, string(modelsJSON),
		next.DefaultModel, next.TimeoutMS, next.AllowedHosts, next.Enabled, next.SortOrder, now, now,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.AIProvider{}, ErrConflict
		}
		return domain.AIProvider{}, err
	}
	if err := s.revealAIProvider(&provider); err != nil {
		return domain.AIProvider{}, err
	}
	return provider, nil
}

// UpdateAIProvider 覆盖更新供应商配置；apiKey==nil 保留原密钥，空串清除密钥。
func (s *PostgresStore) UpdateAIProvider(id string, patch domain.AIProvider, apiKey *string) (domain.AIProvider, error) {
	ctx, cancel := dbContext()
	defer cancel()

	id = strings.TrimSpace(id)
	var existingKey string
	if err := s.pool.QueryRow(ctx, `SELECT api_key_ciphertext FROM ai_providers WHERE id=$1`, id).Scan(&existingKey); err != nil {
		return domain.AIProvider{}, mapNoRows(err)
	}
	next, err := normalizeAIProvider(patch)
	if err != nil {
		return domain.AIProvider{}, err
	}
	next.ID = id
	protectedKey := existingKey
	if apiKey != nil {
		key := strings.TrimSpace(*apiKey)
		if err := validateAIProviderAPIKey(key); err != nil {
			return domain.AIProvider{}, err
		}
		protectedKey, err = protectSecret(s.options.DataEncryptionKey, key)
		if err != nil {
			return domain.AIProvider{}, err
		}
	}
	modelsJSON, err := json.Marshal(next.Models)
	if err != nil {
		return domain.AIProvider{}, err
	}
	provider, err := scanAIProvider(s.pool.QueryRow(ctx, `
		UPDATE ai_providers SET
			name=$2, kind=$3, api_type=$4, base_url=$5, api_key_ciphertext=$6, models=$7::jsonb,
			default_model=$8, timeout_ms=$9, allowed_hosts=$10, enabled=$11, sort_order=$12, updated_at=$13
		WHERE id=$1
		RETURNING `+aiProviderColumns,
		id, next.Name, next.Kind, next.APIType, next.BaseURL, protectedKey, string(modelsJSON),
		next.DefaultModel, next.TimeoutMS, next.AllowedHosts, next.Enabled, next.SortOrder, dbNow(),
	))
	if err != nil {
		return domain.AIProvider{}, err
	}
	if err := s.revealAIProvider(&provider); err != nil {
		return domain.AIProvider{}, err
	}
	return provider, nil
}

// DeleteAIProvider 删除供应商并删除指向它的能力绑定，避免悬挂引用。
func (s *PostgresStore) DeleteAIProvider(id string) error {
	ctx, cancel := dbContext()
	defer cancel()

	id = strings.TrimSpace(id)
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ai_providers WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `DELETE FROM ai_bindings WHERE provider_id=$1`, id)
		return err
	})
}

func scanAIBinding(row scanTarget) (domain.AIBinding, error) {
	var out domain.AIBinding
	var paramsJSON string
	if err := row.Scan(&out.Capability, &out.ProviderID, &out.Model, &paramsJSON, &out.UpdatedAt); err != nil {
		return domain.AIBinding{}, err
	}
	out.Params = map[string]any{}
	if strings.TrimSpace(paramsJSON) != "" {
		_ = json.Unmarshal([]byte(paramsJSON), &out.Params)
	}
	if out.Params == nil {
		out.Params = map[string]any{}
	}
	return out, nil
}

func (s *PostgresStore) AIBindings() map[string]domain.AIBinding {
	ctx, cancel := dbContext()
	defer cancel()

	out := map[string]domain.AIBinding{}
	rows, err := s.pool.Query(ctx, `SELECT `+aiBindingColumns+` FROM ai_bindings`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		binding, err := scanAIBinding(rows)
		if err != nil {
			return out
		}
		out[binding.Capability] = binding
	}
	return out
}

func (s *PostgresStore) UpsertAIBinding(next domain.AIBinding) (domain.AIBinding, error) {
	ctx, cancel := dbContext()
	defer cancel()

	next, err := normalizeAIBinding(next)
	if err != nil {
		return domain.AIBinding{}, err
	}
	paramsJSON, err := json.Marshal(next.Params)
	if err != nil {
		return domain.AIBinding{}, err
	}
	binding, err := scanAIBinding(s.pool.QueryRow(ctx, `
		INSERT INTO ai_bindings (capability, provider_id, model, params, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (capability) DO UPDATE SET
			provider_id=EXCLUDED.provider_id,
			model=EXCLUDED.model,
			params=EXCLUDED.params,
			updated_at=EXCLUDED.updated_at
		RETURNING `+aiBindingColumns,
		next.Capability, next.ProviderID, next.Model, string(paramsJSON), dbNow(),
	))
	if err != nil {
		return domain.AIBinding{}, err
	}
	return binding, nil
}

// AICacheProbe 读取模型缓存探测结果；无记录时返回空结果。
func (s *PostgresStore) AICacheProbe() domain.AICacheProbe {
	ctx, cancel := dbContext()
	defer cancel()
	var out domain.AICacheProbe
	var resultsJSON string
	var probedAt sql.NullTime
	err := s.pool.QueryRow(ctx, "SELECT COALESCE(model_results::text, '{}'), status, error, probed_at FROM ai_cache_probe WHERE id=1").
		Scan(&resultsJSON, &out.Status, &out.Error, &probedAt)
	if err != nil {
		out.ModelResults = map[string]string{}
		return out
	}
	out.ModelResults = parseTranslationsJSON(resultsJSON)
	if probedAt.Valid {
		out.ProbedAt = &probedAt.Time
	}
	return out
}

// SaveAICacheProbe 保存模型缓存探测结果。
func (s *PostgresStore) SaveAICacheProbe(probe domain.AICacheProbe) {
	ctx, cancel := dbContext()
	defer cancel()
	resultsJSON, err := json.Marshal(probe.ModelResults)
	if err != nil {
		resultsJSON = []byte("{}")
	}
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO ai_cache_probe (id, model_results, status, error, probed_at, updated_at)
		VALUES (1, $1::jsonb, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			model_results=EXCLUDED.model_results,
			status=EXCLUDED.status,
			error=EXCLUDED.error,
			probed_at=EXCLUDED.probed_at,
			updated_at=EXCLUDED.updated_at
	`, string(resultsJSON), probe.Status, probe.Error, probe.ProbedAt, dbNow())
}

func (s *PostgresStore) BusinessHours() domain.BusinessHours {
	ctx, cancel := dbContext()
	defer cancel()
	business, err := s.businessHoursTx(ctx, s.pool)
	if err != nil {
		return domain.BusinessHours{}
	}
	return business
}

func (s *PostgresStore) UpdateBusinessHours(next domain.BusinessHours) (domain.BusinessHours, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.businessHoursTx(ctx, s.pool)
	if err := validateBusinessHours(next); err != nil {
		return current, err
	}
	if strings.TrimSpace(next.Timezone) != "" {
		current.Timezone = strings.TrimSpace(next.Timezone)
	}
	if strings.TrimSpace(next.Start) != "" {
		current.Start = strings.TrimSpace(next.Start)
	}
	if strings.TrimSpace(next.End) != "" {
		current.End = strings.TrimSpace(next.End)
	}
	current.Enabled = next.Enabled
	current.OfflineReply = strings.TrimSpace(next.OfflineReply)
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO business_hours (id, timezone, start_time, end_time, enabled, offline_reply, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET timezone=EXCLUDED.timezone, start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, enabled=EXCLUDED.enabled, offline_reply=EXCLUDED.offline_reply, updated_at=EXCLUDED.updated_at
	`, current.Timezone, current.Start, current.End, current.Enabled, current.OfflineReply, dbNow())
	return current, nil
}

// pushSecretPayload 是 push_settings.encrypted_secrets 的明文结构，整体做 AES-GCM 加密。
type pushSecretPayload struct {
	FCMServiceAccountJSON string `json:"fcm_service_account_json"`
	FCMAccessToken        string `json:"fcm_access_token"`
	APNsKeyP8             string `json:"apns_key_p8"`
}

func (s *PostgresStore) PushSettings() domain.PushSettings {
	ctx, cancel := dbContext()
	defer cancel()
	out, err := s.pushSettingsTx(ctx, s.pool)
	if err != nil {
		bootstrap := domain.PushSettings{FCMAccessToken: strings.TrimSpace(s.options.FCMAccessToken)}
		bootstrap.Normalize()
		return bootstrap
	}
	return out
}

func (s *PostgresStore) pushSettingsTx(ctx context.Context, exec dbTx) (domain.PushSettings, error) {
	var publicRaw []byte
	var encrypted string
	var updated sql.NullTime
	err := exec.QueryRow(ctx, `SELECT public_config, encrypted_secrets, updated_at FROM push_settings WHERE id=1`).
		Scan(&publicRaw, &encrypted, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		out := domain.PushSettings{FCMAccessToken: strings.TrimSpace(s.options.FCMAccessToken)}
		out.Normalize()
		return out, nil
	}
	if err != nil {
		return domain.PushSettings{}, err
	}
	var out domain.PushSettings
	if len(publicRaw) > 0 {
		_ = json.Unmarshal(publicRaw, &out)
	}
	if strings.TrimSpace(encrypted) != "" {
		plain, revealErr := revealSecret(s.options.DataEncryptionKey, encrypted)
		if revealErr != nil {
			return domain.PushSettings{}, revealErr
		}
		var secrets pushSecretPayload
		_ = json.Unmarshal([]byte(plain), &secrets)
		out.FCMServiceAccountJSON = secrets.FCMServiceAccountJSON
		out.FCMAccessToken = secrets.FCMAccessToken
		out.APNsKeyP8 = secrets.APNsKeyP8
	}
	if updated.Valid {
		stamp := updated.Time
		out.UpdatedAt = &stamp
	}
	out.Normalize()
	return out, nil
}

func (s *PostgresStore) UpdatePushSettings(next domain.PushSettings) (domain.PushSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()
	current, _ := s.pushSettingsTx(ctx, s.pool)
	if err := validatePushSettings(next); err != nil {
		return current, err
	}
	// json.Marshal 会通过 json:"-" 自动排除所有密钥字段。
	publicRaw, err := json.Marshal(next)
	if err != nil {
		return current, err
	}
	secretRaw, err := json.Marshal(pushSecretPayload{
		FCMServiceAccountJSON: strings.TrimSpace(next.FCMServiceAccountJSON),
		FCMAccessToken:        strings.TrimSpace(next.FCMAccessToken),
		APNsKeyP8:             strings.TrimSpace(next.APNsKeyP8),
	})
	if err != nil {
		return current, err
	}
	encrypted, err := protectSecret(s.options.DataEncryptionKey, string(secretRaw))
	if err != nil {
		return current, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO push_settings (id, public_config, encrypted_secrets, updated_at)
		VALUES (1, $1::jsonb, $2, now())
		ON CONFLICT (id) DO UPDATE SET public_config=EXCLUDED.public_config, encrypted_secrets=EXCLUDED.encrypted_secrets, updated_at=now()
	`, string(publicRaw), encrypted); err != nil {
		return current, err
	}
	out, err := s.pushSettingsTx(ctx, s.pool)
	if err != nil {
		return current, err
	}
	return out, nil
}

func (s *PostgresStore) CreateAgent(next domain.Agent, password string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()
	next.Account = strings.TrimSpace(next.Account)
	next.Name = strings.TrimSpace(next.Name)
	next.Group = strings.TrimSpace(next.Group)
	password = strings.TrimSpace(password)
	if next.Account == "" || next.Name == "" || password == "" {
		return domain.Agent{}, ErrInvalidInput
	}
	if err := validateTemporaryPassword(password); err != nil {
		return domain.Agent{}, err
	}
	if next.ID == "" {
		next.ID = "agent_" + randomID(8)
	}
	if next.Group == "" {
		next.Group = "默认组"
	}
	if next.MaxConversations <= 0 {
		next.MaxConversations = 10
	}
	if next.Status == "" {
		next.Status = domain.AgentOffline
	}
	if err := validateAgentInput(next, true); err != nil {
		return domain.Agent{}, err
	}
	now := dbNow()
	hash, err := hashPassword(password)
	if err != nil {
		return domain.Agent{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agents (id, account, name, group_name, password_hash, status, max_conversations, current_conversations, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8, $8)
		RETURNING `+agentColumns, next.ID, next.Account, next.Name, next.Group, hash, next.Status, next.MaxConversations, now)
	agent, err := scanAgent(row)
	if isUniqueViolation(err) {
		return domain.Agent{}, ErrConflict
	}
	return agent, err
}

func (s *PostgresStore) UpdateAgent(id string, next domain.Agent) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if next.Name != "" || next.Group != "" || next.MaxConversations != 0 || next.Status != "" {
		if err := validateAgentInput(domain.Agent{
			Account:          "existing",
			Name:             nonEmpty(next.Name, "existing"),
			Group:            next.Group,
			Status:           next.Status,
			MaxConversations: next.MaxConversations,
		}, false); err != nil {
			return domain.Agent{}, err
		}
	}
	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		if strings.TrimSpace(next.Name) != "" {
			agent.Name = strings.TrimSpace(next.Name)
		}
		if strings.TrimSpace(next.Group) != "" {
			agent.Group = strings.TrimSpace(next.Group)
		}
		if next.MaxConversations > 0 {
			agent.MaxConversations = next.MaxConversations
		}
		if next.Status != "" {
			if agent.DisabledAt != nil && next.Status == domain.AgentOnline {
				return ErrForbidden
			}
			agent.Status = next.Status
		}
		agent.Updated = dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE agents
			SET name=$2, group_name=$3, status=$4, max_conversations=$5, updated_at=$6
			WHERE id=$1
			RETURNING `+agentColumns, agent.ID, agent.Name, agent.Group, agent.Status, agent.MaxConversations, agent.Updated)
		out, err = scanAgent(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) UpdateAgentAvatar(agentID, avatarURL string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `
		UPDATE agents SET avatar_url=$2, updated_at=$3 WHERE id=$1
		RETURNING `+agentColumns, strings.TrimSpace(agentID), strings.TrimSpace(avatarURL), dbNow())
	agent, err := scanAgent(row)
	if err != nil {
		return domain.Agent{}, mapNoRows(err)
	}
	return agent, nil
}

func (s *PostgresStore) SetAgentManualRating(agentID string, rate float64) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if rate < 0 || rate > 1 {
		return domain.Agent{}, ErrInvalidInput
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE agents SET manual_satisfaction_rate=$2, updated_at=$3 WHERE id=$1
		RETURNING `+agentColumns, strings.TrimSpace(agentID), rate, dbNow())
	agent, err := scanAgent(row)
	if err != nil {
		return domain.Agent{}, mapNoRows(err)
	}
	return agent, nil
}

func (s *PostgresStore) ClearAgentManualRating(agentID string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `
		UPDATE agents SET manual_satisfaction_rate=NULL, updated_at=$2 WHERE id=$1
		RETURNING `+agentColumns, strings.TrimSpace(agentID), dbNow())
	agent, err := scanAgent(row)
	if err != nil {
		return domain.Agent{}, mapNoRows(err)
	}
	return agent, nil
}

func (s *PostgresStore) ResetAgentRatings(agentID string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM service_ratings WHERE assigned_agent_id=$1`, agentID); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE agents SET manual_satisfaction_rate=NULL, updated_at=$2 WHERE id=$1
			RETURNING `+agentColumns, agentID, dbNow())
		agent, err := scanAgent(row)
		if err != nil {
			return mapNoRows(err)
		}
		out = agent
		return nil
	})
	if err != nil {
		return domain.Agent{}, err
	}
	return out, nil
}

func (s *PostgresStore) ResetAgentPassword(id, password string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()
	password = strings.TrimSpace(password)
	if password == "" {
		return domain.Agent{}, ErrInvalidInput
	}
	if err := validateTemporaryPassword(password); err != nil {
		return domain.Agent{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return domain.Agent{}, err
	}
	var agent domain.Agent
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `UPDATE agents SET password_hash=$2, updated_at=$3 WHERE id=$1 RETURNING `+agentColumns, id, hash, dbNow())
		updated, err := scanAgent(row)
		if err != nil {
			return mapNoRows(err)
		}
		agent = updated
		return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, id)
	})
	return agent, err
}

func (s *PostgresStore) ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error {
	ctx, cancel := dbContext()
	defer cancel()

	currentPassword = strings.TrimSpace(currentPassword)
	newPassword = strings.TrimSpace(newPassword)
	if currentPassword == "" || validateNewPassword(newPassword) != nil {
		return ErrInvalidInput
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	switch session.Kind {
	case domain.AccountAdmin:
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			admin, err := scanAdmin(tx.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE id=$1 FOR UPDATE`, session.AccountID))
			if err != nil {
				return mapNoRows(err)
			}
			if !verifyPassword(admin.Password, currentPassword) {
				return ErrInvalidCredentials
			}
			if _, err := tx.Exec(ctx, `UPDATE admin_users SET password_hash=$2 WHERE id=$1`, session.AccountID, hash); err != nil {
				return err
			}
			return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAdmin, session.AccountID)
		})
	case domain.AccountAgent:
		return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			agent, err := s.agentByIDForUpdateTx(ctx, tx, session.AccountID)
			if err != nil {
				return mapNoRows(err)
			}
			if agent.DisabledAt != nil {
				return ErrForbidden
			}
			if !verifyPassword(agent.Password, currentPassword) {
				return ErrInvalidCredentials
			}
			if _, err := tx.Exec(ctx, `UPDATE agents SET password_hash=$2, updated_at=$3 WHERE id=$1`, session.AccountID, hash, dbNow()); err != nil {
				return err
			}
			return s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, session.AccountID)
		})
	default:
		return ErrForbidden
	}
}

func (s *PostgresStore) DisableAgent(id string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		now := dbNow()
		row := tx.QueryRow(ctx, `
			UPDATE agents
			SET disabled_at=$2, status=$3, current_conversations=0, updated_at=$2
			WHERE id=$1
			RETURNING `+agentColumns, agent.ID, now, domain.AgentOffline)
		out, err = scanAgent(row)
		if err != nil {
			return err
		}
		if err := s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, agent.ID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT `+conversationColumns+`
			FROM conversations
			WHERE assigned_agent_id=$1 AND status <> $2
			ORDER BY created_at
			FOR UPDATE
		`, id, domain.ConversationClosed)
		if err != nil {
			return err
		}
		conversations, err := scanConversations(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			conversation.AssignedAgentID = ""
			// 人工已实际接待过的会话才需要重新排队；AI 接待中的会话保持原状态。
			if conversation.Status == domain.ConversationAssigned {
				conversation.Status = domain.ConversationHumanRequested
			}
			conversation.UpdatedAt = now
			if _, err := tx.Exec(ctx, `
				UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
			`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
				return err
			}
			if err := s.assignHumanConversationTx(ctx, tx, &conversation); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func (s *PostgresStore) DeleteAgent(id string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Agent
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		agent, err := s.agentByIDForUpdateTx(ctx, tx, id)
		if err != nil {
			return mapNoRows(err)
		}
		now := dbNow()
		if _, err := tx.Exec(ctx, `
			UPDATE agents
			SET status=$2, current_conversations=0, updated_at=$3
			WHERE id=$1
		`, agent.ID, domain.AgentOffline, now); err != nil {
			return err
		}
		if err := s.revokeAuthSessionsTx(ctx, tx, domain.AccountAgent, agent.ID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT `+conversationColumns+`
			FROM conversations
			WHERE assigned_agent_id=$1 AND status <> $2
			ORDER BY created_at
			FOR UPDATE
		`, id, domain.ConversationClosed)
		if err != nil {
			return err
		}
		conversations, err := scanConversations(rows)
		rows.Close()
		if err != nil {
			return err
		}
		for _, conversation := range conversations {
			conversation.AssignedAgentID = ""
			conversation.Status = domain.ConversationHumanRequested
			conversation.UpdatedAt = now
			if _, err := tx.Exec(ctx, `
				UPDATE conversations SET assigned_agent_id=NULL, status=$2, updated_at=$3 WHERE id=$1
			`, conversation.ID, conversation.Status, conversation.UpdatedAt); err != nil {
				return err
			}
			if err := s.assignHumanConversationTx(ctx, tx, &conversation); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET assigned_agent_id=NULL, updated_at=$2 WHERE assigned_agent_id=$1`, agent.ID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE service_ratings SET assigned_agent_id='' WHERE assigned_agent_id=$1`, agent.ID); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `DELETE FROM agents WHERE id=$1 RETURNING `+agentColumns, agent.ID)
		out, err = scanAgent(row)
		return err
	})
	return out, err
}

func (s *PostgresStore) adminByAccount(ctx context.Context, account string) (domain.AdminUser, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE account=$1`, strings.TrimSpace(account))
	return scanAdmin(row)
}

func (s *PostgresStore) adminByID(ctx context.Context, id string) (domain.AdminUser, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admin_users WHERE id=$1`, id)
	return scanAdmin(row)
}

func (s *PostgresStore) agentByAccount(ctx context.Context, account string) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE account=$1`, strings.TrimSpace(account))
	return scanAgent(row)
}

func (s *PostgresStore) agentByID(ctx context.Context, id string) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1`, id)
	return scanAgent(row)
}

func (s *PostgresStore) agentByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id string) (domain.Agent, error) {
	row := tx.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1 FOR UPDATE`, id)
	return scanAgent(row)
}

func (s *PostgresStore) conversationByID(ctx context.Context, id string) (domain.Conversation, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1`, id)
	conversation, err := scanConversation(row)
	if err != nil {
		return conversation, err
	}
	return s.fillConversationAgent(ctx, s.pool, conversation)
}

func (s *PostgresStore) conversationByIDForUpdateTx(ctx context.Context, tx pgx.Tx, id string) (domain.Conversation, error) {
	row := tx.QueryRow(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id=$1 FOR UPDATE`, id)
	conversation, err := scanConversation(row)
	if err != nil {
		return conversation, err
	}
	return s.fillConversationAgent(ctx, tx, conversation)
}

// fillConversationAgent 为单个会话补齐只读的客服名称/头像，序列化路径统一走这里保证访客能拿到真实客服信息。
func (s *PostgresStore) fillConversationAgent(ctx context.Context, exec dbTx, conversation domain.Conversation) (domain.Conversation, error) {
	conversations := []domain.Conversation{conversation}
	if err := s.fillConversationAgents(ctx, exec, conversations); err != nil {
		return conversation, err
	}
	return conversations[0], nil
}

func (s *PostgresStore) nextWaitingConversationForUpdateTx(ctx context.Context, tx pgx.Tx) (domain.Conversation, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+conversationColumns+`
		FROM conversations
		WHERE assigned_agent_id IS NULL AND status IN ($1, $2, $3)
			AND id NOT IN (SELECT conversation_id FROM conversation_queue)
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.ConversationWaiting, domain.ConversationHumanRequested, domain.ConversationAIServing)
	return scanConversation(row)
}

func (s *PostgresStore) createAuth(ctx context.Context, exec dbTx, kind domain.AccountKind, accountID string, ttl time.Duration) (domain.AuthSession, error) {
	now := dbNow()
	prefix := "a_"
	if kind == domain.AccountVisitor {
		prefix = "v_"
	}
	session := domain.AuthSession{
		Token:     prefix + randomID(24),
		Kind:      kind,
		AccountID: accountID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	_, err := exec.Exec(ctx, `
		INSERT INTO auth_sessions (token, kind, account_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.Token, session.Kind, session.AccountID, session.CreatedAt, session.ExpiresAt)
	return session, err
}

func (s *PostgresStore) revokeAuthSessionsTx(ctx context.Context, exec dbTx, kind domain.AccountKind, accountID string) error {
	_, err := exec.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at=now()
		WHERE kind=$1 AND account_id=$2 AND revoked_at IS NULL
	`, kind, accountID)
	return err
}

func (s *PostgresStore) auth(token string, visitor bool) (domain.AuthSession, bool) {
	ctx, cancel := dbContext()
	defer cancel()
	kinds := []domain.AccountKind{domain.AccountAdmin, domain.AccountAgent}
	if visitor {
		kinds = []domain.AccountKind{domain.AccountVisitor}
	}
	row := s.pool.QueryRow(ctx, `
		SELECT token, kind, account_id, created_at, expires_at
		FROM auth_sessions
		WHERE token=$1 AND kind=ANY($2) AND revoked_at IS NULL AND expires_at > now()
	`, strings.TrimSpace(token), kinds)
	var session domain.AuthSession
	if err := row.Scan(&session.Token, &session.Kind, &session.AccountID, &session.CreatedAt, &session.ExpiresAt); err != nil {
		return domain.AuthSession{}, false
	}
	return session, true
}

func (s *PostgresStore) assignConversationTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation) error {
	ai, err := s.aiSettingsTx(ctx, tx)
	if err != nil {
		return err
	}
	now := dbNow()
	if ai.Mode == domain.AIModeAlwaysAI {
		conversation.Status = domain.ConversationAIServing
		conversation.UpdatedAt = now
		return s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": now})
	}
	return s.assignHumanConversationTx(ctx, tx, conversation)
}

func (s *PostgresStore) assignHumanConversationTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation) error {
	if conversation.AssignedAgentID != "" {
		return nil
	}
	// 已进入排队队列的会话由应用层按 priority DESC/enqueued_at ASC 出队分配，
	// 这里跳过以免静默分配打乱排队顺序。
	var queued bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_queue WHERE conversation_id=$1)`, conversation.ID).Scan(&queued); err != nil {
		return err
	}
	if queued {
		return nil
	}
	// 非营业时间不自动分配人工：AI 继续接待，访客留言由人工在营业时间内跟进。
	business, err := s.businessHoursTx(ctx, tx)
	if err != nil {
		return err
	}
	if !businessOpen(business, dbNow()) {
		return nil
	}
	now := dbNow()
	row := tx.QueryRow(ctx, `
		SELECT `+agentColumns+`
		FROM agents
		WHERE status=$1
			AND disabled_at IS NULL
			AND current_conversations < max_conversations
		ORDER BY current_conversations ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.AgentOnline)
	agent, err := scanAgent(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2 WHERE id=$1`, agent.ID, now); err != nil {
		return err
	}
	conversation.AssignedAgentID = agent.ID
	conversation.AssignedAgentName = agent.Name
	conversation.AssignedAgentAvatar = agent.AvatarURL
	conversation.UpdatedAt = now
	// 静默分配：只记录客服，不改会话状态。客服实际回复后状态才变为 assigned（无痕转人工）。
	return s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{
		"assigned_agent_id": conversation.AssignedAgentID,
		"updated_at":        now,
	})
}

func (s *PostgresStore) selectTransferTargetTx(ctx context.Context, tx pgx.Tx, agentID, group string) (domain.Agent, error) {
	agentID = strings.TrimSpace(agentID)
	group = strings.TrimSpace(group)
	if agentID != "" {
		row := tx.QueryRow(ctx, `SELECT `+agentColumns+` FROM agents WHERE id=$1 AND disabled_at IS NULL FOR UPDATE`, agentID)
		return scanAgent(row)
	}
	row := tx.QueryRow(ctx, `
		SELECT `+agentColumns+`
		FROM agents
		WHERE disabled_at IS NULL
			AND status=$1
			AND current_conversations < max_conversations
			AND ($2 = '' OR group_name = $2)
		ORDER BY current_conversations ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, domain.AgentOnline, group)
	return scanAgent(row)
}

func (s *PostgresStore) createMessageTx(ctx context.Context, tx pgx.Tx, conversationID, clientMsgID string, senderType domain.SenderType, senderID string, messageType domain.MessageType, content string, translations map[string]string) (domain.Message, error) {
	return s.createMessageWithKnowledgeTx(ctx, tx, conversationID, clientMsgID, senderType, senderID, messageType, content, translations, nil)
}

// createMessageWithKnowledgeTx 在 createMessageTx 基础上附带"回答依据"知识条目 ID。
func (s *PostgresStore) createMessageWithKnowledgeTx(ctx context.Context, tx pgx.Tx, conversationID, clientMsgID string, senderType domain.SenderType, senderID string, messageType domain.MessageType, content string, translations map[string]string, knowledgeEntryIDs []string) (domain.Message, error) {
	msg := domain.Message{
		ServerMsgID:       "msg_" + randomID(12),
		ClientMsgID:       strings.TrimSpace(clientMsgID),
		ConversationID:    conversationID,
		SenderType:        senderType,
		SenderID:          senderID,
		MessageType:       messageType,
		Content:           strings.TrimSpace(content),
		Translations:      cleanTranslations(translations),
		KnowledgeEntryIDs: cleanKnowledgeBaseIDs(knowledgeEntryIDs),
		CreatedAt:         dbNow(),
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO messages (server_msg_id, client_msg_id, conversation_id, sender_type, sender_id, message_type, content, translations, knowledge_entry_ids, created_at, revoked_at, revoked_by_kind, revoked_by_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, NULL, '', '')
		RETURNING `+messageColumns, msg.ServerMsgID, msg.ClientMsgID, msg.ConversationID, msg.SenderType, msg.SenderID, msg.MessageType, msg.Content, marshalTranslationsJSON(msg.Translations), marshalStringListJSON(msg.KnowledgeEntryIDs), msg.CreatedAt)
	return scanMessage(row)
}

func (s *PostgresStore) findMessageByClientIDTx(ctx context.Context, tx pgx.Tx, conversationID string, senderType domain.SenderType, senderID, clientMsgID string) (domain.Message, bool, error) {
	clientMsgID = strings.TrimSpace(clientMsgID)
	if clientMsgID == "" {
		return domain.Message{}, false, nil
	}
	row := tx.QueryRow(ctx, `
		SELECT `+messageColumns+`
		FROM messages
		WHERE conversation_id=$1 AND sender_type=$2 AND sender_id=$3 AND client_msg_id=$4
	`, conversationID, senderType, senderID, clientMsgID)
	msg, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Message{}, false, nil
		}
		return domain.Message{}, false, err
	}
	return msg, true, nil
}

func (s *PostgresStore) touchConversationTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, content string, fromVisitor bool, at time.Time) error {
	now := at.UTC().Truncate(time.Microsecond)
	conversation.LastMessage = strings.TrimSpace(content)
	conversation.LastMessageAt = &now
	conversation.UpdatedAt = now
	if fromVisitor {
		conversation.UnreadForAgent++
		row := tx.QueryRow(ctx, `
			UPDATE conversations
			SET last_message=$2, last_message_at=$3, updated_at=$3, unread_for_agent=unread_for_agent+1
			WHERE id=$1
			RETURNING `+conversationColumns, conversation.ID, conversation.LastMessage, now)
		updated, err := scanConversation(row)
		if err != nil {
			return err
		}
		*conversation = updated
		return nil
	}
	conversation.UnreadForVisitor++
	conversation.UnreadForAgent = 0
	row := tx.QueryRow(ctx, `
		UPDATE conversations
		SET last_message=$2, last_message_at=$3, updated_at=$3, unread_for_visitor=unread_for_visitor+1, unread_for_agent=0
		WHERE id=$1
		RETURNING `+conversationColumns, conversation.ID, conversation.LastMessage, now)
	updated, err := scanConversation(row)
	if err != nil {
		return err
	}
	*conversation = updated
	return nil
}

func (s *PostgresStore) touchConversationRevokeTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, msg domain.Message, at time.Time) error {
	now := at.UTC().Truncate(time.Microsecond)
	conversation.LastMessage = "已撤回一条消息"
	conversation.LastMessageAt = &now
	conversation.UpdatedAt = now
	row := tx.QueryRow(ctx, `
		UPDATE conversations
		SET last_message=$2,
			last_message_at=$3,
			updated_at=$3,
			unread_for_visitor=GREATEST(unread_for_visitor - $4, 0)
		WHERE id=$1
		RETURNING `+conversationColumns,
		conversation.ID,
		conversation.LastMessage,
		now,
		boolToInt(msg.SenderType == domain.SenderAgent),
	)
	updated, err := scanConversation(row)
	if err != nil {
		return err
	}
	*conversation = updated
	return nil
}

func (s *PostgresStore) generateAutoRepliesTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation, messageType domain.MessageType, content string) ([]domain.Message, bool, error) {
	normalized := strings.TrimSpace(content)
	// 人工客服已实际回复（assigned）或会话已结束时，任何关键词和 AI 都不再自动回复。
	// 排队等待人工期间（human_requested）AI 继续接待，实现无痕转人工。
	if conversation.Status == domain.ConversationAssigned || conversation.Status == domain.ConversationClosed {
		return nil, false, nil
	}
	generated := []domain.Message{}
	now := dbNow()
	requestHuman := func() ([]domain.Message, bool, error) {
		// 非营业时间只提示一次留言：只有从未进入过 human_requested 状态才追加提示。
		wasHumanRequested := conversation.Status == domain.ConversationHumanRequested
		if err := s.requestHumanTx(ctx, tx, conversation); err != nil {
			return nil, false, err
		}
		business, err := s.businessHoursTx(ctx, tx)
		if err != nil {
			return nil, false, err
		}
		if !businessOpen(business, now) && conversation.AssignedAgentID == "" && !wasHumanRequested {
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", domain.MessageSystem, offlineReplyText(business), nil)
			if err != nil {
				return nil, false, err
			}
			generated = append(generated, msg)
		}
		aiSettings, err := s.aiSettingsTx(ctx, tx)
		if err != nil {
			return nil, false, err
		}
		return generated, aiSettings.Enabled && aiSettings.Mode != domain.AIModeManualOnly, nil
	}
	if messageType == domain.MessageHandoff || isHumanKeyword(normalized) {
		return requestHuman()
	}
	if normalized == "" {
		return nil, false, nil
	}

	// 关键词与 AI 的先后由策略 ai_keyword_priority 决定；判定集中在 resolveAutoReplyPath。
	// keyword_first（默认）完全保持既有行为：先关键词、命中即短路，未命中才请求 AI。
	aiSettings, err := s.aiSettingsTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	policy, err := s.servicePolicyTx(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	aiEnabled := aiSettings.Enabled && aiSettings.Mode != domain.AIModeManualOnly
	path := resolveAutoReplyPath(policy.AIKeywordPriority, aiEnabled, aiUpstreamConfigured(aiSettings))
	if path.keywordAllowed {
		rules, err := s.keywordRules(ctx, tx)
		if err != nil {
			return nil, false, err
		}
		for _, rule := range rules {
			if !rule.Enabled || !keywordMatches(rule, normalized) {
				continue
			}
			if rule.Action == "handoff" {
				return requestHuman()
			}
			messageType := domain.MessageText
			if rule.Action == "telephone" {
				messageType = domain.MessageContactTelephone
			}
			if rule.Action == "whatsapp" {
				messageType = domain.MessageContactWhatsApp
			}
			if rule.Action == "telegram" {
				messageType = domain.MessageContactTelegram
			}
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", messageType, rule.Reply, nil)
			if err != nil {
				return nil, false, err
			}
			return append(generated, msg), false, nil
		}
	}

	if !path.tryAI {
		// AI 不可用（未启用/manual_only/上游未配置）或模式为 keyword_only / ai_only：
		// 不请求 AI；当前非营业时间且无人接待时记录留言并进入人工队列，同样只提示一次。
		business, err := s.businessHoursTx(ctx, tx)
		if err != nil {
			return nil, false, err
		}
		if !businessOpen(business, now) && conversation.AssignedAgentID == "" && conversation.Status != domain.ConversationHumanRequested {
			conversation.Status = domain.ConversationHumanRequested
			conversation.UpdatedAt = now
			if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": now}); err != nil {
				return nil, false, err
			}
			msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", domain.MessageSystem, offlineReplyText(business), nil)
			if err != nil {
				return nil, false, err
			}
			generated = append(generated, msg)
		}
		return generated, false, nil
	}
	// AI 优先介入：无论是否在营业时间，人工实际回复前都由 AI 接待。
	if conversation.Status == domain.ConversationWaiting {
		conversation.Status = domain.ConversationAIServing
	}
	if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
		return nil, false, err
	}
	return generated, true, nil
}

// requestHumanTx 静默转人工：只改状态并分配客服，不再向访客展示任何“转接中”系统消息。
// 访客侧在客服真实回复前由 AI 继续接待，客服回复后对外显示为人工服务（无痕转人工）。
func (s *PostgresStore) requestHumanTx(ctx context.Context, tx pgx.Tx, conversation *domain.Conversation) error {
	conversation.Status = domain.ConversationHumanRequested
	if err := s.updateConversationFieldsTx(ctx, tx, conversation.ID, map[string]any{"status": conversation.Status, "updated_at": dbNow()}); err != nil {
		return err
	}
	if err := s.assignHumanConversationTx(ctx, tx, conversation); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) updateConversationFieldsTx(ctx context.Context, tx pgx.Tx, conversationID string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"assigned_agent_id": true,
		"status":            true,
		"updated_at":        true,
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if !allowed[key] {
			return fmt.Errorf("unsupported conversation field: %s", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{conversationID}
	sets := make([]string, 0, len(keys))
	for _, key := range keys {
		args = append(args, fields[key])
		if key == "assigned_agent_id" && fields[key] == "" {
			sets = append(sets, fmt.Sprintf("%s=NULL", key))
			args = args[:len(args)-1]
			continue
		}
		sets = append(sets, fmt.Sprintf("%s=$%d", key, len(args)))
	}
	query := `UPDATE conversations SET ` + strings.Join(sets, ", ") + ` WHERE id=$1`
	_, err := tx.Exec(ctx, query, args...)
	return err
}

func (s *PostgresStore) contactSettings(ctx context.Context, exec dbTx) (domain.ContactSettings, error) {
	var out domain.ContactSettings
	err := exec.QueryRow(ctx, `
		SELECT phone, wechat, wechat_reply_type, wechat_image_url, qq, qq_reply_type, qq_image_url, entry_reply
		FROM contact_settings WHERE id=1
	`).Scan(&out.Telephone, &out.WhatsApp, new(string), new(string), &out.Telegram, new(string), new(string), &out.EntryReply)
	if errors.Is(err, pgx.ErrNoRows) {
		// 不再内置任何占位联系方式：未配置时返回空，由管理员在后台填写真实信息。
		return domain.ContactSettings{}, nil
	}
	return out, err
}

func (s *PostgresStore) keywordRules(ctx context.Context, exec dbTx) ([]domain.KeywordRule, error) {
	rows, err := exec.Query(ctx, `
		SELECT id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
		FROM keyword_rules
		ORDER BY priority DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.KeywordRule{}
	for rows.Next() {
		var rule domain.KeywordRule
		if err := rows.Scan(
			&rule.ID,
			&rule.Keyword,
			&rule.MatchType,
			&rule.Reply,
			&rule.Enabled,
			&rule.Priority,
			&rule.Action,
			&rule.ShowInQuickReplies,
			&rule.QuickReplyText,
		); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (s *PostgresStore) keywordRuleByID(ctx context.Context, exec dbTx, id string) (domain.KeywordRule, error) {
	var rule domain.KeywordRule
	err := exec.QueryRow(ctx, `
		SELECT id, keyword, match_type, reply, enabled, priority, action, show_in_quick_replies, quick_reply_text
		FROM keyword_rules
		WHERE id=$1
	`, id).Scan(
		&rule.ID,
		&rule.Keyword,
		&rule.MatchType,
		&rule.Reply,
		&rule.Enabled,
		&rule.Priority,
		&rule.Action,
		&rule.ShowInQuickReplies,
		&rule.QuickReplyText,
	)
	return rule, err
}

func (s *PostgresStore) aiSettingsTx(ctx context.Context, exec dbTx) (domain.AISettings, error) {
	var out domain.AISettings
	var storedKey string
	err := exec.QueryRow(ctx, `
		SELECT enabled, mode, base_url, api_key_ciphertext, model, api_type, temperature, max_output_tokens, timeout_seconds, system_prompt
		FROM ai_settings WHERE id=1
	`).Scan(&out.Enabled, &out.Mode, &out.BaseURL, &storedKey, &out.Model, &out.APIType, &out.Temperature, &out.MaxOutputTokens, &out.TimeoutSeconds, &out.SystemPrompt)
	if errors.Is(err, pgx.ErrNoRows) {
		out = domain.AISettings{
			Enabled:         true,
			Mode:            domain.AIModeHumanFirst,
			BaseURL:         nonEmpty(s.options.OpenAIBaseURL, "https://api.openai.com/v1"),
			APIKey:          strings.TrimSpace(s.options.OpenAIAPIKey),
			Model:           nonEmpty(s.options.OpenAIModel, "gpt-4o-mini"),
			APIType:         nonEmpty(s.options.OpenAIAPIType, "chat_completions"),
			Temperature:     0.7,
			MaxOutputTokens: 512,
			TimeoutSeconds:  20,
			SystemPrompt:    defaultPrompt(),
		}
		out.APIKeyMasked = maskAPIKey(out.APIKey)
		return out, nil
	}
	apiKey, err := revealSecret(s.options.DataEncryptionKey, storedKey)
	if err != nil {
		return domain.AISettings{}, err
	}
	out.APIKey = apiKey
	out.APIKeyMasked = maskAPIKey(out.APIKey)
	return out, err
}

func (s *PostgresStore) businessHoursTx(ctx context.Context, exec dbTx) (domain.BusinessHours, error) {
	var out domain.BusinessHours
	err := exec.QueryRow(ctx, `SELECT timezone, start_time, end_time, enabled, COALESCE(offline_reply, '') FROM business_hours WHERE id=1`).
		Scan(&out.Timezone, &out.Start, &out.End, &out.Enabled, &out.OfflineReply)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BusinessHours{Timezone: "Asia/Shanghai", Start: "09:00", End: "18:00", Enabled: true}, nil
	}
	return out, err
}

func (s *PostgresStore) seedDefaults(ctx context.Context) error {
	adminHash, err := hashPassword(nonEmpty(s.options.BootstrapAdminPassword, "123456"))
	if err != nil {
		return err
	}
	agentHash, err := hashPassword(nonEmpty(s.options.BootstrapAgentPassword, "123456"))
	if err != nil {
		return err
	}
	protectedSeedKey, err := protectSecret(s.options.DataEncryptionKey, strings.TrimSpace(s.options.OpenAIAPIKey))
	if err != nil {
		return err
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{
			sql: `
				INSERT INTO admin_users (id, account, name, password_hash)
				VALUES ('admin_super', 'superadmin', '超级管理员', $1)
				ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
				WHERE admin_users.password_hash LIKE 'dev_plain:%'
			`,
			args: []any{adminHash},
		},
		{
			sql: `
				INSERT INTO agents (id, account, name, group_name, password_hash, status, max_conversations)
				VALUES ('agent_lixue', 'admin', '客服-李雪', '售前组', $1, 'offline', 10)
				ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash
				WHERE agents.password_hash LIKE 'dev_plain:%'
			`,
			args: []any{agentHash},
		},
		{
			sql: `
				INSERT INTO ai_settings (id, enabled, mode, base_url, api_key_ciphertext, model, api_type, temperature, max_output_tokens, timeout_seconds, system_prompt)
				VALUES (1, true, 'human_first', $1, $2, $3, $4, 0.70, 512, 20, $5)
				ON CONFLICT (id) DO NOTHING
			`,
			args: []any{
				nonEmpty(s.options.OpenAIBaseURL, "https://api.openai.com/v1"),
				protectedSeedKey,
				nonEmpty(s.options.OpenAIModel, "gpt-4o-mini"),
				nonEmpty(s.options.OpenAIAPIType, "chat_completions"),
				defaultPrompt(),
			},
		},
		{
			sql: `
				INSERT INTO business_hours (id, timezone, start_time, end_time, enabled)
				VALUES (1, 'Asia/Shanghai', '09:00', '18:00', true)
				ON CONFLICT (id) DO NOTHING
			`,
		},
		{
			// 历史版本内置的中文默认提示词升级为多语言版本，保证 AI 可按访客语言回复。
			sql: `
				UPDATE ai_settings SET system_prompt=$1
				WHERE id=1 AND system_prompt=$2
			`,
			args: []any{defaultPrompt(), legacyDefaultPrompt()},
		},
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) ensureServiceRatingsTable(ctx context.Context) error {
	statements := []string{
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS device_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS device_platform TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE business_hours ADD COLUMN IF NOT EXISTS offline_reply TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS push_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1,
			public_config JSONB NOT NULL DEFAULT '{}',
			encrypted_secrets TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT push_settings_singleton CHECK (id=1)
		)`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS visitor_lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS translations JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS avatar_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS manual_satisfaction_rate DOUBLE PRECISION`,
		// 客服账号级翻译偏好；历史账号读取时由 domain.AgentPreferences.WithDefaults 填充默认值。
		`ALTER TABLE agents ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS wechat_reply_type TEXT NOT NULL DEFAULT 'image'`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS wechat_image_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS qq_reply_type TEXT NOT NULL DEFAULT 'text'`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS qq_image_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings ADD COLUMN IF NOT EXISTS entry_reply TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE contact_settings DROP CONSTRAINT IF EXISTS contact_settings_wechat_reply_type_check`,
		`ALTER TABLE contact_settings ADD CONSTRAINT contact_settings_wechat_reply_type_check CHECK (wechat_reply_type IN ('text', 'image'))`,
		`ALTER TABLE contact_settings DROP CONSTRAINT IF EXISTS contact_settings_qq_reply_type_check`,
		`ALTER TABLE contact_settings ADD CONSTRAINT contact_settings_qq_reply_type_check CHECK (qq_reply_type IN ('text', 'image'))`,
		// 清理历史版本内置的假联系方式与假关键词规则：只清理与内置假值完全一致的数据，绝不动管理员配置的真实信息。
		// 不再每次启动都把管理员清空的欢迎语写回。
		`UPDATE contact_settings SET phone='' WHERE id=1 AND phone='400-123-4567'`,
		`UPDATE contact_settings SET wechat='' WHERE id=1 AND wechat='+86 400 888 8888'`,
		`UPDATE contact_settings SET qq='' WHERE id=1 AND qq='@AstrevaSupport'`,
		`DELETE FROM keyword_rules WHERE (id='kw_phone' AND reply='客服电话：400-123-4567') OR (id='kw_whatsapp' AND reply='官方 WhatsApp：+86 400 888 8888') OR (id='kw_telegram' AND reply='官方 Telegram：@AstrevaSupport')`,
		`ALTER TABLE keyword_rules ADD COLUMN IF NOT EXISTS show_in_quick_replies BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE keyword_rules ADD COLUMN IF NOT EXISTS quick_reply_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE keyword_rules DROP CONSTRAINT IF EXISTS keyword_rules_match_type_check`,
		`ALTER TABLE keyword_rules ADD CONSTRAINT keyword_rules_match_type_check CHECK (match_type IN ('contains', 'exact', 'prefix'))`,
		`ALTER TABLE keyword_rules DROP CONSTRAINT IF EXISTS keyword_rules_action_check`,
		`UPDATE keyword_rules SET action='telephone' WHERE action='phone'`,
		`UPDATE keyword_rules SET action='whatsapp' WHERE action='wechat'`,
		`ALTER TABLE keyword_rules ADD CONSTRAINT keyword_rules_action_check CHECK (action IN ('text', 'telephone', 'whatsapp', 'telegram', 'handoff'))`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_by_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN IF NOT EXISTS revoked_by_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_message_type_check`,
		`ALTER TABLE messages ADD CONSTRAINT messages_message_type_check CHECK (message_type IN ('text', 'emoji', 'image', 'audio', 'contact_phone', 'contact_wechat', 'contact_telephone', 'contact_whatsapp', 'contact_telegram', 'system', 'ai_text', 'handoff_request', 'revoked'))`,
		`ALTER TABLE ai_settings DROP CONSTRAINT IF EXISTS ai_settings_api_type_check`,
		`ALTER TABLE ai_settings ADD CONSTRAINT ai_settings_api_type_check CHECK (api_type IN ('chat_completions', 'responses', 'anthropic_messages'))`,
		`CREATE TABLE IF NOT EXISTS ai_cache_probe (
			id INTEGER PRIMARY KEY CHECK (id=1),
			model_results JSONB NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			probed_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT ai_cache_probe_status_check CHECK (status IN ('', 'running', 'done', 'error'))
		)`,
		`INSERT INTO ai_cache_probe (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
		`ALTER TABLE ai_settings DROP COLUMN IF EXISTS agent_no_reply_timeout_seconds`,
		`CREATE TABLE IF NOT EXISTS service_ratings (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
			visitor_id TEXT NOT NULL REFERENCES visitors(id),
			assigned_agent_id TEXT,
			score INTEGER NOT NULL,
			tags TEXT[] NOT NULL DEFAULT '{}',
			comment TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT service_ratings_score_check CHECK (score BETWEEN 1 AND 5)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_service_ratings_created ON service_ratings(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_service_ratings_agent ON service_ratings(assigned_agent_id) WHERE assigned_agent_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_kind TEXT NOT NULL,
			actor_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource TEXT NOT NULL DEFAULT '',
			resource_id TEXT NOT NULL DEFAULT '',
			ip_address TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT audit_events_actor_kind_check CHECK (actor_kind IN ('admin', 'agent', 'visitor'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_kind, actor_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_resource ON audit_events(resource, resource_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS app_releases (
			id TEXT PRIMARY KEY,
			platform TEXT NOT NULL,
			version_name TEXT NOT NULL,
			version_code BIGINT NOT NULL,
			file_name TEXT NOT NULL DEFAULT '',
			file_path TEXT NOT NULL,
			download_url TEXT NOT NULL DEFAULT '',
			file_size BIGINT NOT NULL DEFAULT 0,
			checksum TEXT NOT NULL DEFAULT '',
			release_notes TEXT NOT NULL DEFAULT '',
			force_update BOOLEAN NOT NULL DEFAULT false,
			enabled BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT app_releases_platform_check CHECK (platform IN ('android', 'ios')),
			UNIQUE (platform, version_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_app_releases_platform ON app_releases(platform, version_code DESC)`,
		`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS visitor_ip_source TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS api_apps (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			enabled BOOLEAN NOT NULL DEFAULT true,
			rate_limit INTEGER NOT NULL DEFAULT 0,
			scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
			note TEXT NOT NULL DEFAULT '',
			request_count BIGINT NOT NULL DEFAULT 0,
			last_used_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			CONSTRAINT api_apps_rate_limit_check CHECK (rate_limit >= 0)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_apps_created ON api_apps(created_at DESC)`,
		// 历史库补列：空数组 = 不受限（等同全部范围），既有应用行为不变。
		`ALTER TABLE api_apps ADD COLUMN IF NOT EXISTS scopes JSONB NOT NULL DEFAULT '[]'::jsonb`,
	}
	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func scanAdmin(row scanTarget) (domain.AdminUser, error) {
	var out domain.AdminUser
	err := row.Scan(&out.ID, &out.Account, &out.Name, &out.Password, &out.Created)
	return out, err
}

func scanAgent(row scanTarget) (domain.Agent, error) {
	var out domain.Agent
	var disabled sql.NullTime
	var manualRate sql.NullFloat64
	var preferencesJSON string
	err := row.Scan(
		&out.ID,
		&out.Account,
		&out.Name,
		&out.Group,
		&out.Password,
		&out.Status,
		&out.DeviceToken,
		&out.DevicePlatform,
		&out.MaxConversations,
		&out.CurrentConversations,
		&disabled,
		&out.Created,
		&out.Updated,
		&out.AvatarURL,
		&manualRate,
		&preferencesJSON,
	)
	if err != nil {
		return out, err
	}
	if disabled.Valid {
		out.DisabledAt = &disabled.Time
	}
	if manualRate.Valid {
		rate := manualRate.Float64
		out.ManualSatisfactionRate = &rate
	}
	if trimmed := strings.TrimSpace(preferencesJSON); trimmed != "" && trimmed != "{}" {
		if err := json.Unmarshal([]byte(trimmed), &out.Preferences); err != nil {
			return out, fmt.Errorf("decode agent preferences: %w", err)
		}
	}
	return out, nil
}

func scanAgents(rows pgx.Rows) ([]domain.Agent, error) {
	result := []domain.Agent{}
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, agent)
	}
	return result, rows.Err()
}

func scanConversation(row scanTarget) (domain.Conversation, error) {
	var out domain.Conversation
	var remarkUpdatedAt sql.NullTime
	var lastMessageAt sql.NullTime
	var replyLangOverrideAt sql.NullTime
	var firstResponseAt sql.NullTime
	var lastAgentReplyAt sql.NullTime
	var unattendedNotifiedAt sql.NullTime
	var languageStatsJSON string
	err := row.Scan(
		&out.ID,
		&out.VisitorID,
		&out.VisitorIP,
		&out.VisitorIPSource,
		&out.VisitorRemark,
		&out.RemarkUpdatedBy,
		&remarkUpdatedAt,
		&out.Status,
		&out.AssignedAgentID,
		&out.Source,
		&out.VisitorLang,
		&out.DetectedLang,
		&out.ReplyLangOverride,
		&replyLangOverrideAt,
		&languageStatsJSON,
		&out.LastMessage,
		&lastMessageAt,
		&out.UnreadForAgent,
		&out.UnreadForVisitor,
		&firstResponseAt,
		&lastAgentReplyAt,
		&out.SLAState,
		&out.QueueGroup,
		&unattendedNotifiedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if remarkUpdatedAt.Valid {
		out.RemarkUpdatedAt = &remarkUpdatedAt.Time
	}
	if lastMessageAt.Valid {
		out.LastMessageAt = &lastMessageAt.Time
	}
	if replyLangOverrideAt.Valid {
		out.ReplyLangOverrideAt = &replyLangOverrideAt.Time
	}
	if firstResponseAt.Valid {
		out.FirstResponseAt = &firstResponseAt.Time
	}
	if lastAgentReplyAt.Valid {
		out.LastAgentReplyAt = &lastAgentReplyAt.Time
	}
	if unattendedNotifiedAt.Valid {
		out.UnattendedNotifiedAt = &unattendedNotifiedAt.Time
	}
	if stats := parseLanguageStatsJSON(languageStatsJSON); len(stats) > 0 {
		out.LanguageStats = stats
	}
	return out, err
}

// parseLanguageStatsJSON 解析 conversations.language_stats 列。
func parseLanguageStatsJSON(raw string) map[string]int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	stats := map[string]int{}
	if err := json.Unmarshal([]byte(trimmed), &stats); err != nil {
		return nil
	}
	return stats
}

func scanConversations(rows pgx.Rows) ([]domain.Conversation, error) {
	result := []domain.Conversation{}
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, conversation)
	}
	return result, rows.Err()
}

// fillConversationAgents 批量补齐会话的只读客服名称/头像：按 assigned_agent_id 去重后一次查询，避免逐条 N+1。
func (s *PostgresStore) fillConversationAgents(ctx context.Context, exec dbTx, conversations []domain.Conversation) error {
	ids := make([]string, 0, len(conversations))
	seen := map[string]bool{}
	for _, conversation := range conversations {
		if conversation.AssignedAgentID == "" || seen[conversation.AssignedAgentID] {
			continue
		}
		seen[conversation.AssignedAgentID] = true
		ids = append(ids, conversation.AssignedAgentID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := exec.Query(ctx, `SELECT id, name, COALESCE(avatar_url, '') FROM agents WHERE id=ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type agentBrief struct{ name, avatar string }
	agents := map[string]agentBrief{}
	for rows.Next() {
		var id string
		var brief agentBrief
		if err := rows.Scan(&id, &brief.name, &brief.avatar); err != nil {
			return err
		}
		agents[id] = brief
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range conversations {
		if brief, ok := agents[conversations[i].AssignedAgentID]; ok {
			conversations[i].AssignedAgentName = brief.name
			conversations[i].AssignedAgentAvatar = brief.avatar
		}
	}
	return nil
}

func scanMessage(row scanTarget) (domain.Message, error) {
	var out domain.Message
	var revokedAt sql.NullTime
	var revokedByKind sql.NullString
	var revokedByID sql.NullString
	var translationsJSON string
	var knowledgeEntryIDsJSON string
	err := row.Scan(&out.ServerMsgID, &out.ClientMsgID, &out.ConversationID, &out.SenderType, &out.SenderID, &out.MessageType, &out.Content, &translationsJSON, &out.CreatedAt, &revokedAt, &revokedByKind, &revokedByID, &out.DetectedLang, &out.AuthorLang, &out.TranslationStatus, &knowledgeEntryIDsJSON)
	out.Translations = parseTranslationsJSON(translationsJSON)
	out.KnowledgeEntryIDs = parseStringListJSON(knowledgeEntryIDsJSON)
	if revokedAt.Valid {
		out.RevokedAt = &revokedAt.Time
	}
	if revokedByKind.Valid {
		out.RevokedByKind = domain.AccountKind(revokedByKind.String)
	}
	if revokedByID.Valid {
		out.RevokedByID = revokedByID.String
	}
	return out, err
}

func scanMessages(rows pgx.Rows) ([]domain.Message, error) {
	result := []domain.Message{}
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, msg)
	}
	return result, rows.Err()
}

// parseTranslationsJSON 解析 messages.translations JSONB 列。
func parseTranslationsJSON(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return cleanTranslations(out)
}

// marshalTranslationsJSON 把译文 map 序列化为 JSONB 参数。
func marshalTranslationsJSON(translations map[string]string) string {
	translations = cleanTranslations(translations)
	if len(translations) == 0 {
		return "{}"
	}
	payload, err := json.Marshal(translations)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

// parseStringListJSON 解析 JSONB 字符串数组列（keywords、knowledge_entry_ids 等）。
func parseStringListJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	out := []string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return cleanKnowledgeBaseIDs(out)
}

// marshalStringListJSON 把字符串切片序列化为 JSONB 参数。
func marshalStringListJSON(values []string) string {
	values = cleanKnowledgeBaseIDs(values)
	if len(values) == 0 {
		return "[]"
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func scanRating(row scanTarget) (domain.ServiceRating, error) {
	var out domain.ServiceRating
	err := row.Scan(
		&out.ID,
		&out.ConversationID,
		&out.VisitorID,
		&out.AssignedAgentID,
		&out.Score,
		&out.Tags,
		&out.Comment,
		&out.CreatedAt,
	)
	return out, err
}

func scanRatings(rows pgx.Rows) ([]domain.ServiceRating, error) {
	result := []domain.ServiceRating{}
	for rows.Next() {
		rating, err := scanRating(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rating)
	}
	return result, rows.Err()
}

func scanAuditEvent(row scanTarget) (domain.AuditEvent, error) {
	var out domain.AuditEvent
	err := row.Scan(
		&out.ID,
		&out.ActorKind,
		&out.ActorID,
		&out.Action,
		&out.Resource,
		&out.ResourceID,
		&out.IPAddress,
		&out.UserAgent,
		&out.Description,
		&out.CreatedAt,
	)
	return out, err
}

func scanAuditEvents(rows pgx.Rows) ([]domain.AuditEvent, error) {
	result := []domain.AuditEvent{}
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func dbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultDBTimeout)
}

func dbNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func ipForDB(value string) string {
	value = normalizeIP(value)
	if net.ParseIP(value) == nil {
		return "0.0.0.0"
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isBusinessTime(business domain.BusinessHours, now time.Time) bool {
	location, err := time.LoadLocation(business.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	local := now.In(location)
	start, err1 := time.Parse("15:04", business.Start)
	end, err2 := time.Parse("15:04", business.End)
	if err1 != nil || err2 != nil {
		return true
	}
	currentMinutes := local.Hour()*60 + local.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()
	if startMinutes <= endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

// defaultOfflineReply 是非营业时间未配置留言提示时的内置兜底文案。
const defaultOfflineReply = "当前非工作时间，已记录您的留言，我们会尽快回复。"

// businessOpen 判断营业时间是否放行自动分配人工：开关关闭表示不限制。
func businessOpen(business domain.BusinessHours, now time.Time) bool {
	if !business.Enabled {
		return true
	}
	return isBusinessTime(business, now)
}

// offlineReplyText 返回非营业时间给访客的留言提示，未配置时使用内置默认文案。
func offlineReplyText(business domain.BusinessHours) string {
	if reply := strings.TrimSpace(business.OfflineReply); reply != "" {
		return reply
	}
	return defaultOfflineReply
}

const appReleaseColumns = `id, platform, version_name, version_code, file_name, file_path, download_url, file_size, checksum, release_notes, force_update, enabled, created_at`

func (s *PostgresStore) AppReleases(platform string) []domain.AppRelease {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	query := `SELECT ` + appReleaseColumns + ` FROM app_releases`
	args := []any{}
	if platform != "" {
		query += ` WHERE platform = $1`
		args = append(args, platform)
	}
	query += ` ORDER BY version_code DESC, created_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return []domain.AppRelease{}
	}
	defer rows.Close()
	result := []domain.AppRelease{}
	for rows.Next() {
		var release domain.AppRelease
		if err := rows.Scan(
			&release.ID, &release.Platform, &release.VersionName, &release.VersionCode,
			&release.FileName, &release.FilePath, &release.DownloadURL, &release.FileSize, &release.Checksum,
			&release.ReleaseNotes, &release.ForceUpdate, &release.Enabled, &release.CreatedAt,
		); err != nil {
			return []domain.AppRelease{}
		}
		result = append(result, release)
	}
	return result
}

func (s *PostgresStore) AppReleaseByID(id string) (domain.AppRelease, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	var release domain.AppRelease
	err := s.pool.QueryRow(ctx, `SELECT `+appReleaseColumns+` FROM app_releases WHERE id = $1`, id).Scan(
		&release.ID, &release.Platform, &release.VersionName, &release.VersionCode,
		&release.FileName, &release.FilePath, &release.FileSize, &release.Checksum,
		&release.ReleaseNotes, &release.ForceUpdate, &release.Enabled, &release.CreatedAt,
	)
	if err != nil {
		return domain.AppRelease{}, false
	}
	return release, true
}

func (s *PostgresStore) CreateAppRelease(next domain.AppRelease) (domain.AppRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	next.Platform = strings.TrimSpace(next.Platform)
	next.VersionName = strings.TrimSpace(next.VersionName)
	if next.Platform != "android" && next.Platform != "ios" {
		return domain.AppRelease{}, ErrInvalidInput
	}
	if next.VersionName == "" || next.VersionCode <= 0 || strings.TrimSpace(next.FilePath) == "" {
		return domain.AppRelease{}, ErrInvalidInput
	}
	if next.ID == "" {
		next.ID = "rel_" + randomID(10)
	}
	if next.CreatedAt.IsZero() {
		next.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_releases (`+appReleaseColumns+`)
		VALUES (, , , , , , , , , , , , )
	`, next.ID, next.Platform, next.VersionName, next.VersionCode, next.FileName, next.FilePath,
		next.DownloadURL, next.FileSize, next.Checksum, next.ReleaseNotes, next.ForceUpdate, next.Enabled, next.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.AppRelease{}, ErrConflict
		}
		return domain.AppRelease{}, err
	}
	return next, nil
}

func (s *PostgresStore) UpdateAppRelease(id string, next domain.AppRelease) (domain.AppRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	current, ok := s.AppReleaseByID(id)
	if !ok {
		return domain.AppRelease{}, ErrNotFound
	}
	if strings.TrimSpace(next.VersionName) != "" {
		current.VersionName = strings.TrimSpace(next.VersionName)
	}
	if next.VersionCode > 0 {
		current.VersionCode = next.VersionCode
	}
	if strings.TrimSpace(next.ReleaseNotes) != "" {
		current.ReleaseNotes = strings.TrimSpace(next.ReleaseNotes)
	}
	current.ForceUpdate = next.ForceUpdate
	current.Enabled = next.Enabled
	_, err := s.pool.Exec(ctx, `
		UPDATE app_releases
		SET version_name = $2, version_code = $3, release_notes = $4, force_update = $5, enabled = $6
		WHERE id = $1
	`, id, current.VersionName, current.VersionCode, current.ReleaseNotes, current.ForceUpdate, current.Enabled)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.AppRelease{}, ErrConflict
		}
		return domain.AppRelease{}, err
	}
	return current, nil
}

func (s *PostgresStore) DeleteAppRelease(id string) (domain.AppRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	current, ok := s.AppReleaseByID(id)
	if !ok {
		return domain.AppRelease{}, ErrNotFound
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM app_releases WHERE id = $1`, id); err != nil {
		return domain.AppRelease{}, err
	}
	return current, nil
}

func (s *PostgresStore) RegisterAgentDeviceToken(agentID, deviceToken, platform string) (domain.Agent, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agent, err := s.agentByID(ctx, agentID)
	if err != nil {
		return domain.Agent{}, mapNoRows(err)
	}
	if agent.DisabledAt != nil {
		return domain.Agent{}, ErrForbidden
	}
	token := strings.TrimSpace(deviceToken)
	normalizedPlatform := normalizePushPlatform(platform, token)
	if _, err := s.pool.Exec(ctx, `UPDATE agents SET device_token=$2, device_platform=$3, updated_at=$4 WHERE id=$1`, agentID, token, normalizedPlatform, dbNow()); err != nil {
		return domain.Agent{}, err
	}
	agent.DeviceToken = token
	agent.DevicePlatform = normalizedPlatform
	return agent, nil
}

// AgentPreferences 读取客服账号级偏好；未设置过的字段按默认值返回。
func (s *PostgresStore) AgentPreferences(agentID string) (domain.AgentPreferences, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agent, err := s.agentByID(ctx, strings.TrimSpace(agentID))
	if err != nil {
		return domain.AgentPreferences{}, mapNoRows(err)
	}
	return agent.Preferences.WithDefaults(), nil
}

// UpdateAgentPreferences 覆盖保存客服偏好，非法方言模式归一为 standard。
func (s *PostgresStore) UpdateAgentPreferences(agentID string, next domain.AgentPreferences) (domain.AgentPreferences, error) {
	ctx, cancel := dbContext()
	defer cancel()

	encoded, err := json.Marshal(next.WithDefaults())
	if err != nil {
		return domain.AgentPreferences{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE agents SET preferences=$2::jsonb, updated_at=$3
		WHERE id=$1
		RETURNING `+agentColumns, strings.TrimSpace(agentID), string(encoded), dbNow())
	agent, err := scanAgent(row)
	if err != nil {
		return domain.AgentPreferences{}, mapNoRows(err)
	}
	return agent.Preferences.WithDefaults(), nil
}

const apiAppColumns = `id, name, key_prefix, key_hash, enabled, rate_limit, COALESCE(scopes::text, '[]'), note, request_count, last_used_at, created_at, updated_at`

func scanAPIApp(row scanTarget) (domain.APIApp, error) {
	var out domain.APIApp
	var lastUsed sql.NullTime
	var scopesJSON string
	err := row.Scan(&out.ID, &out.Name, &out.KeyPrefix, &out.KeyHash, &out.Enabled, &out.RateLimit, &scopesJSON, &out.Note, &out.RequestCount, &lastUsed, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.APIApp{}, err
	}
	if lastUsed.Valid {
		out.LastUsedAt = &lastUsed.Time
	}
	// 空数组（或历史库的 '[]'）表示「不受限」，兼容既有应用；统一返回非 nil 空切片。
	if parsed := parseStringListJSON(scopesJSON); len(parsed) > 0 {
		out.Scopes = parsed
	} else {
		out.Scopes = []string{}
	}
	return out, nil
}

func (s *PostgresStore) APIApps() []domain.APIApp {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+apiAppColumns+` FROM api_apps ORDER BY created_at DESC`)
	if err != nil {
		return []domain.APIApp{}
	}
	defer rows.Close()
	result := []domain.APIApp{}
	for rows.Next() {
		app, err := scanAPIApp(rows)
		if err != nil {
			return []domain.APIApp{}
		}
		result = append(result, app)
	}
	return result
}

func (s *PostgresStore) CreateAPIApp(next domain.APIApp) (domain.APIApp, error) {
	ctx, cancel := dbContext()
	defer cancel()

	if err := validateAPIAppInput(&next); err != nil {
		return domain.APIApp{}, err
	}
	if next.ID == "" {
		next.ID = "api_" + randomID(10)
	}
	now := dbNow()
	next.CreatedAt = now
	next.UpdatedAt = now
	next.RequestCount = 0
	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_apps (id, name, key_prefix, key_hash, enabled, rate_limit, scopes, note, request_count, last_used_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, 0, NULL, $9, $10)
	`, next.ID, next.Name, next.KeyPrefix, next.KeyHash, next.Enabled, next.RateLimit, marshalStringListJSON(next.Scopes), next.Note, next.CreatedAt, next.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.APIApp{}, ErrConflict
		}
		return domain.APIApp{}, err
	}
	return next, nil
}

func (s *PostgresStore) UpdateAPIApp(id string, next domain.APIApp) (domain.APIApp, error) {
	ctx, cancel := dbContext()
	defer cancel()

	current, ok := s.APIAppByID(ctx, id)
	if !ok {
		return domain.APIApp{}, ErrNotFound
	}
	if name := strings.TrimSpace(next.Name); name != "" {
		if _, err := cleanRequiredText(name, maxNameRunes); err != nil {
			return domain.APIApp{}, err
		}
		current.Name = name
	}
	if next.RateLimit < 0 {
		return domain.APIApp{}, ErrInvalidInput
	}
	current.RateLimit = next.RateLimit
	note, err := cleanOptionalText(next.Note, maxRemarkRunes)
	if err != nil {
		return domain.APIApp{}, err
	}
	current.Note = note
	current.Enabled = next.Enabled
	// 空数组表示「不受限」（等同全部范围），非空则自动补齐 me:read。
	scopes, err := domain.NormalizeAPIScopes(next.Scopes)
	if err != nil {
		return domain.APIApp{}, ErrInvalidInput
	}
	current.Scopes = scopes
	if strings.TrimSpace(next.KeyHash) != "" {
		current.KeyHash = strings.TrimSpace(next.KeyHash)
		current.KeyPrefix = strings.TrimSpace(next.KeyPrefix)
	}
	current.UpdatedAt = dbNow()
	if _, err := s.pool.Exec(ctx, `
		UPDATE api_apps SET name=$2, rate_limit=$3, note=$4, enabled=$5, key_hash=$6, key_prefix=$7, scopes=$8::jsonb, updated_at=$9
		WHERE id=$1
	`, current.ID, current.Name, current.RateLimit, current.Note, current.Enabled, current.KeyHash, current.KeyPrefix, marshalStringListJSON(current.Scopes), current.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return domain.APIApp{}, ErrConflict
		}
		return domain.APIApp{}, err
	}
	return current, nil
}

func (s *PostgresStore) DeleteAPIApp(id string) error {
	ctx, cancel := dbContext()
	defer cancel()

	tag, err := s.pool.Exec(ctx, `DELETE FROM api_apps WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) APIAppByKeyHash(keyHash string) (domain.APIApp, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return domain.APIApp{}, false
	}
	row := s.pool.QueryRow(ctx, `SELECT `+apiAppColumns+` FROM api_apps WHERE key_hash = $1`, keyHash)
	app, err := scanAPIApp(row)
	if err != nil {
		return domain.APIApp{}, false
	}
	return app, true
}

func (s *PostgresStore) TouchAPIApp(id string) {
	ctx, cancel := dbContext()
	defer cancel()

	_, _ = s.pool.Exec(ctx, `UPDATE api_apps SET request_count = request_count + 1, last_used_at = now() WHERE id = $1`, id)
}

func (s *PostgresStore) APIAppByID(ctx context.Context, id string) (domain.APIApp, bool) {
	row := s.pool.QueryRow(ctx, `SELECT `+apiAppColumns+` FROM api_apps WHERE id = $1`, id)
	app, err := scanAPIApp(row)
	if err != nil {
		return domain.APIApp{}, false
	}
	return app, true
}
