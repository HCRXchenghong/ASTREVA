import assert from 'node:assert/strict';
import http from 'node:http';
import test from 'node:test';
import { createAnalyticsRouter, createVisitorActivityForwarder } from './analytics-router.mjs';

/** 启动本地 http 桩，返回 base 地址与关闭函数。 */
function startStub(handler) {
  const server = http.createServer((req, res) => {
    // 超时/中断场景下避免未处理的 socket 错误影响测试进程。
    res.on('error', () => {});
    Promise.resolve(handler(req, res)).catch(() => {
      try {
        res.destroy();
      } catch {}
    });
  });
  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      resolve({
        base: `http://127.0.0.1:${address.port}`,
        close: () =>
          new Promise((done) => {
            // keep-alive 连接会让 close() 挂到超时；测试里直接切断。
            server.closeAllConnections?.();
            server.close(done);
          })
      });
    });
  });
}

/** 记录桩收到的请求；支持等待第一条请求到达。 */
function createRecorder() {
  const requests = [];
  const waiters = [];
  return {
    requests,
    record(entry) {
      requests.push(entry);
      const pending = waiters.splice(0);
      for (const resolve of pending) resolve(entry);
    },
    waitForRequest(timeoutMs = 2000) {
      if (requests.length) return Promise.resolve(requests[0]);
      return new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error('桩未在预期时间内收到请求')), timeoutMs);
        waiters.push((entry) => {
          clearTimeout(timer);
          resolve(entry);
        });
      });
    }
  };
}

async function readBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const text = Buffer.concat(chunks).toString('utf8');
  return text ? JSON.parse(text) : {};
}

/** 直接驱动 analytics router 的 handleBeacon，不依赖完整 admin-server。 */
function createBeaconHarness({ payload, forwarder = null, geo = {}, headers = {} }) {
  const visits = [];
  const response = { statusCode: 0, body: null };
  const router = createAnalyticsRouter({
    platformStore: {
      recordVisit: async (visit) => {
        visits.push(visit);
      }
    },
    bodyJson: async () => payload,
    json: (res, status, body) => {
      res.statusCode = status;
      res.body = body;
    },
    verifySession: () => true,
    applyPublicCors: () => true,
    resolveClient: () => ({ ip: '203.0.113.9', source: 'direct' }),
    countryFromRequest: geo.countryFromRequest || (() => 'CN'),
    regionFromRequest: geo.regionFromRequest,
    cityFromRequest: geo.cityFromRequest,
    visitorTimezoneFromRequest: geo.visitorTimezoneFromRequest,
    timezoneFromRequest: () => 'Asia/Shanghai',
    visitorActivityForwarder: forwarder
  });
  const req = {
    method: 'POST',
    url: '/site-admin-api/analytics/visit',
    headers: { 'user-agent': 'Mozilla/5.0 Chrome/126.0.0.0', host: 'www.example.com', ...headers }
  };
  return { router, req, response, visits };
}

function silentLogger(logs) {
  return { debug: (message) => logs.push(message) };
}

test('官网访问：可信代理地区信息写入访问记录并保留浏览器解析', async () => {
  const { router, req, response, visits } = createBeaconHarness({
    payload: { path: '/products/', visitor_id: 'vis_geo_1', timezone: '' },
    geo: {
      countryFromRequest: () => 'US',
      regionFromRequest: () => 'California',
      cityFromRequest: () => 'San Francisco',
      visitorTimezoneFromRequest: () => 'America/Los_Angeles'
    }
  });
  await router.handleBeacon(req, response);
  assert.equal(response.statusCode, 202);
  assert.deepEqual(
    {
      country: visits[0].country,
      region: visits[0].region,
      city: visits[0].city,
      timezone: visits[0].timezone,
      browser: visits[0].browser,
      os: visits[0].os,
      device: visits[0].device
    },
    {
      country: 'US',
      region: 'California',
      city: 'San Francisco',
      timezone: 'America/Los_Angeles',
      browser: 'Chrome',
      os: '',
      device: 'desktop'
    }
  );
});

