export function createWebsiteSeoRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  seoService,
  autoSeo,
  siteDomainService,
  publishService,
  readContentRecord,
  saveContent,
  getBootstrap
}) {
  return async function handleWebsiteSeo(req, res, pathname) {
    if (pathname !== '/site-admin-api/seo/ai-generate' && pathname !== '/site-admin-api/seo/auto-status' && pathname !== '/site-admin-api/seo/site-domain') return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    if (pathname === '/site-admin-api/seo/auto-status') {
      if (req.method !== 'GET') json(res, 405, { error: { message: '该接口仅支持 GET。' } });
      else json(res, 200, { data: await autoSeo.status() });
      return true;
    }
    if (pathname === '/site-admin-api/seo/site-domain') {
      if (req.method === 'GET') {
        json(res, 200, { data: await siteDomainService.read() });
      } else if (req.method === 'PUT') {
        try {
          const payload = await bodyJson(req);
          if (typeof payload?.domain !== 'string') throw new Error('请填写官网域名；留空可恢复部署配置。');
          const setting = await siteDomainService.save(payload.domain);
          let publish = null;
          let rebuildError = '';
          if (setting.changed) {
            try { publish = await publishService.requestPublish(verifySession(req).userId || '', 'current'); }
            catch (error) { rebuildError = error.message || '官网重建排队失败'; }
          }
          await auditEvent(req, 'seo.site-domain.updated', 'runtime-setting', 'website-site-domain', { domain: setting.domain, changed: setting.changed, rebuildQueued: Boolean(publish), rebuildError });
          json(res, 200, { data: setting, meta: { publish, rebuildError } });
        } catch (error) {
          json(res, 400, { error: { message: error.message || '保存官网域名失败。' } });
        }
      } else {
        json(res, 405, { error: { message: '该接口仅支持 GET 和 PUT。' } });
      }
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
          translationLocalesQueued: ['en', 'ar'],
          localizedSeoLocales: ['en', 'ar'],
          localizedSeoStage: 'after-translation-before-publish'
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
