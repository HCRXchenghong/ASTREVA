-- ===== 开放 API 权限范围（批次 5，附录 B）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表/补列，
-- 这里的 DDL 供 initdb 场景使用，两处必须保持一致。
--
-- scopes：该 API 应用拥有的权限范围（JSONB 字符串数组）。
-- 空数组 '[]' 表示「不受限」，等同于全部范围，保证既有应用行为不变。
-- 可选值：me:read、conversations:read、conversations:create、conversations:close、
--        messages:read、messages:write、events:subscribe（me:read 保存时自动补齐）。

ALTER TABLE api_apps ADD COLUMN IF NOT EXISTS scopes JSONB NOT NULL DEFAULT '[]'::jsonb;
