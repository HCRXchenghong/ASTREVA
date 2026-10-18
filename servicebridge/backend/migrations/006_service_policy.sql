-- ===== 批次 4 第一波：客服策略 / 排队 / 黑名单 / 提醒渠道 / SMTP =====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表/补列，
-- 这里的 DDL 供 initdb / 手工部署场景使用，两处必须保持一致。
-- 本批次只做存储与管理接口，不接线 SLA 计时器、自动关闭、优先级切换等状态机。

-- 客服策略（单例，id=1）：未写入时运行时返回 DefaultServicePolicy()。
CREATE TABLE IF NOT EXISTS service_policy (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    queue_enabled BOOLEAN NOT NULL DEFAULT true,
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
    CONSTRAINT service_policy_singleton CHECK (id = 1),
    CONSTRAINT service_policy_sla_idle_action_check CHECK (sla_idle_action IN ('ai', 'reassign')),
    CONSTRAINT service_policy_ai_keyword_priority_check CHECK (ai_keyword_priority IN ('keyword_first', 'ai_first', 'keyword_only', 'ai_only'))
);

-- 排队队列：会话删除时级联清理。
CREATE TABLE IF NOT EXISTS conversation_queue (
    conversation_id TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_conversation_queue_order ON conversation_queue(priority DESC, enqueued_at ASC);

-- 黑名单：visitor | ip | account，支持过期时间。
CREATE TABLE IF NOT EXISTS service_blacklist (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    value TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, value),
    CONSTRAINT service_blacklist_kind_check CHECK (kind IN ('visitor', 'ip', 'account'))
);
CREATE INDEX IF NOT EXISTS idx_service_blacklist_kind ON service_blacklist(kind, created_at DESC);

-- 客服提醒渠道（每客服 1–3 个）：配置整体 JSON 后 AES-256-GCM 加密入库。
-- 客服删除时级联清理。
CREATE TABLE IF NOT EXISTS agent_alert_channels (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    config_ciphertext TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agent_alert_channels_kind_check CHECK (kind IN ('telegram', 'bark', 'email'))
);
CREATE INDEX IF NOT EXISTS idx_agent_alert_channels_agent ON agent_alert_channels(agent_id);

-- 全局 SMTP 发信设置（单例）：密码密文入库，接口只回掩码。
CREATE TABLE IF NOT EXISTS alert_smtp_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    host TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 587,
    username TEXT NOT NULL DEFAULT '',
    password_ciphertext TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL DEFAULT '',
    use_tls BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alert_smtp_settings_singleton CHECK (id = 1),
    CONSTRAINT alert_smtp_settings_port_check CHECK (port BETWEEN 1 AND 65535)
);

-- 会话补充列（SLA 计时器与排队分组使用；本批次只落库，不参与流转）。
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS first_response_at TIMESTAMPTZ;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS last_agent_reply_at TIMESTAMPTZ;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS sla_state TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS queue_group TEXT NOT NULL DEFAULT '';
