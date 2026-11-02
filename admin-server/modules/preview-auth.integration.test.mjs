import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import path from 'node:path';
import os from 'node:os';
import { mkdtemp, rm } from 'node:fs/promises';
import test from 'node:test';
import { createAdminSession } from './test-session-helper.mjs';

async function waitFor(base, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try { if ((await fetch(`${base}/healthz`)).ok) return; } catch {}
    await new Promise((r) => setTimeout(r, 80));
  }
  throw new Error('admin-server 未启动');
}

test('预览鉴权：构建 token + 短时 HttpOnly Cookie 同时校验', async () => {
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-preview-'));
  const token = 'b'.repeat(32);
  const fake = createServer((req, res) => {
    if (req.url === '/last-build') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ lastBuild: { status: 'succeeded', previewToken: token } }));
      return;
    }
    res.writeHead(404); res.end();
  });
  await new Promise((resolve) => fake.listen(0, '127.0.0.1', resolve));
  const fakePort = fake.address().port;
  const port = 25000 + Math.floor(Math.random() * 10000);
  const child = spawn('node', ['admin-server/server.mjs'], {
    cwd: process.cwd(),
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: path.join(dataDir, 'uploads'),
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      CMS_STORE_DRIVER: 'json',
      ADMIN_MEDIA_DRIVER: 'local',
      ADMIN_2FA_SESSION_SECRET: 'preview-test-session-secret-0123456789',
      ADMIN_SETUP_TOKEN: 'test-setup-token',
      ADMIN_CORS_ALLOWED_ORIGINS: 'http://127.0.0.1:4321',
      SITE_CONTENT_SEED_FILE: path.join(process.cwd(), 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: `http://127.0.0.1:${fakePort}/rebuild`
    },
    stdio: 'ignore'
  });
  const base = `http://127.0.0.1:${port}`;
  try {
    await waitFor(base);
    const session = await createAdminSession(base);
    const grant = await fetch(`${base}/site-admin-api/preview-grant`, { headers: { Cookie: session.cookieHeader } });
    assert.equal(grant.status, 200);
    const previewCookie = (grant.headers.get('set-cookie') || '').split(';')[0];
    assert.match(previewCookie, /^astreva_preview_access=/);

    const settings = await fetch(`${base}/site-admin-api/security-settings`, { headers: { Cookie: session.cookieHeader } });
    assert.equal(settings.status, 200);
    assert.equal((await settings.json()).data.previewTtlMinutes, 10);
    const patched = await fetch(`${base}/site-admin-api/security-settings`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', Cookie: session.cookieHeader },
      body: JSON.stringify({ preview_ttl_minutes: 5 })
    });
    assert.equal(patched.status, 200);
    assert.equal((await patched.json()).data.previewTtlMinutes, 5);
    const regrant = await fetch(`${base}/site-admin-api/preview-grant`, { headers: { Cookie: session.cookieHeader } });
    assert.match(regrant.headers.get('set-cookie') || '', /Max-Age=300/);

    const previewPath = encodeURIComponent(`/__preview/${token}/`);
    const denied = await fetch(`${base}/site-admin-api/preview-auth?path=${previewPath}`);
    assert.equal(denied.status, 401);

    const allowed = await fetch(`${base}/site-admin-api/preview-auth?path=${previewPath}`, { headers: { Cookie: previewCookie } });
    assert.equal(allowed.status, 204);

    const wrong = await fetch(`${base}/site-admin-api/preview-auth?path=${encodeURIComponent('/__preview/' + 'c'.repeat(32) + '/')}`, { headers: { Cookie: previewCookie } });
    assert.equal(wrong.status, 401);
  } finally {
    child.kill('SIGKILL');
    fake.close();
    await rm(dataDir, { recursive: true, force: true });
  }
});
