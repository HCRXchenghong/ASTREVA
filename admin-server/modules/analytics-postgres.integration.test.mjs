import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import path from 'node:path';
import test from 'node:test';

/**
 * 真实 PostgreSQL 分支下的数据看板回归测试。
 *
 * 为什么必须单独有一个：既有的 analytics-system 测试用 `CMS_STORE_DRIVER=json`（内存/JSON 分支）跑，
 * 而「访问趋势」在 Postgres 分支里是一条手写 SQL。曾经这条 SQL 的参数从 $2 开始编号、$1 从未被引用，
 * PostgreSQL 直接报 "could not determine data type of parameter $1"，导致整个 /analytics/overview 500，
 * 而 JSON 分支完全正常——所以测试全绿、线上看板却是空的。
 *
 * 需要 TEST_DATABASE_URL（真实库）；未设置时跳过，由 scripts/verify-all.sh 第 11 步在临时 PG 上执行。
 */
const databaseURL = process.env.TEST_DATABASE_URL || '';

async function waitFor(base, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${base}/healthz`);
      if (response.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 80));
  }
  throw new Error(`后台未能在 ${timeoutMs}ms 内启动`);
}

test('数据看板在真实 PostgreSQL 分支下可用（访问趋势 SQL 回归）', { skip: !databaseURL }, async () => {
  const root = process.cwd();
  const port = 24000 + Math.floor(Math.random() * 10000);
  const sessionSecret = `analytics-pg-secret-${crypto.randomUUID()}`;
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      // 关键：走 Postgres 存储分支，而不是 JSON/内存分支。
      CMS_STORE_DRIVER: 'postgres',
      DATABASE_URL: databaseURL,
      CMS_DATA_ENCRYPTION_KEY: 'analytics-postgres-test-encryption-key-32',
      ADMIN_2FA_SESSION_SECRET: sessionSecret,
      ADMIN_SESSION_SECRET: sessionSecret,
      ADMIN_CORS_ALLOWED_ORIGINS: 'https://www.example.com',
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild'
    },
    stdio: 'ignore'
  });
  const base = `http://127.0.0.1:${port}`;

  try {
    await waitFor(base);
    const session = await createPgAdminSession(base, sessionSecret);
    const authHeaders = { Cookie: session.cookieHeader };
    const beaconHeaders = {
      'Content-Type': 'application/json',
      Origin: 'https://www.example.com',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36'
    };

    const visitorId = `vis_pg_${crypto.randomUUID().slice(0, 8)}`;
    for (const visitPath of ['/pg-regression-a', '/pg-regression-b']) {
      const posted = await fetch(`${base}/site-admin-api/analytics/visit`, {
        method: 'POST',
        headers: beaconHeaders,
        body: JSON.stringify({ path: visitPath, referrer: 'https://google.com/search', visitor_id: visitorId, session_id: 'ses_pg' })
      });
      assert.equal(posted.status, 202);
    }

    // 回归点：修好前这里 500（could not determine data type of parameter $1）。
    const overviewResponse = await fetch(`${base}/site-admin-api/analytics/overview?tz=Asia/Shanghai`, { headers: authHeaders });
    assert.equal(overviewResponse.status, 200, '数据看板汇总在 Postgres 分支下必须 200');
    const overview = await overviewResponse.json();
    assert.ok(overview.data.today, '应返回 today 汇总');
    assert.ok(overview.data.today.pv >= 2, `今天 PV 应包含刚上报的两次访问，实际 ${overview.data.today?.pv}`);
    assert.ok(overview.data.today.uv >= 1, '今天 UV 应至少包含一个访客');
    assert.ok(Array.isArray(overview.data.series), 'series 必须是数组');
    const today = new Date().toISOString().slice(0, 10);
    const entry = overview.data.series.find((item) => item.date === today) || overview.data.series.at(-1);
    assert.ok(entry, '访问趋势应至少有一天的数据（这条查询就是曾经的故障点）');
    assert.ok(entry.pv >= 2, `趋势里今天的 PV 应 >= 2，实际 ${entry?.pv}`);
    assert.ok(entry.uv >= 1, `趋势里今天的 UV 应 >= 1，实际 ${entry?.uv}`);
  } finally {
    server.kill('SIGKILL');
  }
});

/** 用真实 2FA 流程拿会话；Postgres 库里可能已有管理员，则退化为直接登录。 */
async function createPgAdminSession(base, secret) {
  const { createAdminSession } = await import('./test-session-helper.mjs');
  return createAdminSession(base, { secret });
}
