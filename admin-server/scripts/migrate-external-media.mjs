import { createWriteStream, existsSync, readFileSync } from 'node:fs';
import fs from 'node:fs/promises';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import crypto from 'node:crypto';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createPlatformStore } from '../modules/platform-store.mjs';
import { createMediaStore } from '../modules/media-store.mjs';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(SCRIPT_DIR, '../..');
const SEED_FILE = path.join(ROOT, 'admin-server', 'seed', 'site-content.json');
const EXTERNAL_HOSTS = new Set(['images.unsplash.com', 'assets.mixkit.co']);
const DEMO_TESTIMONIAL_TEXTS = new Set([
  '星渡ASTREVA 的模块化系统彻底改变了我们的施工方式。原本需要两周的排水沟安装，现在仅仅三天就完成了。太棒了！',
  '从外观设计到水力性能，这都是我见过最优秀的排水产品。它能完美融入我们设计的现代商业广场。',
  '他们提供的不仅是产品，更是完整的解决方案。遇到技术难题时，服务团队的支持非常及时专业。'
]);
const MAX_DOWNLOAD_BYTES = Number(process.env.EXTERNAL_MEDIA_MAX_BYTES || 200 * 1024 * 1024);
const DOWNLOAD_TIMEOUT_MS = Number(process.env.EXTERNAL_MEDIA_TIMEOUT_MS || 20_000);

function loadDotEnv() {
  const file = path.join(ROOT, '.env');
  if (!existsSync(file)) return;
  for (const line of readFileSync(file, 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    const key = trimmed.slice(0, index).trim();
    const value = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
    if (!process.env[key]) process.env[key] = value;
  }
}

function externalUrl(value) {
  if (typeof value !== 'string' || !/^https?:\/\//.test(value)) return false;
  try { return EXTERNAL_HOSTS.has(new URL(value).host); } catch { return false; }
}

function collectExternalUrls(value, urls = new Set()) {
  if (Array.isArray(value)) value.forEach((item) => collectExternalUrls(item, urls));
  else if (value && typeof value === 'object') Object.values(value).forEach((item) => collectExternalUrls(item, urls));
  else if (externalUrl(value)) urls.add(value);
  return urls;
}

function replaceValues(value, replacements, seedMode = false) {
  if (Array.isArray(value)) return value.map((item) => replaceValues(item, replacements, seedMode));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, replaceValues(item, replacements, seedMode)]));
  }
  if (externalUrl(value)) return seedMode ? '' : (replacements.get(value) || '');
  if (value === '+86 400 888 8888') return '';
  if (value === '#') return '';
  if (typeof value === 'string') {
    return value
      .replaceAll('后台可继续补充该节点的技术说明。', '具体规格与安装条件请联系星渡技术团队确认。')
      .replaceAll('演示视频：', '安装与承载能力视频：');
  }
  return value;
}

function fillProductGalleries(content) {
  for (const product of content.products || []) {
    if ((!Array.isArray(product.galleryImages) || product.galleryImages.length === 0) && product.coverImage) {
      product.galleryImages = [product.coverImage];
    }
    if (Array.isArray(product.downloads)) product.downloads = product.downloads.filter((item) => item?.url && item.url !== '#');
  }
  return content;
}

function removeDemoTestimonials(content) {
  if (Array.isArray(content.home?.testimonials)) {
    content.home.testimonials = content.home.testimonials.filter((item) => !DEMO_TESTIMONIAL_TEXTS.has(String(item?.text || '')));
  }
  return content;
}

function normalizeMigratedContent(content) {
  return removeDemoTestimonials(fillProductGalleries(content));
}

function extensionFor(contentType, url) {
  const pathnameExtension = path.extname(new URL(url).pathname).toLowerCase();
  if (/^\.[a-z0-9]{2,5}$/.test(pathnameExtension)) return pathnameExtension;
  const mime = String(contentType || '').split(';')[0].trim();
  return ({ 'image/jpeg': '.jpg', 'image/png': '.png', 'image/webp': '.webp', 'image/gif': '.gif', 'video/mp4': '.mp4', 'video/webm': '.webm' })[mime] || '';
}

