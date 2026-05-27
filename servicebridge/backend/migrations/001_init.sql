CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS admin_users (
    id TEXT PRIMARY KEY,
    account TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    account TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'offline',
    max_conversations INTEGER NOT NULL DEFAULT 10,
    current_conversations INTEGER NOT NULL DEFAULT 0,
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agents_status_check CHECK (status IN ('online', 'busy', 'offline')),
    CONSTRAINT agents_max_conversations_check CHECK (max_conversations >= 0),
    CONSTRAINT agents_current_conversations_check CHECK (current_conversations >= 0)
);

CREATE TABLE IF NOT EXISTS visitors (
    id TEXT PRIMARY KEY,
    ip INET NOT NULL,
    remark TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'web',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS push_devices (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'uni-push',
    token TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_push_devices_agent_token
    ON push_devices(agent_id, provider, platform, token);

CREATE TABLE IF NOT EXISTS conversations (
    id TEXT PRIMARY KEY,
    visitor_id TEXT NOT NULL REFERENCES visitors(id),
    visitor_ip INET NOT NULL,
    visitor_remark TEXT NOT NULL,
    remark_updated_by TEXT NOT NULL DEFAULT '',
    remark_updated_at TIMESTAMPTZ,
    status TEXT NOT NULL,
    assigned_agent_id TEXT REFERENCES agents(id),
    source TEXT NOT NULL DEFAULT 'web',
    last_message TEXT NOT NULL DEFAULT '',
    last_message_at TIMESTAMPTZ,
    unread_for_agent INTEGER NOT NULL DEFAULT 0,
    unread_for_visitor INTEGER NOT NULL DEFAULT 0,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT conversations_status_check CHECK (status IN ('waiting', 'assigned', 'ai_serving', 'human_requested', 'closed')),
    CONSTRAINT conversations_unread_agent_check CHECK (unread_for_agent >= 0),
    CONSTRAINT conversations_unread_visitor_check CHECK (unread_for_visitor >= 0)
);

CREATE INDEX IF NOT EXISTS idx_conversations_assigned_agent ON conversations(assigned_agent_id) WHERE assigned_agent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_conversations_status_updated ON conversations(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_visitor ON conversations(visitor_id);

CREATE TABLE IF NOT EXISTS messages (
    server_msg_id TEXT PRIMARY KEY,
    client_msg_id TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_type TEXT NOT NULL,
    sender_id TEXT NOT NULL DEFAULT '',
    message_type TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    revoked_by_kind TEXT NOT NULL DEFAULT '',
    revoked_by_id TEXT NOT NULL DEFAULT '',
    CONSTRAINT messages_sender_type_check CHECK (sender_type IN ('visitor', 'agent', 'ai', 'system')),
    CONSTRAINT messages_message_type_check CHECK (message_type IN ('text', 'emoji', 'image', 'audio', 'contact_phone', 'contact_wechat', 'system', 'ai_text', 'handoff_request', 'revoked'))
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_created ON messages(conversation_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_client_dedup
    ON messages(conversation_id, sender_type, sender_id, client_msg_id)
    WHERE client_msg_id <> '';

CREATE TABLE IF NOT EXISTS service_ratings (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
    visitor_id TEXT NOT NULL REFERENCES visitors(id),
    assigned_agent_id TEXT,
    score INTEGER NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT service_ratings_score_check CHECK (score BETWEEN 1 AND 5)
);

CREATE INDEX IF NOT EXISTS idx_service_ratings_created ON service_ratings(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_service_ratings_agent ON service_ratings(assigned_agent_id) WHERE assigned_agent_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS audit_events (
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
);

CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_kind, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource ON audit_events(resource, resource_id, created_at DESC);

CREATE TABLE IF NOT EXISTS keyword_rules (
    id TEXT PRIMARY KEY,
    keyword TEXT NOT NULL,
    match_type TEXT NOT NULL DEFAULT 'contains',
    reply TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 0,
    action TEXT NOT NULL DEFAULT 'text',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT keyword_rules_match_type_check CHECK (match_type IN ('contains', 'exact')),
    CONSTRAINT keyword_rules_action_check CHECK (action IN ('text', 'phone', 'wechat', 'handoff'))
);

CREATE INDEX IF NOT EXISTS idx_keyword_rules_enabled_priority ON keyword_rules(enabled, priority DESC);

CREATE TABLE IF NOT EXISTS contact_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    phone TEXT NOT NULL DEFAULT '',
    wechat TEXT NOT NULL DEFAULT '',
    wechat_reply_type TEXT NOT NULL DEFAULT 'image',
    wechat_image_url TEXT NOT NULL DEFAULT '',
    qq TEXT NOT NULL DEFAULT '',
    qq_reply_type TEXT NOT NULL DEFAULT 'text',
    qq_image_url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT contact_settings_singleton CHECK (id = 1),
    CONSTRAINT contact_settings_wechat_reply_type_check CHECK (wechat_reply_type IN ('text', 'image')),
    CONSTRAINT contact_settings_qq_reply_type_check CHECK (qq_reply_type IN ('text', 'image'))
);

CREATE TABLE IF NOT EXISTS ai_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    mode TEXT NOT NULL DEFAULT 'human_first',
    base_url TEXT NOT NULL DEFAULT 'https://api.openai.com/v1',
    api_key_ciphertext TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT 'gpt-4o-mini',
    api_type TEXT NOT NULL DEFAULT 'chat_completions',
    temperature NUMERIC(3,2) NOT NULL DEFAULT 0.70,
    max_output_tokens INTEGER NOT NULL DEFAULT 512,
    timeout_seconds INTEGER NOT NULL DEFAULT 20,
    system_prompt TEXT NOT NULL DEFAULT '',
    agent_no_reply_timeout_seconds INTEGER NOT NULL DEFAULT 60,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_settings_singleton CHECK (id = 1),
    CONSTRAINT ai_settings_mode_check CHECK (mode IN ('human_first', 'always_ai', 'manual_only')),
    CONSTRAINT ai_settings_api_type_check CHECK (api_type IN ('chat_completions', 'responses')),
    CONSTRAINT ai_settings_temperature_check CHECK (temperature >= 0 AND temperature <= 2)
);

CREATE TABLE IF NOT EXISTS business_hours (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    start_time TEXT NOT NULL DEFAULT '09:00',
    end_time TEXT NOT NULL DEFAULT '18:00',
    enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT business_hours_singleton CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS auth_sessions (
    token TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    account_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT auth_sessions_kind_check CHECK (kind IN ('admin', 'agent', 'visitor'))
);

CREATE INDEX IF NOT EXISTS idx_auth_sessions_account ON auth_sessions(kind, account_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions(expires_at);

CREATE TABLE IF NOT EXISTS feishu_message_bindings (
    message_id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL DEFAULT '',
    root_message_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feishu_bindings_conversation ON feishu_message_bindings(conversation_id);
CREATE INDEX IF NOT EXISTS idx_feishu_bindings_root ON feishu_message_bindings(root_message_id) WHERE root_message_id <> '';

CREATE TABLE IF NOT EXISTS feishu_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT false,
    base_url TEXT NOT NULL DEFAULT 'https://open.feishu.cn',
    app_id TEXT NOT NULL DEFAULT '',
    app_secret_ciphertext TEXT NOT NULL DEFAULT '',
    verification_token_ciphertext TEXT NOT NULL DEFAULT '',
    encrypt_key_ciphertext TEXT NOT NULL DEFAULT '',
    default_chat_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT 'agent_feishu',
    timeout_seconds INTEGER NOT NULL DEFAULT 8,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT feishu_settings_singleton CHECK (id = 1)
);

INSERT INTO keyword_rules (id, keyword, match_type, reply, enabled, priority, action)
VALUES
    ('kw_phone', '电话', 'contains', '客服电话：400-123-4567', true, 90, 'phone'),
    ('kw_whatsapp', 'WhatsApp', 'contains', '官方 WhatsApp：+86 400 888 8888', true, 80, 'wechat'),
    ('kw_telegram', 'Telegram', 'contains', '官方 Telegram：@AstrevaSupport', true, 75, 'text')
ON CONFLICT (id) DO NOTHING;

INSERT INTO contact_settings (id, phone, wechat, wechat_reply_type, qq, qq_reply_type)
VALUES (1, '400-123-4567', '+86 400 888 8888', 'text', '@AstrevaSupport', 'text')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ai_settings (id, system_prompt)
VALUES (1, '你是一个专业的在线客服助手，名为“小A”。请使用礼貌、专业的语气回答用户问题。')
ON CONFLICT (id) DO NOTHING;

INSERT INTO business_hours (id, timezone, start_time, end_time, enabled)
VALUES (1, 'Asia/Shanghai', '09:00', '18:00', true)
ON CONFLICT (id) DO NOTHING;
