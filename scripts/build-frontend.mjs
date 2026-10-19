import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { resolveFrontend } from './frontend-target.mjs';

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

export async function withSnapshotContentApi(snapshot, callback) {
  const server = createServer((req, res) => {
    // 只比对 pathname，不要比对整个 req.url：前端请求带 `?locale=zh-CN`（commit 097c4be 加的），
    // 早先这里用 `req.url !== '/site-admin-api/public-content'` 精确比较，于是快照服务回 404、
    // 构建失败、dist 被清空 → 控制台「保存并发布」100% 失败且官网直接 404。
    let pathname = '';
    try {
      pathname = new URL(req.url || '/', 'http://127.0.0.1').pathname;
    } catch {
      pathname = '';
    }
    if (req.method !== 'GET' || pathname !== '/site-admin-api/public-content') {
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

/** 把草稿预览产物写入 publishDir/preview，不影响 current 正式发布。 */
async function publishPreview(distDir, publishDir) {
  const previewDir = path.join(publishDir, 'preview');
  await fs.rm(previewDir, { recursive: true, force: true });
  await fs.cp(distDir, previewDir, { recursive: true });
  console.log('[build] 已生成草稿预览产物：preview/');
}

export async function buildFrontend(options = {}) {
  const source = options.source || process.env.BUILD_CONTENT_SOURCE || 'current';
  const isPreview = options.target === 'preview';
  const frontend = resolveFrontend(options.frontend);
  const snapshot = options.snapshotContent
    ? { content: options.snapshotContent, version: { id: 'draft-preview', name: '草稿预览' } }
    : await resolveBuildSnapshot(source);
  await withSnapshotContentApi(snapshot, async (contentApiUrl) => {
    // 契约校验与构建都跟着 frontend.name 走，新旧前端各查各的。
    // 复用 build-site.mjs，避免"哪些步骤算构建"在两处各写一遍。
    await run('node', ['scripts/build-site.mjs'], {
      env: { ...process.env, FRONTEND_DIR: frontend.name, CONTENT_API_URL: contentApiUrl }
    });
  });
  const publishDir = process.env.PUBLISH_DIR || '';
  if (!publishDir) return;
  const resolvedPublishDir = path.resolve(publishDir);
  const distDir = frontend.dist;
  if (isPreview) await publishPreview(distDir, resolvedPublishDir);
  else await publishBuild(distDir, resolvedPublishDir);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  buildFrontend({ source: process.env.BUILD_CONTENT_SOURCE || 'auto' }).catch((error) => {
    console.error(`[build] 官网构建失败：${error.message}`);
    process.exitCode = 1;
  });
}
