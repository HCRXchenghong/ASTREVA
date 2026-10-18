-- ===== AI 频率 / 次数 / 单次字数限额（后台可配，单例表）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表，
-- 这里的 DDL 供 initdb 场景使用，两处必须保持一致。
-- 未写入任何行时，运行时返回 DefaultAILimits()（20/300/60/1200/空文案/开启）；
-- 任意数值为 0 表示该项不限制。

CREATE TABLE IF NOT EXISTS ai_limits (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    visitor_messages_per_minute INTEGER NOT NULL DEFAULT 20,
    visitor_messages_per_day INTEGER NOT NULL DEFAULT 300,
    ai_calls_per_conversation_day INTEGER NOT NULL DEFAULT 60,
    ai_reply_max_chars INTEGER NOT NULL DEFAULT 1200,
    exceed_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ai_limits_singleton CHECK (id = 1)
);
