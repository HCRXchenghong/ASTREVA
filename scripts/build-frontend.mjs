import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const DEFAULT_FALLBACK_URL = 'http://127.0.0.1:1337/site-admin-api/public-fallback-content';
const DEFAULT_CURRENT_URL = 'http://127.0.0.1:1337/site-admin-api/public-content';

function isSiteContent(value) {
  return Boolean(
    value &&
    typeof value === 'object' &&
    value.site &&
    value.home &&
    value.productCenter &&
    value.support &&
    Array.isArray(value.categories) &&
    Array.isArray(value.products) &&
    Array.isArray(value.faqs) &&
    Array.isArray(value.staticPages)
  );
}

function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: ROOT, stdio: 'inherit', env: process.env, ...options });
    child.once('error', reject);
    child.once('exit', (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} ${args.join(' ')} 失败（退出码 ${code ?? 'null'}，信号 ${signal || '无'}）`));
    });
  });
}

export async function verifyActiveFallbackVersion() {
  const fallbackUrl = process.env.FALLBACK_CONTENT_URL || DEFAULT_FALLBACK_URL;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  try {
    const response = await fetch(fallbackUrl, { signal: controller.signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const payload = await response.json();
    if (!isSiteContent(payload?.data?.content)) throw new Error('响应中没有有效的站点内容');
    console.log(`[build] 将使用降级版本构建官网：${payload.data.version?.name || payload.data.version?.id || '未命名版本'}`);
    return payload.data;
  } catch (error) {
    throw new Error(`无法读取当前降级版本：${error.message}`);
  } finally {
    clearTimeout(timer);
  }
}

export async function readCurrentContent() {
  const currentUrl = process.env.CURRENT_CONTENT_URL || (
    process.env.CONTENT_API_URL
      ? `${process.env.CONTENT_API_URL.replace(/\/$/, '')}/site-admin-api/public-content`
      : DEFAULT_CURRENT_URL
  );
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5000);
  try {
    const response = await fetch(currentUrl, { signal: controller.signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const payload = await response.json();
    if (!isSiteContent(payload?.data)) throw new Error('响应中没有有效的站点内容');
    console.log('[build] 将使用当前已保存内容构建官网。');
    return { content: payload.data, version: { id: 'current', name: '当前内容' } };
  } catch (error) {
    throw new Error(`无法读取当前内容：${error.message}`);
  } finally {
    clearTimeout(timer);
  }
}

export async function resolveBuildSnapshot(source = 'current') {
  if (source === 'fallback') return verifyActiveFallbackVersion();
  if (source === 'current') return readCurrentContent();
  if (source !== 'auto') throw new Error(`不支持的构建内容源：${source}`);
  try {
    return await readCurrentContent();
  } catch (error) {
    console.warn(`[build] ${error.message}，改用当前启用的降级版本。`);
    return verifyActiveFallbackVersion();
  }
}

async function withSnapshotContentApi(snapshot, callback) {
  const server = createServer((req, res) => {
    if (req.method !== 'GET' || req.url !== '/site-admin-api/public-content') {
      res.writeHead(404);
      res.end('not found');
      return;
    }
    res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
    res.end(JSON.stringify({ data: snapshot.content, meta: { source: snapshot.version?.id === 'current' ? 'current-content' : 'active-fallback-version', version: snapshot.version } }));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  const contentApiUrl = `http://127.0.0.1:${address.port}`;
  try {
    return await callback(contentApiUrl);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

async function publishBuild(distDir, publishDir) {
  const releasesDir = path.join(publishDir, 'releases');
  const releaseName = `release-${new Date().toISOString().replace(/[:.]/g, '-')}-${process.pid}`;
  const releaseDir = path.join(releasesDir, releaseName);
  const nextLink = path.join(publishDir, '.current-next');
  const currentLink = path.join(publishDir, 'current');

  await fs.mkdir(releasesDir, { recursive: true });
  await fs.rm(nextLink, { force: true });
  await fs.cp(distDir, releaseDir, { recursive: true });
  await fs.symlink(path.join('releases', releaseName), nextLink);
  await fs.rename(nextLink, currentLink);

  const entries = await fs.readdir(releasesDir, { withFileTypes: true });
  const releases = entries.filter((entry) => entry.isDirectory()).map((entry) => entry.name).sort().reverse();
  await Promise.all(releases.slice(3).map((name) => fs.rm(path.join(releasesDir, name), { recursive: true, force: true })));
  console.log(`[build] 已原子发布官网静态文件：${releaseName}`);
}

export async function buildFrontend(options = {}) {
  const source = options.source || process.env.BUILD_CONTENT_SOURCE || 'current';
  const snapshot = await resolveBuildSnapshot(source);
  await withSnapshotContentApi(snapshot, (contentApiUrl) => run('npm', ['run', 'build:frontend'], {
    env: { ...process.env, CONTENT_API_URL: contentApiUrl }
  }));
  const publishDir = process.env.PUBLISH_DIR || '';
  if (publishDir) await publishBuild(path.join(ROOT, 'frontend', 'dist'), path.resolve(publishDir));
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  buildFrontend({ source: process.env.BUILD_CONTENT_SOURCE || 'auto' }).catch((error) => {
    console.error(`[build] 官网构建失败：${error.message}`);
    process.exitCode = 1;
  });
}
