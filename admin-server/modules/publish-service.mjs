/** 发布队列与构建 webhook 适配层。 */
export function createPublishService({ rebuildUrl, rebuildSecret, siteOrigin, platformStore }) {
  async function trigger(source = 'current', extra = {}) {
    const headers = { 'Content-Type': 'application/json' };
    if (rebuildSecret) headers.Authorization = `Bearer ${rebuildSecret}`;
    const response = await fetch(rebuildUrl, { method: 'POST', headers, body: JSON.stringify({ source, ...extra }) });
    const body = await response.text();
    if (!response.ok) throw new Error(`重建服务返回 ${response.status}: ${body}`);
    try {
      return JSON.parse(body);
    } catch {
      return { ok: true, body };
    }
  }

  async function checkUrl(url) {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 3000);
      const response = await fetch(url, { signal: controller.signal });
      clearTimeout(timer);
      return { ok: response.ok, status: response.status, message: response.ok ? '可访问' : `HTTP ${response.status}` };
    } catch (error) {
      return { ok: false, status: null, message: error.name === 'AbortError' ? '连接超时' : error.message };
    }
  }

  async function statusPayload() {
    const rebuildStatusUrl = new URL('/last-build', rebuildUrl).toString();
    const [preview, rebuildResponse] = await Promise.all([
      checkUrl(siteOrigin),
      fetch(rebuildStatusUrl).then((response) => response.ok ? response.json() : null).catch(() => null)
    ]);
    const lastBuild = rebuildResponse?.lastBuild;
    if (!rebuildResponse?.running && !rebuildResponse?.queued && ['succeeded', 'failed'].includes(lastBuild?.status)) {
      await platformStore.settleLatestPublish(lastBuild.status, {
        exitCode: lastBuild.exitCode,
        finishedAt: lastBuild.finishedAt,
        errorMessage: lastBuild.status === 'failed' ? '官网静态构建失败，请查看构建服务日志。' : ''
      });
    }
    return {
      preview: {
        url: siteOrigin,
        // 草稿预览产物由构建服务写到发布卷的 preview/<随机 token>/；
        // 固定路径会让未发布草稿被任意人读取，因此 URL 必须带本次构建的 token。
        draftUrl: lastBuild?.previewToken
          ? `${siteOrigin.replace(/\/$/, '')}/__preview/${lastBuild.previewToken}/`
          : `${siteOrigin.replace(/\/$/, '')}/__preview/`,
        ...preview
      },
      rebuild: {
        url: rebuildStatusUrl,
        ok: Boolean(rebuildResponse),
        running: Boolean(rebuildResponse?.running),
        queued: Boolean(rebuildResponse?.queued),
        lastBuild: lastBuild || null,
        message: rebuildResponse ? '可访问' : '重建服务未启动'
      }
    };
  }

  let previewTokenCache = { value: '', expiresAt: 0 };

  /** 读取最近一次预览构建的随机 token；供 Caddy forward_auth 校验预览路径。 */
  async function previewToken() {
    if (previewTokenCache.expiresAt > Date.now()) return previewTokenCache.value;
    const statusUrl = new URL('/last-build', rebuildUrl).toString();
    const response = await fetch(statusUrl, { signal: AbortSignal.timeout(5000) });
    if (!response.ok) return '';
    const state = await response.json().catch(() => null);
    const value = String(state?.lastBuild?.previewToken || '');
    // 预览页会并发请求多个资源；短缓存避免每个资源都打一次构建服务。
    previewTokenCache = { value, expiresAt: Date.now() + 3000 };
    return value;
  }

  async function requestPublish(requestedBy = '', source = 'current') {
    const jobId = await platformStore.enqueuePublish(requestedBy);
    try {
      const rebuild = await trigger(source);
      await platformStore.markPublishRunning(jobId, { rebuildRequestedAt: new Date().toISOString(), source });
      return { jobId, rebuild };
    } catch (error) {
      await platformStore.completePublish(jobId, 'failed', error.message || '发布请求失败');
      throw Object.assign(error, { jobId });
    }
  }

  /** 草稿预览：不落库、不影响线上内容，只把编辑器里的内容快照构建到 preview/。 */
  async function requestPreview(content) {
    return trigger('preview', { content });
  }

  async function requestAutomaticRebuild() {
    await trigger('current');
    const statusUrl = new URL('/last-build', rebuildUrl).toString();
    const deadline = Date.now() + 120000;
    while (Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      const response = await fetch(statusUrl, { signal: AbortSignal.timeout(5000) });
      if (!response.ok) throw new Error(`无法确认前台构建结果（HTTP ${response.status}）。`);
      const state = await response.json();
      if (state.running || state.queued) continue;
      if (state.lastBuild?.status === 'succeeded') return state.lastBuild;
      if (state.lastBuild?.status === 'failed') throw new Error('前台自动构建失败。');
    }
    throw new Error('等待前台自动构建超时。');
  }

  return { statusPayload, previewToken, requestPublish, requestPreview, requestAutomaticRebuild };
}
