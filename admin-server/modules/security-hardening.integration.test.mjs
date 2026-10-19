import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { createAdminSession, totpCode } from './test-session-helper.mjs';

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

async function startServer(extraEnv = {}) {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-security-data-'));
  const uploadDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-security-upload-'));
  const port = 23000 + Math.floor(Math.random() * 20000);
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: 'security-test-session-secret',
      ADMIN_CORS_ALLOWED_ORIGINS: 'https://admin.example.com',
      ADMIN_LOGIN_MAX_FAILURES: '3',
      ADMIN_LOGIN_MAX_2FA_ATTEMPTS: '3',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: uploadDir,
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild',
      ...extraEnv
    },
    stdio: ['ignore', 'pipe', 'pipe']
  });
  const base = `http://127.0.0.1:${port}`;
  await waitFor(base);
  return { server, base };
}

test('安全加固：登录锁定、2FA 重放、会话撤销、响应头、来源校验与内容消毒', async () => {
  const { server, base } = await startServer();
  try {
    // 1) 安全响应头
    const health = await fetch(`${base}/healthz`);
    assert.match(health.headers.get('content-security-policy') || '', /default-src 'self'/);
    assert.equal(health.headers.get('x-content-type-options'), 'nosniff');
    assert.equal(health.headers.get('x-frame-options'), 'SAMEORIGIN');

    // 2) 连续登录失败触发锁定
    const attempt = () =>
      fetch(`${base}/site-admin-auth/login/start`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: 'locked@test.local', password: 'wrong-password' })
      });
    assert.equal((await attempt()).status, 401);
    assert.equal((await attempt()).status, 401);
    assert.equal((await attempt()).status, 401);
    const locked = await attempt();
    assert.equal(locked.status, 429, '第 4 次失败后应被临时锁定');
    assert.ok(Number(locked.headers.get('retry-after')) > 0);

    // 3) 正常会话 + 2FA 重放保护
    const session = await createAdminSession(base, { email: 'security@test.local' });
    const auth = { Cookie: session.cookieHeader };
    const bootstrap = await (await fetch(`${base}/site-admin-api/bootstrap`, { headers: auth })).json();
    assert.ok(bootstrap.data);

    const loginStart = await fetch(`${base}/site-admin-auth/login/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: session.email, password: session.password })
    });
    const challenge = await loginStart.json();
    // 绑定 2FA 时已经用掉了当前时间窗的验证码，这里取下一个时间窗的新码。
    const code = totpCode(session.secret, Date.now() + 30000);
    const first = await fetch(`${base}/site-admin-auth/login/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ challengeId: challenge.challengeId, code })
    });
    assert.equal(first.status, 200);

    const replayStart = await fetch(`${base}/site-admin-auth/login/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: session.email, password: session.password })
    });
    const replayChallenge = await replayStart.json();
    const replayed = await fetch(`${base}/site-admin-auth/login/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ challengeId: replayChallenge.challengeId, code })
    });
    assert.equal(replayed.status, 400, '同一个 TOTP 验证码不允许重复使用');

    // 4) 来源校验：非白名单 Origin 的写请求被拒
    const blockedOrigin = await fetch(`${base}/site-admin-api/save`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: session.cookieHeader, Origin: 'https://attacker.example' },
      body: JSON.stringify({ section: 'site', data: {}, revision: bootstrap.data.contentRevision })
    });
    assert.equal(blockedOrigin.status, 403);

    // 4b) 控制台自身来源（同源）必须放行：否则浏览器里所有写操作都会 403。
    //     这是真实发生过的故障——本地 ADMIN_CORS_ALLOWED_ORIGINS 指向官网地址，
    //     控制台自己的 Origin 不在白名单里，保存营业时间/推送/提醒/素材/AI 设置全部被拦。
    //     这里故意用不存在的 section：请求会通过来源校验、随后因参数非法被拒，
    //     既能证明"同源不被 403 拦"，又不会真的改动内容（否则会 bump revision，
    //     让后面用同一 revision 的保存步骤变成 409 版本冲突）。
    const sameOriginWrite = await fetch(`${base}/site-admin-api/save`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: session.cookieHeader, Origin: base },
      body: JSON.stringify({ section: '__origin_probe__', data: {}, revision: bootstrap.data.contentRevision })
    });
    assert.notEqual(sameOriginWrite.status, 403, '控制台同源写请求不应被来源校验拒绝');
    assert.equal(sameOriginWrite.status, 400, '非法 section 应通过来源校验后被参数校验拒绝');

    // 5) 富文本消毒：脚本与事件处理器写入前被清除
    const content = await (await fetch(`${base}/site-admin-api/public-content`)).json();
    const payload = structuredClone(content.data);
    if (Array.isArray(payload.staticPages) && payload.staticPages.length) {
      payload.staticPages[0].body = '<p>正常段落</p><script>alert(1)</script><img src="/x.png" onerror="alert(2)">';
    }
    payload.site.brandTagline = '消毒测试';
    const saved = await fetch(`${base}/site-admin-api/save`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: session.cookieHeader },
      body: JSON.stringify({ section: 'content', data: payload, revision: bootstrap.data.contentRevision })
    });
    assert.equal(saved.status, 200);
    const publicContent = await (await fetch(`${base}/site-admin-api/public-content`)).json();
    const publicBody = String(publicContent.data.staticPages?.[0]?.body || '');
    assert.ok(publicBody.includes('正常段落'));
    assert.doesNotMatch(publicBody, /<script/i);
    assert.doesNotMatch(publicBody, /onerror/i);

    // 6) 会话可撤销：登出后旧 Cookie 立即失效
    const logout = await fetch(`${base}/site-admin-auth/logout`, { method: 'POST', headers: auth });
    assert.ok(logout.status < 400);
    const afterLogout = await fetch(`${base}/site-admin-api/bootstrap`, { headers: auth });
    assert.equal(afterLogout.status, 401, '登出后旧会话必须立即失效');
  } finally {
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});

test('安全加固：生产环境缺少强随机密钥时拒绝启动', async () => {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-prodguard-data-'));
  const child = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      APP_ENV: 'production',
      PORT: '0',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: 'change-me-astreva-admin-session-secret',
      CMS_DATA_ENCRYPTION_KEY: 'change-me-at-least-32-random-characters',
      ADMIN_DATA_DIR: dataDir
    },
    stdio: ['ignore', 'pipe', 'pipe']
  });
  let stderr = '';
  child.stderr.on('data', (chunk) => {
    stderr += String(chunk);
  });
  const exitCode = await new Promise((resolve) => child.once('exit', resolve));
  assert.notEqual(exitCode, 0, '生产环境使用示例密钥必须启动失败');
  assert.match(stderr, /生产环境必须配置/);
});
