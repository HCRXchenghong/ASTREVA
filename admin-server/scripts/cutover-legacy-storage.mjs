import { existsSync, readFileSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createPlatformStore } from '../modules/platform-store.mjs';
import { createMediaStore } from '../modules/media-store.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const LEGACY_DATA_DIR = path.resolve(process.env.LEGACY_CMS_DATA_DIR || path.join(ROOT, 'data'));
const LEGACY_UPLOAD_DIR = path.resolve(process.env.LEGACY_CMS_UPLOAD_DIR || path.join(ROOT, 'uploads', 'site-admin'));
const LEGACY_FILES = [
  'site-content.json',
  'admin-users.json',
  'leads.json',
  'fallback-versions.json',
  'site-audit-events.json',
  'site-publish-jobs.json',
  // 构建期导出的旧降级快照是派生数据，已被 PostgreSQL 版本库取代。
  'active-fallback-content.json'
];

function loadDotEnv() {
  const envFile = path.join(ROOT, '.env');
  if (!existsSync(envFile)) return;
  for (const line of readFileSync(envFile, 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    const key = trimmed.slice(0, index).trim();
    const value = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
    if (!process.env[key]) process.env[key] = value;
  }
}

function inDirectory(file, directory) {
  return path.dirname(file) === directory;
}

async function mediaFiles() {
  const entries = await fs.readdir(LEGACY_UPLOAD_DIR, { withFileTypes: true }).catch(() => []);
  return entries.filter((entry) => entry.isFile()).map((entry) => path.join(LEGACY_UPLOAD_DIR, entry.name));
}

loadDotEnv();
if (process.env.CMS_STORE_DRIVER !== 'postgres') throw new Error('切换必须使用 CMS_STORE_DRIVER=postgres。');
if (process.env.ADMIN_MEDIA_DRIVER !== 's3') throw new Error('切换必须使用 ADMIN_MEDIA_DRIVER=s3。');
if (process.env.CMS_CUTOVER_PURGE !== 'confirmed') throw new Error('这是删除旧数据的操作；请显式设置 CMS_CUTOVER_PURGE=confirmed。');

const files = {
  content: path.join(LEGACY_DATA_DIR, 'site-content.json'),
  users: path.join(LEGACY_DATA_DIR, 'admin-users.json'),
  leads: path.join(LEGACY_DATA_DIR, 'leads.json'),
  fallbackVersions: path.join(LEGACY_DATA_DIR, 'fallback-versions.json'),
  audit: path.join(LEGACY_DATA_DIR, 'site-audit-events.json'),
  publishJobs: path.join(LEGACY_DATA_DIR, 'site-publish-jobs.json'),
  uploadDir: LEGACY_UPLOAD_DIR
};
const platformStore = createPlatformStore({
  driver: 'postgres',
  databaseUrl: process.env.DATABASE_URL || '',
  encryptionKey: process.env.CMS_DATA_ENCRYPTION_KEY || process.env.DATA_ENCRYPTION_KEY || '',
  files
});
const mediaStore = createMediaStore({
  driver: 's3',
  uploadDir: LEGACY_UPLOAD_DIR,
  publicOrigin: process.env.PUBLIC_ADMIN_ORIGIN || 'http://127.0.0.1:1337',
  platformStore,
  s3: {
    endpoint: process.env.S3_ENDPOINT,
    region: process.env.S3_REGION,
    bucket: process.env.S3_BUCKET,
    accessKeyId: process.env.S3_ACCESS_KEY_ID,
    secretAccessKey: process.env.S3_SECRET_ACCESS_KEY,
    sessionToken: process.env.S3_SESSION_TOKEN,
    forcePathStyle: process.env.S3_FORCE_PATH_STYLE,
    prefix: process.env.ADMIN_S3_KEY_PREFIX || 'site-admin',
    publicBaseUrl: process.env.S3_PUBLIC_BASE_URL,
    serverSideEncryption: process.env.ADMIN_S3_SERVER_SIDE_ENCRYPTION
  }
});

try {
  await platformStore.init();
  const report = await platformStore.migrateLegacyJson();
  if (!report.verified) throw new Error(`数据库核验失败：${JSON.stringify({ source: report.source, target: report.target })}`);

  const oldMedia = await mediaFiles();
  const migratedMedia = await mediaStore.migrateLocalMedia();
  if (migratedMedia.length !== oldMedia.length) {
    throw new Error(`媒体核验失败：源文件 ${oldMedia.length} 个，已写入对象存储 ${migratedMedia.length} 个。`);
  }

  const oldDataFiles = LEGACY_FILES.map((name) => path.join(LEGACY_DATA_DIR, name));
  if (!oldDataFiles.every((file) => inDirectory(file, LEGACY_DATA_DIR)) || !oldMedia.every((file) => inDirectory(file, LEGACY_UPLOAD_DIR))) {
    throw new Error('检测到不安全的删除目标，已中止。');
  }
  await Promise.all(oldDataFiles.map((file) => fs.rm(file, { force: true })));
  await Promise.all(oldMedia.map((file) => fs.rm(file, { force: true })));
  console.log(JSON.stringify({
    ok: true,
    postgres: report.target,
    discardedUnmigratable: report.discarded,
    deletedLegacyJson: oldDataFiles.filter((file) => !existsSync(file)).map((file) => path.relative(ROOT, file)),
    deletedLegacyMedia: oldMedia.map((file) => path.relative(ROOT, file))
  }, null, 2));
} finally {
  await platformStore.close();
}
