function errorStatus(error, fallback = 400) {
  const message = String(error?.message || '');
  if (message.includes('不存在') || message.includes('已被删除')) return 404;
  if (message.includes('仍被') || message.includes('被官网内容引用')) return 409;
  if (message.includes('未配置') || message.includes('暂不可用')) return 503;
  return fallback;
}

/** Media-management HTTP boundary. Storage, PostgreSQL and AI details stay behind injected services. */
export function createMediaRouter({
  upload, replace, search, get, remove, references, referenceUrls, stats, storageHealth,
  listCategories, createCategory, deleteCategory, createPresignedUpload, confirmPresignedUpload,
  aiModels, aiGenerate, aiPreview, aiCommitPreview, aiDiscardPreview, runtimeStatus,
  bodyJson, json, verifySession, auditEvent
}) {
  async function referencesFor(media) {
    const matches = [];
    for (const url of referenceUrls(media)) {
      for (const field of await references(url)) matches.push(field);
    }
    return [...new Set(matches)].sort();
  }

  return async function handleMedia(req, res, pathname) {
    const uploadPath = ['/site-admin-api/upload', '/site-admin-api/media/upload'].includes(pathname) && req.method === 'POST';
    const listPath = pathname === '/site-admin-api/media' && req.method === 'GET';
    const statsPath = pathname === '/site-admin-api/media/stats' && req.method === 'GET';
    const categoriesPath = pathname === '/site-admin-api/media/categories';
    const categoryDeleteMatch = pathname.match(/^\/site-admin-api\/media\/categories\/(.+)$/);
    const batchDeletePath = pathname === '/site-admin-api/media/batch-delete' && req.method === 'POST';
    const presignPath = pathname === '/site-admin-api/media/presign' && req.method === 'POST';
    const confirmPath = pathname === '/site-admin-api/media/confirm' && req.method === 'POST';
    const aiModelsPath = pathname === '/site-admin-api/media/ai-models' && req.method === 'GET';
    const aiGeneratePath = pathname === '/site-admin-api/media/ai-generate' && req.method === 'POST';
    const aiPreviewPath = pathname === '/site-admin-api/media/ai-preview' && req.method === 'POST';
    const aiPreviewCommitMatch = pathname.match(/^\/site-admin-api\/media\/ai-preview\/([^/]+)\/commit$/);
    const aiPreviewMatch = pathname.match(/^\/site-admin-api\/media\/ai-preview\/([^/]+)$/);
    const runtimePath = pathname === '/site-admin-api/media/runtime' && req.method === 'GET';
    const referenceMatch = pathname.match(/^\/site-admin-api\/media\/([^/]+)\/references$/);
    const replaceMatch = pathname.match(/^\/site-admin-api\/media\/([^/]+)\/replace$/);
    const mediaMatch = pathname.match(/^\/site-admin-api\/media\/([^/]+)$/);
    const matched = uploadPath || listPath || statsPath || categoriesPath || categoryDeleteMatch || batchDeletePath || presignPath || confirmPath ||
      aiModelsPath || aiGeneratePath || aiPreviewPath || aiPreviewCommitMatch || aiPreviewMatch || runtimePath || referenceMatch || replaceMatch || mediaMatch;
    if (!matched) return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }

    try {
      if (listPath) {
        const url = new URL(req.url, 'http://admin.local');
        const result = await search({
          q: url.searchParams.get('q') || '', category: url.searchParams.get('category') || '', type: url.searchParams.get('type') || '',
          page: url.searchParams.get('page') || '1', pageSize: url.searchParams.get('pageSize') || '24'
        });
        json(res, 200, { data: result.items, meta: { total: result.total, page: result.page, pageSize: result.pageSize, pages: result.pages } });
        return true;
      }
      if (statsPath) {
        const [data, storage] = await Promise.all([stats(), storageHealth()]);
        json(res, 200, { data: { ...data, storage } });
        return true;
      }
      if (runtimePath) {
        json(res, 200, { data: await runtimeStatus() });
        return true;
      }
      if (categoriesPath && req.method === 'GET') {
        json(res, 200, { data: await listCategories() });
        return true;
      }
      if (categoriesPath && req.method === 'POST') {
        const payload = await bodyJson(req);
        const category = await createCategory(payload.name);
        await auditEvent(req, 'media.category.created', 'media-category', category.name, {});
        json(res, 201, { data: category });
        return true;
      }
      if (categoryDeleteMatch && req.method === 'DELETE') {
        const result = await deleteCategory(categoryDeleteMatch[1]);
        await auditEvent(req, 'media.category.deleted', 'media-category', result.name, { reassigned: result.reassigned });
        json(res, 200, { data: result });
        return true;
      }
      if (uploadPath) {
        const media = await upload(req, res);
        if (media) {
          await auditEvent(req, 'media.uploaded', 'media', media.id, {
            name: media.name, size: media.size, driver: media.storageDriver, category: media.category,
            sourceType: media.sourceType, variants: Array.isArray(media.variants?.webp) ? media.variants.webp.length : 0
          });
          json(res, 201, { data: media });
        }
        return true;
      }
      if (presignPath) {
        const direct = await createPresignedUpload(await bodyJson(req));
        await auditEvent(req, 'media.direct-upload.authorized', 'media', '', { key: direct.key, expiresAt: direct.expiresAt });
        json(res, 200, { data: direct });
        return true;
      }
      if (confirmPath) {
        const media = await confirmPresignedUpload((await bodyJson(req)).token);
        await auditEvent(req, 'media.direct-upload.confirmed', 'media', media.id, { name: media.name, size: media.size, key: media.key });
        json(res, 201, { data: media });
        return true;
      }
      if (aiModelsPath) {
        const url = new URL(req.url, 'http://admin.local');
        json(res, 200, { data: await aiModels({ refresh: url.searchParams.get('refresh') === 'true' }) });
        return true;
      }
      if (aiGeneratePath) {
        const media = await aiGenerate(await bodyJson(req));
        await auditEvent(req, 'media.ai.generated', 'media', media.id, {
          model: media.metadata?.ai?.model || '', size: media.metadata?.ai?.size || '', category: media.category
        });
        json(res, 201, { data: media });
        return true;
      }
      if (aiPreviewPath) {
        const preview = await aiPreview(await bodyJson(req));
        await auditEvent(req, 'media.ai.previewed', 'ai-preview', preview.id, {
          model: preview.model || '', size: preview.size || '', bytes: preview.bytes || 0
        });
        json(res, 201, { data: preview });
        return true;
      }
      if (aiPreviewCommitMatch && req.method === 'POST') {
        const media = await aiCommitPreview({ ...(await bodyJson(req)), previewId: aiPreviewCommitMatch[1] });
        await auditEvent(req, 'media.ai.preview-committed', 'media', media.id, {
          model: media.metadata?.ai?.model || '', size: media.metadata?.ai?.size || '', category: media.category
        });
        json(res, 201, { data: media });
        return true;
      }
      if (aiPreviewMatch && req.method === 'DELETE') {
        const discarded = aiDiscardPreview(aiPreviewMatch[1]);
        if (!discarded) throw new Error('AI 临时结果不存在或已过期。');
        await auditEvent(req, 'media.ai.preview-discarded', 'ai-preview', aiPreviewMatch[1], {});
        json(res, 200, { data: { id: aiPreviewMatch[1], discarded: true } });
        return true;
      }
      if (batchDeletePath) {
        const payload = await bodyJson(req);
        const ids = [...new Set(Array.isArray(payload.ids) ? payload.ids.map(String) : [])].slice(0, 100);
        if (!ids.length) throw new Error('请至少选择一个要删除的素材。');
        const deleted = [];
        const blocked = [];
        const failed = [];
        for (const id of ids) {
          try {
            const media = await get(id);
            if (!media) { failed.push({ id, message: '媒体文件不存在或已被删除。' }); continue; }
            const usedBy = await referencesFor(media);
            if (usedBy.length) { blocked.push({ id, name: media.name, references: usedBy }); continue; }
            deleted.push(await remove(id));
          } catch (error) {
            failed.push({ id, message: error.message || '删除失败。' });
          }
        }
        await auditEvent(req, 'media.batch-deleted', 'media', '', {
          requested: ids.length, deleted: deleted.map((item) => item.id), blocked: blocked.map((item) => item.id), failed
        });
        json(res, 200, { data: { deleted, blocked, failed } });
        return true;
      }
      if (referenceMatch && req.method === 'GET') {
        const media = await get(referenceMatch[1]);
        if (!media) {
          json(res, 404, { error: { message: '媒体文件不存在或已被删除。' } });
          return true;
        }
        json(res, 200, { data: { media, references: await referencesFor(media) } });
        return true;
      }
      if (replaceMatch && ['PUT', 'POST'].includes(req.method)) {
        const media = await replace(req, res, replaceMatch[1]);
        await auditEvent(req, 'media.replaced', 'media', media.id, {
          name: media.name, key: media.key, category: media.category, variants: media.variants?.webp?.length || 0
        });
        json(res, 200, { data: media });
        return true;
      }
      if (mediaMatch && req.method === 'GET') {
        const media = await get(mediaMatch[1]);
        if (!media) json(res, 404, { error: { message: '媒体文件不存在或已被删除。' } });
        else json(res, 200, { data: media });
        return true;
      }
      if (mediaMatch && req.method === 'DELETE') {
        const media = await get(mediaMatch[1]);
        if (!media) throw new Error('媒体文件不存在或已被删除。');
        const usedBy = await referencesFor(media);
        if (usedBy.length) {
          json(res, 409, { error: { message: '媒体文件仍被官网内容引用，请先替换对应字段。' }, data: { references: usedBy } });
          return true;
        }
        const deleted = await remove(media.id);
        await auditEvent(req, 'media.deleted', 'media', deleted.id, { name: deleted.name, key: deleted.key, driver: deleted.storageDriver });
        json(res, 200, { data: deleted });
        return true;
      }
      json(res, 405, { error: { message: '该媒体接口不支持当前请求方法。' } });
      return true;
    } catch (error) {
      json(res, errorStatus(error), { error: { message: error.message || '媒体操作失败。' } });
      return true;
    }
  };
}
