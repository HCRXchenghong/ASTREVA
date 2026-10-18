export function createWebsiteSeoRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  seoService,
  readContentRecord,
  saveContent,
  getBootstrap
}) {
  return async function handleWebsiteSeo(req, res, pathname) {
    if (pathname !== '/site-admin-api/seo/ai-generate') return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    if (req.method !== 'POST') {
      json(res, 405, { error: { message: '该接口仅支持 POST。' } });
      return true;
    }
    try {
      const payload = await bodyJson(req);
      const source = await readContentRecord();
      const requestedRevision = Number(payload?.revision || 0);
      if (requestedRevision && requestedRevision !== source.revision) {
        const error = new Error('官网内容已被其他页面更新，请刷新后台后再生成 SEO。');
        error.code = 'CONTENT_REVISION_CONFLICT';
        throw error;
      }
      const result = await seoService.generate(source.content);
      const saved = await saveContent(result.content, { expectedRevision: source.revision });
      await auditEvent(req, 'seo.ai-generated', 'site-content', 'seo', {
        generatedCount: result.generatedCount,
        sourceRevision: source.revision,
        savedRevision: saved.revision,
        locales: ['zh-CN', 'en', 'ar']
      });
      json(res, 200, {
        data: await getBootstrap(),
        meta: {
          generatedCount: result.generatedCount,
          generatedAt: result.generatedAt,
          sourceLocale: result.locale,
          translationLocalesQueued: ['en', 'ar']
        }
      });
    } catch (error) {
      const status = error?.code === 'CONTENT_REVISION_CONFLICT'
        ? 409
        : error?.code === 'AI_UNCONFIGURED'
          ? 503
          : error?.code === 'AI_UPSTREAM_ERROR'
            ? 502
            : 400;
      json(res, status, { error: { message: error.message || 'AI SEO 生成失败。' } });
    }
    return true;
  };
}
