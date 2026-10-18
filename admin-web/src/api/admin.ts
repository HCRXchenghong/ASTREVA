import client, { serviceBridgePrefix } from './client';

// 客服引擎接口统一经 admin-server 代理（serviceBridgePrefix），
// 站点后台接口（内容/媒体/发布/咨询单）使用 /site-admin-api/*。

const bridge = (path: string) => `${serviceBridgePrefix}${path}`;

// ---------- 工作台 / 会话 ----------
export const fetchDashboard = () =>
  client.get(bridge('/api/admin/dashboard')).then((r) => r.data);
export const fetchConversations = () =>
  client.get(bridge('/api/admin/conversations')).then((r) => r.data);
export const fetchAgents = () =>
  client.get(bridge('/api/admin/agents')).then((r) => r.data);
export const fetchMessages = (id: string, limit = 100, before = '') =>
  client
    .get(bridge(`/api/conversations/${encodeURIComponent(id)}/messages`), {
      params: before ? { limit, before } : { limit },
    })
    .then((r) => r.data);
export const closeConversation = (id: string) =>
  client.post(bridge(`/api/conversations/${encodeURIComponent(id)}/close`), {});
export const transferConversation = (id: string, agentId: string) =>
  client.post(
    bridge(`/api/admin/conversations/${encodeURIComponent(id)}/transfer`),
    { agent_id: agentId }
  );
export const exportConversationsUrl = () =>
  bridge('/api/admin/conversations/export.csv');

// ---------- 客服账号 ----------
export const createAgent = (payload: Record<string, unknown>) =>
  client.post(bridge('/api/admin/agents'), payload).then((r) => r.data);
export const updateAgent = (id: string, payload: Record<string, unknown>) =>
  client
    .patch(bridge(`/api/admin/agents/${encodeURIComponent(id)}`), payload)
    .then((r) => r.data);
export const resetAgentPassword = (id: string, password: string) =>
  client
    .post(bridge(`/api/admin/agents/${encodeURIComponent(id)}/reset-password`), {
      password,
    })
    .then((r) => r.data);
export const disableAgent = (id: string) =>
  client.post(bridge(`/api/admin/agents/${encodeURIComponent(id)}/disable`), {});
export const deleteAgent = (id: string) =>
  client.delete(bridge(`/api/admin/agents/${encodeURIComponent(id)}`));

// ---------- 评价 ----------
export const fetchRatingSummaries = () =>
  client.get(bridge('/api/admin/ratings/summaries')).then((r) => r.data);
export const fetchRatingSummary = () =>
  client.get(bridge('/api/admin/ratings/summary')).then((r) => r.data);
export const fetchRecentRatings = (limit = 100) =>
  client
    .get(bridge('/api/admin/ratings'), { params: { limit } })
    .then((r) => r.data);
export const fetchAgentRatings = (id: string) =>
  client
    .get(bridge(`/api/admin/agents/${encodeURIComponent(id)}/ratings`))
    .then((r) => r.data);
export const setAgentManualRating = (id: string, rate: number) =>
  client
    .post(bridge(`/api/admin/agents/${encodeURIComponent(id)}/rating`), {
      satisfaction_rate: rate,
    })
    .then((r) => r.data);
export const clearAgentManualRating = (id: string) =>
  client
    .delete(bridge(`/api/admin/agents/${encodeURIComponent(id)}/rating`))
    .then((r) => r.data);
export const resetAgentRatings = (id: string) =>
  client
    .post(bridge(`/api/admin/agents/${encodeURIComponent(id)}/ratings/reset`), {})
    .then((r) => r.data);

// ---------- 关键词 ----------
export const fetchKeywordRules = () =>
  client.get(bridge('/api/admin/keyword-rules')).then((r) => r.data);
export const createKeywordRule = (payload: Record<string, unknown>) =>
  client.post(bridge('/api/admin/keyword-rules'), payload).then((r) => r.data);
export const updateKeywordRule = (
  id: string,
  payload: Record<string, unknown>
) =>
  client
    .patch(bridge(`/api/admin/keyword-rules/${encodeURIComponent(id)}`), payload)
    .then((r) => r.data);
export const deleteKeywordRule = (id: string) =>
  client.delete(bridge(`/api/admin/keyword-rules/${encodeURIComponent(id)}`));

// ---------- 审计 ----------
export const fetchBridgeAudit = (limit = 200) =>
  client
    .get(bridge('/api/admin/audit-events'), { params: { limit } })
    .then((r) => r.data);
export const fetchSiteAudit = (limit = 500) =>
  client
    .get('/site-admin-api/audit-events', { params: { limit } })
    .then((r) => r.data);

// ---------- App 版本 ----------
export const fetchReleases = () =>
  client.get(bridge('/api/admin/app-releases')).then((r) => r.data);
export const uploadRelease = (form: FormData) =>
  client.post(bridge('/api/admin/app-releases'), form).then((r) => r.data);
