CREATE TABLE IF NOT EXISTS feishu_message_bindings (
    message_id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    chat_id TEXT NOT NULL DEFAULT '',
    root_message_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feishu_bindings_conversation ON feishu_message_bindings(conversation_id);
CREATE INDEX IF NOT EXISTS idx_feishu_bindings_root ON feishu_message_bindings(root_message_id) WHERE root_message_id <> '';
