CREATE TABLE IF NOT EXISTS api_apps (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    rate_limit INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    request_count BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT api_apps_rate_limit_check CHECK (rate_limit >= 0)
);

CREATE INDEX IF NOT EXISTS idx_api_apps_created ON api_apps(created_at DESC);
