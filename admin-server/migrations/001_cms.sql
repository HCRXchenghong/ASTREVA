CREATE TABLE IF NOT EXISTS site_content (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  content JSONB NOT NULL,
  revision BIGINT NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE site_content ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS site_content_translations (
  locale TEXT PRIMARY KEY CHECK (locale IN ('en', 'ar')),
  content JSONB,
  source_revision BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'ready', 'failed', 'unconfigured')),
  last_error TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_site_content_translations_status
  ON site_content_translations(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS site_admin_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  totp_secret TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_fallback_versions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  content JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS site_fallback_state (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  active_version_id TEXT NOT NULL REFERENCES site_fallback_versions(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS site_leads (
  id TEXT PRIMARY KEY,
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  data JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_site_leads_submitted_at ON site_leads(submitted_at DESC);

CREATE TABLE IF NOT EXISTS site_media (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  object_key TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  mime_type TEXT NOT NULL DEFAULT '',
  media_type TEXT NOT NULL DEFAULT 'file' CHECK (media_type IN ('image', 'video', 'file')),
  size_bytes BIGINT NOT NULL DEFAULT 0,
  storage_driver TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE site_media ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '未分类';
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'upload';
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS original_key TEXT NOT NULL DEFAULT '';
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS width INTEGER;
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS height INTEGER;
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS checksum_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS variants JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE site_media ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS site_media_categories (
  name TEXT PRIMARY KEY,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO site_media_categories (name) VALUES ('未分类') ON CONFLICT (name) DO NOTHING;
INSERT INTO site_media_categories (name)
SELECT DISTINCT category FROM site_media WHERE category <> ''
ON CONFLICT (name) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_site_media_updated_at ON site_media(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_media_category ON site_media(category, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_media_type ON site_media(media_type, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_media_name_lower ON site_media(lower(name));
CREATE UNIQUE INDEX IF NOT EXISTS idx_site_media_categories_name_lower ON site_media_categories(lower(name));

CREATE TABLE IF NOT EXISTS site_audit_events (
  id TEXT PRIMARY KEY,
  actor_id TEXT NOT NULL DEFAULT '',
  actor_email TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  resource TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  ip_address TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  detail JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_site_audit_events_created_at ON site_audit_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_audit_events_resource ON site_audit_events(resource, resource_id, created_at DESC);

CREATE TABLE IF NOT EXISTS site_publish_jobs (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
  requested_by TEXT NOT NULL DEFAULT '',
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error_message TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_site_publish_jobs_requested_at ON site_publish_jobs(requested_at DESC);

CREATE TABLE IF NOT EXISTS site_runtime_settings (
  setting_key TEXT PRIMARY KEY,
  config JSONB NOT NULL DEFAULT '{}'::jsonb,
  encrypted_secrets TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ===== 二期：官网访问统计与系统日志 =====

CREATE TABLE IF NOT EXISTS site_analytics_visits (
  id BIGSERIAL PRIMARY KEY,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  path TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  referrer TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  ip_source TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  browser TEXT NOT NULL DEFAULT '',
  browser_version TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL DEFAULT '',
  device TEXT NOT NULL DEFAULT '',
  visitor_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  -- 未同意 analytics 的匿名上报：visitor_id 为服务端派生的「当日匿名键」，仅用于当日 UV 去重。
  anonymous BOOLEAN NOT NULL DEFAULT false,
  event_type TEXT NOT NULL DEFAULT 'pageview',
  is_bot BOOLEAN NOT NULL DEFAULT false
);

-- 老库补齐匿名标记列（迁移可重复执行）。
ALTER TABLE site_analytics_visits ADD COLUMN IF NOT EXISTS anonymous BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_site_analytics_visits_time ON site_analytics_visits(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_analytics_visits_visitor ON site_analytics_visits(visitor_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_analytics_visits_path ON site_analytics_visits(path, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_analytics_visits_ip ON site_analytics_visits(ip, occurred_at DESC);

CREATE TABLE IF NOT EXISTS site_system_events (
  id TEXT PRIMARY KEY,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  category TEXT NOT NULL DEFAULT 'system',
  level TEXT NOT NULL DEFAULT 'info',
  action TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  actor_kind TEXT NOT NULL DEFAULT '',
  actor_id TEXT NOT NULL DEFAULT '',
  actor_email TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  ip_source TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  status_code INTEGER,
  user_agent TEXT NOT NULL DEFAULT '',
  browser TEXT NOT NULL DEFAULT '',
  os TEXT NOT NULL DEFAULT '',
  device TEXT NOT NULL DEFAULT '',
  request_id TEXT NOT NULL DEFAULT '',
  detail JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_site_system_events_time ON site_system_events(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_system_events_level ON site_system_events(level, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_system_events_category ON site_system_events(category, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_system_events_ip ON site_system_events(ip, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_system_events_action ON site_system_events(action, occurred_at DESC);

-- ===== 二期：可撤销的管理员会话 =====

CREATE TABLE IF NOT EXISTS site_admin_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_site_admin_sessions_user ON site_admin_sessions(user_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_site_admin_sessions_expires ON site_admin_sessions(expires_at DESC);
