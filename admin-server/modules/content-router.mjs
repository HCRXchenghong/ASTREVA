/** 官网内容与降级版本的 HTTP 边界。内容的读写规则通过依赖注入，避免耦合认证和媒体实现。 */
export function createContentRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  readContent,
  readContentRecord,
  readContentTranslation,
  applyPublicCors = () => true,
  activeFallbackVersion,
  readFallbackStore,
  createFallbackVersion,
  activateFallbackVersion,
  deleteFallbackVersion,
  fallbackVersionSummary,
  getBootstrap,
  saveSection,
  previewUrl
}) {
  function unauthorized(res) {
    json(res, 401, { error: { message: '请先登录。' } });
  }

  function fallbackPayload(version, store) {
    return {
      version: fallbackVersionSummary(version, store.activeId),
      activeId: store.activeId,
      versions: store.versions.map((item) => fallbackVersionSummary(item, store.activeId))
    };
  }

  return async function handleContent(req, res, pathname) {
    if (pathname === '/site-admin-api/public-content') {
      if (!applyPublicCors(req, res)) {
        json(res, 403, { error: { message: '当前来源不允许读取官网内容。' } });
        return true;
      }
      if (req.method === 'OPTIONS') {
        res.writeHead(204);
        res.end();
        return true;
      }
      if (req.method === 'GET') {
        const locale = String(new URL(req.url, 'http://localhost').searchParams.get('locale') || 'zh-CN');
        if (!['zh-CN', 'en', 'ar'].includes(locale)) {
          json(res, 400, { error: { message: '仅支持 zh-CN、en 和 ar。' } });
          return true;
        }
        const source = readContentRecord ? await readContentRecord() : { content: await readContent(), revision: 1, updatedAt: null };
        if (locale === 'zh-CN') {
          json(res, 200, { data: source.content, meta: { locale, status: 'ready', sourceRevision: source.revision, generatedAt: new Date().toISOString() } });
          return true;
        }
        const translation = await readContentTranslation(locale);
        if (translation?.status === 'ready' && translation.sourceRevision === source.revision && translation.content) {
          json(res, 200, { data: translation.content, meta: { locale, status: 'ready', sourceRevision: source.revision, generatedAt: new Date().toISOString() } });
          return true;
        }
        json(res, 409, {
          error: { message: translation?.lastError || '该语言正在翻译或尚未配置。' },
          meta: { locale, status: translation?.status || 'pending', sourceRevision: source.revision, translationRevision: translation?.sourceRevision || 0 }
        });
        return true;
      }
    }
    if (pathname === '/site-admin-api/public-fallback-content' && req.method === 'GET') {
      const version = await activeFallbackVersion();
      json(res, 200, {
        data: { content: version.content, version: fallbackVersionSummary(version, version.id) },
        meta: { generatedAt: new Date().toISOString() }
      });
      return true;
    }

    const activateMatch = pathname.match(/^\/site-admin-api\/fallback-versions\/([^/]+)\/activate$/);
    const deleteMatch = pathname.match(/^\/site-admin-api\/fallback-versions\/([^/]+)$/);
    const isContentRoute =
      pathname === '/site-admin-api/fallback-versions' ||
      Boolean(activateMatch) ||
      Boolean(deleteMatch) ||
      pathname === '/site-admin-api/bootstrap' ||
      pathname === '/site-admin-api/save';
    if (!isContentRoute) return false;
    if (!verifySession(req)) {
      unauthorized(res);
      return true;
    }

    if (pathname === '/site-admin-api/fallback-versions' && req.method === 'GET') {
      const store = await readFallbackStore();
      json(res, 200, {
        data: {
          activeId: store.activeId,
          versions: store.versions.map((version) => fallbackVersionSummary(version, store.activeId))
        }
      });
      return true;
    }
    if (pathname === '/site-admin-api/fallback-versions' && req.method === 'POST') {
      try {
        const { version, store } = await createFallbackVersion((await bodyJson(req)).name);
        await auditEvent(req, 'fallback-version.created', 'fallback-version', version.id, { name: version.name });
        json(res, 201, { data: fallbackPayload(version, store) });
      } catch (error) {
        json(res, 400, { error: { message: error.message || '创建降级版本失败。' } });
      }
      return true;
    }
    if (activateMatch && req.method === 'POST') {
      try {
        const { version, store } = await activateFallbackVersion(activateMatch[1]);
        await auditEvent(req, 'fallback-version.activated', 'fallback-version', version.id, { name: version.name });
        json(res, 200, { data: fallbackPayload(version, store) });
      } catch (error) {
        json(res, 404, { error: { message: error.message || '启用降级版本失败。' } });
      }
      return true;
    }
    if (deleteMatch && req.method === 'DELETE') {
      try {
        const { deleted, activeChanged, store } = await deleteFallbackVersion(deleteMatch[1]);
        json(res, 200, {
          data: {
            deleted: fallbackVersionSummary(deleted, ''),
            activeChanged,
            activeId: store.activeId,
            versions: store.versions.map((item) => fallbackVersionSummary(item, store.activeId))
          }
        });
        await auditEvent(req, 'fallback-version.deleted', 'fallback-version', deleted.id, { name: deleted.name, activeChanged });
      } catch (error) {
        json(res, 400, { error: { message: error.message || '删除降级版本失败。' } });
      }
      return true;
    }
    if (pathname === '/site-admin-api/bootstrap' && req.method === 'GET') {
      json(res, 200, { data: await getBootstrap(), meta: { previewUrl } });
      return true;
    }
    if (pathname === '/site-admin-api/save' && req.method === 'POST') {
      try {
        const payload = await bodyJson(req);
        const saved = await saveSection(payload);
        const isLead = payload.section === 'lead';
        await auditEvent(
          req,
          isLead ? 'lead.updated' : 'content.saved',
          isLead ? 'lead' : String(payload.section || 'unknown'),
          saved?.id || saved?.slug || '',
          isLead ? { status: saved?.status, priority: saved?.priority } : { section: payload.section }
        );
        json(res, 200, { data: await getBootstrap(), meta: { savedAt: new Date().toISOString(), saved } });
      } catch (error) {
        json(res, error.code === 'CONTENT_REVISION_CONFLICT' ? 409 : 400, { error: { message: error.message || '保存失败。' } });
      }
      return true;
    }
    return false;
  };
}