export const updateRelease = (id: string, payload: Record<string, unknown>) =>
  client
    .patch(bridge(`/api/admin/app-releases/${encodeURIComponent(id)}`), payload)
    .then((r) => r.data);
export const deleteRelease = (id: string) =>
  client.delete(bridge(`/api/admin/app-releases/${encodeURIComponent(id)}`));

// ---------- API 管理 ----------
export const fetchApiApps = () =>
  client.get(bridge('/api/admin/api-apps')).then((r) => r.data);
export const createApiApp = (payload: Record<string, unknown>) =>
  client.post(bridge('/api/admin/api-apps'), payload).then((r) => r.data);
export const updateApiApp = (id: string, payload: Record<string, unknown>) =>
  client
    .patch(bridge(`/api/admin/api-apps/${encodeURIComponent(id)}`), payload)
    .then((r) => r.data);
export const resetApiAppKey = (id: string) =>
  client
    .post(bridge(`/api/admin/api-apps/${encodeURIComponent(id)}/reset-key`), {})
    .then((r) => r.data);
export const deleteApiApp = (id: string) =>
  client.delete(bridge(`/api/admin/api-apps/${encodeURIComponent(id)}`));
export const apiDocsUrl = () => '/site-admin/api-docs';

// ---------- 系统设置：营业时间 / 联系方式 / AI / 推送 ----------
export const fetchBusinessHours = () =>
  client.get(bridge('/api/admin/business-hours')).then((r) => r.data);
export const updateBusinessHours = (payload: Record<string, unknown>) =>
  client.patch(bridge('/api/admin/business-hours'), payload).then((r) => r.data);
export const fetchContactSettings = () =>
  client.get('/site-admin-api/servicebridge/api/contact-settings').then((r) => r.data);
export const updateContactSettings = (payload: Record<string, unknown>) =>
  client
    .patch(bridge('/api/admin/contact-settings'), payload)
    .then((r) => r.data);
export const fetchAiSettings = () =>
  client.get(bridge('/api/admin/ai-settings')).then((r) => r.data);
export const updateAiSettings = (payload: Record<string, unknown>) =>
  client.patch(bridge('/api/admin/ai-settings'), payload).then((r) => r.data);
export const fetchAiModels = (payload: Record<string, unknown>) =>
  client
    .post(bridge('/api/admin/ai-settings/models'), payload)
    .then((r) => r.data);
export const testAiReply = (input: string) =>
  client
    .post(bridge('/api/admin/ai-settings/test'), { input })
    .then((r) => r.data);
export const fetchAiCacheProbe = () =>
  client.get(bridge('/api/admin/ai-settings/cache-probe')).then((r) => r.data);
export const probeAiCache = (models: string[]) =>
  client
    .post(bridge('/api/admin/ai-settings/cache-probe'), { models })
    .then((r) => r.data);
export const fetchSystemStatus = () =>
  client.get(bridge('/api/admin/system-status')).then((r) => r.data);

// ---------- AI 多供应商 / 能力绑定 / 客服翻译偏好 ----------
// 供应商密钥只回显末四位（api_key_masked）；PATCH 时 api_key 不传=不改，传 ""=清除。
export const fetchAiProviders = () =>
  client.get(bridge('/api/admin/ai-providers')).then((r) => r.data);
export const createAiProvider = (payload: Record<string, unknown>) =>
  client.post(bridge('/api/admin/ai-providers'), payload).then((r) => r.data);
export const updateAiProvider = (id: string, payload: Record<string, unknown>) =>
  client
    .patch(bridge(`/api/admin/ai-providers/${encodeURIComponent(id)}`), payload)
    .then((r) => r.data);
export const deleteAiProvider = (id: string) =>
  client.delete(bridge(`/api/admin/ai-providers/${encodeURIComponent(id)}`));
export const fetchAiProviderModels = (id: string) =>
  client
    .post(
      bridge(`/api/admin/ai-providers/${encodeURIComponent(id)}/models`),
      {}
    )
    .then((r) => r.data);
export const testAiProvider = (id: string) =>
  client
    .post(bridge(`/api/admin/ai-providers/${encodeURIComponent(id)}/test`), {})
    .then((r) => r.data);
export const fetchAiBindings = () =>
  client.get(bridge('/api/admin/ai-bindings')).then((r) => r.data);
export const updateAiBindings = (bindings: Array<Record<string, unknown>>) =>
  client
    .put(bridge('/api/admin/ai-bindings'), { bindings })
    .then((r) => r.data);
export const fetchAgentPreferences = (id: string) =>
  client
    .get(bridge(`/api/admin/agents/${encodeURIComponent(id)}/preferences`))
    .then((r) => r.data);
export const updateAgentPreferences = (
  id: string,
  payload: Record<string, unknown>
) =>
  client
    .patch(
      bridge(`/api/admin/agents/${encodeURIComponent(id)}/preferences`),
      payload
    )
    .then((r) => r.data);

