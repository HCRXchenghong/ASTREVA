/** 客服引擎的服务端代理：浏览器不会接触客服管理员口令。 */
export function createServiceBridgeProxy({ apiUrl, adminAccount, adminPassword, publicOrigin, bodyRaw, bodyJson, json, send, verifySession, onConfigChange = null }) {
  let adminSession = { token: '', expiresAt: 0 };

  function isAllowedPath(pathname) {
    return pathname === '/api/contact-settings' ||
      pathname.startsWith('/api/admin/') ||
      // 客服引擎上传的图片/语音：统一后台需要同源代理才能在消息里回显。
      pathname.startsWith('/uploads/') ||
      /^\/api\/conversations\/[^/]+\/(messages|read|remark|close)(?:\/[^/]+\/revoke)?$/.test(pathname);
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
    if (!isAllowedPath(targetPath)) {
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
      const body = await response.text();
      const headers = {
        'Content-Type': response.headers.get('content-type') || 'application/json; charset=utf-8',
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
