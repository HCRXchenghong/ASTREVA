-- ===== AI 能力候选供应商（同能力多供应商 + 故障转移）=====
-- 注意：运行时由 internal/store/postgres.go 的 ensureCoreTables 建表/建索引，
-- 这里的 DDL 供 initdb / 手工部署场景使用，两处必须保持一致（幂等）。
--
-- 语义：同一 capability 可挂多个 provider，运行时按 priority 升序依次尝试，
-- 前一个失败（网络/超时/5xx/401/403）时自动切到下一个；连续失败达到阈值后熔断跳过。
-- 旧的 ai_bindings 表保留：其单条记录等价于 targets 之后的兜底候选，已有部署行为不变。
--
-- provider_id 建外键并 ON DELETE CASCADE：删除供应商时候选一并清理，不留悬挂引用。

CREATE TABLE IF NOT EXISTS ai_binding_targets (
    capability TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES ai_providers(id) ON DELETE CASCADE,
    model TEXT NOT NULL DEFAULT '',
    priority INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (capability, provider_id),
    CONSTRAINT ai_binding_targets_capability_check CHECK (capability IN ('reception','translation','detection','seo','image'))
);

CREATE INDEX IF NOT EXISTS idx_ai_binding_targets_provider ON ai_binding_targets(provider_id);
CREATE INDEX IF NOT EXISTS idx_ai_binding_targets_order ON ai_binding_targets(capability, priority);