// ---------- AI 知识库 ----------
// 契约冻结：bases / entries / search / import / settings 五组接口，字段名与后端保持一致。
export const fetchKnowledgeBases = () =>
  client.get(bridge('/api/admin/knowledge/bases')).then((r) => r.data);
export const createKnowledgeBase = (payload: Record<string, unknown>) =>
  client.post(bridge('/api/admin/knowledge/bases'), payload).then((r) => r.data);
export const updateKnowledgeBase = (
  id: string,
  payload: Record<string, unknown>
) =>
  client
    .patch(
      bridge(`/api/admin/knowledge/bases/${encodeURIComponent(id)}`),
      payload
    )
    .then((r) => r.data);
export const deleteKnowledgeBase = (id: string) =>
  client.delete(bridge(`/api/admin/knowledge/bases/${encodeURIComponent(id)}`));

export const fetchKnowledgeEntries = (params: {
  base_id?: string;
  q?: string;
  limit?: number;
  offset?: number;
}) =>
  client
    .get(bridge('/api/admin/knowledge/entries'), { params })
    .then((r) => r.data);
export const createKnowledgeEntry = (payload: Record<string, unknown>) =>
  client
    .post(bridge('/api/admin/knowledge/entries'), payload)
    .then((r) => r.data);
export const updateKnowledgeEntry = (
  id: string,
  payload: Record<string, unknown>
) =>
  client
    .patch(
      bridge(`/api/admin/knowledge/entries/${encodeURIComponent(id)}`),
      payload
    )
    .then((r) => r.data);
export const deleteKnowledgeEntry = (id: string) =>
  client.delete(
    bridge(`/api/admin/knowledge/entries/${encodeURIComponent(id)}`)
  );

export const searchKnowledge = (payload: {
  query: string;
  base_ids?: string[];
  limit?: number;
}) =>
  client
    .post(bridge('/api/admin/knowledge/search'), payload)
    .then((r) => r.data);
export const importKnowledgeEntries = (
  baseId: string,
  entries: Array<Record<string, unknown>>
) =>
  client
    .post(bridge('/api/admin/knowledge/import'), {
      base_id: baseId,
      entries,
    })
    .then((r) => r.data);

export const fetchKnowledgeSettings = () =>
  client.get(bridge('/api/admin/knowledge/settings')).then((r) => r.data);
export const updateKnowledgeSettings = (payload: Record<string, unknown>) =>
  client
    .patch(bridge('/api/admin/knowledge/settings'), payload)
    .then((r) => r.data);

export const fetchPushSettings = () =>
  client.get(bridge('/api/admin/push-settings')).then((r) => r.data);
export const updatePushSettings = (payload: Record<string, unknown>) =>
  client.patch(bridge('/api/admin/push-settings'), payload).then((r) => r.data);
export const testPushSettings = () =>
  client
    .post(bridge('/api/admin/push-settings/test'), {})
    .then((r) => r.data);

// ---------- 站点后台：bootstrap（含咨询单）/ 发布状态 ----------
export const fetchBootstrap = () =>
  client.get('/site-admin-api/bootstrap').then((r) => r.data);
export const saveSection = (payload: Record<string, unknown>) =>
  client.post('/site-admin-api/save', payload).then((r) => r.data);
export const updateLead = (id: string, payload: Record<string, unknown>) =>
  client
    .patch(`/site-admin-api/leads/${encodeURIComponent(id)}`, payload)
    .then((r) => r.data);
export const fetchPublishStatus = () =>
  client.get('/site-admin-api/status').then((r) => r.data);
export const publishSite = (source = 'current') =>
  client
    .post(`/site-admin-api/publish?source=${encodeURIComponent(source)}`)
    .then((r) => r.data);
// 草稿预览：把编辑器当前内容构建到 /__preview/，不修改线上内容。
export const requestPreview = (content: unknown) =>
  client.post('/site-admin-api/preview', { content }).then((r) => r.data);

// ---------- 官网访问统计（数据看板） ----------
export const fetchAnalyticsOverview = (params: {
  tz: string;
  month?: string;
}) =>
  client
    .get('/site-admin-api/analytics/overview', { params })
    .then((r) => r.data);

export const fetchAnalyticsVisits = (params: Record<string, unknown> = {}) =>
  client
    .get('/site-admin-api/analytics/visits', { params })
    .then((r) => r.data);

// ---------- 系统健康 & 系统日志 ----------
export const fetchSystemHealth = () =>
  client.get('/site-admin-api/system/health').then((r) => r.data);

export const fetchSystemEvents = (params: Record<string, unknown> = {}) =>
  client
    .get('/site-admin-api/system/events', { params })
    .then((r) => r.data);

export const fetchSystemEvent = (id: string) =>
  client
    .get(`/site-admin-api/system/events/${encodeURIComponent(id)}`)
    .then((r) => r.data);

export const purgeSystemEvents = () =>
  client.post('/site-admin-api/system/events/purge').then((r) => r.data);
