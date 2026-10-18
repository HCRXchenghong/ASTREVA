import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { createAdminSession } from './test-session-helper.mjs';

async function waitFor(base, timeoutMs = 8000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${base}/healthz`);
      if (response.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 60));
  }
  throw new Error(`后台未能在 ${timeoutMs}ms 内启动`);
}

test('匿名埋点：未同意也计入 UV，且不影响访问明细与危险日志', { timeout: 30000 }, async (t) => {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-anon-data-'));
  const uploadDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-anon-upload-'));
  const port = 21000 + Math.floor(Math.random() * 20000);
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: 'analytics-anonymous-test-secret',
      ADMIN_ANALYTICS_ANON_SALT: 'analytics-anonymous-salt',
      // 让测试用 X-Forwarded-For 模拟不同访客 IP。
      ADMIN_TRUST_PROXY: 'true',
      ADMIN_CORS_ALLOWED_ORIGINS: 'https://www.example.com',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: uploadDir,
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild'
    },
    stdio: 'ignore'
  });
  const base = `http://127.0.0.1:${port}`;
  const userAgent =
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36';
  const beacon = (ip, payload) => fetch(`${base}/site-admin-api/analytics/visit`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Origin: 'https://www.example.com',
      'User-Agent': userAgent,
      'X-Forwarded-For': ip
    },
    body: JSON.stringify(payload)
  });

  try {
    await waitFor(base);
    const session = await createAdminSession(base);
    const authHeaders = { Cookie: session.cookieHeader };
    const overview = async () => (await (await fetch(`${base}/site-admin-api/analytics/overview`, { headers: authHeaders })).json()).data;
    const visits = async () => (await (await fetch(`${base}/site-admin-api/analytics/visits?limit=100`, { headers: authHeaders })).json()).data;

    // 1) 匿名上报：即使客户端伪造了 visitor_id 也不采信，服务端只认派生匿名键。
    const forged = await beacon('198.51.100.10', { path: '/forged', anonymous: true, visitor_id: 'attacker_supplied', session_id: 'attacker_session' });
    assert.equal(forged.status, 202);
    // 2) 匿名上报两次：同 IP 同 UA 同路径 -> uv=1、pv=2（加上伪造那条共 pv=3）。
    for (let index = 0; index < 2; index += 1) {
      const response = await beacon('198.51.100.10', { path: '/landing?a=1', referrer: 'https://google.com/search', host: 'www.example.com', event_type: 'pageview', anonymous: true });
      assert.equal(response.status, 202);
    }

    let summary = await overview();
    assert.equal(summary.today.pv, 3, '匿名 pageview 全部计入 PV');
    assert.equal(summary.today.uv, 1, '同 IP 匿名上报当日去重为 1 个 UV');

    let rows = await visits();
    const anonymousRows = rows.filter((item) => item.anonymous === true);
    assert.equal(anonymousRows.length, 3);
    assert.ok(anonymousRows.every((item) => /^anon_[0-9a-f]{32}$/.test(item.visitorId)), '匿名访问的 visitor_id 应是服务端派生的匿名键');
    assert.ok(rows.every((item) => item.visitorId !== 'attacker_supplied'), '匿名上报不得采信客户端 visitor_id');
    assert.ok(anonymousRows.every((item) => item.sessionId === ''), '匿名上报不保存 session_id');
    // 访问明细仍保留 IP / UA，供安全审计。
    assert.ok(anonymousRows.every((item) => item.ip === '198.51.100.10'));
    assert.match(anonymousRows[0].userAgent, /Chrome/);
    assert.equal(anonymousRows.find((item) => item.path === '/landing').path, '/landing');

    // 3) 不同 IP 的匿名访客 -> uv=2。
    assert.equal((await beacon('203.0.113.7', { path: '/landing', anonymous: true })).status, 202);
    summary = await overview();
    assert.equal(summary.today.pv, 4);
    assert.equal(summary.today.uv, 2, '不同 IP 的匿名访客计为不同 UV');

    // 4) 已同意上报：按前端真实 visitor_id 计数，同 id 不同 IP 不再新增 UV。
    assert.equal((await beacon('192.0.2.50', { path: '/pricing', visitor_id: 'vis_real_1', session_id: 'ses_real_1' })).status, 202);
    assert.equal((await beacon('192.0.2.51', { path: '/pricing', visitor_id: 'vis_real_1', session_id: 'ses_real_1' })).status, 202);
    summary = await overview();
    assert.equal(summary.today.pv, 6);
    assert.equal(summary.today.uv, 3, '匿名键与真实标识分别去重，真实 id 跨 IP 保持 1 个 UV');

    rows = await visits();
    const identified = rows.find((item) => item.visitorId === 'vis_real_1');
    assert.ok(identified);
    assert.equal(identified.anonymous, false);
    assert.equal(identified.sessionId, 'ses_real_1');

    // 5) 匿名采集不影响危险日志：危险请求照常记 danger，匿名访问明细照常进系统日志。
    const dangerResponse = await fetch(`${base}/wp-admin/setup.php`, {
      headers: { 'User-Agent': 'sqlmap/1.7#stable (http://sqlmap.org)' }
    });
    assert.equal(dangerResponse.status, 404);
    await new Promise((resolve) => setTimeout(resolve, 200));
    const events = await (await fetch(`${base}/site-admin-api/system/events?limit=200`, { headers: authHeaders })).json();
    const suspicious = events.data.find((item) => item.action === 'security.suspicious_request');
    assert.ok(suspicious, '危险请求仍应写入系统日志');
    assert.equal(suspicious.level, 'danger');
    const visitEvents = events.data.filter((item) => item.source === 'visit');
    assert.ok(visitEvents.some((item) => item.detail?.anonymous === true && item.ip === '198.51.100.10'), '匿名访问明细应进入系统日志且保留 IP');
  } finally {
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