test('访客活跃转发：配置 token 时把 visitor_id 与 path 原样发给客服引擎', async () => {
  const recorder = createRecorder();
  const stub = await startStub(async (req, res) => {
    const body = await readBody(req);
    recorder.record({ method: req.method, url: req.url, headers: req.headers, body });
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ ok: true }));
  });
  try {
    const forwarder = createVisitorActivityForwarder({
      apiUrl: `${stub.base}/`,
      token: 'secret-internal-token'
    });
    assert.equal(forwarder.enabled, true);

    const { router, req, response, visits } = createBeaconHarness({
      payload: { path: '/products/?utm=1#top', visitor_id: 'vis_real_1', session_id: 'ses_1' },
      forwarder
    });
    await router.handleBeacon(req, response);
    assert.equal(response.statusCode, 202);
    assert.equal(visits.length, 1);

    const received = await recorder.waitForRequest();
    assert.equal(received.method, 'POST');
    assert.equal(received.url, '/api/internal/visitor-activity');
    assert.equal(received.headers['x-internal-token'], 'secret-internal-token');
    assert.equal(received.headers['content-type'], 'application/json');
    assert.deepEqual(received.body, { visitor_id: 'vis_real_1', path: '/products/' });
  } finally {
    await stub.close();
  }
});

test('访客活跃转发：未同意的匿名上报用服务端派生的 anon_* 同样转发', async () => {
  const recorder = createRecorder();
  const stub = await startStub(async (req, res) => {
    recorder.record({ headers: req.headers, body: await readBody(req) });
    res.writeHead(200).end('{}');
  });
  try {
    const forwarder = createVisitorActivityForwarder({ apiUrl: stub.base, token: 'tok' });
    const { router, req, response, visits } = createBeaconHarness({
      payload: { path: '/landing', anonymous: true, visitor_id: 'attacker_supplied' },
      forwarder
    });
    await router.handleBeacon(req, response);
    assert.equal(response.statusCode, 202);
    assert.equal(visits.length, 1);
    assert.match(visits[0].visitorId, /^anon_[0-9a-f]{32}$/);

    const received = await recorder.waitForRequest();
    assert.match(received.body.visitor_id, /^anon_[0-9a-f]{32}$/);
    // 匿名上报不采信客户端 visitor_id，服务端派生键才是画像侧身份。
    assert.notEqual(received.body.visitor_id, 'attacker_supplied');
    assert.equal(received.body.path, '/landing');
  } finally {
    await stub.close();
  }
});

test('访客活跃转发：INTERNAL_API_TOKEN 未配置时完全不发请求', async () => {
  const recorder = createRecorder();
  const stub = await startStub(async (req, res) => {
    recorder.record({ url: req.url });
    res.writeHead(200).end('{}');
  });
  try {
    const forwarder = createVisitorActivityForwarder({ apiUrl: stub.base, token: '' });
    assert.equal(forwarder.enabled, false);
    const { router, req, response } = createBeaconHarness({
      payload: { path: '/pricing', visitor_id: 'vis_real_2' },
      forwarder
    });
    await router.handleBeacon(req, response);
    assert.equal(response.statusCode, 202);
    // 未启用时 forward() 直接跳过且不 reject。
    assert.deepEqual(await forwarder.forward({ visitorId: 'vis_real_2', path: '/pricing' }), {
      ok: false,
      skipped: 'disabled'
    });
    await new Promise((resolve) => setTimeout(resolve, 120));
    assert.equal(recorder.requests.length, 0, '没有 token 时不应发出任何服务间请求');
  } finally {
    await stub.close();
  }
});

