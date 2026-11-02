/**
 * 统一错误体：客服引擎（Go）失败时返回 `{"error":"<code>","message":"..."}`，
 * 而 admin-server 自己的错误体是 `{"error":{"message":"..."}}`。控制台已经双形状兼容，
 * 但两种形状混用会持续产生歧义，所以在代理层收敛：
 *   `{"error":"code","message":"msg"}` -> `{"error":{"code":"code","message":"msg"},"message":"msg"}`
 *
 * 这条归一化**只**作用于非 2xx 的 JSON 响应：
 * - 已经是 `{"error":{"...":...}}` 形状的响应原样透传（幂等）；
 * - 非 JSON（纯文本 500、上传/下载的二进制、流式图片/语音）一律原样返回，
 *   避免改写 Content-Type 与字节内容；
 * - JSON 数组、标量、以及没有字符串 error 字段的 JSON 都视为"无法识别"，原样返回。
 * 顶层 message 继续保留，旧客户端即使仍按字符串 error 读取，也能先拿到可读文案。
 */
export function normalizeUpstreamErrorBody(text, contentType) {
  if (!text) return text;
  if (!/json/i.test(String(contentType || ''))) return text;
  let payload;
  try {
    payload = JSON.parse(text);
  } catch {
    return text;
  }
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return text;
  // 只认 Go 侧的字符串 error；对象 error（admin-server / 已归一化）原样透传。
  if (typeof payload.error !== 'string') return text;
  const code = payload.error;
  const message = typeof payload.message === 'string' && payload.message ? payload.message : code;
  // 展开原字段以保留上游附带的额外信息（如 details），再覆盖 error/message。
  return JSON.stringify({ ...payload, error: { code, message }, message });
}

/** 客服引擎的服务端代理：浏览器不会接触客服管理员口令。 */
export function createServiceBridgeProxy({ apiUrl, adminAccount, adminPassword, internalToken = '', publicOrigin, bodyRaw, bodyJson, json, send, verifySession, onConfigChange = null }) {
  let adminSession = { token: '', expiresAt: 0 };

  function isAllowedPath(pathname) {
    return pathname === '/api/contact-settings' ||
      pathname.startsWith('/api/admin/') ||
      // 客服引擎上传的图片/语音：统一后台需要同源代理才能在消息里回显。
      pathname.startsWith('/uploads/') ||
      /^\/api\/conversations\/[^/]+\/(messages|read|remark|close)(?:\/[^/]+\/revoke)?$/.test(pathname);
  }

  // 供应商明文配置只允许服务间读取；浏览器侧代理必须拒绝，否则任何后台会话都能导出 Key。
  function isBrowserAllowedPath(pathname) {
    if (/^\/api\/admin\/ai-providers\/[^/]+\/(secret|resolve)$/.test(pathname)) return false;
    return isAllowedPath(pathname);
  }

  // 控制台保存 AI 供应商/能力绑定后，立即让 admin-server 的供应商缓存失效。
  function isAiConfigPath(pathname) {
    return /^\/api\/admin\/ai-(?:providers|bindings)(?:\/|$)/.test(pathname);
  }

  async function adminToken(forceRefresh = false, signal = undefined) {
    if (!forceRefresh && adminSession.token && adminSession.expiresAt > Date.now()) return adminSession.token;
    const response = await fetch(`${apiUrl}/api/admin/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ account: adminAccount, password: adminPassword }),
      signal
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.token) {
      throw new Error(payload.message || payload.error?.message || `客服引擎管理员登录失败（HTTP ${response.status}）`);
    }
    adminSession = { token: payload.token, expiresAt: Date.now() + 12 * 60 * 60 * 1000 };
    return adminSession.token;
  }

  async function adminJson(targetPath, options = {}) {
    if (!isAllowedPath(targetPath)) throw new Error('该客服接口未开放给统一后台。');
    const method = options.method || 'GET';
    const query = options.query || '';
    const call = async (forceRefresh = false) => fetch(`${apiUrl}${targetPath}${query}`, {
      method,
      headers: {
        Authorization: `Bearer ${await adminToken(forceRefresh, options.signal)}`,
        ...(internalToken && /^\/api\/admin\/ai-providers\/[^/]+\/(secret|resolve)$/.test(targetPath)
          ? { 'X-Internal-Token': internalToken }
          : {}),
        ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }),
        ...(options.headers || {})
      },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal
    });
    let response = await call(false);
    if (response.status === 401 || response.status === 403) response = await call(true);
    const text = await response.text();
    let payload = {};
    try { payload = text ? JSON.parse(text) : {}; } catch { payload = { message: text }; }
    if (!response.ok) {
      const message = typeof payload.error === 'string'
        ? payload.message || payload.error
        : payload.error?.message || payload.message;
      throw new Error(message || `客服引擎请求失败（HTTP ${response.status}）`);
    }
    return payload;
  }

  const proxy = async function proxy(req, res, pathname) {
    const prefix = '/site-admin-api/servicebridge';
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return;
    }
    const targetPath = pathname.slice(prefix.length);
    if (!isBrowserAllowedPath(targetPath)) {
      json(res, 403, { error: { message: '该客服接口未开放给统一后台。' } });
      return;
    }

    const query = new URL(req.url || '/', publicOrigin).search;
    const contentType = req.headers['content-type'] || '';
    const isMultipart = contentType.startsWith('multipart/form-data');
    const writeMethods = ['POST', 'PATCH', 'PUT'].includes(req.method || '');
    const rawBody = writeMethods && isMultipart ? await bodyRaw(req) : null;
    const requestBody = writeMethods && !isMultipart ? await bodyJson(req) : null;
    const call = async (forceRefresh = false) => fetch(`${apiUrl}${targetPath}${query}`, {
      method: req.method,
      headers: {
        Authorization: `Bearer ${await adminToken(forceRefresh)}`,
        ...(isMultipart ? { 'Content-Type': contentType } : {}),
        ...(requestBody ? { 'Content-Type': 'application/json' } : {})
      },
      body: isMultipart ? rawBody : (requestBody ? JSON.stringify(requestBody) : undefined)
    });

    try {
      let response = await call(false);
      if (response.status === 401 || response.status === 403) response = await call(true);
      const upstreamContentType = response.headers.get('content-type') || 'application/json; charset=utf-8';
      let body = await response.text();
      // 仅收敛失败响应的错误体；成功响应与二进制/流式响应不做任何改写。
      if (!response.ok) body = normalizeUpstreamErrorBody(body, upstreamContentType);
      const headers = {
        'Content-Type': upstreamContentType,
        'Cache-Control': 'no-store'
      };
      if (response.headers.get('content-disposition')) headers['Content-Disposition'] = response.headers.get('content-disposition');
      // 控制台保存 AI 供应商/绑定成功后立即失效 admin-server 的供应商缓存；
      // 回调失败只影响缓存时效，绝不能影响控制台响应。
      if (response.ok && ['POST', 'PUT', 'PATCH', 'DELETE'].includes(req.method || '') && isAiConfigPath(targetPath)) {
        try { await onConfigChange?.(targetPath); } catch {}
      }
      send(res, response.status, body, headers);
    } catch (error) {
      json(res, 503, { error: { message: `客服引擎不可用：${error.message}` } });
    }
  };
  proxy.adminJson = adminJson;
  return proxy;
}
