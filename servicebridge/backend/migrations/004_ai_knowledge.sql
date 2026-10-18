-- ===== AI 知识库（多库、两种用途、库外拒答）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表，
-- 这里的 DDL 供 initdb 场景使用，两处必须保持一致。

CREATE TABLE IF NOT EXISTS ai_knowledge_bases (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_enabled_sort ON ai_knowledge_bases(enabled, sort_order, created_at);

CREATE TABLE IF NOT EXISTS ai_knowledge_entries (
    id TEXT PRIMARY KEY,
    base_id TEXT NOT NULL REFERENCES ai_knowledge_bases(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    category TEXT NOT NULL DEFAULT '',
    priority INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_entries_base ON ai_knowledge_entries(base_id);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_entries_enabled ON ai_knowledge_entries(enabled);

-- 库外行为配置：单例表；未写入时运行时返回默认值（handoff / 0.35 / 3 / true）。
CREATE TABLE IF NOT EXISTS ai_knowledge_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    unknown_action TEXT NOT NULL DEFAULT 'handoff',
    refuse_reply TEXT NOT NULL DEFAULT '',
    score_threshold NUMERIC(4,3) NOT NULL DEFAULT 0.35,
    max_entries INTEGER NOT NULL DEFAULT 3,
    enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_knowledge_settings_singleton CHECK (id = 1),
    CONSTRAINT ai_knowledge_settings_unknown_action_check CHECK (unknown_action IN ('refuse', 'handoff'))
);

-- AI 回复的"回答依据"知识条目 ID，供客服端追溯。
ALTER TABLE messages ADD COLUMN IF NOT EXISTS knowledge_entry_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
