import { sanitizeCmsContent } from './html-sanitize.mjs';

/** 发布队列、构建状态与审计记录的 HTTP 边界。 */
export function createPublishRouter({ platformStore, publishService, verifySession, auditEvent, json, publicOrigin }) {
  return async function handlePublish(req, res, pathname) {
    const isRoute =
      pathname === '/site-admin-api/audit-events' ||
      pathname === '/site-admin-api/publish-jobs' ||
      pathname === '/site-admin-api/status' ||
      pathname === '/site-admin-api/publish' ||
      pathname === '/site-admin-api/preview';
    if (!isRoute) return false;
    const session = verifySession(req);
    if (!session) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    const limit = new URL(req.url || '/', publicOrigin).searchParams.get('limit');
    if (pathname === '/site-admin-api/audit-events' && req.method === 'GET') {
      json(res, 200, { data: await platformStore.listAudit(limit) });
      return true;
    }
    if (pathname === '/site-admin-api/publish-jobs' && req.method === 'GET') {
      json(res, 200, { data: await platformStore.listPublishJobs(limit) });
      return true;
    }
    if (pathname === '/site-admin-api/status' && req.method === 'GET') {
      json(res, 200, { data: await publishService.statusPayload() });
      return true;
    }
    if (pathname === '/site-admin-api/publish' && req.method === 'POST') {
      try {
        const source = new URL(req.url || '/', publicOrigin).searchParams.get('source') === 'fallback' ? 'fallback' : 'current';
        const { jobId, rebuild } = await publishService.requestPublish(session.userId || '', source);
        await auditEvent(req, 'publish.requested', 'publish-job', jobId, { queued: Boolean(rebuild.queued), source });
        json(res, 202, { data: { ok: true, jobId, rebuild } });
      } catch (error) {
        await auditEvent(req, 'publish.request.failed', 'publish-job', error.jobId || '', { message: error.message || '' });
        json(res, 503, { error: { message: `重建服务未启动或发布失败：${error.message}` } });
      }
      return true;
    }
    // 草稿预览：把编辑器当前内容构建到 /__preview/，不修改线上内容。
    if (pathname === '/site-admin-api/preview' && req.method === 'POST') {
      try {
        const payload = await bodyJson(req, 8 * 1024 * 1024);
        const rawContent = payload?.content;
        if (!rawContent || typeof rawContent !== 'object' || !rawContent.site || !rawContent.home) {
          json(res, 400, { error: { message: '预览内容无效：缺少 site/home 节点。' } });
          return true;
        }
        // 预览产物会被浏览器以源码方式渲染：先走与正式保存相同的富文本/链接消毒，
        // 避免“未保存草稿”成为绕过写入消毒的 XSS 通道。
        const content = sanitizeCmsContent(rawContent);
        const rebuild = await publishService.requestPreview(content);
        await auditEvent(req, 'publish.preview-requested', 'site-content', 'preview', {});
        json(res, 202, { data: { ok: true, rebuild } });
      } catch (error) {
        json(res, 503, { error: { message: `草稿预览构建失败：${error.message}` } });
      }
      return true;
    }
    return false;
  };
}
