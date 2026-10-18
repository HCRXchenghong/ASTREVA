import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

const SECRET = 'analytics-system-test-secret';

function sessionCookie(secret = SECRET) {
  const payload = Buffer.from(
    JSON.stringify({ userId: 'admin_test', email: 'admin@test.local', exp: Date.now() + 600000 })
  ).toString('base64url');
  const signature = crypto.createHmac('sha256', secret).update(payload).digest('base64url');
  return `${payload}.${signature}`;
}

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

test('访问埋点、数据汇总、系统日志与健康度', async () => {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-analytics-data-'));
  const uploadDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-analytics-upload-'));
  const port = 21000 + Math.floor(Math.random() * 20000);
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: SECRET,
      ADMIN_CORS_ALLOWED_ORIGINS: 'https://www.example.com',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: uploadDir,
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild'
    },
    stdio: 'ignore'
  });
  const base = `http://127.0.0.1:${port}`;
  const authHeaders = { Cookie: `astreva_admin_session=${encodeURIComponent(sessionCookie())}` };
  const beaconHeaders = {
    'Content-Type': 'application/json',
    Origin: 'https://www.example.com',
    'User-Agent':
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36'
  };

  try {
    await waitFor(base);

    // 1) 公开埋点：允许来源可写，非允许来源拒绝
    const allowed = await fetch(`${base}/site-admin-api/analytics/visit`, {
      method: 'POST',
      headers: beaconHeaders,
      body: JSON.stringify({ path: '/products/?from=test', referrer: 'https://google.com/search', visitor_id: 'vis_test_1', session_id: 'ses_1' })
    });
    assert.equal(allowed.status, 202);
    const rejected = await fetch(`${base}/site-admin-api/analytics/visit`, {
      method: 'POST',
      headers: { ...beaconHeaders, Origin: 'https://attacker.example' },
      body: JSON.stringify({ path: '/' })
    });
    assert.equal(rejected.status, 403);
    await fetch(`${base}/site-admin-api/analytics/visit`, {
      method: 'POST',
      headers: { ...beaconHeaders, 'User-Agent': 'Googlebot/2.1 (+http://www.google.com/bot.html)' },
      body: JSON.stringify({ path: '/bot', visitor_id: 'vis_bot' })
    });

    // 2) 未登录不能读分析数据
    const unauthorized = await fetch(`${base}/site-admin-api/analytics/overview`);
    assert.equal(unauthorized.status, 401);

    // 3) 管理员读取汇总：过滤爬虫、统计今天
    const overview = await (await fetch(`${base}/site-admin-api/analytics/overview`, { headers: authHeaders })).json();
    assert.ok(overview.data.today.pv >= 1, '今天的 PV 应至少包含刚才的埋点');
    assert.ok(overview.data.today.uv >= 1, '今天的 UV 应至少包含一个访客');
    assert.ok(Array.isArray(overview.data.series));
    assert.equal(overview.data.today.leads, 0);

    // 4) 访问列表与详情：记录 IP、浏览器、来源
    const visits = await (await fetch(`${base}/site-admin-api/analytics/visits`, { headers: authHeaders })).json();
    const visit = visits.data.find((item) => item.visitorId === 'vis_test_1');
    assert.ok(visit, '应能查到刚上报的访问');
    assert.equal(visit.browser, 'Chrome');
    assert.match(visit.os, /macOS/);
    assert.equal(visit.path, '/products/');
    const detail = await (await fetch(`${base}/site-admin-api/system/events/visit_${visit.id}`, { headers: authHeaders })).json();
    assert.match(detail.data.userAgent, /Chrome/);
    assert.equal(detail.data.source, 'visit');

    // 5) 系统日志合并访问 + 危险识别
    const dangerResponse = await fetch(`${base}/wp-admin/setup.php`, {
      headers: { 'User-Agent': 'sqlmap/1.7#stable (http://sqlmap.org)' }
    });
    assert.equal(dangerResponse.status, 404);
    await new Promise((resolve) => setTimeout(resolve, 200));
    const events = await (await fetch(`${base}/site-admin-api/system/events?limit=200`, { headers: authHeaders })).json();
    assert.ok(events.data.some((item) => item.source === 'visit'), '系统日志应包含访客访问');
    const suspicious = events.data.find((item) => item.action === 'security.suspicious_request');
    assert.ok(suspicious, '应记录可疑请求');
    assert.equal(suspicious.level, 'danger');
    assert.equal(suspicious.category, 'security');

    // 6) 服务器健康度
    const health = await (await fetch(`${base}/site-admin-api/system/health`, { headers: authHeaders })).json();
    assert.ok(health.data.cpu.cores > 0);
    assert.ok(health.data.memory.totalBytes > 0);
    assert.ok(Array.isArray(health.data.disks) && health.data.disks.length >= 1);
    assert.ok(health.data.services.length >= 4);
    assert.equal(health.data.database.driver, 'json');

    // 7) 手工清理保留策略接口可用
    const purge = await fetch(`${base}/site-admin-api/system/events/purge`, { method: 'POST', headers: authHeaders });
    assert.equal(purge.status, 200);
  } finally {
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
