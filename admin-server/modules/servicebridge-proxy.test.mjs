import assert from 'node:assert/strict';
import test from 'node:test';
import http from 'node:http';
import { createServiceBridgeProxy } from './servicebridge-proxy.mjs';

/** 桩客服引擎：只需支持管理员登录与 AI 绑定读写，用于验证代理的缓存失效时机。 */
async function startFakeServiceBridge() {
  const calls = [];
  const server = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf8');
    calls.push({ method: req.method, url: req.url, auth: req.headers.authorization || '', body: raw ? JSON.parse(raw) : null });
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/api/admin/login') {
      res.end(JSON.stringify({ token: 'fake-admin-token' }));
      return;
    }
    if (String(req.url).startsWith('/api/admin/ai-bindings') || String(req.url).startsWith('/api/admin/ai-providers')) {
      res.end(JSON.stringify({ bindings: [] }));
      return;
    }
    res.end(JSON.stringify({ ok: true }));
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  return {
    calls,
    apiUrl: `http://127.0.0.1:${port}`,
    close: () => new Promise((resolve) => {
      server.closeAllConnections?.();
      server.close(() => resolve());
    })
  };
}

function createHarness(apiUrl, onConfigChange) {
  const proxy = createServiceBridgeProxy({
    apiUrl,
    adminAccount: 'admin',
    adminPassword: 'secret',
    publicOrigin: 'http://admin.local',
    bodyRaw: async () => null,
    bodyJson: async (req) => req.body,
    json: (res, status, payload) => { res.statusCode = status; res.body = JSON.stringify(payload); },
    send: (res, status, body, headers) => { res.statusCode = status; res.body = body; res.headers = headers || {}; },
    verifySession: () => true,
    onConfigChange
  });
  return proxy;
}

function fakeRequest(method, targetPath, body) {
  return {
    url: targetPath,
    method,
    headers: { 'content-type': 'application/json' },
    body
  };
}

test('代理：AI 供应商/绑定写操作成功后触发缓存失效回调', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const changed = [];
  const proxy = createHarness(bridge.apiUrl, (pathname) => { changed.push(pathname); });

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('PUT', '/site-admin-api/servicebridge/api/admin/ai-bindings', { bindings: [] }), res,
    '/site-admin-api/servicebridge/api/admin/ai-bindings');

  assert.equal(res.statusCode, 200);
  assert.deepEqual(changed, ['/api/admin/ai-bindings']);
  // 代理确实把写请求转发给了客服引擎，并且带上了受信管理员令牌。
  const forwarded = bridge.calls.find((call) => call.method === 'PUT');
  assert.ok(forwarded);
  assert.equal(forwarded.auth, 'Bearer fake-admin-token');
});

test('代理：读请求或非 AI 配置写请求不触发缓存失效', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const changed = [];
  const proxy = createHarness(bridge.apiUrl, (pathname) => { changed.push(pathname); });

  const readRes = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('GET', '/site-admin-api/servicebridge/api/admin/ai-bindings'), readRes,
    '/site-admin-api/servicebridge/api/admin/ai-bindings');
  assert.equal(readRes.statusCode, 200);

  const otherRes = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('PATCH', '/site-admin-api/servicebridge/api/admin/business-hours', { enabled: true }), otherRes,
    '/site-admin-api/servicebridge/api/admin/business-hours');
  assert.equal(otherRes.statusCode, 200);

  assert.deepEqual(changed, []);
});

test('代理：缓存失效回调抛错不影响控制台响应', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const proxy = createHarness(bridge.apiUrl, () => { throw new Error('invalidate boom'); });

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('DELETE', '/site-admin-api/servicebridge/api/admin/ai-providers/provider-1', undefined), res,
    '/site-admin-api/servicebridge/api/admin/ai-providers/provider-1');
  assert.equal(res.statusCode, 200);
});
