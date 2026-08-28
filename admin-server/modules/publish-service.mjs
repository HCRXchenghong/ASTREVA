/** 发布队列与构建 webhook 适配层。 */
export function createPublishService({ rebuildUrl, rebuildSecret, siteOrigin, platformStore }) {
  async function trigger(source = 'current') {
    const headers = { 'Content-Type': 'application/json' };
    if (rebuildSecret) headers.Authorization = `Bearer ${rebuildSecret}`;
    const response = await fetch(rebuildUrl, { method: 'POST', headers, body: JSON.stringify({ source }) });
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
      preview: { url: siteOrigin, ...preview },
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

  return { statusPayload, requestPublish };
}