test('访客活跃转发：visitor_id 为空时不转发', async () => {
  const recorder = createRecorder();
  const stub = await startStub(async (req, res) => {
    recorder.record({ url: req.url });
    res.writeHead(200).end('{}');
  });
  try {
    const forwarder = createVisitorActivityForwarder({ apiUrl: stub.base, token: 'tok' });
    const { router, req, response } = createBeaconHarness({
      payload: { path: '/pricing' },
      forwarder
    });
    await router.handleBeacon(req, response);
    assert.equal(response.statusCode, 202);
    await new Promise((resolve) => setTimeout(resolve, 120));
    assert.equal(recorder.requests.length, 0);
  } finally {
    await stub.close();
  }
});

test('访客活跃转发：上游 500 或超时都不影响埋点响应（仍为 202）', async () => {
  const logs = [];
  const recorder = createRecorder();
  const stub = await startStub(async (req, res) => {
    recorder.record({ url: req.url });
    if (req.url.includes('slow')) {
      // 超时场景：故意拖到超过 forwarder 的 timeout。
      setTimeout(() => {
        try {
          res.writeHead(200).end('{}');
        } catch {}
      }, 300);
      return;
    }
    res.writeHead(500, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: 'boom' }));
  });
  try {
    // 500：埋点立即 202，转发结果标记为失败。
    const failing = createVisitorActivityForwarder({
      apiUrl: stub.base,
      token: 'tok',
      logger: silentLogger(logs)
    });
    const failingHarness = createBeaconHarness({
      payload: { path: '/pricing', visitor_id: 'vis_real_3' },
      forwarder: failing
    });
    await failingHarness.router.handleBeacon(failingHarness.req, failingHarness.response);
    assert.equal(failingHarness.response.statusCode, 202);
    await recorder.waitForRequest();
    await new Promise((resolve) => setTimeout(resolve, 60));
    const failed = await failing.forward({ visitorId: 'vis_real_3', path: '/pricing' });
    assert.equal(failed.ok, false);
    assert.equal(failed.status, 500);

    // 超时：forwarder 不 reject，埋点也仍然 202。
    const slow = createVisitorActivityForwarder({
      apiUrl: `${stub.base}/slow`,
      token: 'tok',
      timeoutMs: 40,
      logger: silentLogger(logs)
    });
    const slowHarness = createBeaconHarness({
      payload: { path: '/pricing', visitor_id: 'vis_real_4' },
      forwarder: slow
    });
    await slowHarness.router.handleBeacon(slowHarness.req, slowHarness.response);
    assert.equal(slowHarness.response.statusCode, 202);
    const timedOut = await slow.forward({ visitorId: 'vis_real_4', path: '/pricing' });
    assert.equal(timedOut.ok, false);
    // 去重后的 debug 日志不会刷屏：同一种失败只记一次。
    const timeoutLogs = logs.filter((line) => /TimeoutError|转发失败/.test(line));
    assert.ok(timeoutLogs.length >= 1);
    const before = logs.length;
    await slow.forward({ visitorId: 'vis_real_4', path: '/pricing' });
    assert.equal(logs.length, before, '同一种失败重复发生时不重复记录');
  } finally {
    await stub.close();
  }
});

test('访客活跃转发：客服引擎端口不可达时埋点仍是 202 且不抛错', async () => {
  const probe = await startStub((req, res) => res.writeHead(200).end('{}'));
  const deadBase = probe.base;
  await probe.close();
  const logs = [];
  const forwarder = createVisitorActivityForwarder({
    apiUrl: deadBase,
    token: 'tok',
    timeoutMs: 500,
    logger: silentLogger(logs)
  });
  const { router, req, response } = createBeaconHarness({
    payload: { path: '/contact', visitor_id: 'vis_real_5' },
    forwarder
  });
  await router.handleBeacon(req, response);
  assert.equal(response.statusCode, 202);
  const result = await forwarder.forward({ visitorId: 'vis_real_5', path: '/contact' });
  assert.equal(result.ok, false);
  assert.ok(result.error, '应把连接失败记录为失败结果而不是抛出');
});
