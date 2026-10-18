import assert from 'node:assert/strict';
import test from 'node:test';
import http from 'node:http';
import { createServiceBridgeProxy, normalizeUpstreamErrorBody } from './servicebridge-proxy.mjs';

/** 桩客服引擎：只需支持管理员登录与 AI 绑定读写，用于验证代理的缓存失效时机。 */
async function startFakeServiceBridge() {
  const calls = [];
  const server = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const raw = Buffer.concat(chunks).toString('utf8');
    calls.push({ method: req.method, url: req.url, auth: req.headers.authorization || '', body: raw ? JSON.parse(raw) : null });
    if (req.url === '/api/admin/login') {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ token: 'fake-admin-token' }));
      return;
    }
    if (String(req.url).startsWith('/api/admin/ai-bindings') || String(req.url).startsWith('/api/admin/ai-providers')) {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ bindings: [] }));
      return;
    }
    // 三种真实上游错误形状：Go 字符串 error、已归一化对象 error、纯文本 500。
    if (req.url === '/api/admin/fail-string') {
      res.statusCode = 400;
      res.setHeader('Content-Type', 'application/json; charset=utf-8');
      res.end(JSON.stringify({ error: 'bad_request', message: '参数不合法' }));
      return;
    }
    if (req.url === '/api/admin/fail-object') {
      res.statusCode = 400;
      res.setHeader('Content-Type', 'application/json; charset=utf-8');
      res.end(JSON.stringify({ error: { message: '对象错误体' } }));
      return;
    }
    if (req.url === '/api/admin/fail-text') {
      res.statusCode = 500;
      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      res.end('客服引擎内部错误');
      return;
    }
    if (req.url === '/api/admin/fail-binary') {
      res.statusCode = 404;
      res.setHeader('Content-Type', 'application/octet-stream');
      res.end('PK\u0003\u0004binary-payload');
      return;
    }
    res.setHeader('Content-Type', 'application/json');
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

test('代理：Go 字符串 error 归一化为对象 error，同时保留顶层 message', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const proxy = createHarness(bridge.apiUrl);

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('GET', '/site-admin-api/servicebridge/api/admin/fail-string'), res,
    '/site-admin-api/servicebridge/api/admin/fail-string');

  assert.equal(res.statusCode, 400);
  assert.match(res.headers['Content-Type'], /application\/json/);
  assert.deepEqual(JSON.parse(res.body), {
    error: { code: 'bad_request', message: '参数不合法' },
    message: '参数不合法'
  });
});

test('代理：已是对象 error 的响应原样透传（幂等）', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const proxy = createHarness(bridge.apiUrl);

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('GET', '/site-admin-api/servicebridge/api/admin/fail-object'), res,
    '/site-admin-api/servicebridge/api/admin/fail-object');

  assert.equal(res.statusCode, 400);
  assert.equal(res.body, JSON.stringify({ error: { message: '对象错误体' } }));
});

test('代理：纯文本 500 保持原样与原始 Content-Type', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const proxy = createHarness(bridge.apiUrl);

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('GET', '/site-admin-api/servicebridge/api/admin/fail-text'), res,
    '/site-admin-api/servicebridge/api/admin/fail-text');

  assert.equal(res.statusCode, 500);
  assert.match(res.headers['Content-Type'], /text\/plain/);
  assert.equal(res.body, '客服引擎内部错误');
});

test('代理：非 JSON 的二进制失败响应（如 /uploads/* 文件）原样透传', { timeout: 15000 }, async (t) => {
  const bridge = await startFakeServiceBridge();
  t.after(async () => { await bridge.close(); });
  const proxy = createHarness(bridge.apiUrl);

  const res = { statusCode: 0, body: '', headers: {} };
  await proxy(fakeRequest('GET', '/site-admin-api/servicebridge/api/admin/fail-binary'), res,
    '/site-admin-api/servicebridge/api/admin/fail-binary');

  assert.equal(res.statusCode, 404);
  assert.match(res.headers['Content-Type'], /application\/octet-stream/);
  assert.equal(res.body, 'PK\u0003\u0004binary-payload');
  assert.doesNotMatch(res.body, /"error"/);
});

test('归一化辅助：非 JSON、坏 JSON、数组与标量一律原样返回', () => {
  assert.equal(normalizeUpstreamErrorBody('', 'application/json'), '');
  assert.equal(normalizeUpstreamErrorBody('plain', 'text/plain'), 'plain');
  assert.equal(normalizeUpstreamErrorBody('{not json', 'application/json; charset=utf-8'), '{not json');
  assert.equal(normalizeUpstreamErrorBody('[1,2]', 'application/json'), '[1,2]');
  assert.equal(normalizeUpstreamErrorBody('"oops"', 'application/json'), '"oops"');
  assert.equal(normalizeUpstreamErrorBody('{"error":{"message":"m"}}', 'application/json'), '{"error":{"message":"m"}}');
  assert.equal(normalizeUpstreamErrorBody('{"ok":true}', 'application/json'), '{"ok":true}');
  // 缺少 message 时用 code 兜底，旧客户端仍能拿到可读文案。
  assert.equal(
    normalizeUpstreamErrorBody('{"error":"not_found"}', 'application/json'),
    JSON.stringify({ error: { code: 'not_found', message: 'not_found' }, message: 'not_found' })
  );
});
