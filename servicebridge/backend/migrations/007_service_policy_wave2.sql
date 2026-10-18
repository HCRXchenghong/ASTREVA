-- ===== 客服策略第二波：无人值守兜底记录列 =====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表/补列，
-- 这里的 DDL 供 initdb 场景使用，两处必须保持一致。
--
-- unattended_notified_at：无人值守兜底话术的发送时间。
-- 非空表示该会话已经发过兜底话术，保证"每个会话只发一次"在进程重启后依然成立。

ALTER TABLE conversations ADD COLUMN IF NOT EXISTS unattended_notified_at TIMESTAMPTZ;
