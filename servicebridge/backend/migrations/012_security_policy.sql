-- 安全策略：开放 API 总开关 + 客服只读围观范围。
ALTER TABLE service_policy
    ADD COLUMN IF NOT EXISTS open_api_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS agent_watch_scope TEXT NOT NULL DEFAULT 'own_group';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'service_policy_agent_watch_scope_check'
    ) THEN
        ALTER TABLE service_policy
            ADD CONSTRAINT service_policy_agent_watch_scope_check
            CHECK (agent_watch_scope IN ('own_group', 'all'));
    END IF;
END $$;
