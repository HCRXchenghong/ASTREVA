import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import http from 'node:http';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

/** 直接发原始 path 的请求，避免 fetch/undici 对 URL 做归一化，确保测的是服务端行为。 */
function request(port, rawPath) {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port, path: rawPath, method: 'GET' }, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve({ status: res.statusCode, body: Buffer.concat(chunks).toString('utf8') }));
    });
    req.on('error', reject);
    req.end();
  });
}

async function waitFor(port, timeoutMs = 8000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await request(port, '/healthz');
      if (response.status === 200) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`后台未能在 ${timeoutMs}ms 内启动（端口 ${port}）`);
}

test('静态资源路由拒绝目录穿越，且不影响正常资源', async () => {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-static-data-'));
  const uploadDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-static-upload-'));
  const port = 20000 + Math.floor(Math.random() * 20000);
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: 'static-path-test-session-secret',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: uploadDir,
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild'
    },
    stdio: 'ignore'
  });

  try {
    await waitFor(port);

    // 正常静态资源必须仍然可访问。
    const logo = await request(port, '/assets/astreva-logo.png');
    assert.equal(logo.status, 200);
    const adminAsset = await request(port, '/site-admin-assets/unified-admin.js');
    assert.equal(adminAsset.status, 200);

    // 目录穿越必须被拒绝（403），绝不能返回仓库/容器内的文件内容。
    const traversals = [
      '/assets/%2e%2e%2f%2e%2e%2f%2e%2e%2fpackage.json',
      '/assets/%2e%2e%2f%2e%2e%2f%2e%2e%2f%2e%2e%2fetc%2fpasswd',
      '/assets/..%2f..%2f..%2fpackage.json',
      '/site-admin-assets/..%2f..%2fpackage.json',
      '/site-admin-assets/%2e%2e%2f%2e%2e%2fpackage.json',
      '/uploads/site-admin/..%2f..%2f..%2fpackage.json'
    ];
    const results = await Promise.all(traversals.map((rawPath) => request(port, rawPath)));
    for (const [index, result] of results.entries()) {
      assert.equal(result.status, 403, `${traversals[index]} 应返回 403，实际 ${result.status}`);
      assert.ok(!result.body.includes('"name": "astreva-independent-site"'), `${traversals[index]} 泄露了 package.json`);
    }

    // 非法百分号编码不应导致 500，而是 400。
    const malformed = await request(port, '/assets/%zz');
    assert.equal(malformed.status, 400);
  } finally {
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
