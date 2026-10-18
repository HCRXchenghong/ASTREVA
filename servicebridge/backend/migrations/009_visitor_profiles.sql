-- ===== 访客画像（P5）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表/补列，
-- 这里的 DDL 供 initdb / 手工部署场景使用，两处必须保持一致。
--
-- 数据来源三级降级：CDN 头（CF-IPCountry / X-Vercel-IP-Country / X-Country-Code）
-- → 本地 GeoLite2 CSV 目录（geoip_dir）→ 后台配置的第三方 IP 库（third_party_url）。
-- 三级都拿不到时地理字段留空，只按 Accept-Language 推断语言，绝不推断国家。
--
-- country 存可直接展示的名称（如"沙特阿拉伯"），country_code 存 ISO alpha-2（如 SA）；
-- nationality_guess 是加权"推测"（ISO alpha-2），依据写在 nationality_basis，
-- 取值只允许 ip_cdn / geoip / third_party / language / timezone。

CREATE TABLE IF NOT EXISTS visitor_profiles (
    visitor_id TEXT PRIMARY KEY,
    country TEXT NOT NULL DEFAULT '',
    country_code TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT '',
    nationality_guess TEXT NOT NULL DEFAULT '',
    nationality_confidence NUMERIC(3,2) NOT NULL DEFAULT 0,
    nationality_basis JSONB NOT NULL DEFAULT '[]'::jsonb,
    ip_first TEXT NOT NULL DEFAULT '',
    ip_last TEXT NOT NULL DEFAULT '',
    languages JSONB NOT NULL DEFAULT '{}'::jsonb,
    dialect TEXT NOT NULL DEFAULT '',
    code_switching BOOLEAN NOT NULL DEFAULT false,
    summary TEXT NOT NULL DEFAULT '',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    interests JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
    visits INTEGER NOT NULL DEFAULT 0,
    last_path TEXT NOT NULL DEFAULT '',
    ai_updated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT visitor_profiles_visits_check CHECK (visits >= 0),
    CONSTRAINT visitor_profiles_confidence_check CHECK (nationality_confidence >= 0 AND nationality_confidence <= 1)
);

CREATE INDEX IF NOT EXISTS idx_visitor_profiles_updated ON visitor_profiles(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_visitor_profiles_country ON visitor_profiles(country_code);

-- 历史库补齐列（新库由上面的 CREATE TABLE 直接包含）。
ALTER TABLE visitor_profiles ADD COLUMN IF NOT EXISTS country_code TEXT NOT NULL DEFAULT '';

-- 画像配置单例（id 恒为 1）：未写入时运行时返回 DefaultVisitorProfileSettings()。
-- third_party_key_ciphertext 为 AES-256-GCM 密文，接口只回末四位掩码。
CREATE TABLE IF NOT EXISTS visitor_profile_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    geoip_dir TEXT NOT NULL DEFAULT '',
    third_party_url TEXT NOT NULL DEFAULT '',
    third_party_key_ciphertext TEXT NOT NULL DEFAULT '',
    third_party_timeout_ms INTEGER NOT NULL DEFAULT 2000,
    cache_ttl_hours INTEGER NOT NULL DEFAULT 24,
    ai_summary_enabled BOOLEAN NOT NULL DEFAULT true,
    ai_summary_every_messages INTEGER NOT NULL DEFAULT 10,
    show_full_ip BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT visitor_profile_settings_singleton CHECK (id = 1),
    CONSTRAINT visitor_profile_settings_timeout_check CHECK (third_party_timeout_ms BETWEEN 200 AND 30000),
    CONSTRAINT visitor_profile_settings_cache_check CHECK (cache_ttl_hours BETWEEN 1 AND 720),
    CONSTRAINT visitor_profile_settings_every_check CHECK (ai_summary_every_messages BETWEEN 1 AND 200)
);