loadDotEnv();
const seed = JSON.parse(await fs.readFile(SEED_FILE, 'utf8'));
const platformStore = createPlatformStore({
  driver: process.env.CMS_STORE_DRIVER || 'postgres',
  databaseUrl: process.env.DATABASE_URL || '',
  encryptionKey: process.env.CMS_DATA_ENCRYPTION_KEY || process.env.DATA_ENCRYPTION_KEY || '',
  files: {
    content: path.join(ROOT, 'data', 'site-content.json'), users: path.join(ROOT, 'data', 'admin-users.json'),
    leads: path.join(ROOT, 'data', 'leads.json'), audit: path.join(ROOT, 'data', 'site-audit-events.json'),
    publishJobs: path.join(ROOT, 'data', 'site-publish-jobs.json'), fallbackVersions: path.join(ROOT, 'data', 'fallback-versions.json'),
    uploadDir: path.join(ROOT, 'uploads', 'site-admin')
  }
});
await platformStore.init();
const mediaStore = createMediaStore({
  driver: process.env.ADMIN_MEDIA_DRIVER || 's3',
  uploadDir: path.join(ROOT, 'uploads', 'site-admin'),
  publicOrigin: process.env.PUBLIC_ADMIN_ORIGIN || process.env.PUBLIC_ADMIN_URL || 'http://127.0.0.1:1337',
  platformStore,
  s3: {
    endpoint: process.env.S3_ENDPOINT, region: process.env.S3_REGION, bucket: process.env.S3_BUCKET,
    accessKeyId: process.env.S3_ACCESS_KEY_ID, secretAccessKey: process.env.S3_SECRET_ACCESS_KEY,
    sessionToken: process.env.S3_SESSION_TOKEN, forcePathStyle: process.env.S3_FORCE_PATH_STYLE,
    prefix: process.env.ADMIN_S3_KEY_PREFIX || 'site-admin', publicBaseUrl: process.env.S3_PUBLIC_BASE_URL,
    serverSideEncryption: process.env.ADMIN_S3_SERVER_SIDE_ENCRYPTION
  }
});

const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-external-media-'));
const replacements = new Map();
const failures = [];
try {
  const current = await platformStore.readContent(seed);
  const fallbackStore = await platformStore.readFallbackStore({ activeId: '', versions: [] });
  const urlsToMigrate = collectExternalUrls(current);
  for (const version of fallbackStore.versions || []) collectExternalUrls(version.content, urlsToMigrate);
  const urls = [...urlsToMigrate];
  const existingMedia = await mediaStore.list();
  for (let index = 0; index < urls.length; index += 1) {
    const url = urls[index];
    const digest = crypto.createHash('sha256').update(url).digest('hex').slice(0, 16);
    const existing = existingMedia.find((item) => item.name.startsWith(`external-${digest}.`));
    if (existing) {
      replacements.set(url, existing.url);
      console.log(`[media ${index + 1}/${urls.length}] 复用已迁移素材：${existing.name}`);
      continue;
    }
    try {
      const response = await fetch(url, { redirect: 'follow', headers: { 'User-Agent': 'ASTREVA-Media-Migrator/1.0', Accept: 'image/*,video/*;q=0.9,*/*;q=0.1' }, signal: AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS) });
      if (!response.ok || !response.body) throw new Error(`HTTP ${response.status}`);
      const declaredSize = Number(response.headers.get('content-length') || 0);
      if (declaredSize > MAX_DOWNLOAD_BYTES) throw new Error(`文件超过 ${MAX_DOWNLOAD_BYTES} 字节`);
      const extension = extensionFor(response.headers.get('content-type'), response.url || url);
      if (!extension) throw new Error(`不支持的媒体类型 ${response.headers.get('content-type') || ''}`);
      const filename = `external-${digest}${extension}`;
      const temporaryPath = path.join(tempDir, filename);
      let received = 0;
      const input = Readable.fromWeb(response.body);
      input.on('data', (chunk) => {
        received += chunk.length;
        if (received > MAX_DOWNLOAD_BYTES) input.destroy(new Error(`文件超过 ${MAX_DOWNLOAD_BYTES} 字节`));
      });
      await pipeline(input, createWriteStream(temporaryPath));
      const media = await mediaStore.upload({
        temporaryPath, filename, originalName: filename,
        mime: response.headers.get('content-type') || '', size: received
      });
      replacements.set(url, media.url);
      console.log(`[media ${index + 1}/${urls.length}] 已迁移 ${filename} (${received} bytes)`);
    } catch (error) {
      replacements.set(url, '');
      failures.push({ url, reason: error.message });
      console.warn(`[media ${index + 1}/${urls.length}] 迁移失败，将清空该引用：${error.message}`);
    }
  }

  const migratedCurrent = normalizeMigratedContent(replaceValues(current, replacements));
  await platformStore.saveContent(migratedCurrent);
  if (fallbackStore.versions?.length) {
    fallbackStore.versions = fallbackStore.versions.map((version) => ({ ...version, content: normalizeMigratedContent(replaceValues(version.content, replacements)), updatedAt: new Date().toISOString() }));
    await platformStore.saveFallbackStore(fallbackStore);
  }
  // 种子文件不写入某个环境的 S3 地址；新环境由后台上传素材后配置。
  const portableSeed = normalizeMigratedContent(replaceValues(seed, replacements, true));
  await fs.writeFile(SEED_FILE, `${JSON.stringify(portableSeed, null, 2)}\n`);
  console.log(JSON.stringify({ migrated: [...replacements.values()].filter(Boolean).length, cleared: failures.length, mediaRecords: (await mediaStore.list()).length, failures }, null, 2));
} finally {
  await platformStore.close();
  await fs.rm(tempDir, { recursive: true, force: true });
}
