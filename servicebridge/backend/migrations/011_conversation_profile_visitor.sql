-- ===== 会话画像键（官网 visitor_ref）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建列/补列，
-- 这里的 DDL 供 initdb / 手工部署场景使用，两处必须保持一致（幂等）。
--
-- 背景：官网埋点写入的访客 ID（astreva_visitor_id）与引擎自增的 vis_xxx 是两套命名空间，
-- 跨服务上报 /api/internal/visitor-activity 按 visitor_id 找画像时必然 miss。
-- 因此会话新增画像聚合键 profile_visitor_id：
--   - 访客建会话时若带上 visitor_ref（官网 ID），就存这里，画像也以此键创建；
--   - 未提供时保持空串，画像键在应用层回落到 visitor_id（历史行为完全不变）。

ALTER TABLE conversations ADD COLUMN IF NOT EXISTS profile_visitor_id TEXT NOT NULL DEFAULT '';

-- 按画像键反查某访客的全部会话（画像详情、语言统计窗口、清理策略）。
CREATE INDEX IF NOT EXISTS idx_conversations_profile_visitor ON conversations(profile_visitor_id)
    WHERE profile_visitor_id <> '';
