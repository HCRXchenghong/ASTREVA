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
