import http from 'node:http';
import { createReadStream, existsSync, readFileSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import crypto from 'node:crypto';
import os from 'node:os';
import formidable from 'formidable';
import { createPlatformStore } from './modules/platform-store.mjs';
import { createMediaStore } from './modules/media-store.mjs';
import { createMediaAiService } from './modules/media-ai-service.mjs';
import {
  hydrateMediaRuntimeConfig,
  mediaRuntimeConfigFromEnv,
  normalizeAiImageUpdate,
  normalizeImageUpdate,
  normalizeStorageUpdate,
  persistedMediaRuntimeConfig,
  publicMediaRuntimeConfig,
  storageIdentity
} from './modules/media-runtime-config.mjs';
import { createOpenApiDocument } from './modules/openapi.mjs';
import { createAuthRouter } from './modules/auth-router.mjs';
import { createContentRouter } from './modules/content-router.mjs';
import { createLeadsModule } from './modules/leads-router.mjs';
import { createMediaRouter } from './modules/media-router.mjs';
import { createPublishService } from './modules/publish-service.mjs';
import { createPublishRouter } from './modules/publish-router.mjs';
import { createServiceBridgeProxy } from './modules/servicebridge-proxy.mjs';
import { createWebsiteTranslationService } from './modules/website-translation-service.mjs';
import { createWebsiteTranslationRouter } from './modules/website-translation-router.mjs';
import { createWebsiteSeoService } from './modules/website-seo-service.mjs';
import { createAnalyticsRouter, detectDanger } from './modules/analytics-router.mjs';
import { createSystemRouter } from './modules/system-router.mjs';
import { sanitizeContentRichText, sanitizeHtml } from './modules/html-sanitize.mjs';
import { createWebsiteSeoRouter } from './modules/website-seo-router.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '..');

function loadDotEnv() {
  const file = path.join(ROOT, '.env');
  if (!existsSync(file)) return;
  const text = readFileSync(file, 'utf8');
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    const key = trimmed.slice(0, index).trim();
    const value = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
    if (!process.env[key]) process.env[key] = value;
  }
}

loadDotEnv();

const DATA_DIR = process.env.ADMIN_DATA_DIR || path.join(ROOT, 'data');
const CONTENT_FILE = path.join(DATA_DIR, 'site-content.json');
const CONTENT_SEED_FILE = process.env.SITE_CONTENT_SEED_FILE || path.join(__dirname, 'seed', 'site-content.json');
const FALLBACK_VERSIONS_FILE = path.join(DATA_DIR, 'fallback-versions.json');
const USERS_FILE = path.join(DATA_DIR, 'admin-users.json');
const LEADS_FILE = path.join(DATA_DIR, 'leads.json');
const AUDIT_FILE = path.join(DATA_DIR, 'site-audit-events.json');
const PUBLISH_JOBS_FILE = path.join(DATA_DIR, 'site-publish-jobs.json');
const UPLOAD_DIR = process.env.ADMIN_UPLOAD_DIR || path.join(ROOT, 'uploads', 'site-admin');
// Arco Design Pro Vue 控制台构建产物；缺失时回退到旧的单页后台，避免整站不可用。
const ADMIN_WEB_DIST = path.join(ROOT, 'admin-web', 'dist');
const PORT = Number(process.env.PORT || 1337);
const HOST = process.env.HOST || '127.0.0.1';
const PUBLIC_ORIGIN = process.env.PUBLIC_ADMIN_ORIGIN || `http://${HOST}:${PORT}`;
const SITE_ORIGIN = process.env.PUBLIC_SITE_URL || process.env.FRONTEND_SITE_ORIGIN || 'http://127.0.0.1:4321';
const REBUILD_URL = process.env.FRONTEND_REBUILD_WEBHOOK_URL || 'http://127.0.0.1:8787/rebuild';
const SERVICEBRIDGE_API_URL = normalizeServiceBridgeUrl(process.env.SERVICEBRIDGE_API_URL || 'http://127.0.0.1:8080');
const SERVICEBRIDGE_ADMIN_ACCOUNT = process.env.SERVICEBRIDGE_ADMIN_ACCOUNT || 'superadmin';
const SERVICEBRIDGE_ADMIN_PASSWORD = process.env.SERVICEBRIDGE_ADMIN_PASSWORD || '123456';
const SESSION_COOKIE = 'astreva_admin_session';
const ISSUER = process.env.ADMIN_2FA_ISSUER || '星渡官网账号';
const SESSION_TTL_MS = Number(process.env.ADMIN_2FA_SESSION_TTL_MS || 12 * 60 * 60 * 1000);
const SESSION_SECRET =
  process.env.ADMIN_2FA_SESSION_SECRET ||
  process.env.ADMIN_SESSION_SECRET ||
  'change-me-astreva-admin-session-secret';
const SESSION_COOKIE_SECURE = process.env.ADMIN_SESSION_COOKIE_SECURE === 'true' || PUBLIC_ORIGIN.startsWith('https://');
// 生产环境必须提供强随机会话密钥，避免使用仓库里的示例值被伪造管理员会话。
const IS_PRODUCTION = (process.env.APP_ENV || process.env.NODE_ENV || '').toLowerCase() === 'production';
if (IS_PRODUCTION) {
  const weakSession = SESSION_SECRET.length < 24 || /change-me|changeme|placeholder|secret/i.test(SESSION_SECRET);
  if (weakSession) {
    throw new Error('生产环境必须配置随机的 ADMIN_2FA_SESSION_SECRET（至少 24 位，且不能是示例值）。');
  }
  const encryptionKey = process.env.CMS_DATA_ENCRYPTION_KEY || process.env.DATA_ENCRYPTION_KEY || '';
  if (encryptionKey.length < 32 || /change-me|changeme|placeholder/i.test(encryptionKey)) {
    throw new Error('生产环境必须配置 CMS_DATA_ENCRYPTION_KEY / DATA_ENCRYPTION_KEY（至少 32 位随机值）。');
  }
}

const CMS_STORE_DRIVER = process.env.CMS_STORE_DRIVER || 'postgres';
const CMS_DATA_ENCRYPTION_KEY = process.env.CMS_DATA_ENCRYPTION_KEY || process.env.DATA_ENCRYPTION_KEY || '';
const ADMIN_MEDIA_DRIVER = process.env.ADMIN_MEDIA_DRIVER || 's3';
const ADMIN_METRICS_BEARER_TOKEN = process.env.ADMIN_METRICS_BEARER_TOKEN || '';
const ADMIN_TRUST_PROXY = process.env.ADMIN_TRUST_PROXY === 'true';
const ADMIN_CORS_ALLOWED_ORIGINS = new Set(
  String(process.env.ADMIN_CORS_ALLOWED_ORIGINS || SITE_ORIGIN)
    .split(',')
    .map((value) => value.trim().replace(/\/$/, ''))
    .filter(Boolean)
);

if (process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS !== 'true' && CMS_STORE_DRIVER !== 'postgres') {
  throw new Error('后台运行必须使用 CMS_STORE_DRIVER=postgres；旧 JSON 仅可通过 npm run cms:cutover 导入。');
}
if (process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS !== 'true' && ADMIN_MEDIA_DRIVER !== 's3') {
  throw new Error('后台运行必须使用 ADMIN_MEDIA_DRIVER=s3；旧本地媒体仅可通过 npm run cms:cutover 导入。');
}

const platformStore = createPlatformStore({
  driver: CMS_STORE_DRIVER,
  databaseUrl: process.env.DATABASE_URL || '',
  encryptionKey: CMS_DATA_ENCRYPTION_KEY,
  files: {
    content: CONTENT_FILE,
    users: USERS_FILE,
    leads: LEADS_FILE,
    audit: AUDIT_FILE,
    publishJobs: PUBLISH_JOBS_FILE,
    fallbackVersions: FALLBACK_VERSIONS_FILE,
    uploadDir: UPLOAD_DIR
  }
});
await platformStore.init();
const translationService = createWebsiteTranslationService({ platformStore });
const seoService = createWebsiteSeoService({ aiService: translationService });
const publishService = createPublishService({
  rebuildUrl: REBUILD_URL,
  rebuildSecret: process.env.REBUILD_WEBHOOK_SECRET || '',
  siteOrigin: SITE_ORIGIN,
  platformStore
});
const MEDIA_RUNTIME_SETTING_KEY = 'media-runtime';
const mediaEnvironmentConfig = mediaRuntimeConfigFromEnv(process.env);
const storedMediaRuntimeConfig = await platformStore.readRuntimeSetting(MEDIA_RUNTIME_SETTING_KEY);
let mediaRuntimeConfig = hydrateMediaRuntimeConfig(storedMediaRuntimeConfig, mediaEnvironmentConfig);
if (!storedMediaRuntimeConfig) {
  await platformStore.saveRuntimeSetting(MEDIA_RUNTIME_SETTING_KEY, persistedMediaRuntimeConfig(mediaRuntimeConfig));
}

function configuredMediaStore(config) {
  return createMediaStore({
    driver: config.storage.driver,
    uploadDir: UPLOAD_DIR,
    publicOrigin: PUBLIC_ORIGIN,
    platformStore,
    directUploadSecret: SESSION_SECRET,
    imageWidths: config.image.variantWidths,
    imageQuality: config.image.webpQuality,
    maxUploadBytes: config.image.maxUploadBytes,
    allowedMimeTypes: config.image.allowedMimeTypes,
    s3: {
      endpoint: config.storage.endpoint,
      region: config.storage.region,
      bucket: config.storage.bucket,
      accessKeyId: config.storage.accessKeyId,
      secretAccessKey: config.storage.secretAccessKey,
      sessionToken: config.storage.sessionToken,
      forcePathStyle: config.storage.forcePathStyle,
      prefix: config.storage.prefix,
      publicBaseUrl: config.storage.publicBaseUrl,
      serverSideEncryption: config.storage.serverSideEncryption
    }
  });
}

function configuredMediaAiService(config, store = mediaStore) {
  return createMediaAiService({
    mediaStore: store,
    apiKey: config.aiImage.apiKey,
    baseUrl: config.aiImage.baseUrl,
    defaultModel: config.aiImage.defaultModel,
    allowedHosts: config.aiImage.allowedHosts,
    downloadHosts: config.aiImage.downloadHosts,
    configuredModels: config.aiImage.configuredModels,
    requestTimeoutMs: config.aiImage.requestTimeoutMs
  });
}

let mediaStore = configuredMediaStore(mediaRuntimeConfig);
let mediaAiService = configuredMediaAiService(mediaRuntimeConfig, mediaStore);
const serviceBridgeProxy = createServiceBridgeProxy({
  apiUrl: SERVICEBRIDGE_API_URL,
  adminAccount: SERVICEBRIDGE_ADMIN_ACCOUNT,
  adminPassword: SERVICEBRIDGE_ADMIN_PASSWORD,
  publicOrigin: PUBLIC_ORIGIN,
  bodyRaw,
  bodyJson,
  json,
  send,
  verifySession
});

const SYSTEM_SETTINGS_DEFAULTS = {
  business: { timezone: 'Asia/Shanghai', start: '09:00', end: '18:00', enabled: true },
  contact: {
    telephone: '',
    whatsapp: '',
    telegram: '',
    entry_reply: '您好，欢迎咨询在线客服，请问有什么可以帮您？'
  },
  status: { database: { ok: false }, ai: { enabled: false, mode: 'human_first', model: 'gpt-4o-mini' } },
  ai: {
    enabled: false,
    mode: 'human_first',
    base_url: 'https://api.openai.com/v1',
    model: 'gpt-4o-mini',
    api_type: 'chat_completions',
    temperature: 0.3,
    max_output_tokens: 512,
    timeout_seconds: 20,
    system_prompt: '',
    api_key_masked: ''
  }
};

async function readSystemSettingsBootstrap() {
  const calls = [
    ['营业时间', serviceBridgeProxy.adminJson('/api/admin/business-hours', { signal: AbortSignal.timeout(6_000) })],
    ['访客端配置', serviceBridgeProxy.adminJson('/api/contact-settings', { signal: AbortSignal.timeout(6_000) })],
    ['ServiceBridge 状态', serviceBridgeProxy.adminJson('/api/admin/system-status', { signal: AbortSignal.timeout(6_000) })],
    ['AI 设置', serviceBridgeProxy.adminJson('/api/admin/ai-settings', { signal: AbortSignal.timeout(6_000) })],
    ['R2/S3 存储', mediaStore.health()],
    ['媒体统计', platformStore.mediaStats()]
  ];
  const results = await Promise.allSettled(calls.map(([, promise]) => promise));
  const errors = [];
  const value = (index, fallback) => {
    const result = results[index];
    if (result.status === 'fulfilled') return result.value;
    errors.push(`${calls[index][0]}：${result.reason?.message || '读取失败'}`);
    return structuredClone(fallback);
  };
  const business = mergeDefaults(value(0, SYSTEM_SETTINGS_DEFAULTS.business), SYSTEM_SETTINGS_DEFAULTS.business);
  const contact = mergeDefaults(value(1, SYSTEM_SETTINGS_DEFAULTS.contact), SYSTEM_SETTINGS_DEFAULTS.contact);
  const status = mergeDefaults(value(2, SYSTEM_SETTINGS_DEFAULTS.status), SYSTEM_SETTINGS_DEFAULTS.status);
  const ai = mergeDefaults(value(3, SYSTEM_SETTINGS_DEFAULTS.ai), SYSTEM_SETTINGS_DEFAULTS.ai);
  const media = value(4, { driver: ADMIN_MEDIA_DRIVER, ok: false, bucket: '', endpoint: '', prefix: '', message: '存储状态读取失败' });
  const mediaStats = value(5, { total: 0 });
  if (media.ok === false && media.message && !errors.some((item) => item.startsWith('R2/S3 存储：'))) {
    errors.push(`R2/S3 存储：${media.message}`);
  }
  return {
    generatedAt: new Date().toISOString(),
    business,
    contact,
    status,
    ai,
    media,
    mediaStats,
    mediaRuntime: publicMediaRuntimeConfig(mediaRuntimeConfig),
    aiImage: mediaAiService.capability(),
    serviceBridgeOk: results[2].status === 'fulfilled',
    errors,
    upload: {
      maxBytes: mediaRuntimeConfig.image.maxUploadBytes,
      variantWidths: mediaRuntimeConfig.image.variantWidths,
      webpQuality: mediaRuntimeConfig.image.webpQuality
    }
  };
}

const mimeTypes = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.mp4': 'video/mp4',
  '.webm': 'video/webm',
  '.mov': 'video/quicktime',
  '.pdf': 'application/pdf',
  '.zip': 'application/zip'
};

const escapeHtml = (value = '') =>
  String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');

const safeJson = (value) =>
  JSON.stringify(value)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029');

async function ensureDirs() {
  await fs.mkdir(DATA_DIR, { recursive: true });
  await fs.mkdir(UPLOAD_DIR, { recursive: true });
}

async function readJson(file, fallback) {
  try {
    return JSON.parse(await fs.readFile(file, 'utf8'));
  } catch {
    return fallback;
  }
}

async function writeJson(file, value) {
  await ensureDirs();
  await fs.writeFile(file, `${JSON.stringify(value, null, 2)}\n`);
}

function normalizeServiceBridgeUrl(value) {
  const candidate = `${value || ''}`.trim().replace(/\/$/, '');
  if (!candidate) return '';
  try {
    const url = new URL(candidate);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString().replace(/\/$/, '') : '';
  } catch {
    return '';
  }
}

function resolveServiceBridgeUrl(rawValue) {
  return (
    normalizeServiceBridgeUrl(rawValue) ||
    normalizeServiceBridgeUrl(process.env.PUBLIC_SERVICEBRIDGE_URL) ||
    ''
  );
}

function normalizeContent(content) {
  content.site ||= {};
  content.home ||= {};
  content.productCenter ||= {};
  content.support ||= {};
  content.categories ||= [];
  content.products ||= [];
  content.faqs ||= [];
  content.staticPages ||= [];
  content.staticPages = content.staticPages.filter((page) => page.slug !== 'projects');
  if (content.site.navigation) {
    delete content.site.navigation.categoryPrompt;
    delete content.site.navigation.productDetailLabel;
  }
  if (content.support.contact?.form) {
    delete content.support.contact.form.nameCompanyLabel;
    delete content.support.contact.form.nameCompanyPlaceholder;
    delete content.support.contact.form.contactLabel;
    delete content.support.contact.form.contactPlaceholder;
  }
  content.site.serviceBridgeUrl = resolveServiceBridgeUrl(content.site.serviceBridgeUrl);
  if (!content.site.serviceBridgeUrl) {
    content.site.serviceBridgeUrl = resolveServiceBridgeUrl(process.env.PUBLIC_SERVICEBRIDGE_URL);
  }
  return content;
}

function mergeDefaults(value, defaults) {
  if (value == null) return structuredClone(defaults);
  if (Array.isArray(defaults)) return Array.isArray(value) ? value : structuredClone(defaults);
  if (typeof defaults !== 'object') return value;
  if (typeof value !== 'object' || Array.isArray(value)) return structuredClone(defaults);
  const output = { ...value };
  for (const [key, defaultValue] of Object.entries(defaults)) {
    output[key] = mergeDefaults(output[key], defaultValue);
  }
  return output;
}

function mergeContentDefaults(value, defaults) {
  const content = normalizeContent(mergeDefaults(value, defaults));
  for (const defaultPage of defaults.staticPages || []) {
    const index = content.staticPages.findIndex((item) => item.slug === defaultPage.slug);
    if (index >= 0) content.staticPages[index] = mergeDefaults(content.staticPages[index], defaultPage);
    else content.staticPages.push(structuredClone(defaultPage));
  }
  return content;
}

async function readContentRecord() {
  await ensureDirs();
  const fallback = await readJson(CONTENT_SEED_FILE, null);
  if (!isSiteContent(fallback)) {
    throw new Error(`站点内容初始化文件无效：${CONTENT_SEED_FILE}`);
  }
  const record = await platformStore.readContentRecord(fallback);
  return { ...record, content: mergeContentDefaults(record.content, fallback) };
}

async function readContent() {
  return (await readContentRecord()).content;
}

async function saveContent(content, options = {}) {
  const normalized = normalizeContent(content);
  const saved = await platformStore.saveContent(normalized, options);
  await translationService.schedule(normalized, saved.revision);
  return saved;
}

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

function fallbackVersionSummary(version, activeId) {
  return {
    id: version.id,
    name: version.name,
    createdAt: version.createdAt,
    updatedAt: version.updatedAt,
    active: version.id === activeId
  };
}

function normalizeFallbackStore(raw, initialContent) {
  const versions = Array.isArray(raw?.versions)
    ? raw.versions
        .filter((version) => version && typeof version === 'object' && isSiteContent(version.content))
        .map((version) => ({
          id: String(version.id || `fallback_${crypto.randomUUID()}`),
          name: String(version.name || '未命名降级版本').trim().slice(0, 80) || '未命名降级版本',
          createdAt: version.createdAt || new Date().toISOString(),
          updatedAt: version.updatedAt || version.createdAt || new Date().toISOString(),
          content: mergeContentDefaults(structuredClone(version.content), initialContent)
        }))
    : [];

  if (!versions.length) {
    const now = new Date().toISOString();
    versions.push({
      id: `fallback_${crypto.randomUUID()}`,
      name: '初始降级版本',
      createdAt: now,
      updatedAt: now,
      content: normalizeContent(structuredClone(initialContent))
    });
  }

  const activeId = versions.some((version) => version.id === raw?.activeId)
    ? raw.activeId
    : versions[0].id;
  return { activeId, versions };
}

async function readFallbackStore(content = null) {
  const currentContent = content || await readContent();
  const raw = await platformStore.readFallbackStore({ activeId: '', versions: [] });
  const store = normalizeFallbackStore(raw, currentContent);
  if (!raw || JSON.stringify(raw) !== JSON.stringify(store)) {
    await platformStore.saveFallbackStore(store);
  }
  return store;
}

async function activeFallbackVersion() {
  const store = await readFallbackStore();
  return store.versions.find((version) => version.id === store.activeId) || store.versions[0];
}

async function createFallbackVersion(name) {
  const content = await readContent();
  const store = await readFallbackStore(content);
  const now = new Date().toISOString();
  const version = {
    id: `fallback_${crypto.randomUUID()}`,
    name: String(name || '').trim().slice(0, 80) || `降级版本 ${now.slice(0, 16).replace('T', ' ')}`,
    createdAt: now,
    updatedAt: now,
    content: normalizeContent(structuredClone(content))
  };
  store.versions.unshift(version);
  await platformStore.saveFallbackStore(store);
  return { version, store };
}

async function activateFallbackVersion(id) {
  const store = await readFallbackStore();
  const version = store.versions.find((item) => item.id === id);
  if (!version) throw new Error('降级版本不存在或已被删除。');
  store.activeId = version.id;
  await platformStore.saveFallbackStore(store);
  return { version, store };
}

async function deleteFallbackVersion(id) {
  const store = await readFallbackStore();
  const index = store.versions.findIndex((item) => item.id === id);
  if (index < 0) throw new Error('降级版本不存在或已被删除。');
  if (store.versions.length === 1) {
    throw new Error('至少需要保留一个降级版本，以保证后台不可用时官网仍可构建。');
  }
  const [deleted] = store.versions.splice(index, 1);
  const activeChanged = deleted.id === store.activeId;
  if (activeChanged) store.activeId = store.versions[0].id;
  await platformStore.saveFallbackStore(store);
  return { deleted, activeChanged, store };
}

async function readUsers() {
  return platformStore.readUsers();
}

async function saveUsers(users) {
  await platformStore.saveUsers(users);
}

async function readLeads() {
  return platformStore.readLeads();
}

async function saveLeads(leads) {
  await platformStore.saveLeads(leads);
}

function bodyRaw(req, maxBytes = 250 * 1024 * 1024) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > maxBytes) {
        reject(new Error('请求体太大'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => resolve(Buffer.concat(chunks)));
    req.on('error', reject);
  });
}

function bodyJson(req, maxBytes = 20 * 1024 * 1024) {
  return new Promise((resolve, reject) => {
    let raw = '';
    req.on('data', (chunk) => {
      raw += chunk;
      if (Buffer.byteLength(raw) > maxBytes) {
        reject(new Error('请求体太大'));
        req.destroy();
      }
    });
    req.on('end', () => {
      if (!raw) return resolve({});
      try {
        resolve(JSON.parse(raw));
      } catch {
        reject(new Error('JSON 格式错误'));
      }
    });
    req.on('error', reject);
  });
}

function send(res, status, body, headers = {}) {
  res.writeHead(status, headers);
  res.end(body);
}

function json(res, status, payload) {
  send(res, status, JSON.stringify(payload), {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store'
  });
}

function redirect(res, location) {
  send(res, 302, '', { Location: location });
}

function parseCookies(req) {
  const header = req.headers.cookie || '';
  return Object.fromEntries(
    header
      .split(';')
      .map((part) => part.trim())
      .filter(Boolean)
      .map((part) => {
        const index = part.indexOf('=');
        return index === -1
          ? [decodeURIComponent(part), '']
          : [decodeURIComponent(part.slice(0, index)), decodeURIComponent(part.slice(index + 1))];
      })
  );
}

function sign(value) {
  return crypto.createHmac('sha256', SESSION_SECRET).update(value).digest('base64url');
}

async function createSession(user, req = null) {
  const client = req ? resolveAdminClient(req) : { ip: '', source: '' };
  const record = await platformStore.createAdminSession({
    userId: user.id,
    email: user.email,
    ip: client.ip,
    userAgent: req ? String(req.headers['user-agent'] || '') : '',
    expiresAt: Date.now() + SESSION_TTL_MS
  }).catch(() => null);
  const payload = Buffer.from(JSON.stringify({
    userId: user.id,
    email: user.email,
    sid: record?.id || '',
    exp: Date.now() + SESSION_TTL_MS
  })).toString('base64url');
  return `${payload}.${sign(payload)}`;
}

async function verifySessionAsync(req) {
  const token = parseCookies(req)[SESSION_COOKIE];
  if (!token || !token.includes('.')) return null;
  const [payload, signature] = token.split('.');
  const expected = sign(payload);
  // 常量时间比较，避免签名被逐字节猜测。
  const a = Buffer.from(signature);
  const b = Buffer.from(expected);
  if (a.length !== b.length || !crypto.timingSafeEqual(a, b)) return null;
  try {
    const session = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'));
    if (!session.exp || session.exp < Date.now()) return null;
    if (!session.sid) return null;
    const active = await platformStore.isAdminSessionActive(session.sid).catch(() => false);
    return active ? session : null;
  } catch {
    return null;
  }
}

/**
 * 兼容既有同步调用点：这里做的是"签名 + 过期 + 会话存在性"的轻量校验，
 * 真正的异步撤销校验用 verifySessionAsync（用于关键接口）。
 */
function verifySession(req) {
  const token = parseCookies(req)[SESSION_COOKIE];
  if (!token || !token.includes('.')) return null;
  const [payload, signature] = token.split('.');
  const expected = sign(payload);
  const a = Buffer.from(signature);
  const b = Buffer.from(expected);
  if (a.length !== b.length || !crypto.timingSafeEqual(a, b)) return null;
  try {
    const session = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'));
    if (!session.exp || session.exp < Date.now()) return null;
    return session;
  } catch {
    return null;
  }
}

function requestIp(req) {
  if (ADMIN_TRUST_PROXY) {
    const forwarded = String(req.headers['x-forwarded-for'] || '').split(',')[0].trim();
    if (forwarded) return forwarded;
  }
  return req.socket?.remoteAddress || '';
}

function applyLeadCors(req, res) {
  const origin = String(req.headers.origin || '').replace(/\/$/, '');
  if (!origin) return true;
  if (!ADMIN_CORS_ALLOWED_ORIGINS.has(origin)) return false;
  res.setHeader('Access-Control-Allow-Origin', origin);
  res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  res.setHeader('Access-Control-Max-Age', '86400');
  res.setHeader('Vary', 'Origin');
  return true;
}

function applyPublicContentCors(req, res) {
  const origin = String(req.headers.origin || '').replace(/\/$/, '');
  if (!origin) return true;
  if (!ADMIN_CORS_ALLOWED_ORIGINS.has(origin)) return false;
  res.setHeader('Access-Control-Allow-Origin', origin);
  res.setHeader('Access-Control-Allow-Methods', 'GET, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  res.setHeader('Access-Control-Max-Age', '86400');
  res.setHeader('Vary', 'Origin');
  return true;
}

async function auditEvent(req, action, resource = '', resourceId = '', detail = {}, actor = null) {
  const session = actor || verifySession(req);
  try {
    await platformStore.appendAudit({
      actorId: session?.userId || '',
      actorEmail: session?.email || '',
      action,
      resource,
      resourceId,
      ip: requestIp(req),
      userAgent: String(req.headers['user-agent'] || '').slice(0, 512),
      detail
    });
  } catch (error) {
    console.error(`[audit] ${action} 写入失败：${error.message}`);
  }
}

async function adminMetrics() {
  const [content, fallbackStore, publishStatus] = await Promise.all([
    readContent(),
    readFallbackStore(),
    publishService.statusPayload()
  ]);
  const buildState = publishStatus.rebuild.lastBuild?.status || 'unknown';
  return [
    '# HELP astreva_admin_up Unified admin process health.',
    '# TYPE astreva_admin_up gauge',
    'astreva_admin_up 1',
    '# HELP astreva_cms_store_info Active CMS persistence backend.',
    '# TYPE astreva_cms_store_info gauge',
    `astreva_cms_store_info{driver="${CMS_STORE_DRIVER}"} 1`,
    '# HELP astreva_site_content_products Current product count.',
    '# TYPE astreva_site_content_products gauge',
    `astreva_site_content_products ${content.products.length}`,
    '# HELP astreva_fallback_versions_total Saved fallback content versions.',
    '# TYPE astreva_fallback_versions_total gauge',
    `astreva_fallback_versions_total ${fallbackStore.versions.length}`,
    '# HELP astreva_publish_build_state Last static build state.',
    '# TYPE astreva_publish_build_state gauge',
    `astreva_publish_build_state{state="${buildState}"} 1`
  ].join('\n') + '\n';
}

async function setSessionCookie(res, user, req = null) {
  const token = await createSession(user, req);
  const cookie = `${SESSION_COOKIE}=${encodeURIComponent(token)}; Path=/; HttpOnly; SameSite=Lax;${SESSION_COOKIE_SECURE ? ' Secure;' : ''} Max-Age=${Math.floor(SESSION_TTL_MS / 1000)}`;
  res.setHeader('Set-Cookie', cookie);
}

function clearSessionCookie(res) {
  res.setHeader('Set-Cookie', `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`);
}

const handleAuth = createAuthRouter({
  readUsers,
  saveUsers,
  bodyJson,
  json,
  generateTotpSecret,
  verifyTotp,
  otpAuthUrl,
  setSessionCookie,
  clearSessionCookie,
  verifySession,
  auditEvent,
  recordSystemEvent,
  resolveClient: resolveAdminClient,
  platformStore
});

const base32Alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function base32Encode(buffer) {
  let bits = 0;
  let value = 0;
  let output = '';
  for (const byte of buffer) {
    value = (value << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      output += base32Alphabet[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) output += base32Alphabet[(value << (5 - bits)) & 31];
  return output;
}

function base32Decode(secret) {
  const clean = String(secret).toUpperCase().replace(/=+$/g, '').replace(/\s+/g, '');
  let bits = 0;
  let value = 0;
  const bytes = [];
  for (const char of clean) {
    const index = base32Alphabet.indexOf(char);
    if (index === -1) throw new Error('Invalid TOTP secret');
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 255);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

function generateTotpSecret() {
  return base32Encode(crypto.randomBytes(20));
}

function generateTotp(secret, counter) {
  const key = base32Decode(secret);
  const buffer = Buffer.alloc(8);
  buffer.writeUInt32BE(Math.floor(counter / 0x100000000), 0);
  buffer.writeUInt32BE(counter & 0xffffffff, 4);
  const hmac = crypto.createHmac('sha1', key).update(buffer).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const binary =
    ((hmac[offset] & 0x7f) << 24) |
    ((hmac[offset + 1] & 0xff) << 16) |
    ((hmac[offset + 2] & 0xff) << 8) |
    (hmac[offset + 3] & 0xff);
  return String(binary % 1000000).padStart(6, '0');
}

function verifyTotp(secret, code) {
  const normalized = String(code || '').replace(/\D/g, '');
  if (!/^\d{6}$/.test(normalized)) return false;
  const currentCounter = Math.floor(Date.now() / 30000);
  return [-1, 0, 1].some((window) => generateTotp(secret, currentCounter + window) === normalized);
}

function otpAuthUrl(email, secret) {
  const label = `${ISSUER}:${email}`;
  const params = new URLSearchParams({
    secret,
    issuer: ISSUER,
    algorithm: 'SHA1',
    digits: '6',
    period: '30'
  });
  return `otpauth://totp/${encodeURIComponent(label)}?${params.toString()}`;
}

function slugify(value, fallback = 'item') {
  const slug = String(value || '')
    .trim()
    .toLowerCase()
    .replace(/[\u4e00-\u9fa5]+/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return slug || `${fallback}-${Date.now().toString(36)}`;
}

async function readMediaLibrary() {
  return mediaStore.list();
}

async function mediaReferences(url) {
  if (!url) return [];
  const content = await readContent();
  const matches = [];
  function visit(value, fieldPath = '') {
    if (Array.isArray(value)) {
      value.forEach((item, index) => visit(item, `${fieldPath}[${index}]`));
      return;
    }
    if (value && typeof value === 'object') {
      for (const [key, item] of Object.entries(value)) visit(item, fieldPath ? `${fieldPath}.${key}` : key);
      return;
    }
    if (typeof value === 'string' && value.includes(url)) matches.push(fieldPath);
  }
  visit(content);
  return matches;
}

async function getBootstrap() {
  const [contentRecord, leadsStore, media] = await Promise.all([readContentRecord(), readLeads(), readMediaLibrary()]);
  const content = contentRecord.content;
  const fallbackStore = await readFallbackStore(content);
  const leads = leadsStore.leads || [];
  return {
    content,
    contentRevision: contentRecord.revision,
    leads,
    media,
    fallbackVersions: fallbackStore.versions.map((version) => fallbackVersionSummary(version, fallbackStore.activeId)),
    fallbackActiveId: fallbackStore.activeId,
    counts: {
      categories: content.categories.length,
      products: content.products.length,
      faqs: content.faqs.length,
      leads: leads.length,
      newLeads: leads.filter((lead) => lead.status === '新咨询').length
    },
    generatedAt: new Date().toISOString()
  };
}

function getStaticPage(content, slug) {
  let page = content.staticPages.find((item) => item.slug === slug);
  if (!page) {
    page = {
      slug,
      title: slug,
      hero: { title: slug, description: '' },
      body: '',
      sections: {},
      seo: { title: `${slug} | 星渡ASTREVA`, description: slug }
    };
    content.staticPages.push(page);
  }
  return page;
}

const CONTENT_SLUG_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const STATIC_PAGE_SLUGS = new Set(['about', 'solutions', 'warranty', 'install', 'faq', 'privacy', 'agreement']);

function requireText(value, label, maxLength = 200) {
  const text = String(value || '').trim();
  if (!text) throw new Error(`${label}不能为空。`);
  if (text.length > maxLength) throw new Error(`${label}不能超过 ${maxLength} 个字符。`);
  return text;
}

function requireSlug(value, label) {
  const slug = requireText(value, label, 120).toLowerCase();
  if (!CONTENT_SLUG_PATTERN.test(slug)) throw new Error(`${label}只能使用小写字母、数字和单个连字符。`);
  return slug;
}

function duplicateValues(items, keyFor) {
  const seen = new Set();
  const duplicates = new Set();
  for (const item of items) {
    const key = keyFor(item);
    if (!key) continue;
    if (seen.has(key)) duplicates.add(key);
    seen.add(key);
  }
  return [...duplicates];
}

function assertContentIntegrity(content) {
  if (!isSiteContent(content)) throw new Error('完整内容配置缺少必填根节点。');
  for (const category of content.categories) {
    requireText(category.id, '品类 ID');
    requireText(category.name, '品类名称');
    requireSlug(category.slug, '品类 Slug');
  }
  for (const product of content.products) {
    requireText(product.id, '产品 ID');
    requireText(product.name, '产品名称');
    requireSlug(product.slug, '产品 Slug');
    if (!content.categories.some((category) => category.slug === product.categorySlug)) {
      throw new Error(`产品“${product.name}”引用了不存在的品类 ${product.categorySlug}。`);
    }
  }
  const checks = [
    ['品类 ID', duplicateValues(content.categories, (item) => item.id)],
    ['品类 Slug', duplicateValues(content.categories, (item) => item.slug)],
    ['产品 ID', duplicateValues(content.products, (item) => item.id)],
    ['同品类产品 Slug', duplicateValues(content.products, (item) => `${item.categorySlug}/${item.slug}`)],
    ['FAQ ID', duplicateValues(content.faqs, (item) => item.id)],
    ['静态页 Slug', duplicateValues(content.staticPages, (item) => item.slug)]
  ];
  for (const [label, duplicates] of checks) {
    if (duplicates.length) throw new Error(`${label}重复：${duplicates.join('、')}`);
  }
}

async function saveSection(payload) {
  const content = await readContent();
  const section = String(payload.section || '');
  const data = payload.data || {};
  let saved = null;

  if (section === 'content') {
    // 全量保存也要先做富文本消毒，避免绕过下面的白名单清洗。
    const sanitizedData = sanitizeContentRichText(data);
    assertContentIntegrity(sanitizedData);
    const result = await saveContent(sanitizedData, { expectedRevision: payload.revision });
    return { id: 'site-content', revision: result.revision, updatedAt: result.updatedAt };
  } else if (section === 'site') {
    content.site = { ...content.site, ...data };
    saved = content.site;
  } else if (section === 'home') {
    content.home = { ...content.home, ...data };
    saved = content.home;
  } else if (section === 'productCenter') {
    content.productCenter = { ...content.productCenter, ...data };
    saved = content.productCenter;
  } else if (section === 'support') {
    content.support = { ...content.support, ...data };
    saved = content.support;
  } else if (section === 'category') {
    if (data._delete && data.id) {
      const existing = content.categories.find((item) => item.id === data.id);
      if (!existing) throw new Error('品类不存在或已被删除。');
      const deletedProducts = content.products.filter((item) => item.categorySlug === existing.slug).length;
      content.categories = content.categories.filter((item) => item.id !== data.id);
      content.products = content.products.filter((item) => item.categorySlug !== existing.slug);
      saved = { deleted: true, id: data.id, deletedProducts };
    } else {
      const existing = data.id ? content.categories.find((item) => item.id === data.id) : null;
      const name = requireText(data.name, '品类名称');
      const slug = requireSlug(data.slug || slugify(name, 'category'), '品类 Slug');
      if (content.categories.some((item) => item.id !== data.id && item.slug === slug)) throw new Error(`品类 Slug“${slug}”已被使用。`);
      const category = {
        id: data.id || `cat_${crypto.randomUUID()}`,
        name,
        slug,
        description: data.description || '',
        coverImage: data.coverImage || '',
        heroVideo: data.heroVideo || '',
        sortOrder: Number(data.sortOrder || 0),
        seo: data.seo || { title: `${data.name || '品类'} | 星渡ASTREVA`, description: data.description || '' }
      };
      const index = content.categories.findIndex((item) => item.id === category.id);
      if (index >= 0) content.categories[index] = category;
      else content.categories.push(category);
      if (existing && existing.slug !== category.slug) {
        content.products = content.products.map((product) => product.categorySlug === existing.slug ? { ...product, categorySlug: category.slug } : product);
      }
      saved = category;
    }
  } else if (section === 'product') {
    if (data._delete && data.id) {
      content.products = content.products.filter((item) => item.id !== data.id);
      saved = { deleted: true, id: data.id };
    } else {
      const name = requireText(data.name, '产品名称');
      const slug = requireSlug(data.slug || slugify(name, 'product'), '产品 Slug');
      const categorySlug = requireSlug(data.categorySlug || content.categories[0]?.slug || '', '所属品类');
      if (!content.categories.some((category) => category.slug === categorySlug)) throw new Error('所属品类不存在。');
      if (content.products.some((item) => item.id !== data.id && item.categorySlug === categorySlug && item.slug === slug)) throw new Error(`该品类下的产品 Slug“${slug}”已被使用。`);
      const product = {
        id: data.id || `prod_${crypto.randomUUID()}`,
        name,
        slug,
        categorySlug,
        coverImage: data.coverImage || '',
        galleryImages: Array.isArray(data.galleryImages) ? data.galleryImages : [],
        videoUrl: data.videoUrl || '',
        summary: data.summary || '',
        features: Array.isArray(data.features) ? data.features : [],
        specifications: Array.isArray(data.specifications) ? data.specifications : [],
        downloads: Array.isArray(data.downloads) ? data.downloads : [],
        body: data.body || '',
        isFeatured: Boolean(data.isFeatured),
        sortOrder: Number(data.sortOrder || 0),
        seo: data.seo || { title: `${data.name || '产品'} | 星渡ASTREVA`, description: data.summary || '' }
      };
      const index = content.products.findIndex((item) => item.id === product.id);
      if (index >= 0) content.products[index] = product;
      else content.products.push(product);
      saved = product;
    }
  } else if (section === 'faq') {
    if (data._delete && data.id) {
      content.faqs = content.faqs.filter((item) => item.id !== data.id);
      saved = { deleted: true, id: data.id };
    } else {
      const question = requireText(data.question, 'FAQ 问题', 500);
      const answer = requireText(data.answer, 'FAQ 答案', 10000);
      const faq = {
        id: data.id || `faq_${crypto.randomUUID()}`,
        question,
        answer,
        sortOrder: Number(data.sortOrder || 0),
        enabled: data.enabled !== false
      };
      const index = content.faqs.findIndex((item) => item.id === faq.id);
      if (index >= 0) content.faqs[index] = faq;
      else content.faqs.push(faq);
      saved = faq;
    }
  } else if (section === 'staticPage') {
    if (!STATIC_PAGE_SLUGS.has(String(data.slug || ''))) throw new Error('静态页 Slug 不允许修改。');
    const page = getStaticPage(content, data.slug);
    Object.assign(page, data);
    saved = page;
  } else if (section === 'lead') {
    return leadsModule.saveLead(data);
  } else {
    throw new Error('未知的保存模块。');
  }

  // 富文本字段写入前做白名单消毒，避免存储型 XSS 带到官网。
  const sanitized = sanitizeContentRichText(content);
  for (const key of Object.keys(sanitized)) content[key] = sanitized[key];
  assertContentIntegrity(content);
  await saveContent(content, { expectedRevision: payload.revision });
  return saved;
}

async function parseUpload(req) {
  const form = formidable({
    multiples: false,
    maxFileSize: mediaRuntimeConfig.image.maxUploadBytes,
    keepExtensions: true
  });
  return new Promise((resolve, reject) => {
    form.parse(req, (error, fields, files) => {
      if (error) reject(error);
      else resolve({ fields, files });
    });
  });
}

async function handleUpload(req, res) {
  const { fields, files } = await parseUpload(req);
  const rawFile = files.file || files.files || Object.values(files)[0];
  const file = Array.isArray(rawFile) ? rawFile[0] : rawFile;
  if (!file) throw new Error('请选择要上传的文件。');
  const originalName = file.originalFilename || file.name || 'asset';
  const value = (name, fallback = '') => {
    const raw = fields[name];
    return String(Array.isArray(raw) ? raw[0] : raw ?? fallback);
  };
  return mediaStore.upload({
    temporaryPath: file.filepath || file.path,
    originalName,
    mode: ['smart', 'original'].includes(value('mode', 'auto')) ? value('mode', 'auto') : 'auto',
    category: value('category', '未分类'),
    preserveOriginal: value('preserveOriginal', String(mediaRuntimeConfig.image.keepOriginal)) !== 'false'
  });
}

async function handleMediaReplace(req, res, id) {
  const { fields, files } = await parseUpload(req);
  const rawFile = files.file || files.files || Object.values(files)[0];
  const file = Array.isArray(rawFile) ? rawFile[0] : rawFile;
  if (!file) throw new Error('请选择用于替换的文件。');
  const value = (name, fallback = '') => {
    const raw = fields[name];
    return String(Array.isArray(raw) ? raw[0] : raw ?? fallback);
  };
  return mediaStore.replace(id, {
    temporaryPath: file.filepath || file.path,
    originalName: file.originalFilename || file.name || '',
    mode: ['smart', 'original'].includes(value('mode', 'smart')) ? value('mode', 'smart') : 'smart',
    category: value('category', ''),
    preserveOriginal: value('preserveOriginal', String(mediaRuntimeConfig.image.keepOriginal)) !== 'false'
  });
}

const leadsModule = createLeadsModule({
  readLeads,
  saveLeads,
  bodyJson,
  json,
  verifySession,
  auditEvent,
  requestIp
});

const handleContent = createContentRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  readContent,
  readContentRecord,
  readContentTranslation: (locale) => translationService.read(locale),
  applyPublicCors: applyPublicContentCors,
  activeFallbackVersion,
  readFallbackStore,
  createFallbackVersion,
  activateFallbackVersion,
  deleteFallbackVersion,
  fallbackVersionSummary,
  getBootstrap,
  saveSection,
  previewUrl: SITE_ORIGIN
});

const handleWebsiteTranslation = createWebsiteTranslationRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  translationService,
  readContentRecord,
  applyPublicCors: applyPublicContentCors
});

const handleWebsiteSeo = createWebsiteSeoRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  seoService,
  readContentRecord,
  saveContent,
  getBootstrap
});

let previousCpuSnapshot = null;
let previousProcessCpuSnapshot = null;

function currentCpuSnapshot() {
  const cpus = os.cpus();
  const totals = cpus.reduce((sum, item) => sum + Object.values(item.times).reduce((part, value) => part + Number(value || 0), 0), 0);
  const idle = cpus.reduce((sum, item) => sum + Number(item.times.idle || 0), 0);
  return { cores: cpus.length, totals, idle };
}

function realtimeCpuUsage() {
  const current = currentCpuSnapshot();
  const processUsage = process.cpuUsage();
  const now = process.hrtime.bigint();
  let systemPercent = 0;
  let processPercent = 0;
  if (previousCpuSnapshot) {
    const totalDelta = current.totals - previousCpuSnapshot.totals;
    const idleDelta = current.idle - previousCpuSnapshot.idle;
    if (totalDelta > 0) systemPercent = Math.max(0, Math.min(100, (1 - idleDelta / totalDelta) * 100));
  }
  if (previousProcessCpuSnapshot) {
    const cpuDelta = (processUsage.user - previousProcessCpuSnapshot.usage.user) + (processUsage.system - previousProcessCpuSnapshot.usage.system);
    const elapsedMicros = Number(now - previousProcessCpuSnapshot.at) / 1000;
    if (elapsedMicros > 0) processPercent = Math.max(0, Math.min(100, cpuDelta / elapsedMicros / Math.max(1, current.cores) * 100));
  }
  previousCpuSnapshot = current;
  previousProcessCpuSnapshot = { usage: processUsage, at: now };
  return { systemPercent, processPercent };
}

async function mediaRuntimeStatus() {
  const memory = process.memoryUsage();
  const [storage, mediaStats, disk] = await Promise.all([
    mediaStore.health(),
    platformStore.mediaStats(),
    fs.statfs(ROOT).catch(() => null)
  ]);
  const diskBlockSize = Number(disk?.bsize || 0);
  const diskTotal = Number(disk?.blocks || 0) * diskBlockSize;
  const diskFree = Number(disk?.bavail || disk?.bfree || 0) * diskBlockSize;
  const cpu = os.cpus()[0] || {};
  const cpuUsage = realtimeCpuUsage();
  return {
    service: {
      ok: true,
      pid: process.pid,
      node: process.version,
      platform: `${process.platform}/${process.arch}`,
      startedAt: new Date(Date.now() - process.uptime() * 1000).toISOString(),
      uptimeSeconds: Math.floor(process.uptime())
    },
    database: { ok: true, driver: platformStore.driver === 'postgres' ? 'PostgreSQL' : platformStore.driver },
    storage,
    processing: { ok: true, engine: 'sharp / libvips' },
    cpu: { cores: os.cpus().length, model: cpu.model || '', loadAverage: os.loadavg(), usagePercent: cpuUsage.systemPercent, processUsagePercent: cpuUsage.processPercent },
    memory: {
      rss: memory.rss,
      heapUsed: memory.heapUsed,
      heapTotal: memory.heapTotal,
      systemTotal: os.totalmem(),
      systemFree: os.freemem()
    },
    disk: { available: Boolean(disk), total: diskTotal, free: diskFree, used: Math.max(0, diskTotal - diskFree) },
    media: mediaStats,
    generatedAt: new Date().toISOString()
  };
}

const handleMedia = createMediaRouter({
  upload: handleUpload,
  replace: handleMediaReplace,
  search: (options) => platformStore.searchMedia(options),
  get: (id) => platformStore.getMedia(id),
  remove: (id) => mediaStore.remove(id),
  references: mediaReferences,
  referenceUrls: (media) => mediaStore.referenceUrls(media),
  stats: () => platformStore.mediaStats(),
  storageHealth: () => mediaStore.health(),
  listCategories: () => platformStore.listMediaCategories(),
  createCategory: (name) => platformStore.createMediaCategory(name),
  deleteCategory: (name) => platformStore.deleteMediaCategory(name),
  createPresignedUpload: (payload) => mediaStore.createPresignedUpload(payload),
  confirmPresignedUpload: (token) => mediaStore.confirmPresignedUpload(token),
  aiModels: (options) => mediaAiService.models(options),
  aiGenerate: (payload) => mediaAiService.generate(payload),
  aiPreview: (payload) => mediaAiService.preview(payload),
  aiCommitPreview: (payload) => mediaAiService.commitPreview(payload),
  aiDiscardPreview: (id) => mediaAiService.discardPreview(id),
  runtimeStatus: mediaRuntimeStatus,
  bodyJson,
  json,
  verifySession,
  auditEvent
});

// ===== 二期：访问统计、系统日志与健康度 =====
function resolveAdminClient(req) {
  const ip = requestIp(req) || '';
  const forwarded = String(req.headers['x-forwarded-for'] || '').trim();
  return { ip, source: forwarded ? 'proxy' : 'direct' };
}

function countryFromRequest(req) {
  return String(req.headers['cf-ipcountry'] || req.headers['x-vercel-ip-country'] || req.headers['x-country-code'] || '').trim().toUpperCase().slice(0, 8);
}

function analyticsTimezone() {
  return process.env.ADMIN_ANALYTICS_TIMEZONE || 'Asia/Shanghai';
}

function analyticsCors(req, res) {
  const origin = String(req.headers.origin || '').replace(/\/$/, '');
  if (!origin) return true;
  if (!ADMIN_CORS_ALLOWED_ORIGINS.has(origin)) return false;
  res.setHeader('Access-Control-Allow-Origin', origin);
  res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  res.setHeader('Access-Control-Max-Age', '86400');
  res.setHeader('Vary', 'Origin');
  return true;
}

/** 写系统日志：失败不影响主流程。 */
function recordSystemEvent(event) {
  return platformStore.recordSystemEvent(event).catch((error) => {
    console.error('[system-log]', error.message);
    return null;
  });
}

const analyticsModule = createAnalyticsRouter({
  platformStore,
  bodyJson,
  json,
  verifySession,
  applyPublicCors: analyticsCors,
  resolveClient: resolveAdminClient,
  countryFromRequest,
  timezoneFromRequest: analyticsTimezone
});

const systemModule = createSystemRouter({
  platformStore,
  json,
  bodyJson,
  verifySession,
  auditEvent,
  storagePaths: [
    { label: '上传目录', path: UPLOAD_DIR },
    { label: '数据目录', path: DATA_DIR }
  ],
  serviceBridgeStatus: () => serviceBridgeProxy.adminJson('/api/admin/system-status', { signal: AbortSignal.timeout(4000) }),
  rebuildStatus: () => publishService.statusPayload(),
  storageHealth: () => mediaStore.health(),
  aiCapability: () => mediaAiService.capability(),
  translationSettings: () => translationService.publicSettings(),
  serviceBridgeAudit: () => serviceBridgeProxy.adminJson('/api/admin/audit-events?limit=200', { signal: AbortSignal.timeout(4000) }),
  eventRecorder: recordSystemEvent
});

const handlePublish = createPublishRouter({
  platformStore,
  publishService,
  verifySession,
  auditEvent,
  bodyJson,
  json,
  publicOrigin: PUBLIC_ORIGIN
});

const CONTENT_EDITOR_ITEMS = [
    ['allContent', '全部内容字段', '☷', '/'],
    ['homeHero', '首页首屏', '⌂', '/#home-hero'],
    ['homeProducts', '首页产品区', '▦', '/#home-products'],
    ['homeAdvantages', '首页优势', '✦', '/#home-advantages'],
    ['homeTestimonials', '客户评价', '', '/#home-testimonials'],
    ['homeCta', '首页行动区', '', '/#home-cta'],
    ['productCenter', '产品中心', '□', '/products/#products-hero'],
    ['categories', '产品品类', '', '/products/#product-categories'],
    ['products', '产品管理', '', '/products/#product-categories'],
    ['solutions', '解决方案', '◇', '/solutions/#solutions-hero'],
    ['support', '服务支持', '☏', '/support/#support-hero'],
    ['about', '关于我们', '♙', '/about/#about-hero'],
    ['warranty', '质保页面', '', '/support/warranty/#warranty-content'],
    ['install', '安装指南', '', '/support/install/#install-content'],
    ['faq', '常见问题', '?', '/support/faq/#faq-list'],
    ['faqPage', 'FAQ 页面设置', '', '/support/faq/#faq-list'],
    ['privacy', '隐私政策', '', '/privacy/#privacy-content'],
    ['agreement', '用户协议', '', '/agreement/#agreement-content'],
    ['media', '媒体库', '▧', '/#home-hero'],
    ['leads', '客户咨询', '▤', '/support/#support-contact'],
    ['seo', 'SEO 设置', 'SEO', '/#home-hero'],
    ['site', '全站设置', '⚙', '/#home-hero'],
    ['fallbackVersions', '降级版本', '⎙', '/#home-hero'],
    ['navigation', '导航文案', '', '/#home-hero'],
    ['footer', '页脚链接', '', '/#site-footer'],
    ['social', '社交方式', '', '/#site-footer'],
    ['cookie', 'Cookie 提示', '', '/#cookie-consent'],
    ['chat', '在线客服', '', '/#home-hero']
];

const CONTENT_MODULE_GROUPS = [
  {
    id: 'pages',
    label: '页面',
    icon: '▣',
    description: '编辑首页、公司介绍、解决方案、支持与法务页面。',
    items: ['homeHero', 'homeProducts', 'homeAdvantages', 'homeTestimonials', 'homeCta', 'solutions', 'support', 'about', 'warranty', 'install', 'faqPage', 'privacy', 'agreement']
  },
  {
    id: 'products',
    label: '产品',
    icon: '▦',
    description: '管理产品中心、产品品类、产品资料和常见问题。',
    items: ['productCenter', 'categories', 'products', 'faq']
  },
  {
    id: 'media',
    label: '素材',
    icon: '▧',
    description: '统一上传和查看图片、视频与文件。',
    items: ['media']
  },
  {
    id: 'global',
    label: '全局组件',
    icon: '⚙',
    description: '管理品牌、导航、页脚、联系方式、Cookie 和在线客服。',
    items: ['site', 'navigation', 'footer', 'social', 'cookie', 'chat']
  },
  {
    id: 'seo',
    label: 'SEO',
    icon: '⌕',
    description: '集中管理站点默认 SEO 和各主要页面的搜索摘要。',
    items: ['seo']
  },
  {
    id: 'leads',
    label: '客户咨询',
    icon: '✉',
    description: '查看咨询单并维护处理状态、优先级和内部备注。',
    items: ['leads']
  },
  {
    id: 'publish',
    label: '发布中心',
    icon: '⇧',
    description: '创建、启用和删除官网降级版本。',
    items: ['fallbackVersions']
  }
];

const CONTENT_EDITOR_ITEM_MAP = new Map(CONTENT_EDITOR_ITEMS.map((item) => [item[0], item]));

function navMarkup(active = 'homeHero') {
  return CONTENT_EDITOR_ITEMS.map(([id, label, icon, preview]) => {
    const child = !icon;
    return `<button class="${child ? 'nav-child' : 'nav-button'} ${id === active ? 'active' : ''}" type="button" data-nav="${id}" data-preview="${preview}">${child ? '' : `<span class="nav-icon">${escapeHtml(icon)}</span>`}<span>${escapeHtml(label)}</span></button>`;
  }).join('');
}

function renderContentHub() {
  const editorHref = (section) => section === 'media' ? '/site-admin/media' : `/site-admin/content/editor?section=${encodeURIComponent(section)}`;
  const groupMarkup = CONTENT_MODULE_GROUPS.map((group, index) => {
    const items = group.items.map((id) => CONTENT_EDITOR_ITEM_MAP.get(id)).filter(Boolean);
    return `<details class="module" ${index === 0 ? 'open' : ''}>
      <summary><span class="module-icon">${escapeHtml(group.icon)}</span><span class="module-copy"><strong>${escapeHtml(group.label)}</strong><small>${escapeHtml(group.description)}</small></span><span class="module-count">${items.length} 项</span><span class="chevron">›</span></summary>
      <div class="module-items">${items.map(([id, label, , preview]) => `<a class="module-item" href="${editorHref(id)}" target="_top"><span><strong>${escapeHtml(label)}</strong><small>预览位置：${escapeHtml(preview)}</small></span><span class="enter">进入编辑 →</span></a>`).join('')}</div>
    </details>`;
  }).join('');
  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>官网内容管理</title>
  <style>
    :root { --blue:#0666d8; --navy:#063b78; --ink:#17213a; --muted:#6c7d95; --line:#dbe5f0; --wash:#f4f8fd; }
    * { box-sizing: border-box; }
    html { background: var(--wash); }
    body { margin: 0; min-height: 100vh; padding: 30px clamp(20px,4vw,54px) 60px; color: var(--ink); background: radial-gradient(circle at 92% 0%, rgba(6,102,216,.10), transparent 28rem), var(--wash); font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif; }
    .shell { width: min(1080px, 100%); margin: 0 auto; }
    .hero { display:flex; justify-content:space-between; align-items:end; gap:24px; margin-bottom:24px; }
    h1 { margin:0 0 8px; font-size:30px; }
    .hero p { margin:0; color:var(--muted); line-height:1.7; }
    .security { flex:0 0 auto; padding:9px 13px; border:1px solid #cde5d8; border-radius:999px; color:#137047; background:#edf9f3; font-size:13px; font-weight:800; }
    .modules { display:grid; gap:13px; }
    .module { overflow:hidden; border:1px solid var(--line); border-radius:15px; background:#fff; box-shadow:0 8px 26px rgba(19,51,86,.05); }
    summary { min-height:82px; display:flex; align-items:center; gap:15px; padding:15px 20px; cursor:pointer; list-style:none; user-select:none; }
    summary::-webkit-details-marker { display:none; }
    summary:hover { background:#fafdff; }
    .module-icon { width:44px; height:44px; flex:0 0 44px; display:grid; place-items:center; border-radius:12px; color:var(--blue); background:#eaf4ff; font-size:22px; font-weight:900; }
    .module-copy { min-width:0; display:grid; gap:4px; }
    .module-copy strong { font-size:18px; }
    .module-copy small { color:var(--muted); line-height:1.5; }
    .module-count { margin-left:auto; white-space:nowrap; color:#52708e; font-size:13px; font-weight:800; }
    .chevron { margin-left:5px; color:#66809b; font-size:29px; transform:rotate(90deg); transition:transform .18s; }
    details[open] .chevron { transform:rotate(-90deg); }
    .module-items { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; padding:0 18px 18px 77px; border-top:1px solid #edf2f7; }
    .module-item { min-height:66px; display:flex; align-items:center; justify-content:space-between; gap:12px; margin-top:12px; padding:12px 14px; border:1px solid #dce6f1; border-radius:11px; color:var(--ink); background:#fbfdff; text-decoration:none; }
    .module-item:hover { border-color:#8bbbe9; background:#f1f8ff; box-shadow:0 6px 16px rgba(6,102,216,.08); }
    .module-item span:first-child { min-width:0; display:grid; gap:4px; }
    .module-item small { overflow:hidden; color:var(--muted); text-overflow:ellipsis; white-space:nowrap; }
    .enter { flex:0 0 auto; color:var(--blue); font-size:13px; font-weight:900; }
    .advanced { margin-top:18px; border:1px dashed #b8c8d9; border-radius:13px; background:rgba(255,255,255,.62); }
    .advanced summary { min-height:60px; }
    .advanced-body { padding:0 20px 20px; color:var(--muted); line-height:1.7; }
    .advanced a { display:inline-flex; margin-top:8px; padding:10px 14px; border-radius:9px; color:#fff; background:var(--navy); font-weight:800; text-decoration:none; }
    @media (max-width:760px) { body{padding:22px 14px 42px}.hero{align-items:start;flex-direction:column}.module-items{grid-template-columns:1fr;padding-left:18px}.module-copy small,.module-item small{white-space:normal}.module-count{display:none} }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero"><div><h1>官网内容管理</h1><p>按业务分组展开需要的栏目，再进入独立编辑页；暂时用不到的分组可以保持折叠。</p></div><span class="security">2FA 会话保护中</span></section>
    <section class="modules">${groupMarkup}</section>
    <details class="advanced"><summary><strong>开发者高级模式</strong><span class="module-count">低频功能</span><span class="chevron">›</span></summary><div class="advanced-body">完整字段和 JSON 编辑误操作风险较高，默认收起，仅在批量核对或前端新增字段时使用。<br><a href="${editorHref('allContent')}" target="_top">进入全部内容字段 →</a></div></details>
  </main>
</body>
</html>`;
}

function renderLoginPage() {
  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>星渡官网后台登录</title>
  <style>
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; color: #17213a; background: radial-gradient(circle at 8% 0%, #eef7ff, transparent 26rem), #f6f9fd; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; }
    .shell { width: min(1180px, calc(100vw - 64px)); min-height: 690px; display: grid; grid-template-columns: 1fr 1.26fr; border-radius: 24px; overflow: hidden; background: white; box-shadow: 0 28px 90px rgba(18, 39, 82, .16); }
    .brand { position: relative; padding: 68px 54px; color: white; background: radial-gradient(circle at 20% 10%, rgba(0, 149, 255, .42), transparent 22rem), linear-gradient(160deg, #075fb5, #063d8b 58%, #04316f); overflow: hidden; }
    .brand:after { content: ""; position: absolute; inset: 18% -20% 0 24%; opacity: .18; background: linear-gradient(120deg, transparent, rgba(255,255,255,.35), transparent); transform: skewX(-18deg); }
    .logo { display: flex; align-items: center; gap: 16px; font-size: 30px; font-weight: 900; }
    .logo img { width: 64px; height: 64px; object-fit: contain; }
    .tagline { margin-left: 82px; margin-top: -10px; font-size: 16px; color: rgba(255,255,255,.82); }
    .brand h1 { position: relative; z-index: 1; margin: 230px 0 18px; font-size: 46px; letter-spacing: 0; }
    .brand p { position: relative; z-index: 1; font-size: 18px; color: rgba(255,255,255,.86); }
    .protected { position: absolute; left: 54px; bottom: 60px; font-size: 14px; color: rgba(255,255,255,.9); }
    .panel { padding: 86px 82px; display: flex; flex-direction: column; justify-content: center; }
    .eyebrow { color: #0666d8; font-weight: 800; margin-bottom: 14px; }
    h2 { margin: 0 0 18px; font-size: 42px; }
    .sub { color: #6b7894; font-size: 18px; margin-bottom: 34px; }
    label { display: block; font-weight: 800; margin: 18px 0 10px; }
    input { width: 100%; height: 54px; border: 1px solid #cfd9e7; border-radius: 8px; padding: 0 16px; font-size: 16px; outline: none; }
    input:focus { border-color: #0666d8; box-shadow: 0 0 0 4px rgba(6,102,216,.12); }
    .row { display: grid; grid-template-columns: 168px 1fr; gap: 18px; align-items: center; margin: 20px 0; }
    .qr { width: 168px; height: 168px; border: 1px solid #dbe5f1; border-radius: 12px; padding: 8px; background: white; }
    .secret { font-size: 13px; color: #697891; word-break: break-all; line-height: 1.6; }
    button { width: 100%; height: 56px; border: 0; border-radius: 8px; background: linear-gradient(135deg, #0875eb, #0655b7); color: #fff; font-size: 17px; font-weight: 900; margin-top: 24px; cursor: pointer; }
    .hint { margin-top: 18px; color: #6b7894; line-height: 1.7; }
    .error { margin-top: 14px; color: #b42318; font-weight: 700; min-height: 22px; }
    .footer { position: fixed; bottom: 26px; color: #7b88a1; font-size: 14px; }
    a { color: inherit; text-decoration: none; margin: 0 14px; }
  </style>
</head>
<body>
  <main class="shell">
    <section class="brand">
      <div class="logo"><img src="/assets/astreva-logo-transparent.png" alt="ASTREVA" /><span>ASTREVA</span></div>
      <div class="tagline">A Grate Innovation</div>
      <h1>星渡官网后台</h1>
      <p>内容、产品、SEO 与客服系统的统一管理入口</p>
      <div class="protected">2FA 安全保护</div>
    </section>
    <section class="panel">
      <div class="eyebrow">Admin Login</div>
      <h2 id="title">管理员登录</h2>
      <p class="sub" id="subtitle">先输入后台账号密码，通过后再进行 2FA 验证</p>
      <form id="form"></form>
      <div class="error" id="error"></div>
      <p class="hint" id="hint"></p>
    </section>
  </main>
  <footer class="footer">© 2026 星渡 ASTREVA <a href="/site-admin/help">帮助文档</a> | <a href="/site-admin/privacy">隐私政策</a></footer>
  <script>
    const form = document.querySelector('#form');
    const errorBox = document.querySelector('#error');
    const title = document.querySelector('#title');
    const subtitle = document.querySelector('#subtitle');
    const hint = document.querySelector('#hint');
    let challengeId = '';
    let mode = 'login';
    const requestedReturnTo = new URLSearchParams(location.search).get('returnTo') || '';
    const returnTo = requestedReturnTo.startsWith('/site-admin/') && !requestedReturnTo.startsWith('//') ? requestedReturnTo : '/site-admin/';
    function msg(text) { errorBox.textContent = text || ''; }
    async function api(path, body) {
      const res = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error?.message || '请求失败');
      return data;
    }
    function renderCredentials(setup) {
      mode = setup ? 'setup' : 'login';
      title.textContent = setup ? '创建后台管理员账号' : '管理员登录';
      subtitle.textContent = setup ? '创建管理员账号后绑定 2FA，后续登录必须使用验证码' : '先输入后台账号密码，通过后再进行 2FA 验证';
      hint.textContent = setup ? '请使用 Microsoft Authenticator 扫描下一步生成的二维码，应用中会显示“星渡官网账号”。' : '';
      form.innerHTML = '<label>管理员邮箱</label><input name="email" type="email" autocomplete="username" required placeholder="请输入管理员邮箱" />' +
        '<label>管理员密码</label><input name="password" type="password" autocomplete="' + (setup ? 'new-password' : 'current-password') + '" required placeholder="请输入管理员密码" />' +
        (setup ? '<label>确认密码</label><input name="confirmPassword" type="password" autocomplete="new-password" required placeholder="再次输入管理员密码" />' : '') +
        '<button>' + (setup ? '下一步：绑定 2FA' : '下一步：验证 2FA') + '</button>';
    }
    function renderTotp(data, setup) {
      mode = setup ? 'setup-verify' : 'login-verify';
      challengeId = data.challengeId;
      title.textContent = setup ? '绑定 2FA' : '输入 2FA 验证码';
      subtitle.textContent = setup ? '用 Microsoft Authenticator 扫码后，输入 6 位验证码完成绑定' : '打开 Microsoft Authenticator，输入“星渡官网账号”的 6 位验证码';
      hint.textContent = setup ? '密钥仅用于无法扫码时手动输入，请妥善保管。' : '';
      form.innerHTML = (setup ? '<div class="row"><img class="qr" src="' + data.qr + '" alt="2FA QR" /><div class="secret">手动密钥：<br><strong>' + data.secret + '</strong></div></div>' : '') +
        '<label>2FA 验证码</label><input name="code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" required placeholder="请输入 6 位验证码" />' +
        '<button>' + (setup ? '完成创建并进入后台' : '进入官网后台') + '</button>';
    }
    async function boot() {
      const res = await fetch('/site-admin-auth/status');
      const data = await res.json();
      if (data.authenticated) location.href = returnTo;
      else renderCredentials(!data.hasAdmin);
    }
    form.addEventListener('submit', async (event) => {
      event.preventDefault();
      msg('');
      const fd = new FormData(form);
      try {
        if (mode === 'setup') {
          const password = fd.get('password');
          if (password !== fd.get('confirmPassword')) throw new Error('两次输入的密码不一致');
          renderTotp(await api('/site-admin-auth/setup/start', { email: fd.get('email'), password }), true);
        } else if (mode === 'login') {
          renderTotp(await api('/site-admin-auth/login/start', { email: fd.get('email'), password: fd.get('password') }), false);
        } else if (mode === 'setup-verify') {
          await api('/site-admin-auth/setup/verify', { challengeId, code: fd.get('code') });
          location.href = returnTo;
        } else if (mode === 'login-verify') {
          await api('/site-admin-auth/login/verify', { challengeId, code: fd.get('code') });
          location.href = returnTo;
        }
      } catch (error) {
        msg(error.message);
      }
    });
    boot().catch((error) => msg(error.message));
  </script>
</body>
</html>`;
}

function renderSystemSettingsPage(bootstrap, options = {}) {
  const embedded = options.embedded === true;
  const business = bootstrap.business || SYSTEM_SETTINGS_DEFAULTS.business;
  const contact = bootstrap.contact || SYSTEM_SETTINGS_DEFAULTS.contact;
  const status = bootstrap.status || SYSTEM_SETTINGS_DEFAULTS.status;
  const ai = bootstrap.ai || SYSTEM_SETTINGS_DEFAULTS.ai;
  const media = bootstrap.media || {};
  const mediaRuntime = bootstrap.mediaRuntime || publicMediaRuntimeConfig(mediaRuntimeConfig);
  const storageConfig = mediaRuntime.storage || {};
  const aiImage = mediaRuntime.aiImage || {};
  const selected = (actual, expected) => String(actual) === String(expected) ? ' selected' : '';
  const state = (ok) => ok
    ? '<span class="health-dot ok">正常</span>'
    : '<span class="health-dot bad">异常</span>';
  const errorMarkup = bootstrap.errors?.length
    ? `<div class="banner error" id="settingsState" role="alert"><strong>部分服务当前不可用，页面已显示安全默认值，不会再空白。</strong><ul>${bootstrap.errors.map((item) => `<li>${escapeHtml(item)}</li>`).join('')}</ul></div>`
    : '<div class="banner ok" id="settingsState"><strong>当前配置已从服务器直接读取并显示。</strong><span>无需等待浏览器脚本填充。</span></div>';
  const apiKeyLabel = ai.api_key_masked
    ? `已配置（${escapeHtml(ai.api_key_masked)}）`
    : '未配置';
  const aiImageKeyLabel = aiImage.apiKeyConfigured
    ? `已配置（${escapeHtml(aiImage.apiKeyMasked)}）`
    : '未配置';
  const timezoneOptions = [
    ['Asia/Shanghai', '中国标准时间（上海）'],
    ['Asia/Hong_Kong', '中国香港'],
    ['Asia/Singapore', '新加坡'],
    ['Asia/Tokyo', '日本（东京）'],
    ['Europe/London', '英国（伦敦）'],
    ['Europe/Berlin', '欧洲中部（柏林）'],
    ['America/New_York', '美国东部（纽约）'],
    ['America/Los_Angeles', '美国西部（洛杉矶）'],
    ['UTC', 'UTC 协调世界时']
  ].map(([value, label]) => `<option value="${value}"${selected(business.timezone, value)}>${label} · ${value}</option>`).join('');
  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>系统设置 · ASTREVA</title>
  <link rel="stylesheet" href="/site-admin-assets/system-settings.css">
</head>
<body class="${embedded ? 'embedded' : ''}">
  <header class="page-head">
    <div><p class="eyebrow">ASTREVA CONTROL CENTER</p><h1>系统设置</h1><p>营业时间、访客入口、AI 接待策略与基础设施运行状态。</p></div>
    <div class="head-actions"><button type="button" class="button" id="refreshSettings">刷新状态</button>${embedded ? '' : '<a class="button primary" href="/site-admin/?view=settings">返回统一后台</a>'}</div>
  </header>
  <main>
    ${errorMarkup}
    <section class="health-grid" aria-label="当前运行状态">
      <article class="health-card"><span>PostgreSQL</span><strong>${state(Boolean(bootstrap.serviceBridgeOk && status.database?.ok))}</strong><small>${bootstrap.serviceBridgeOk ? (status.database?.ok ? '数据库连接正常' : '数据库健康检查异常') : 'ServiceBridge 不可达，暂无法确认'}</small></article>
      <article class="health-card"><span>ServiceBridge</span><strong>${state(Boolean(bootstrap.serviceBridgeOk))}</strong><small>${bootstrap.serviceBridgeOk ? '客服配置与运行状态可读取' : '已使用安全默认配置降级显示'}</small></article>
      <article class="health-card"><span>R2 / S3 对象存储</span><strong>${state(Boolean(media.ok))}</strong><small>${escapeHtml(media.bucket || media.message || '未配置 Bucket')}</small></article>
      <article class="health-card"><span>客服接待 AI</span><strong>${ai.api_key_masked ? '<span class="health-dot ok">已配置</span>' : '<span class="health-dot warn">未配置</span>'}</strong><small>${apiKeyLabel}</small></article>
      <article class="health-card"><span>素材库 AI 生图</span><strong>${aiImage.apiKeyConfigured ? '<span class="health-dot ok">已配置</span>' : '<span class="health-dot warn">未配置</span>'}</strong><small>${aiImageKeyLabel} · ${escapeHtml(aiImage.defaultModel || 'gpt-image-1')}</small></article>
    </section>

    <section class="settings-grid">
      <article class="panel">
        <div class="panel-head"><div><h2>营业时间</h2><p>控制访客端显示的在线服务时段。</p></div></div>
        <form id="businessForm" class="form-grid">
          <label>时区<select name="timezone" required>${timezoneOptions}</select></label>
          <label>启用状态<select name="enabled"><option value="true"${selected(business.enabled, true)}>启用</option><option value="false"${selected(business.enabled, false)}>停用</option></select></label>
          <label>开始时间<input name="start" type="time" required value="${escapeHtml(business.start)}"></label>
          <label>结束时间<input name="end" type="time" required value="${escapeHtml(business.end)}"></label>
          <div class="form-actions wide"><span class="form-state" aria-live="polite"></span><button class="button primary" type="submit">保存营业时间</button></div>
        </form>
      </article>

      <article class="panel">
        <div class="panel-head"><div><h2>访客联系方式与欢迎语</h2><p>访客端只对外提供 Telephone、WhatsApp 和 Telegram。</p></div></div>
        <form id="contactForm" class="form-grid">
          <label>Telephone<input name="telephone" value="${escapeHtml(contact.telephone)}"></label>
          <label>WhatsApp<input name="whatsapp" value="${escapeHtml(contact.whatsapp)}"></label>
          <label>Telegram<input name="telegram" value="${escapeHtml(contact.telegram)}"></label>
          <label class="wide">客服欢迎语<textarea name="entry_reply" rows="4">${escapeHtml(contact.entry_reply)}</textarea></label>
          <div class="form-actions wide"><span class="form-state" aria-live="polite"></span><button class="button primary" type="submit">保存访客配置</button></div>
        </form>
      </article>

      <article class="panel wide-panel">
        <div class="panel-head"><div><h2>客服接待 AI 策略</h2><p>先选协议、填上游地址和密钥，再真实读取上游模型；密钥仅加密落库。</p></div><span class="credential">${apiKeyLabel}</span></div>
        <form id="aiForm" class="form-grid three">
          <label>启用状态<select name="enabled"><option value="true"${selected(ai.enabled, true)}>启用</option><option value="false"${selected(ai.enabled, false)}>停用</option></select></label>
          <label>接待模式<select name="mode"><option value="human_first"${selected(ai.mode, 'human_first')}>人工优先</option><option value="always_ai"${selected(ai.mode, 'always_ai')}>AI 优先</option><option value="manual_only"${selected(ai.mode, 'manual_only')}>仅人工</option></select></label>
          <label>使用模型<select name="model" id="aiModelSelect" required><option value="${escapeHtml(ai.model)}" selected>${escapeHtml(ai.model)}</option></select></label>
          <label class="span-two">API Base URL<input name="base_url" type="url" value="${escapeHtml(ai.base_url)}"></label>
          <label>API 类型<select name="api_type"><option value="chat_completions"${selected(ai.api_type, 'chat_completions')}>OpenAI Chat Completions</option><option value="responses"${selected(ai.api_type, 'responses')}>OpenAI Responses</option><option value="anthropic_messages"${selected(ai.api_type, 'anthropic_messages')}>Anthropic Messages</option></select></label>
          <label class="span-two">新 API Key<input name="api_key" type="password" autocomplete="new-password" placeholder="留空则保留当前密钥"></label>
          <label>温度<input name="temperature" type="number" min="0" max="2" step="0.1" value="${escapeHtml(ai.temperature)}"></label>
          <label>接口超时（秒）<input name="timeout_seconds" type="number" min="1" value="${escapeHtml(ai.timeout_seconds)}"></label>
          <label>最大输出 Token<input name="max_output_tokens" type="number" min="1" value="${escapeHtml(ai.max_output_tokens)}"></label>
          <label class="wide">系统提示词<textarea name="system_prompt" rows="6">${escapeHtml(ai.system_prompt)}</textarea></label>
          <div class="form-actions wide"><span class="form-state" aria-live="polite"></span><button class="button" type="button" id="fetchAiModels">获取上游模型</button><button class="button primary" type="submit">加密保存 AI 设置</button></div>
        </form>
        <div class="form-actions wide"><span class="form-state" id="aiCacheProbeStatus" aria-live="polite"></span><button class="button" type="button" id="probeAiCache">重新探测模型缓存</button></div>
      </article>

      <article class="panel wide-panel">
        <div class="panel-head"><div><h2>R2 / S3 对象存储配置</h2><p>保存前会真实执行 HeadBucket；配置加密保存到 PostgreSQL，当前进程立即切换，后台重启后仍按保存值生效。</p></div><span class="credential">${storageConfig.accessKeyConfigured ? `凭据 ${escapeHtml(storageConfig.accessKeyMasked)}` : '凭据未配置'}</span></div>
        <form id="storageForm" class="form-grid three">
          <div class="inline-warning wide">当前素材库已有 ${Number(bootstrap.mediaStats?.total || 0)} 条记录。为保护现有对象，存在素材时只能更新凭据、Region、寻址方式和服务端加密策略，不能直接切换 Endpoint、Bucket、对象前缀或 Public URL。</div>
          <label class="span-two">S3 Endpoint<input name="endpoint" type="url" required value="${escapeHtml(storageConfig.endpoint)}"></label>
          <label>Region<input name="region" required value="${escapeHtml(storageConfig.region)}"></label>
          <label>Bucket<input name="bucket" required value="${escapeHtml(storageConfig.bucket)}"></label>
          <label class="span-two">Public Base URL<input name="publicBaseUrl" type="url" required value="${escapeHtml(storageConfig.publicBaseUrl)}"></label>
          <label>对象前缀<input name="prefix" value="${escapeHtml(storageConfig.prefix)}"></label>
          <label>寻址方式<select name="forcePathStyle"><option value="false"${selected(storageConfig.forcePathStyle, false)}>虚拟主机</option><option value="true"${selected(storageConfig.forcePathStyle, true)}>Path Style</option></select></label>
          <label>服务端加密<input name="serverSideEncryption" placeholder="例如 AES256" value="${escapeHtml(storageConfig.serverSideEncryption)}"></label>
          <label>新 Access Key ID<input name="accessKeyId" type="password" autocomplete="new-password" placeholder="${storageConfig.accessKeyConfigured ? `留空保留 ${escapeHtml(storageConfig.accessKeyMasked)}` : '请输入 Access Key ID'}"></label>
          <label>新 Secret Access Key<input name="secretAccessKey" type="password" autocomplete="new-password" placeholder="${storageConfig.secretAccessKeyConfigured ? '已配置，留空保留' : '请输入 Secret Access Key'}"></label>
          <label>新 Session Token<input name="sessionToken" type="password" autocomplete="new-password" placeholder="${storageConfig.sessionTokenConfigured ? '已配置，留空保留' : '可选'}"></label>
          <label class="check-label wide"><input name="clearSessionToken" type="checkbox">清除当前 Session Token</label>
          <div class="form-actions wide"><span class="form-state" aria-live="polite"></span><button class="button primary" type="submit">验证连接并保存 R2 配置</button></div>
        </form>
      </article>

      <article class="panel wide-panel">
        <div class="panel-head"><div><h2>AI 生图配置</h2><p>先获取上游生图模型再选择；密钥加密存入 PostgreSQL，当前进程和重启后都生效。</p></div><span class="credential">${aiImageKeyLabel}</span></div>
        <form id="aiImageForm" class="form-grid three">
          <label class="span-two">API Base URL<input name="baseUrl" type="url" required value="${escapeHtml(aiImage.baseUrl)}"></label>
          <label>使用生图模型<select name="defaultModel" id="aiImageModelSelect" required><option value="${escapeHtml(aiImage.defaultModel)}" selected>${escapeHtml(aiImage.defaultModel)}</option></select></label>
          <label class="span-two">新 API Key<input name="apiKey" type="password" autocomplete="new-password" placeholder="${aiImage.apiKeyConfigured ? `留空保留 ${escapeHtml(aiImage.apiKeyMasked)}` : '请输入 AI 生图 API Key'}"></label>
          <label>请求超时（毫秒）<input name="requestTimeoutMs" type="number" min="10000" max="600000" value="${escapeHtml(aiImage.requestTimeoutMs)}"></label>
          <label class="wide">允许请求的主机（逗号分隔）<input name="allowedHosts" required value="${escapeHtml(aiImage.allowedHosts)}"></label>
          <label class="wide">允许下载图片的主机（逗号分隔）<input name="downloadHosts" required value="${escapeHtml(aiImage.downloadHosts)}"></label>
          <input name="configuredModels" type="hidden" value="${escapeHtml(aiImage.configuredModels)}">
          <label class="check-label wide"><input name="clearApiKey" type="checkbox">清除当前 AI 生图 API Key，并停用生图</label>
          <div class="form-actions wide"><span class="form-state" aria-live="polite"></span><button class="button" type="button" id="fetchAiImageModels">获取上游模型</button><button class="button primary" type="submit">验证并加密保存</button></div>
        </form>
      </article>

      <article class="panel wide-panel readonly-panel">
        <div class="panel-head"><div><h2>存储与上传策略</h2><p>只读运行参数，不展示 Access Key、Secret 或管理员口令。</p></div></div>
        <dl class="facts">
          <div><dt>存储驱动</dt><dd>${escapeHtml(storageConfig.driver || media.driver || ADMIN_MEDIA_DRIVER)}</dd></div>
          <div><dt>Bucket</dt><dd>${escapeHtml(storageConfig.bucket || media.bucket || '-')}</dd></div>
          <div><dt>Endpoint</dt><dd>${escapeHtml(storageConfig.endpoint || media.endpoint || '-')}</dd></div>
          <div><dt>对象前缀</dt><dd>${escapeHtml(storageConfig.prefix || media.prefix || '-')}</dd></div>
          <div><dt>图片变体宽度</dt><dd>${escapeHtml(bootstrap.upload.variantWidths)} px</dd></div>
          <div><dt>WebP 质量</dt><dd>${escapeHtml(bootstrap.upload.webpQuality)}</dd></div>
          <div><dt>单文件上限</dt><dd>${escapeHtml(Math.round(bootstrap.upload.maxBytes / 1024 / 1024))} MB</dd></div>
          <div><dt>服务端读取时间</dt><dd>${escapeHtml(new Date(bootstrap.generatedAt).toLocaleString('zh-CN', { hour12: false, timeZone: 'Asia/Shanghai' }))}</dd></div>
        </dl>
      </article>
    </section>
  </main>
  <div class="notice" id="notice" role="status" aria-live="polite"></div>
  <script src="/site-admin-assets/system-settings.js" defer></script>
</body>
</html>`;
}

function renderAdminPage(bootstrap, options = {}) {
  const embedded = options.embedded === true;
  const standalone = options.standalone === true;
  const requestedSection = String(options.active || 'homeHero');
  const initialItem = CONTENT_EDITOR_ITEM_MAP.get(requestedSection) || CONTENT_EDITOR_ITEM_MAP.get('homeHero');
  const initialSection = initialItem[0];
  const initialPreview = initialItem[3] || '/';
  const operationOnly = ['media', 'fallbackVersions'].includes(initialSection);
  return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>星渡官网后台</title>
  <style>
    :root { --brand: #0666d8; --brand-dark: #054a9e; --ink: #131a2a; --muted: #6f7f99; --line: #d9e4f0; --wash: #f2f7fd; --ok: #10935a; --warn: #9a6421; --danger: #b42318; }
    * { box-sizing: border-box; }
    html, body { height: 100%; overflow: hidden; }
    body { margin: 0; color: var(--ink); background: #f4f8fd; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; }
    button, input, textarea, select { font: inherit; }
    button { cursor: pointer; }
    .app { height: 100vh; display: grid; grid-template-columns: 272px minmax(520px, 1fr) 560px; grid-template-rows: 76px minmax(0, 1fr); overflow: hidden; }
    .sidebar { grid-row: 1 / span 2; min-height: 0; max-height: 100vh; display: flex; flex-direction: column; overflow: hidden; padding: 18px 12px; color: rgba(255,255,255,.78); background: radial-gradient(circle at 12% 0%, rgba(0,158,255,.38), transparent 17rem), linear-gradient(180deg, #064f9e 0%, #043f86 46%, #032f69 100%); box-shadow: 10px 0 32px rgba(3,47,105,.15); }
    .brand { display: flex; align-items: center; gap: 12px; min-height: 64px; padding: 0 14px 18px; margin-bottom: 10px; border-bottom: 1px solid rgba(255,255,255,.14); color: #fff; font-size: 24px; font-weight: 900; white-space: nowrap; }
    .brand img { width: 44px; height: 44px; object-fit: contain; filter: drop-shadow(0 8px 18px rgba(0,0,0,.16)); }
    .nav { min-height: 0; overflow-y: auto; overscroll-behavior: contain; display: grid; gap: 8px; padding: 0 4px 24px 0; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,.32) transparent; }
    .nav::-webkit-scrollbar { width: 7px; } .nav::-webkit-scrollbar-thumb { background: rgba(255,255,255,.32); border-radius: 999px; }
    .nav-button, .nav-child { width: 100%; min-height: 50px; border: 0; border-radius: 8px; display: flex; align-items: center; gap: 12px; padding: 0 18px; color: rgba(255,255,255,.78); background: transparent; text-align: left; font-size: 18px; font-weight: 760; }
    .nav-child { min-height: 44px; padding-left: 58px; font-size: 16px; font-weight: 700; }
    .nav-button:hover, .nav-child:hover { color: #fff; background: rgba(255,255,255,.11); }
    .nav-button.active, .nav-child.active { color: #fff; background: linear-gradient(135deg, #0b7ff1, #0868d2); box-shadow: 0 12px 26px rgba(2,26,76,.18); }
    .nav-icon { width: 28px; text-align: center; font-size: 20px; }
    .topbar { grid-column: 2 / span 2; display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 0 24px 0 30px; background: rgba(255,255,255,.97); border-bottom: 1px solid var(--line); box-shadow: 0 2px 12px rgba(30,48,78,.05); }
    .status { display: flex; align-items: center; gap: 14px; font-size: 18px; font-weight: 900; white-space: nowrap; }
    .saved { color: var(--ok); } .pending { color: var(--warn); background: #f8ebd9; padding: 7px 14px; border-radius: 10px; }
    .build { color: #0a4d8c; background: #e9f3ff; padding: 7px 14px; border-radius: 10px; } .build.error { color: var(--danger); background: #fff1f1; }
    .actions { display: flex; align-items: center; gap: 12px; }
    .top-button { height: 44px; border-radius: 8px; border: 1px solid #cbd7e6; background: #fff; color: #111827; padding: 0 18px; font-weight: 900; box-shadow: 0 2px 7px rgba(20,38,68,.04); }
    .top-button.primary { background: #0666d8; border-color: #0666d8; color: #fff; }
    .top-button:disabled, .save-current:disabled, .ghost:disabled, .danger:disabled, .seo-ai-button:disabled { opacity: .58; cursor: not-allowed; filter: grayscale(.12); }
    .avatar { width: 44px; height: 44px; border-radius: 50%; display: grid; place-items: center; background: #eaf4ff; border: 1px solid #cfe0f2; font-size: 23px; }
    .preview-pane { min-width: 0; min-height: 0; overflow: auto; padding: 36px 34px; background: linear-gradient(90deg, #f5f9ff, #eef5fc); }
    .device-tabs { width: fit-content; margin: 0 auto 30px; display: flex; gap: 22px; padding: 6px; border: 1px solid #d1ddeb; border-radius: 10px; background: rgba(255,255,255,.88); box-shadow: 0 6px 20px rgba(15,49,95,.07); }
    .device-tabs button { height: 42px; border: 0; border-radius: 8px; background: transparent; padding: 0 18px; font-weight: 900; color: #1f2937; }
    .device-tabs button.active { color: #0a4d8c; background: #e9f4ff; outline: 1px solid #2377bc; }
    .preview-frame-wrap { width: min(760px, 100%); margin: 0 auto; border-radius: 8px; background: #fff; border: 1px solid #d8e3ef; box-shadow: 0 18px 44px rgba(24,62,111,.12); overflow: hidden; transition: width .2s; }
    .preview-frame-wrap.tablet { width: 620px; } .preview-frame-wrap.mobile { width: 390px; }
    iframe { display: block; width: 100%; height: 980px; border: 0; background: #fff; }
    .editor { min-height: 0; overflow: auto; background: #fff; border-left: 1px solid var(--line); padding: 32px 36px 60px; }
    .editor h1 { margin: 0 0 18px; font-size: 28px; } .rule { height: 1px; background: var(--line); margin: 0 0 24px; }
    .form { display: grid; gap: 18px; } .field label { display: block; margin-bottom: 8px; color: #202a3d; font-weight: 900; }
    input, textarea, select { width: 100%; border: 1px solid #cbd7e6; border-radius: 8px; background: #fff; color: #111827; padding: 11px 12px; outline: none; }
    textarea { min-height: 96px; resize: vertical; line-height: 1.6; } input:focus, textarea:focus, select:focus { border-color: #0666d8; box-shadow: 0 0 0 3px rgba(6,102,216,.12); }
    .small { color: var(--muted); font-size: 13px; line-height: 1.6; } .grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
    .list { display: grid; gap: 10px; } .card { border: 1px solid #d7e2ef; border-radius: 10px; background: #fbfdff; padding: 14px; display: grid; gap: 12px; }
    .card-head { display: flex; justify-content: space-between; gap: 10px; align-items: center; font-weight: 900; }
    .ghost, .danger { height: 38px; border-radius: 8px; border: 1px solid #cbd7e6; background: #fff; padding: 0 12px; font-weight: 800; }
    .danger { color: var(--danger); border-color: #ffd1d1; } .save-current { height: 54px; border: 0; border-radius: 8px; background: linear-gradient(135deg, #0875eb, #0655b7); color: #fff; font-size: 17px; font-weight: 900; margin-top: 8px; }
    .seo-ai-panel { border-color: #a9c8e8; background: #f5faff; }
    .seo-ai-panel p { margin: 0; }
    .seo-ai-button { min-height: 48px; border: 0; border-radius: 8px; padding: 0 18px; background: #075fae; color: #fff; font-size: 16px; font-weight: 900; }
    .seo-ai-status { min-height: 22px; color: #11633b; font-weight: 800; }
    .picker { display: grid; grid-template-columns: 150px 1fr; gap: 12px; } .select-list { display: grid; gap: 8px; align-content: start; }
    .select-list button { min-height: 38px; border-radius: 8px; border: 1px solid #d6e1ef; background: #fff; text-align: left; padding: 8px 10px; font-weight: 800; }
    .select-list button.active { border-color: #0666d8; color: #064b9f; background: #eef7ff; }
    .media-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; } .media-item { border: 1px solid #dbe5f1; border-radius: 10px; overflow: hidden; background: #fff; }
    .media-item img, .media-item video { width: 100%; aspect-ratio: 1.5; object-fit: cover; display: block; background: #eef3f8; } .media-item div { padding: 9px; font-size: 12px; color: #5f6f86; word-break: break-all; }
    .error-panel { position: fixed; left: 300px; right: 24px; bottom: 22px; z-index: 50; display: none; padding: 14px 16px; border: 1px solid #ffd1d1; border-radius: 10px; color: #941b1b; background: #fff4f4; font-weight: 800; box-shadow: 0 12px 30px rgba(100,20,20,.12); }
    .error-panel.show { display: block; }
    body.embedded .app { grid-template-columns: 238px minmax(520px, 1fr) 520px; grid-template-rows: minmax(0, 1fr); }
    body.embedded .sidebar { grid-row: 1; padding-top: 10px; }
    body.embedded .brand, body.embedded .topbar { display: none; }
    body.embedded .preview-pane { grid-column: 2; }
    body.embedded .editor { grid-column: 3; }
    body.embedded .error-panel { left: 258px; }
    body.standalone .app { grid-template-columns: minmax(430px, 1fr) minmax(470px, 580px); }
    body.standalone .sidebar { display: none; }
    body.standalone .topbar { grid-column: 1 / span 2; padding-left: 20px; }
    body.standalone .preview-pane { grid-column: 1; }
    body.standalone .editor { grid-column: 2; }
    body.standalone .error-panel { left: 24px; }
    .return-button { color: #064b9f; border-color: #b9d4ef; background: #edf6ff; }
    @media (max-width: 1120px) {
      body.standalone .app { grid-template-columns: minmax(350px, 1fr) minmax(430px, 510px); }
      body.standalone .status { font-size: 15px; gap: 8px; }
      body.standalone .actions { gap: 7px; }
      body.standalone .top-button { padding: 0 11px; }
      body.standalone .avatar, body.standalone .actions strong { display: none; }
    }
    @media (max-width: 860px) {
      body.standalone .app { grid-template-columns: 1fr; }
      body.standalone .topbar { grid-column: 1; }
      body.standalone .preview-pane { display: none; }
      body.standalone .editor { grid-column: 1; }
      body.standalone .status .build, body.standalone #previewOpen { display: none; }
    }
  </style>
</head>
<body class="${[embedded ? 'embedded' : '', standalone ? 'standalone' : ''].filter(Boolean).join(' ')}">
  <div class="app">
    <aside class="sidebar">
      <div class="brand"><img src="/assets/astreva-logo-transparent.png" alt="星渡" /><span>星渡官网后台</span></div>
      <nav class="nav" id="nav">${navMarkup(initialSection)}</nav>
    </aside>
    <header class="topbar">
      <div class="status">${standalone ? '<button class="top-button return-button" id="exitEditor">← 返回内容管理</button>' : ''}<span class="saved" id="saved">✓ 已保存</span><span class="pending" id="dirty">● 可编辑</span><span class="build" id="build">重建状态读取中</span></div>
      <div class="actions"><button class="top-button" id="previewOpen">预览已发布官网</button><button class="top-button" id="saveTop" data-operation-only="${operationOnly}" ${operationOnly ? 'disabled title="当前模块的操作会立即保存"' : ''}>保存草稿</button><button class="top-button primary" id="publish" data-operation-only="${operationOnly}" ${operationOnly ? 'disabled title="当前模块不需要整体发布"' : ''}>保存并发布</button>${standalone ? '<button class="top-button primary" id="saveExit">保存并退出</button>' : ''}<span class="avatar">👤</span><strong>管理员</strong><button class="top-button" id="logout">退出登录</button></div>
    </header>
    <section class="preview-pane">
      <div class="device-tabs"><button class="active" data-device="desktop">▣ 桌面</button><button data-device="tablet">▯ 平板</button><button data-device="mobile">▯ 手机</button></div>
      <div class="preview-frame-wrap" id="frameWrap"><iframe id="preview" src="${SITE_ORIGIN}/"></iframe></div>
    </section>
    <main class="editor" id="editor"><h1>正在加载编辑器</h1><div class="rule"></div><p>正在加载内容...</p></main>
  </div>
  <div class="error-panel" id="errorPanel"></div>
  <script>window.__BOOTSTRAP__=${safeJson(bootstrap)}; window.__SITE_ORIGIN__=${safeJson(SITE_ORIGIN)};</script>
  <script>
    const state = { data: window.__BOOTSTRAP__, content: window.__BOOTSTRAP__.content, fallbackVersions: window.__BOOTSTRAP__.fallbackVersions || [], fallbackActiveId: window.__BOOTSTRAP__.fallbackActiveId || '', active: ${safeJson(initialSection)}, dirty: false, busy: false, allowLeave: false, selected: { category: 0, product: 0, faq: 0, staticPage: 'about', lead: 0 }, previewPath: ${safeJson(initialPreview)} };
    const editor = document.querySelector('#editor');
    const nav = document.querySelector('#nav');
    const preview = document.querySelector('#preview');
    const frameWrap = document.querySelector('#frameWrap');
    const errorPanel = document.querySelector('#errorPanel');
    const build = document.querySelector('#build');
    const saved = document.querySelector('#saved');
    const dirty = document.querySelector('#dirty');
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (...args) => {
      const response = await originalFetch(...args);
      const input = args[0] instanceof Request ? args[0].url : String(args[0] || '');
      const requestUrl = new URL(input, location.origin);
      if (response.status === 401 && requestUrl.origin === location.origin && requestUrl.pathname.startsWith('/site-admin-api/')) {
        const returnTo = location.pathname + location.search;
        state.allowLeave = true;
        location.href = '/admin-2fa?returnTo=' + encodeURIComponent(returnTo);
      }
      return response;
    };
    function showError(error) { errorPanel.textContent = error?.message || String(error); errorPanel.classList.add('show'); }
    function clearError() { errorPanel.textContent = ''; errorPanel.classList.remove('show'); }
    function syncBusy() { document.querySelectorAll('#saveTop,#publish,#saveExit,#saveCurrent,#generateSeo,[data-delete],[data-create-fallback],[data-activate-fallback],[data-delete-fallback]').forEach((button) => { button.disabled = state.busy || button.dataset.operationOnly === 'true'; }); }
    window.onerror = (_m, _s, _l, _c, error) => showError(error || _m);
    window.onunhandledrejection = (event) => showError(event.reason || '后台运行错误');
    const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#039;'}[c]));
    const byPath = (obj, path) => path.split('.').reduce((acc, key) => acc?.[key], obj);
    function setPath(obj, path, value) { const keys = path.split('.'); let cur = obj; keys.slice(0, -1).forEach((key) => { cur[key] ||= {}; cur = cur[key]; }); cur[keys.at(-1)] = value; markDirty(); }
    function markDirty() { state.dirty = true; saved.textContent = '未保存'; dirty.textContent = '● 有未保存修改'; }
    function field(label, path, type = 'text', placeholder = '') { const v = byPath(state.content, path) ?? ''; return '<div class="field"><label>'+esc(label)+'</label><input data-path="'+esc(path)+'" type="'+type+'" value="'+esc(v)+'" placeholder="'+esc(placeholder)+'" /></div>'; }
    function text(label, path, rows = 3) { const v = byPath(state.content, path) ?? ''; return '<div class="field"><label>'+esc(label)+'</label><textarea data-path="'+esc(path)+'" rows="'+rows+'">'+esc(v)+'</textarea></div>'; }
    function checkbox(label, path) { const v = Boolean(byPath(state.content, path)); return '<label class="field"><input data-path="'+esc(path)+'" type="checkbox" '+(v?'checked':'')+' style="width:auto;margin-right:8px" />'+esc(label)+'</label>'; }
    function mediaField(label, path, accept = 'image/*,video/*') { return field(label, path) + '<div class="field"><input type="file" data-upload-target="'+esc(path)+'" accept="'+accept+'" /><p class="small">上传后会自动回填上方地址，也可以直接粘贴图片或视频 URL。</p></div>'; }
    function selectedStaticPage() { return state.content.staticPages.find(p=>p.slug===state.selected.staticPage) || {}; }
    function staticMediaField(label, path, accept = 'image/*,video/*') { const page=selectedStaticPage(); const v=byPath(page,path) ?? ''; return '<div class="field"><label>'+esc(label)+'</label><input data-static-key="'+esc(path)+'" value="'+esc(v)+'"></div><div class="field"><input type="file" data-static-upload="'+esc(path)+'" accept="'+accept+'" /><p class="small">上传后会自动回填上方地址，也可以直接粘贴 URL。</p></div>'; }
    function listText(label, path, hint = '每行一条') { const value = (byPath(state.content, path) || []).join('\\n'); return '<div class="field"><label>'+esc(label)+'</label><textarea data-list-path="'+esc(path)+'" rows="5">'+esc(value)+'</textarea><p class="small">'+esc(hint)+'</p></div>'; }
    function pairList(label, path, a, b) { const items = byPath(state.content, path) || []; return '<div class="field"><label>'+esc(label)+'</label><div class="list" data-pair-list="'+esc(path)+'" data-a="'+esc(a)+'" data-b="'+esc(b)+'">'+items.map((item,i)=>'<div class="card"><div class="card-head"><span>条目 '+(i+1)+'</span><button class="danger" data-remove-pair="'+i+'">删除</button></div><input data-pair-index="'+i+'" data-pair-key="'+esc(a)+'" value="'+esc(item[a]||'')+'" placeholder="'+esc(a)+'" /><input data-pair-index="'+i+'" data-pair-key="'+esc(b)+'" value="'+esc(item[b]||'')+'" placeholder="'+esc(b)+'" /></div>').join('')+'</div><button class="ghost" data-add-pair="'+esc(path)+'" type="button">新增条目</button></div>'; }
    function objectList(label, path, schema) { const items = byPath(state.content, path) || []; return '<div class="field"><label>'+esc(label)+'</label><div class="list" data-object-list="'+esc(path)+'">'+items.map((item,i)=>'<div class="card"><div class="card-head"><span>条目 '+(i+1)+'</span><button class="danger" data-remove-object="'+i+'">删除</button></div>'+schema.map(([key,name,type]) => type==='textarea' ? '<textarea data-object-index="'+i+'" data-object-key="'+esc(key)+'" rows="3" placeholder="'+esc(name)+'">'+esc(item[key]||'')+'</textarea>' : '<input data-object-index="'+i+'" data-object-key="'+esc(key)+'" value="'+esc(item[key] ?? '')+'" placeholder="'+esc(name)+'" />').join('')+'</div>').join('')+'</div><button class="ghost" data-add-object="'+esc(path)+'" type="button">新增条目</button></div>'; }
    function staticObjectList(label, path, schema) {
      const page = selectedStaticPage();
      const items = byPath(page, path) || [];
      const inputFor = (item, i, key, name, type) => {
        const value = Array.isArray(item[key]) ? item[key].join('\\n') : (item[key] ?? '');
        if (type === 'textarea') return '<textarea data-static-object-index="'+i+'" data-static-object-key="'+esc(key)+'" rows="3" placeholder="'+esc(name)+'">'+esc(value)+'</textarea>';
        const media = type === 'image' || type === 'video' || /image|video|cover|poster|visual/i.test(key);
        return '<input data-static-object-index="'+i+'" data-static-object-key="'+esc(key)+'" value="'+esc(value)+'" placeholder="'+esc(name)+'" />'+(media ? '<input type="file" data-static-object-upload-index="'+i+'" data-static-object-upload-key="'+esc(key)+'" accept="'+(type === 'video' || /video/i.test(key) ? 'video/*' : 'image/*')+'"><p class="small">可上传文件自动回填，也可直接粘贴 URL。</p>' : '');
      };
      return '<div class="field"><label>'+esc(label)+'</label><div class="list" data-static-object-list="'+esc(path)+'">'+items.map((item,i)=>'<div class="card"><div class="card-head"><span>条目 '+(i+1)+'</span><button class="danger" data-remove-static-object="'+i+'">删除</button></div>'+schema.map(([key,name,type]) => inputFor(item, i, key, name, type)).join('')+'</div>').join('')+'</div><button class="ghost" data-add-static-object="'+esc(path)+'" type="button">新增条目</button><p class="small">要点、标签、参数等字段可用换行或逗号分隔，保存后前台会自动解析。</p></div>';
    }
    function saveButton() { return '<button class="save-current" id="saveCurrent">保存当前模块</button>'; }
    function title(text) { return '<h1>正在编辑：'+esc(text)+'</h1><div class="rule"></div><div class="form">'; }
    function configMediaPath(path) { return /(^|\\.)(image|images|video|videos|cover|poster|logo|gallery|media|asset|banner|background|thumbnail|url|src)(\\.|$)/i.test(path); }
    function configInput(value, path) {
      const label = path.split('.').at(-1) || '内容';
      const media = configMediaPath(path);
      if (typeof value === 'boolean') return '<label class="field"><input data-config-path="'+esc(path)+'" type="checkbox" '+(value?'checked':'')+' style="width:auto;margin-right:8px" />'+esc(label)+'</label>';
      if (typeof value === 'number') return '<div class="field"><label>'+esc(label)+'</label><input data-config-path="'+esc(path)+'" data-config-number type="number" value="'+esc(value)+'" /></div>';
      const longText = /(description|body|text|answer|message|summary|content|notes|prompt)$/i.test(path) || String(value || '').length > 100;
      const control = longText
        ? '<textarea data-config-path="'+esc(path)+'" rows="4">'+esc(value ?? '')+'</textarea>'
        : '<input data-config-path="'+esc(path)+'" value="'+esc(value ?? '')+'" />';
      const upload = media ? '<input type="file" data-config-upload="'+esc(path)+'" accept="image/*,video/*,.pdf,.zip,.dwg,.dxf" /><p class="small">上传后会自动回填当前字段，也可以直接输入媒体 URL。</p>' : '';
      return '<div class="field"><label>'+esc(label)+'</label>'+control+upload+'</div>';
    }
    function configNode(value, path, depth) {
      const label = path ? path.split('.').at(-1) : '官网内容';
      if (value === null || typeof value !== 'object') return configInput(value, path);
      if (Array.isArray(value)) {
        const rows = value.map((item, index) => '<div class="card"><div class="card-head"><span>'+esc(label)+' '+(index + 1)+'</span><button class="danger" type="button" data-config-remove-parent="'+esc(path)+'" data-config-remove-index="'+index+'">删除</button></div>'+configNode(item, path+'.'+index, depth + 1)+'</div>').join('');
        return '<details class="field" '+(depth < 2 ? 'open' : '')+'><summary><strong>'+esc(label)+'（'+value.length+' 项）</strong></summary><div class="list" style="margin-top:12px">'+(rows || '<p class="small">暂无条目，可新增。</p>')+'</div><button class="ghost" type="button" data-config-add="'+esc(path)+'">新增条目</button></details>';
      }
      const fields = Object.entries(value).map(([key, child]) => configNode(child, path ? path+'.'+key : key, depth + 1)).join('');
      return '<details class="field" '+(depth < 2 ? 'open' : '')+'><summary><strong>'+esc(label)+'</strong></summary><div class="form" style="margin-top:12px">'+fields+'</div></details>';
    }
    function renderAllContent() {
      editor.innerHTML = title('全部内容字段')
        + '<p class="small">此页面直接读取当前前端内容模型，完整展示每一项文字、图片、视频、产品、品类、SEO 与静态页面配置。快捷菜单用于日常编辑；这里用于核对和编辑所有字段，前端新增并写入内容种子的新字段也会自动出现。</p>'
        + configNode(state.content, '', 0)
        + '<details class="field"><summary><strong>高级：完整 JSON</strong></summary><textarea id="allContentJson" rows="24" spellcheck="false" style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace">'+esc(JSON.stringify(state.content, null, 2))+'</textarea><button class="ghost" type="button" data-config-apply-json>校验并应用 JSON</button><p class="small">适用于新增任意自定义字段、空数组对象或批量粘贴。应用后仍需点击保存。</p></details>'
        + saveButton() + '</div>';
    }
    function render() {
      nav.querySelectorAll('button').forEach((btn) => btn.classList.toggle('active', btn.dataset.nav === state.active));
      const c = state.content;
      if (state.active === 'allContent') renderAllContent();
      else if (state.active === 'homeHero') editor.innerHTML = title('首页首屏') + field('眉标', 'home.hero.eyebrow') + field('主标题', 'home.hero.title') + field('商标角标', 'home.hero.trademark') + text('副标题', 'home.hero.subtitle') + mediaField('背景图', 'home.hero.backgroundImage') + mediaField('背景视频', 'home.hero.backgroundVideo', 'video/*') + field('主按钮文字', 'home.hero.primaryCta') + field('主按钮链接', 'home.hero.primaryHref') + field('播放按钮文字', 'home.hero.secondaryCta') + field('播放视频链接', 'home.hero.videoUrl') + field('视频弹窗标题', 'home.hero.videoTitle') + field('视频占位提示', 'home.hero.videoPreviewLabel') + field('外部打开按钮文字', 'home.hero.videoOpenExternalLabel') + field('关闭按钮文字', 'home.hero.videoCloseLabel') + field('SEO 标题', 'home.seo.title') + text('SEO 描述', 'home.seo.description') + field('SEO 关键词', 'home.seo.keywords') + field('SEO 分享图', 'home.seo.image') + saveButton() + '</div>';
      else if (state.active === 'homeProducts') editor.innerHTML = title('首页产品区') + field('产品区标题', 'home.featuredProducts.title') + text('产品区描述', 'home.featuredProducts.description') + field('产品卡片链接文字', 'home.featuredProducts.linkLabel') + field('产品卡片链接图标', 'home.featuredProducts.linkIcon') + field('产品介绍标题', 'home.productIntro.title') + text('产品介绍描述', 'home.productIntro.description', 5) + mediaField('产品介绍图片', 'home.productIntro.image', 'image/*') + listText('产品介绍要点', 'home.productIntro.bullets') + field('产品介绍要点图标', 'home.productIntro.bulletIcon') + field('技术规格按钮文字', 'home.productIntro.ctaLabel') + field('技术规格按钮链接', 'home.productIntro.ctaHref') + saveButton() + '</div>';
      else if (state.active === 'homeAdvantages') editor.innerHTML = title('首页优势') + field('优势区标题', 'home.advantagesIntro.title') + text('优势区描述', 'home.advantagesIntro.description') + objectList('优势卡片', 'home.advantages', [['title','标题'],['description','描述','textarea'],['icon','图标代号']]) + saveButton() + '</div>';
      else if (state.active === 'homeTestimonials') editor.innerHTML = title('客户评价') + field('区域标题', 'home.testimonialsTitle') + objectList('评价列表', 'home.testimonials', [['name','姓名'],['role','身份'],['text','评价内容','textarea']]) + saveButton() + '</div>';
      else if (state.active === 'homeCta') editor.innerHTML = title('首页行动区') + field('标题', 'home.cta.title') + text('描述', 'home.cta.description') + field('主按钮', 'home.cta.primaryLabel') + field('主按钮链接', 'home.cta.primaryHref') + field('次按钮', 'home.cta.secondaryLabel') + field('次按钮链接', 'home.cta.secondaryHref') + mediaField('背景图', 'home.cta.backgroundImage') + saveButton() + '</div>';
      else if (state.active === 'productCenter') editor.innerHTML = title('产品中心') + field('首屏标题', 'productCenter.hero.title') + text('首屏描述', 'productCenter.hero.description') + mediaField('首屏背景图', 'productCenter.hero.backgroundImage') + mediaField('首屏背景视频', 'productCenter.hero.backgroundVideo', 'video/*') + field('选择区标题', 'productCenter.intro.title') + text('选择区描述', 'productCenter.intro.description') + field('品类按钮文字', 'productCenter.intro.categoryCtaLabel') + field('品类按钮图标', 'productCenter.categoryPage.categoryCtaIcon') + field('品类页标题模板', 'productCenter.categoryPage.titleTemplate') + field('返回按钮文字', 'productCenter.categoryPage.backLabel') + field('返回按钮图标', 'productCenter.categoryPage.backIcon') + field('产品悬浮按钮文字', 'productCenter.categoryPage.productHoverLabel') + field('面包屑分隔符', 'productCenter.detailPage.breadcrumbSeparator') + field('详情页徽标', 'productCenter.detailPage.badgeLabel') + field('详情页徽标图标', 'productCenter.detailPage.badgeIcon') + field('卖点列表图标', 'productCenter.detailPage.featureIcon') + field('报价按钮文字', 'productCenter.detailPage.quoteButtonLabel') + field('报价按钮图标', 'productCenter.detailPage.quoteButtonIcon') + field('下载按钮图标', 'productCenter.detailPage.downloadButtonIcon') + field('规格区标题', 'productCenter.detailPage.specsTitle') + text('规格区描述', 'productCenter.detailPage.specsDescription') + field('规格左侧图标', 'productCenter.detailPage.specsPrimaryIcon') + field('规格左侧标题', 'productCenter.detailPage.specsPrimaryTitle') + field('说明右侧图标', 'productCenter.detailPage.descriptionIcon') + field('说明右侧标题', 'productCenter.detailPage.descriptionTitle') + field('图库标题', 'productCenter.detailPage.galleryTitle') + field('视频标题', 'productCenter.detailPage.videoTitle') + field('SEO 标题', 'productCenter.seo.title') + text('SEO 描述', 'productCenter.seo.description') + field('SEO 关键词', 'productCenter.seo.keywords') + field('SEO 分享图', 'productCenter.seo.image') + saveButton() + '</div>';
      else if (state.active === 'categories') renderCollection('产品品类', c.categories, 'category', ['name','slug','description','coverImage','sortOrder']);
      else if (state.active === 'products') renderProduct();
      else if (state.active === 'support') editor.innerHTML = title('服务支持') + field('首屏标题', 'support.hero.title') + text('首屏描述', 'support.hero.description') + mediaField('首屏背景图', 'support.hero.backgroundImage') + mediaField('首屏背景视频', 'support.hero.backgroundVideo', 'video/*') + objectList('服务卡片', 'support.services', [['title','标题'],['description','描述','textarea'],['icon','图标'],['href','链接'],['cta','按钮文字']]) + field('联系标题', 'support.contact.title') + text('联系说明', 'support.contact.description') + field('电话标签', 'support.contact.phoneLabel') + field('电话图标', 'support.contact.phoneIcon') + field('邮箱标签', 'support.contact.emailLabel') + field('邮箱图标', 'support.contact.emailIcon') + field('姓名标签', 'support.contact.form.nameLabel') + field('姓名占位', 'support.contact.form.namePlaceholder') + field('公司标签', 'support.contact.form.companyLabel') + field('公司占位', 'support.contact.form.companyPlaceholder') + field('电话标签', 'support.contact.form.phoneLabel') + field('电话占位', 'support.contact.form.phonePlaceholder') + field('邮箱标签', 'support.contact.form.emailLabel') + field('邮箱占位', 'support.contact.form.emailPlaceholder') + field('留言标签', 'support.contact.form.messageLabel') + field('留言占位', 'support.contact.form.messagePlaceholder') + field('选填提示', 'support.contact.form.optionalLabel') + field('提交按钮', 'support.contact.form.submitLabel') + field('提交中按钮', 'support.contact.form.submittingLabel') + field('提交成功提示', 'support.contact.form.successMessage') + field('提交失败提示', 'support.contact.form.errorMessage') + field('接口未配置提示', 'support.contact.form.missingEndpointMessage') + field('SEO 标题', 'support.seo.title') + text('SEO 描述', 'support.seo.description') + field('SEO 关键词', 'support.seo.keywords') + field('SEO 分享图', 'support.seo.image') + saveButton() + '</div>';
      else if (state.active === 'faq') renderCollection('常见问题', c.faqs, 'faq', ['question','answer','sortOrder','enabled']);
      else if (['about','solutions','warranty','install','privacy','agreement','faqPage'].includes(state.active)) renderStatic(state.active === 'faqPage' ? 'faq' : state.active);
      else if (state.active === 'media') renderMedia();
      else if (state.active === 'leads') renderLeads();
      else if (state.active === 'seo') editor.innerHTML = title('SEO 设置') + '<div class="card seo-ai-panel"><div class="card-head"><span>AI SEO 生成</span></div><p class="small">根据当前已保存的网站内容，为全站、首页、产品中心、服务页、全部品类、产品和静态页生成中文 SEO，并自动排队生成英文与阿拉伯语版本。现有分享图会保留，空白分享图自动使用页面主图或 ASTREVA Logo。</p><button class="seo-ai-button" id="generateSeo" type="button">一键 AI 生成三语 SEO</button><p class="seo-ai-status" id="seoAiStatus" aria-live="polite">'+esc(state.seoMessage || '')+'</p></div><div class="card"><div class="card-head"><span>搜索引擎站长验证</span></div><p class="small">在各平台站长后台添加网站后，把平台给出的 meta 验证码填入。留空时前台不会输出对应标签。</p>'+field('Google 验证码', 'site.searchVerification.google')+field('Bing 验证码', 'site.searchVerification.bing')+field('百度验证码', 'site.searchVerification.baidu')+field('360 搜索验证码', 'site.searchVerification.so360')+field('搜狗验证码', 'site.searchVerification.sogou')+field('Yandex 验证码', 'site.searchVerification.yandex')+'</div>' + field('默认 SEO 标题', 'site.defaultSeo.title') + text('默认 SEO 描述', 'site.defaultSeo.description') + field('默认分享图', 'site.defaultSeo.image') + field('默认关键词', 'site.defaultSeo.keywords') + field('首页 SEO 标题', 'home.seo.title') + text('首页 SEO 描述', 'home.seo.description') + field('首页 SEO 关键词', 'home.seo.keywords') + field('首页 SEO 分享图', 'home.seo.image') + field('产品中心 SEO 标题', 'productCenter.seo.title') + text('产品中心 SEO 描述', 'productCenter.seo.description') + field('产品中心 SEO 关键词', 'productCenter.seo.keywords') + field('产品中心 SEO 分享图', 'productCenter.seo.image') + field('服务页 SEO 标题', 'support.seo.title') + text('服务页 SEO 描述', 'support.seo.description') + field('服务页 SEO 关键词', 'support.seo.keywords') + field('服务页 SEO 分享图', 'support.seo.image') + saveButton() + '</div>';
      else if (state.active === 'site') editor.innerHTML = title('全站设置') + field('品牌名', 'site.brandName') + field('品牌标语', 'site.brandTagline') + mediaField('Logo', 'site.logoUrl') + field('电话', 'site.phone') + field('邮箱', 'site.email') + field('版权文案', 'site.copyright') + field('ServiceBridge 地址', 'site.serviceBridgeUrl') + field('语言选择标签', 'site.localization.languageLabel') + field('语言切换无障碍标签', 'site.localization.switchAriaLabel') + field('中文名称', 'site.localization.chineseLabel') + field('英语名称', 'site.localization.englishLabel') + field('阿拉伯语名称', 'site.localization.arabicLabel') + field('切换中提示', 'site.localization.switchingText') + field('译文未就绪提示', 'site.localization.unavailableText') + saveButton() + '</div>';
      else if (state.active === 'fallbackVersions') renderFallbackVersions();
      else if (state.active === 'navigation') editor.innerHTML = title('导航文案') + field('首页', 'site.navigation.homeLabel') + field('产品中心', 'site.navigation.productsLabel') + field('解决方案', 'site.navigation.solutionsLabel') + field('服务支持', 'site.navigation.supportLabel') + field('关于我们', 'site.navigation.aboutLabel') + field('移动端菜单无障碍标签', 'site.navigation.mobileMenuLabel') + saveButton() + '</div>';
      else if (state.active === 'footer') editor.innerHTML = title('页脚链接') + text('页脚描述', 'site.footer.description') + field('快速链接标题', 'site.footer.quickLinksTitle') + pairList('快速链接', 'site.footer.quickLinks', 'label', 'href') + field('服务链接标题', 'site.footer.supportTitle') + pairList('服务链接', 'site.footer.supportLinks', 'label', 'href') + field('关注标题', 'site.footer.socialTitle') + field('无社交链接提示', 'site.footer.socialEmptyText') + field('隐私政策文字', 'site.footer.privacyLabel') + field('用户协议文字', 'site.footer.agreementLabel') + saveButton() + '</div>';
      else if (state.active === 'social') editor.innerHTML = title('社交方式') + field('WhatsApp 链接', 'site.socialLinks.whatsapp') + field('WhatsApp 图标文字', 'site.socialIcons.whatsapp') + field('Telegram 链接', 'site.socialLinks.telegram') + field('Telegram 图标文字', 'site.socialIcons.telegram') + field('Facebook 链接', 'site.socialLinks.facebook') + field('Facebook 图标文字', 'site.socialIcons.facebook') + field('Instagram 链接', 'site.socialLinks.instagram') + field('Instagram 图标文字', 'site.socialIcons.instagram') + field('Twitter / X 链接', 'site.socialLinks.twitter') + field('Twitter / X 图标文字', 'site.socialIcons.twitter') + field('YouTube 链接', 'site.socialLinks.youtube') + field('YouTube 图标文字', 'site.socialIcons.youtube') + field('TikTok 链接', 'site.socialLinks.tiktok') + field('TikTok 图标文字', 'site.socialIcons.tiktok') + saveButton() + '</div>';
      else if (state.active === 'cookie') editor.innerHTML = title('Cookie 提示') + field('标题', 'site.cookie.title') + text('正文', 'site.cookie.text') + field('隐私政策文字', 'site.cookie.privacyLabel') + field('后缀文字', 'site.cookie.suffixText') + field('接受全部按钮', 'site.cookie.acceptAllLabel') + field('仅必要按钮', 'site.cookie.necessaryLabel') + saveButton() + '</div>';
      else if (state.active === 'chat') editor.innerHTML = title('在线客服') + field('客服标题', 'site.chat.title') + field('无障碍标签', 'site.chat.ariaLabel') + field('连接中提示', 'site.chat.loadingText') + field('加载失败标题', 'site.chat.errorTitle') + text('加载失败说明', 'site.chat.errorDescription') + field('初始未读数字', 'site.chat.initialUnreadCount', 'number') + field('ServiceBridge 用户端地址', 'site.serviceBridgeUrl') + saveButton() + '</div>';
      syncBusy();
    }
    function renderCollection(label, items, section, keys) {
      const indexKey = section === 'category' ? 'category' : 'faq';
      state.selected[indexKey] = Math.min(state.selected[indexKey] || 0, Math.max(items.length - 1, 0));
      const item = items[state.selected[indexKey]] || {};
      const buttons = items.map((it, i) => '<button class="'+(i===state.selected[indexKey]?'active':'')+'" data-select="'+section+'" data-index="'+i+'">'+esc(it.name || it.question || ('条目 '+(i+1)))+'</button>').join('');
      let form = title(label) + '<div class="picker"><div class="select-list">'+buttons+'<button class="ghost" data-new="'+section+'">新增</button></div><div class="form">';
      if (section === 'category') form += '<input data-item-section="category" data-key="id" value="'+esc(item.id||'')+'" type="hidden" /><div class="field"><label>名称</label><input data-item-section="category" data-key="name" value="'+esc(item.name||'')+'"></div><div class="field"><label>Slug</label><input data-item-section="category" data-key="slug" value="'+esc(item.slug||'')+'"></div><div class="field"><label>简介</label><textarea data-item-section="category" data-key="description">'+esc(item.description||'')+'</textarea></div><div class="field"><label>封面图</label><input data-item-section="category" data-key="coverImage" value="'+esc(item.coverImage||'')+'"></div><div class="field"><input type="file" data-item-upload="coverImage" accept="image/*"></div><div class="field"><label>首屏背景视频</label><input data-item-section="category" data-key="heroVideo" value="'+esc(item.heroVideo||'')+'"></div><div class="field"><input type="file" data-item-upload="heroVideo" accept="video/*"></div><div class="field"><label>排序</label><input data-item-section="category" data-key="sortOrder" type="number" value="'+esc(item.sortOrder||0)+'"></div><div class="field"><label>SEO 标题</label><input data-item-section="category" data-key="seo.title" value="'+esc(item.seo?.title||'')+'"></div><div class="field"><label>SEO 描述</label><textarea data-item-section="category" data-key="seo.description">'+esc(item.seo?.description||'')+'</textarea></div><div class="field"><label>SEO 关键词</label><input data-item-section="category" data-key="seo.keywords" value="'+esc(item.seo?.keywords||'')+'"></div><div class="field"><label>SEO 分享图</label><input data-item-section="category" data-key="seo.image" value="'+esc(item.seo?.image||'')+'"></div>';
      else form += '<input data-item-section="faq" data-key="id" value="'+esc(item.id||'')+'" type="hidden" /><div class="field"><label>问题</label><input data-item-section="faq" data-key="question" value="'+esc(item.question||'')+'"></div><div class="field"><label>答案</label><textarea data-item-section="faq" data-key="answer">'+esc(item.answer||'')+'</textarea></div><div class="field"><label>排序</label><input data-item-section="faq" data-key="sortOrder" type="number" value="'+esc(item.sortOrder||0)+'"></div><label class="field"><input data-item-section="faq" data-key="enabled" type="checkbox" '+(item.enabled!==false?'checked':'')+' style="width:auto;margin-right:8px">启用</label>';
      editor.innerHTML = form + '<button class="save-current" id="saveCurrent">保存当前条目</button><button class="danger" data-delete="'+section+'">删除当前条目</button></div></div></div>';
    }
    function renderProduct() {
      const items = state.content.products; state.selected.product = Math.min(state.selected.product || 0, Math.max(items.length - 1, 0)); const item = items[state.selected.product] || {};
      const buttons = items.map((it, i) => '<button class="'+(i===state.selected.product?'active':'')+'" data-select="product" data-index="'+i+'">'+esc(it.name||('产品 '+(i+1)))+'</button>').join('');
      const categoryOptions = state.content.categories.map((cat)=>'<option value="'+esc(cat.slug)+'" '+(cat.slug===item.categorySlug?'selected':'')+'>'+esc(cat.name)+'</option>').join('');
      editor.innerHTML = title('产品管理') + '<div class="picker"><div class="select-list">'+buttons+'<button class="ghost" data-new="product">新增</button></div><div class="form"><input data-item-section="product" data-key="id" value="'+esc(item.id||'')+'" type="hidden" /><div class="field"><label>产品名称</label><input data-item-section="product" data-key="name" value="'+esc(item.name||'')+'"></div><div class="field"><label>Slug</label><input data-item-section="product" data-key="slug" value="'+esc(item.slug||'')+'"></div><div class="field"><label>所属品类</label><select data-item-section="product" data-key="categorySlug">'+categoryOptions+'</select></div><div class="field"><label>封面图</label><input data-item-section="product" data-key="coverImage" value="'+esc(item.coverImage||'')+'"></div><div class="field"><input type="file" data-item-upload="coverImage" accept="image/*"></div><div class="field"><label>图库图片</label><textarea data-item-section="product" data-key="galleryImages" rows="4">'+esc((item.galleryImages||[]).join('\\n'))+'</textarea><p class="small">每行一个图片 URL，也可以上传多张图片自动追加。</p><input type="file" data-item-gallery-upload accept="image/*" multiple></div><div class="field"><label>产品视频</label><input data-item-section="product" data-key="videoUrl" value="'+esc(item.videoUrl||'')+'"><p class="small">可填 YouTube/Bilibili iframe 地址，也可上传 mp4/webm。</p><input type="file" data-item-upload="videoUrl" accept="video/*"></div><div class="field"><label>简介</label><textarea data-item-section="product" data-key="summary">'+esc(item.summary||'')+'</textarea></div><div class="field"><label>卖点</label><textarea data-item-section="product" data-key="features">'+esc((item.features||[]).join('\\n'))+'</textarea><p class="small">每行一个卖点</p></div><div class="field"><label>规格参数</label><textarea data-item-section="product" data-key="specifications">'+esc((item.specifications||[]).map(s=>s.label+'：'+s.value).join('\\n'))+'</textarea><p class="small">每行一条，格式：参数名：参数值</p></div><div class="field"><label>下载资料</label><textarea data-item-section="product" data-key="downloads">'+esc((item.downloads||[]).map(s=>s.label+'：'+s.url).join('\\n'))+'</textarea><p class="small">每行一条，格式：名称：URL；上传文件会自动追加。</p><input type="file" data-item-download-upload accept=".pdf,.zip,.dwg,.dxf"></div><div class="field"><label>正文</label><textarea data-item-section="product" data-key="body" rows="5">'+esc(item.body||'')+'</textarea></div><label class="field"><input data-item-section="product" data-key="isFeatured" type="checkbox" '+(item.isFeatured?'checked':'')+' style="width:auto;margin-right:8px">首页推荐</label><div class="field"><label>排序</label><input data-item-section="product" data-key="sortOrder" type="number" value="'+esc(item.sortOrder||0)+'"></div><div class="field"><label>SEO 标题</label><input data-item-section="product" data-key="seo.title" value="'+esc(item.seo?.title||'')+'"></div><div class="field"><label>SEO 描述</label><textarea data-item-section="product" data-key="seo.description">'+esc(item.seo?.description||'')+'</textarea></div><div class="field"><label>SEO 关键词</label><input data-item-section="product" data-key="seo.keywords" value="'+esc(item.seo?.keywords||'')+'"></div><div class="field"><label>SEO 分享图</label><input data-item-section="product" data-key="seo.image" value="'+esc(item.seo?.image||'')+'"></div><button class="save-current" id="saveCurrent">保存当前产品</button><button class="danger" data-delete="product">删除当前产品</button></div></div></div>';
    }
    function renderStatic(slug) {
      state.selected.staticPage = slug; let page = state.content.staticPages.find(p => p.slug === slug); if (!page) { page = { slug, title: slug, hero:{title:slug,description:''}, body:'', sections:{}, seo:{title:slug,description:''} }; state.content.staticPages.push(page); }
      const aboutFields = slug === 'about' ? '<div class="field"><label>正文图标</label><input data-static-key="sections.introIcon" value="'+esc(page.sections?.introIcon||'')+'"></div><div class="field"><label>合作伙伴标题</label><input data-static-key="sections.partnersTitle" value="'+esc(page.sections?.partnersTitle||'')+'"></div>' : '';
      const solutionFields = slug === 'solutions'
        ? '<div class="field"><label>顶部列表标题</label><input data-static-key="sections.introTitle" value="'+esc(page.sections?.introTitle||'')+'"></div><div class="field"><label>顶部列表描述</label><textarea data-static-key="sections.introDescription">'+esc(page.sections?.introDescription||'')+'</textarea></div><div class="grid2"><div class="field"><label>二级导航标题</label><input data-static-key="sections.sideNavTitle" value="'+esc(page.sections?.sideNavTitle||'方案导航')+'"></div><div class="field"><label>商业架构标题</label><input data-static-key="sections.architectureTitle" value="'+esc(page.sections?.architectureTitle||'隐形缝隙式系统架构')+'"></div></div><div class="grid2"><div class="field"><label>详情按钮文字</label><input data-static-key="sections.ctaLabel" value="'+esc(page.sections?.ctaLabel||'查看详细技术手册')+'"></div><div class="field"><label>详情按钮链接</label><input data-static-key="sections.ctaHref" value="'+esc(page.sections?.ctaHref||'/support/')+'"></div></div><div class="grid2"><div class="field"><label>侧栏咨询标题</label><input data-static-key="sections.adviceTitle" value="'+esc(page.sections?.adviceTitle||'需要专业选型建议？')+'"></div><div class="field"><label>技术提示文字</label><input data-static-key="sections.detailNote" value="'+esc(page.sections?.detailNote||'后台可继续补充该节点的技术说明。')+'"></div></div><div class="grid2"><div class="field"><label>侧栏主按钮</label><input data-static-key="sections.advicePrimaryLabel" value="'+esc(page.sections?.advicePrimaryLabel||'咨询方案专家')+'"></div><div class="field"><label>侧栏主按钮链接</label><input data-static-key="sections.advicePrimaryHref" value="'+esc(page.sections?.advicePrimaryHref||'/support/')+'"></div></div><div class="grid2"><div class="field"><label>侧栏次按钮</label><input data-static-key="sections.adviceSecondaryLabel" value="'+esc(page.sections?.adviceSecondaryLabel||'下载全线目录')+'"></div><div class="field"><label>侧栏次按钮链接</label><input data-static-key="sections.adviceSecondaryHref" value="'+esc(page.sections?.adviceSecondaryHref||'/products/')+'"></div></div>'+staticObjectList('方案长页面分区', 'sections.solutionSections', [['id','锚点 ID，例如 sol-sec-1'],['number','编号'],['navLabel','导航标签'],['title','标题'],['description','场景说明','textarea'],['image','图片 URL','image'],['videoImage','视频封面 URL','image'],['videoUrl','视频 URL','video'],['videoCaption','视频说明'],['focusTitle','重点标题'],['focusDescription','重点说明','textarea'],['ctaLabel','当前分区按钮文字'],['ctaHref','当前分区按钮链接'],['bullets','要点，每行一条','textarea']])+staticObjectList('商业系统架构卡片', 'sections.systemCards', [['step','步骤'],['title','说明','textarea']])
          : '';
      const iconFields = ['warranty','install'].includes(slug) ? '<div class="field"><label>页面图标</label><input data-static-key="sections.icon" value="'+esc(page.sections?.icon||'')+'"></div>' : '';
      editor.innerHTML = title(page.title || slug) + '<input data-static-key="slug" type="hidden" value="'+esc(page.slug)+'"><div class="field"><label>页面标题</label><input data-static-key="title" value="'+esc(page.title||'')+'"></div><div class="field"><label>首屏眉标</label><input data-static-key="hero.eyebrow" value="'+esc(page.hero?.eyebrow||'')+'"></div><div class="field"><label>首屏标题</label><input data-static-key="hero.title" value="'+esc(page.hero?.title||'')+'"></div><div class="field"><label>首屏描述</label><textarea data-static-key="hero.description">'+esc(page.hero?.description||'')+'</textarea></div>'+staticMediaField('首屏背景图', 'hero.backgroundImage', 'image/*')+staticMediaField('首屏背景视频', 'hero.backgroundVideo', 'video/*')+aboutFields+solutionFields+iconFields+'<div class="field"><label>正文内容</label><textarea data-static-key="body" rows="10">'+esc(page.body||'')+'</textarea><p class="small">可输入正文文本，换行会保留；需要加粗或小标题时也可以使用少量 HTML。</p></div><div class="field"><label>SEO 标题</label><input data-static-key="seo.title" value="'+esc(page.seo?.title||'')+'"></div><div class="field"><label>SEO 描述</label><textarea data-static-key="seo.description">'+esc(page.seo?.description||'')+'</textarea></div><div class="field"><label>SEO 关键词</label><input data-static-key="seo.keywords" value="'+esc(page.seo?.keywords||'')+'"></div><div class="field"><label>SEO 分享图</label><input data-static-key="seo.image" value="'+esc(page.seo?.image||'')+'"></div>'+saveButton()+'</div>';
    }
    function renderMedia() { editor.innerHTML = title('媒体库') + '<div class="field"><label>上传媒体</label><input type="file" data-upload-media accept="image/*,video/*,.pdf,.zip,.dwg,.dxf"><p class="small">删除前会检查官网引用，仍被使用的素材不允许删除。</p></div><div class="media-grid">'+(state.data.media||[]).map(m=>'<div class="media-item">'+(m.type==='video'?'<video src="'+esc(m.url)+'" controls></video>':m.type==='image'?'<img src="'+esc(m.url)+'" alt="">':'<div style="height:130px;display:grid;place-items:center;background:#eef3f8">文件</div>')+'<div>'+esc(m.name)+'<br><div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:8px"><button class="ghost" data-copy="'+esc(m.url)+'">复制地址</button><button class="ghost" data-media-references="'+esc(m.id)+'">查看引用</button><button class="danger" data-delete-media="'+esc(m.id)+'">删除</button></div></div></div>').join('')+'</div></div>'; }
    function renderFallbackVersions() {
      const rows = state.fallbackVersions.map((version) => {
        const active = version.id === state.fallbackActiveId;
        const createdAt = version.createdAt ? new Date(version.createdAt).toLocaleString('zh-CN', { hour12: false }) : '未知时间';
        return '<div class="card"><div class="card-head"><span>'+esc(version.name)+(active ? ' <strong style="color:#0875eb">（当前启用）</strong>' : '')+'</span><span class="small">'+esc(createdAt)+'</span></div><p class="small">ID：'+esc(version.id)+'</p><div style="display:flex;gap:8px;flex-wrap:wrap">'+(active ? '' : '<button class="ghost" data-activate-fallback="'+esc(version.id)+'">启用并发布</button>')+'<button class="danger" data-delete-fallback="'+esc(version.id)+'">删除版本</button></div></div>';
      }).join('') || '<p class="small">暂无降级版本。</p>';
      editor.innerHTML = title('官网降级版本') + '<p class="small">当后台内容接口不可用时，官网构建会使用当前启用的快照。版本从已保存的官网内容创建；启用或删除当前版本会自动重新发布官网。</p><div class="field"><label>新版本名称</label><input id="fallbackVersionName" placeholder="例如：2026-08-22 发布前备份"><button class="save-current" type="button" data-create-fallback>从当前官网内容创建版本</button></div><div class="rule"></div>'+rows+'</div>';
    }
    function syncFallbackVersions(payload) {
      state.fallbackVersions = payload.versions || [];
      state.fallbackActiveId = payload.activeId || '';
      state.data.fallbackVersions = state.fallbackVersions;
      state.data.fallbackActiveId = state.fallbackActiveId;
    }
    async function fallbackRequest(url, options = {}) {
      const response = await fetch(url, options);
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error?.message || '降级版本操作失败');
      return payload.data;
    }
    async function createFallbackVersion() {
      if (state.busy) return;
      state.busy = true; clearError(); syncBusy();
      try {
        const name = document.querySelector('#fallbackVersionName')?.value || '';
        syncFallbackVersions(await fallbackRequest('/site-admin-api/fallback-versions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) }));
        render();
      } finally { state.busy = false; syncBusy(); }
    }
    async function activateFallbackVersion(id) {
      if (state.busy) return;
      state.busy = true; clearError(); syncBusy();
      try {
        syncFallbackVersions(await fallbackRequest('/site-admin-api/fallback-versions/'+encodeURIComponent(id)+'/activate', { method: 'POST' }));
        render();
        await publishSite('fallback');
      } finally { state.busy = false; syncBusy(); }
    }
    async function deleteFallbackVersion(id) {
      const version = state.fallbackVersions.find((item) => item.id === id);
      if (!window.confirm('确认删除降级版本“' + (version?.name || '') + '”？此操作不可恢复。')) return;
      if (state.busy) return;
      state.busy = true; clearError(); syncBusy();
      try {
        const result = await fallbackRequest('/site-admin-api/fallback-versions/'+encodeURIComponent(id), { method: 'DELETE' });
        syncFallbackVersions(result);
        render();
        if (result.activeChanged) await publishSite('fallback');
      } finally { state.busy = false; syncBusy(); }
    }
    function renderLeads() { const leads = state.data.leads||[]; const rows = leads.map((lead,i)=>'<div class="card"><div class="card-head"><span>'+esc(lead.name||'未命名')+' · '+esc(lead.contactInfo||lead.phone||lead.email||'')+'</span><button class="ghost" data-select-lead="'+i+'">编辑</button></div><p class="small">'+esc(lead.email||lead.phone||'')+'<br>'+esc(lead.message||'')+'</p></div>').join(''); const lead = leads[state.selected.lead] || {}; editor.innerHTML = title('客户咨询') + rows + '<div class="rule"></div><input data-lead-key="id" type="hidden" value="'+esc(lead.id||'')+'"><div class="grid2"><div class="field"><label>状态</label><input data-lead-key="status" value="'+esc(lead.status||'新咨询')+'"></div><div class="field"><label>优先级</label><input data-lead-key="priority" value="'+esc(lead.priority||'普通')+'"></div></div><div class="field"><label>内部备注</label><textarea data-lead-key="internalNotes">'+esc(lead.internalNotes||'')+'</textarea></div>'+saveButton()+'</div>'; }
    function sectionForActive() { if (['allContent','seo'].includes(state.active)) return ['content', state.content]; if (state.active.startsWith('home')) return ['home', state.content.home]; if (state.active === 'productCenter') return ['productCenter', state.content.productCenter]; if (state.active === 'support') return ['support', state.content.support]; if (['site','navigation','footer','social','cookie','chat'].includes(state.active)) return ['site', state.content.site]; if (['about','solutions','warranty','install','privacy','agreement','faqPage'].includes(state.active)) return ['staticPage', state.content.staticPages.find(p => p.slug === state.selected.staticPage)]; return [state.active, null]; }
    function collectItem(section) { const data = {}; editor.querySelectorAll('[data-item-section="'+section+'"]').forEach(el => { let value = el.type === 'checkbox' ? el.checked : el.value; if (['sortOrder'].includes(el.dataset.key)) value = Number(value||0); if (['galleryImages','features'].includes(el.dataset.key)) value = value.split('\\n').map(s=>s.trim()).filter(Boolean); if (el.dataset.key === 'specifications') value = value.split('\\n').map(s=>s.trim()).filter(Boolean).map(s=>{const [label,...rest]=s.split(/[：:]/); return {label:label||'', value:rest.join('：')||''};}); if (el.dataset.key === 'downloads') value = value.split('\\n').map(s=>s.trim()).filter(Boolean).map(s=>{const [label,...rest]=s.split(/[：:]/); return {label:label||'', url:rest.join(':')||''};}); setNested(data, el.dataset.key, value); }); return data; }
    function setNested(obj, path, value) { const keys = path.split('.'); let cur = obj; keys.slice(0,-1).forEach(k=>{cur[k] ||= {}; cur = cur[k];}); cur[keys.at(-1)] = value; }
    async function uploadAsset(file) { const fd = new FormData(); fd.append('file', file); const res = await fetch('/site-admin-api/upload', { method:'POST', body: fd }); const data = await res.json(); if (!res.ok) throw new Error(data.error?.message || '上传失败'); state.data.media = (await (await fetch('/site-admin-api/media')).json()).data; return data.data; }
    async function upload(file) { return (await uploadAsset(file)).url; }
    async function saveCurrent(options = {}) {
      if (state.busy) return false;
      if (state.active === 'fallbackVersions') throw new Error('降级版本通过本页面的创建、启用和删除按钮管理，无需保存内容模块。');
      if (state.active === 'media') throw new Error('媒体文件上传成功后会立即保存，无需再保存整个模块。');
      state.busy = true;
      clearError();
      syncBusy();
      const shouldPublish = options.publish !== false;
      try {
        let section, data;
        if (state.active === 'categories') { section='category'; data=collectItem('category'); }
        else if (state.active === 'products') { section='product'; data=collectItem('product'); }
        else if (state.active === 'faq') { section='faq'; data=collectItem('faq'); }
        else if (state.active === 'leads') { section='lead'; data={}; editor.querySelectorAll('[data-lead-key]').forEach(el=>setNested(data, el.dataset.leadKey, el.value)); }
        else { [section, data] = sectionForActive(); }
        saved.textContent='保存中...';
        const res = await fetch('/site-admin-api/save', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ section, data, revision: state.data.contentRevision }) });
        const payload = await res.json(); if (!res.ok) throw new Error(payload.error?.message || '保存失败');
        state.data = payload.data; state.content = payload.data.content; state.dirty = false; saved.textContent='✓ 已保存'; dirty.textContent=shouldPublish && section !== 'lead' ? '● 正在同步' : '● 草稿已保存'; render();
        if (shouldPublish && section !== 'lead') await publishSite();
        else refreshPreview();
        return true;
      } finally {
        state.busy = false;
        syncBusy();
      }
    }
    async function generateSeo() {
      if (state.busy) return;
      if (state.dirty) throw new Error('当前页面还有未保存修改，请先保存后再生成 SEO。');
      if (!window.confirm('AI 将覆盖全站所有页面现有的 SEO 标题、描述和关键词。现有分享图会保留。确认继续？')) return;
      state.busy = true;
      state.seoMessage = '';
      clearError();
      syncBusy();
      const button = document.querySelector('#generateSeo');
      const status = document.querySelector('#seoAiStatus');
      if (button) button.textContent = 'AI 正在分析全站内容...';
      if (status) status.textContent = '正在生成，请勿关闭页面。';
      try {
        const response = await fetch('/site-admin-api/seo/ai-generate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ revision: state.data.contentRevision })
        });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error?.message || 'AI SEO 生成失败。');
        state.data = payload.data;
        state.content = payload.data.content;
        state.dirty = false;
        state.seoMessage = '已生成并保存 '+Number(payload.meta?.generatedCount || 0)+' 个页面的中文 SEO；英文和阿拉伯语已进入自动翻译队列。';
        saved.textContent = '✓ 已保存';
        dirty.textContent = '● SEO 草稿已更新';
        render();
        refreshPreview();
      } finally {
        state.busy = false;
        syncBusy();
      }
    }
    function buildPreviewUrl(path) {
      const raw = (path || '/').startsWith('/') ? (path || '/') : '/' + path;
      const hashIndex = raw.indexOf('#');
      const beforeHash = hashIndex === -1 ? raw : raw.slice(0, hashIndex);
      const hash = hashIndex === -1 ? '' : raw.slice(hashIndex);
      const separator = beforeHash.includes('?') ? '&' : '?';
      return window.__SITE_ORIGIN__ + beforeHash + separator + 'adminPreview=' + Date.now() + hash;
    }
    function selectedPreview(section) {
      if (section === 'category') {
        const category = state.content.categories[state.selected.category];
        return category?.slug ? '/products/' + category.slug + '/#category-hero' : '/products/#product-categories';
      }
      if (section === 'product') {
        const product = state.content.products[state.selected.product];
        return product?.categorySlug && product?.slug ? '/products/' + product.categorySlug + '/' + product.slug + '/#product-detail-hero' : '/products/#product-categories';
      }
      if (section === 'faq') return '/support/faq/#faq-list';
      return '';
    }
    function previewForNav(navId, fallback) {
      if (navId === 'categories') return selectedPreview('category') || fallback || '/products/#product-categories';
      if (navId === 'products') return selectedPreview('product') || fallback || '/products/#product-categories';
      if (navId === 'faq') return '/support/faq/#faq-list';
      return fallback || '/';
    }
    function refreshPreview(path) { if (path) state.previewPath = path; preview.src = buildPreviewUrl(state.previewPath || '/'); }
    async function refreshStatus() { try { const res = await fetch('/site-admin-api/status'); const p = await res.json(); const r = p.data.rebuild; build.textContent = r.running ? '构建中' : (r.queued ? '等待构建' : (r.lastBuild?.status === 'succeeded' ? '已同步前台' : (r.message || '可发布'))); build.classList.toggle('error', !r.ok || r.lastBuild?.status === 'failed'); return r; } catch (error) { build.textContent = '重建服务未启动'; build.classList.add('error'); return { ok: false, running: false, queued: false, message: error.message }; } }
    function wait(ms) { return new Promise((resolve) => setTimeout(resolve, ms)); }
    async function waitForBuild() {
      for (let i = 0; i < 90; i += 1) {
        const status = await refreshStatus();
        if (!status.running && !status.queued) {
          if (status.lastBuild?.status === 'failed') throw new Error('官网静态构建失败，请查看重建日志。');
          if (status.lastBuild?.status === 'succeeded') return status;
        }
        await wait(800);
      }
      throw new Error('官网静态构建超时，请稍后查看重建状态。');
    }
    async function publishSite(source = 'current') {
      build.textContent = '发布中';
      const res = await fetch('/site-admin-api/publish?source='+encodeURIComponent(source), { method:'POST' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error?.message || '发布失败');
      build.textContent = '构建中';
      await waitForBuild();
      dirty.textContent='● 已同步';
      refreshPreview();
    }
    editor.addEventListener('input', (event) => { const el = event.target; if (el.dataset.path) setPath(state.content, el.dataset.path, el.type==='checkbox'?el.checked:el.value); if (el.dataset.listPath) setPath(state.content, el.dataset.listPath, el.value.split('\\n').map(s=>s.trim()).filter(Boolean)); });
    editor.addEventListener('input', (event) => { const el = event.target; if (!el.dataset.configPath) return; setPath(state.content, el.dataset.configPath, 'configNumber' in el.dataset ? (el.value === '' ? 0 : Number(el.value)) : el.value); });
    editor.addEventListener('change', async (event) => { const el = event.target; if (el.dataset.configPath && el.type === 'checkbox') setPath(state.content, el.dataset.configPath, el.checked); if (el.dataset.configUpload && el.files[0]) { setPath(state.content, el.dataset.configUpload, await upload(el.files[0])); render(); } });
    editor.addEventListener('change', async (event) => { const el = event.target; if (el.dataset.uploadTarget && el.files[0]) setPath(state.content, el.dataset.uploadTarget, await upload(el.files[0])); if (el.dataset.itemUpload && el.files[0]) { const input = editor.querySelector('[data-key="'+el.dataset.itemUpload+'"]'); input.value = await upload(el.files[0]); input.dispatchEvent(new Event('input',{bubbles:true})); } if ('itemGalleryUpload' in el.dataset && el.files?.length) { const input = editor.querySelector('[data-key="galleryImages"]'); const urls=[]; for (const file of el.files) urls.push(await upload(file)); input.value = [input.value, ...urls].filter(Boolean).join('\\n'); input.dispatchEvent(new Event('input',{bubbles:true})); } if ('itemDownloadUpload' in el.dataset && el.files[0]) { const asset = await uploadAsset(el.files[0]); const input = editor.querySelector('[data-key="downloads"]'); input.value = [input.value, (asset.name || '下载资料') + '：' + asset.url].filter(Boolean).join('\\n'); input.dispatchEvent(new Event('input',{bubbles:true})); } if (el.dataset.staticUpload && el.files[0]) { const input = editor.querySelector('[data-static-key="'+el.dataset.staticUpload+'"]'); input.value = await upload(el.files[0]); input.dispatchEvent(new Event('input',{bubbles:true})); } if (el.dataset.staticObjectUploadKey && el.files[0]) { const page=selectedStaticPage(); const box=el.closest('[data-static-object-list]'); const arr=byPath(page,box.dataset.staticObjectList)||[]; const index=Number(el.dataset.staticObjectUploadIndex); arr[index] ||= {}; arr[index][el.dataset.staticObjectUploadKey]=await upload(el.files[0]); setNested(page,box.dataset.staticObjectList,arr); markDirty(); render(); } if ('uploadMedia' in el.dataset && el.files[0]) { await upload(el.files[0]); renderMedia(); } });
    editor.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-media-references],[data-delete-media]');
      if (!button) return;
      try {
        const id = button.dataset.mediaReferences || button.dataset.deleteMedia;
        const referenceResponse = await fetch('/site-admin-api/media/'+encodeURIComponent(id)+'/references');
        const referencePayload = await referenceResponse.json().catch(() => ({}));
        if (!referenceResponse.ok) throw new Error(referencePayload.error?.message || '读取媒体引用失败。');
        const references = referencePayload.data.references || [];
        if (button.dataset.mediaReferences) {
          window.alert(references.length ? '当前素材被以下字段引用：\\n'+references.join('\\n') : '当前素材没有被官网内容引用。');
          return;
        }
        if (references.length) throw new Error('该素材仍被引用：'+references.join('、')+'。请先替换这些字段。');
        if (!window.confirm('确认删除该媒体文件？对象存储中的文件也会删除，此操作不可恢复。')) return;
        const response = await fetch('/site-admin-api/media/'+encodeURIComponent(id), { method: 'DELETE' });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error?.message || '删除媒体失败。');
        state.data.media = (await (await fetch('/site-admin-api/media')).json()).data || [];
        renderMedia();
      } catch (error) { showError(error); }
    });
    editor.addEventListener('click', (event) => {
      const button = event.target.closest('[data-delete]');
      if (!button || button.dataset.deleteConfirmed === 'true') return;
      event.preventDefault();
      event.stopImmediatePropagation();
      const type = button.dataset.delete;
      let message = '确认删除当前条目？此操作不可恢复。';
      if (type === 'category') {
        const category = state.content.categories[state.selected.category];
        const linked = state.content.products.filter((product) => product.categorySlug === category?.slug).length;
        message = linked
          ? '确认删除品类“' + (category?.name || '') + '”及其关联的 ' + linked + ' 个产品？此操作不可恢复。'
          : '确认删除品类“' + (category?.name || '') + '”？此操作不可恢复。';
      }
      if (!window.confirm(message)) return;
      button.dataset.deleteConfirmed = 'true';
      button.click();
    }, true);
    editor.addEventListener('click', async (event) => { const btn = event.target.closest('button'); if (!btn) return; if (btn.id === 'generateSeo') { generateSeo().catch(showError); return; } if (btn.dataset.createFallback !== undefined) { createFallbackVersion().catch(showError); return; } if (btn.dataset.activateFallback) { activateFallbackVersion(btn.dataset.activateFallback).catch(showError); return; } if (btn.dataset.deleteFallback) { deleteFallbackVersion(btn.dataset.deleteFallback).catch(showError); return; } if (btn.id === 'saveCurrent') saveCurrent({ publish: false }).catch(showError); if (btn.dataset.select) { state.selected[btn.dataset.select] = Number(btn.dataset.index); render(); const nextPreview = selectedPreview(btn.dataset.select); if (nextPreview) refreshPreview(nextPreview); } if (btn.dataset.selectLead) { state.selected.lead = Number(btn.dataset.selectLead); renderLeads(); refreshPreview('/support/#support-contact'); } if (btn.dataset.new) { const type=btn.dataset.new; if(type==='category') state.content.categories.push({id:'',name:'新产品品类',slug:'',description:'',coverImage:'',heroVideo:'',sortOrder:0,seo:{title:'',description:''}}); if(type==='product') state.content.products.push({id:'',name:'新产品',slug:'',categorySlug:state.content.categories[0]?.slug||'',coverImage:'',galleryImages:[],videoUrl:'',summary:'',features:[],specifications:[],downloads:[],body:'',isFeatured:false,sortOrder:0,seo:{title:'',description:''}}); state.selected[type] = state.content[type === 'category' ? 'categories' : 'products'].length - 1; render(); markDirty(); refreshPreview(previewForNav(state.active, state.previewPath)); } if (btn.dataset.delete) { if (state.busy) return; state.busy = true; clearError(); syncBusy(); try { const type=btn.dataset.delete; const data=collectItem(type); data._delete=true; const response=await fetch('/site-admin-api/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({section:type,data,revision:state.data.contentRevision})}); const payload=await response.json(); if(!response.ok) throw new Error(payload.error?.message||'删除失败'); state.data=payload.data; state.content=payload.data.content; state.selected[type]=0; render(); await publishSite(); } finally { state.busy = false; syncBusy(); } } if (btn.dataset.copy) navigator.clipboard?.writeText(btn.dataset.copy); if (btn.dataset.addPair) { const arr=byPath(state.content,btn.dataset.addPair)||[]; arr.push({label:'',href:''}); setPath(state.content,btn.dataset.addPair,arr); render(); } if (btn.dataset.removePair) { const box=btn.closest('[data-pair-list]'); const arr=byPath(state.content,box.dataset.pairList)||[]; arr.splice(Number(btn.dataset.removePair),1); setPath(state.content,box.dataset.pairList,arr); render(); } if (btn.dataset.addObject) { const arr=byPath(state.content,btn.dataset.addObject)||[]; arr.push({title:'',description:'',icon:''}); setPath(state.content,btn.dataset.addObject,arr); render(); } if (btn.dataset.removeObject) { const box=btn.closest('[data-object-list]'); const arr=byPath(state.content,box.dataset.objectList)||[]; arr.splice(Number(btn.dataset.removeObject),1); setPath(state.content,box.dataset.objectList,arr); render(); } if (btn.dataset.addStaticObject) { const page=selectedStaticPage(); const arr=byPath(page,btn.dataset.addStaticObject)||[]; arr.push({}); setNested(page,btn.dataset.addStaticObject,arr); markDirty(); render(); } if (btn.dataset.removeStaticObject) { const page=selectedStaticPage(); const box=btn.closest('[data-static-object-list]'); const arr=byPath(page,box.dataset.staticObjectList)||[]; arr.splice(Number(btn.dataset.removeStaticObject),1); setNested(page,box.dataset.staticObjectList,arr); markDirty(); render(); } });
    editor.addEventListener('input', (event) => { const el = event.target; const pairBox = el.closest('[data-pair-list]'); if (pairBox && el.dataset.pairIndex) { const arr=byPath(state.content,pairBox.dataset.pairList)||[]; arr[Number(el.dataset.pairIndex)] ||= {}; arr[Number(el.dataset.pairIndex)][el.dataset.pairKey]=el.value; markDirty(); } const objBox = el.closest('[data-object-list]'); if (objBox && el.dataset.objectIndex) { const arr=byPath(state.content,objBox.dataset.objectList)||[]; arr[Number(el.dataset.objectIndex)] ||= {}; arr[Number(el.dataset.objectIndex)][el.dataset.objectKey]=el.value; markDirty(); } const staticObjBox = el.closest('[data-static-object-list]'); if (staticObjBox && el.dataset.staticObjectIndex) { const page=selectedStaticPage(); const arr=byPath(page,staticObjBox.dataset.staticObjectList)||[]; arr[Number(el.dataset.staticObjectIndex)] ||= {}; arr[Number(el.dataset.staticObjectIndex)][el.dataset.staticObjectKey]=el.value; setNested(page,staticObjBox.dataset.staticObjectList,arr); markDirty(); } if (el.dataset.staticKey) { const page=state.content.staticPages.find(p=>p.slug===state.selected.staticPage); setNested(page, el.dataset.staticKey, el.value); markDirty(); } });
    editor.addEventListener('click', (event) => {
      const btn = event.target.closest('button');
      if (!btn || (!('configApplyJson' in btn.dataset) && !btn.dataset.configAdd && !('configRemoveParent' in btn.dataset))) return;
      try {
        if ('configApplyJson' in btn.dataset) {
          const next = JSON.parse(editor.querySelector('#allContentJson').value);
          if (!next || typeof next !== 'object' || !next.site || !next.home || !next.productCenter || !next.support || !Array.isArray(next.categories) || !Array.isArray(next.products) || !Array.isArray(next.faqs) || !Array.isArray(next.staticPages)) throw new Error('完整 JSON 缺少官网内容的必填根节点。');
          state.content = next;
          state.data.content = next;
          markDirty();
          render();
          return;
        }
        if (btn.dataset.configAdd) {
          const items = byPath(state.content, btn.dataset.configAdd);
          if (!Array.isArray(items)) throw new Error('无法新增：目标不是列表。');
          items.push(items.length ? structuredClone(items.at(-1)) : '');
          markDirty();
          render();
          return;
        }
        const items = byPath(state.content, btn.dataset.configRemoveParent);
        if (!Array.isArray(items)) throw new Error('无法删除：目标不是列表。');
        items.splice(Number(btn.dataset.configRemoveIndex), 1);
        markDirty();
        render();
      } catch (error) {
        showError(error);
      }
    });
    nav.addEventListener('click', (event) => { const btn = event.target.closest('[data-nav]'); if (!btn) return; state.active = btn.dataset.nav; refreshPreview(previewForNav(btn.dataset.nav, btn.dataset.preview || '/')); render(); });
    document.querySelector('#saveTop').addEventListener('click', () => saveCurrent({ publish: false }).catch(showError));
    document.querySelector('#previewOpen').addEventListener('click', () => window.open(buildPreviewUrl(state.previewPath || '/'), '_blank'));
    document.querySelector('#publish').addEventListener('click', () => saveCurrent({ publish: true }).catch(showError));
    const contentHubPath = '/site-admin/?view=content';
    function exitToContentHub() {
      if (state.dirty && !window.confirm('当前有未保存修改，确定放弃并返回内容管理吗？')) return;
      state.allowLeave = true;
      location.href = contentHubPath;
    }
    document.querySelector('#exitEditor')?.addEventListener('click', exitToContentHub);
    document.querySelector('#saveExit')?.addEventListener('click', async () => {
      try {
        if (!['media', 'fallbackVersions'].includes(state.active)) {
          const savedOk = await saveCurrent({ publish: false });
          if (!savedOk) return;
        }
        state.allowLeave = true;
        location.href = contentHubPath;
      } catch (error) { showError(error); }
    });
    document.querySelector('#logout').addEventListener('click', async () => {
      if (state.dirty && !window.confirm('当前有未保存修改，确定放弃并退出登录吗？')) return;
      await fetch('/site-admin-auth/logout',{method:'POST'});
      state.allowLeave = true;
      location.href='/admin-2fa';
    });
    window.addEventListener('beforeunload', (event) => { if (state.dirty && !state.allowLeave) { event.preventDefault(); event.returnValue = ''; } });
    document.querySelector('.device-tabs').addEventListener('click', (event) => { const btn=event.target.closest('[data-device]'); if(!btn) return; document.querySelectorAll('[data-device]').forEach(b=>b.classList.toggle('active',b===btn)); frameWrap.className='preview-frame-wrap '+(btn.dataset.device==='desktop'?'':btn.dataset.device); });
    render(); refreshPreview(state.previewPath); refreshStatus().catch(()=>{}); setInterval(() => refreshStatus().catch(()=>{}), 8000);
  </script>
</body>
</html>`;
}

/**
 * 判断解析后的绝对路径是否真的位于 root 目录内。
 * 必须带 path.sep，否则 /a/public-evil 会被 /a/public 前缀误判为合法。
 */
function isInsideRoot(root, file) {
  const base = path.resolve(root);
  const resolved = path.resolve(file);
  return resolved === base || resolved.startsWith(base + path.sep);
}

async function serveFile(res, file, cache = false) {
  const safeFile = path.normalize(file);
  if (!existsSync(safeFile)) {
    send(res, 404, 'not found');
    return;
  }
  const ext = path.extname(safeFile).toLowerCase();
  res.writeHead(200, {
    'Content-Type': mimeTypes[ext] || 'application/octet-stream',
    'Cache-Control': cache ? 'public, max-age=31536000, immutable' : 'no-store'
  });
  createReadStream(safeFile).pipe(res);
}

/** 所有响应统一安全头：CSP 按控制台/官网/客服三个来源收敛。 */
function applySecurityHeaders(res) {
  const connectSources = new Set(["'self'", SITE_ORIGIN, PUBLIC_ORIGIN]);
  try {
    connectSources.add(new URL(SERVICEBRIDGE_API_URL).origin);
  } catch {}
  const frameSources = new Set(["'self'", SITE_ORIGIN]);
  const csp = [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob: https: http:",
    "font-src 'self' data:",
    `connect-src ${[...connectSources].join(' ')}`,
    `frame-src ${[...frameSources].join(' ')}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'self'"
  ].join('; ');
  res.setHeader('Content-Security-Policy', csp);
  res.setHeader('X-Content-Type-Options', 'nosniff');
  res.setHeader('Referrer-Policy', 'strict-origin-when-cross-origin');
  res.setHeader('Permissions-Policy', 'camera=(), microphone=(), geolocation=()');
  res.setHeader('X-Frame-Options', 'SAMEORIGIN');
  if (SESSION_COOKIE_SECURE) {
    res.setHeader('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');
  }
}

async function activeAdminSession(req) {
  const session = verifySession(req);
  if (!session) return null;
  if (!session.sid) return null;
  const active = await platformStore.isAdminSessionActive(session.sid).catch(() => false);
  return active ? session : null;
}

/** 对状态变更的后台接口做来源校验（纵深防御，抗 CSRF）。 */
function adminOriginAllowed(req, pathname) {
  if (!['POST', 'PATCH', 'PUT', 'DELETE'].includes(req.method || '')) return true;
  if (!pathname.startsWith('/site-admin-api/')) return true;
  // 公开写入口有自己的来源白名单
  if (pathname === '/site-admin-api/leads' || pathname === '/site-admin-api/analytics/visit') return true;
  const origin = String(req.headers.origin || '').replace(/\/$/, '');
  if (!origin) return true; // 命令行/自动化工具不带 Origin
  return ADMIN_CORS_ALLOWED_ORIGINS.has(origin);
}

/** 托管 Arco 控制台；首页 index.html 由 SPA 自己决定跳登录还是工作台。 */
async function serveAdminWeb(res) {
  const indexFile = path.join(ADMIN_WEB_DIST, 'index.html');
  if (!existsSync(indexFile)) {
    send(res, 503, '控制台尚未构建，请先运行 npm run build:admin-web', { 'Content-Type': 'text/plain; charset=utf-8' });
    return;
  }
  return serveFile(res, indexFile, false);
}

async function mediaRuntimeSettingsPayload() {
  return {
    config: publicMediaRuntimeConfig(mediaRuntimeConfig),
    storageHealth: await mediaStore.health(),
    aiImage: mediaAiService.capability()
  };
}

async function handleMediaRuntimeSettings(req, res, pathname) {
  const root = '/site-admin-api/system/media-settings';
  if (pathname !== root && !pathname.startsWith(`${root}/`)) return false;
  if (!verifySession(req)) {
    json(res, 401, { error: { message: '请先登录。' } });
    return true;
  }
  try {
    if (pathname === root && req.method === 'GET') {
      json(res, 200, { data: await mediaRuntimeSettingsPayload() });
      return true;
    }
    if (pathname === `${root}/storage` && req.method === 'PATCH') {
      const payload = await bodyJson(req);
      const nextStorage = normalizeStorageUpdate(payload, mediaRuntimeConfig.storage);
      const identityChanged = storageIdentity(nextStorage) !== storageIdentity(mediaRuntimeConfig.storage);
      const stats = identityChanged ? await platformStore.mediaStats() : { total: 0 };
      if (identityChanged && stats.total > 0) {
        json(res, 409, { error: { message: `当前已有 ${stats.total} 条素材。为防止旧素材指向错误对象，不能直接切换 Endpoint、Bucket、对象前缀或 Public URL；请先执行对象迁移。` } });
        return true;
      }
      const nextConfig = { ...mediaRuntimeConfig, storage: nextStorage };
      const candidateStore = configuredMediaStore(nextConfig);
      const health = await candidateStore.health();
      if (!health.ok) throw new Error(`R2/S3 连通验证失败：${health.message || '无法访问 Bucket'}`);
      await platformStore.saveRuntimeSetting(MEDIA_RUNTIME_SETTING_KEY, persistedMediaRuntimeConfig(nextConfig));
      const previousStore = mediaStore;
      mediaRuntimeConfig = nextConfig;
      mediaStore = candidateStore;
      mediaAiService = configuredMediaAiService(mediaRuntimeConfig, mediaStore);
      previousStore.close?.();
      await auditEvent(req, 'system.media-storage.updated', 'runtime-setting', MEDIA_RUNTIME_SETTING_KEY, {
        endpoint: nextStorage.endpoint,
        region: nextStorage.region,
        bucket: nextStorage.bucket,
        prefix: nextStorage.prefix,
        publicBaseUrl: nextStorage.publicBaseUrl,
        forcePathStyle: nextStorage.forcePathStyle,
        credentialsUpdated: Boolean(payload.accessKeyId || payload.secretAccessKey || payload.sessionToken || payload.clearSessionToken)
      });
      json(res, 200, { data: await mediaRuntimeSettingsPayload() });
      return true;
    }
    if (pathname === `${root}/image` && req.method === 'PATCH') {
      const payload = await bodyJson(req);
      const nextImage = normalizeImageUpdate(payload, mediaRuntimeConfig.image);
      const nextConfig = { ...mediaRuntimeConfig, image: nextImage };
      const candidateStore = configuredMediaStore(nextConfig);
      const health = await candidateStore.health();
      if (!health.ok) throw new Error(`应用媒体处理配置前存储连通验证失败：${health.message || '无法访问 Bucket'}`);
      await platformStore.saveRuntimeSetting(MEDIA_RUNTIME_SETTING_KEY, persistedMediaRuntimeConfig(nextConfig));
      const previousStore = mediaStore;
      mediaRuntimeConfig = nextConfig;
      mediaStore = candidateStore;
      mediaAiService = configuredMediaAiService(mediaRuntimeConfig, mediaStore);
      previousStore.close?.();
      await auditEvent(req, 'system.media-processing.updated', 'runtime-setting', MEDIA_RUNTIME_SETTING_KEY, {
        variantWidths: nextImage.variantWidths,
        webpQuality: nextImage.webpQuality,
        keepOriginal: nextImage.keepOriginal,
        maxUploadBytes: nextImage.maxUploadBytes,
        allowedMimeTypes: nextImage.allowedMimeTypes
      });
      json(res, 200, { data: await mediaRuntimeSettingsPayload() });
      return true;
    }
    if (pathname === `${root}/ai-image/test` && req.method === 'POST') {
      const payload = await bodyJson(req);
      const nextAiImage = normalizeAiImageUpdate(payload, mediaRuntimeConfig.aiImage);
      if (!nextAiImage.apiKey) throw new Error('请先填写 AI 生图 API Key。');
      const candidate = configuredMediaAiService({ ...mediaRuntimeConfig, aiImage: nextAiImage }, mediaStore);
      const capability = await candidate.models({ refresh: true });
      json(res, 200, { data: capability });
      return true;
    }
    if (pathname === `${root}/ai-image` && req.method === 'PATCH') {
      const payload = await bodyJson(req);
      const nextAiImage = normalizeAiImageUpdate(payload, mediaRuntimeConfig.aiImage);
      const nextConfig = { ...mediaRuntimeConfig, aiImage: nextAiImage };
      const candidate = configuredMediaAiService(nextConfig, mediaStore);
      let capability = candidate.capability();
      if (nextAiImage.apiKey) capability = await candidate.models({ refresh: true });
      await platformStore.saveRuntimeSetting(MEDIA_RUNTIME_SETTING_KEY, persistedMediaRuntimeConfig(nextConfig));
      mediaRuntimeConfig = nextConfig;
      mediaAiService = candidate;
      await auditEvent(req, 'system.ai-image.updated', 'runtime-setting', MEDIA_RUNTIME_SETTING_KEY, {
        baseUrl: nextAiImage.baseUrl,
        defaultModel: nextAiImage.defaultModel,
        allowedHosts: nextAiImage.allowedHosts,
        apiKeyUpdated: Boolean(payload.apiKey || payload.clearApiKey)
      });
      json(res, 200, { data: { ...(await mediaRuntimeSettingsPayload()), aiImage: capability } });
      return true;
    }
    json(res, 405, { error: { message: '该媒体运行配置接口不支持当前请求方法。' } });
    return true;
  } catch (error) {
    const status = /连通|不可用|无法访问|AI 生图|同步 AI 模型/.test(error.message || '') ? 503 : 400;
    json(res, status, { error: { message: error.message || '媒体运行配置操作失败。' } });
    return true;
  }
}

async function handleAdminApi(req, res, pathname) {
  const adminUrl = new URL(req.url || '/', PUBLIC_ORIGIN);
  for (const handler of [leadsModule.handle, handleContent, handleWebsiteTranslation, handleWebsiteSeo, handleMedia, handlePublish, handleMediaRuntimeSettings, analyticsModule.handleAdmin, systemModule.handle]) {
    if (await handler(req, res, pathname, adminUrl)) return true;
  }
  if (pathname.startsWith('/site-admin-api/servicebridge/')) {
    await serviceBridgeProxy(req, res, pathname);
    return true;
  }
  if (!verifySession(req)) {
    json(res, 401, { error: { message: '请先登录。' } });
    return true;
  }
  return false;
}

const server = http.createServer(async (req, res) => {
  try {
    applySecurityHeaders(res);
    const url = new URL(req.url || '/', PUBLIC_ORIGIN);
    let pathname;
    try {
      pathname = decodeURIComponent(url.pathname);
    } catch {
      send(res, 400, 'bad request');
      return;
    }

    if (pathname === '/site-admin-api/leads') {
      if (!applyLeadCors(req, res)) {
        json(res, 403, { error: { message: '该网站来源不允许提交咨询单。' } });
        return;
      }
      if (req.method === 'OPTIONS') {
        send(res, 204, '');
        return;
      }
    }

    // 官网第一方访问埋点（公开、限流、来源白名单）。
    if (pathname === '/site-admin-api/analytics/visit') {
      await analyticsModule.handleBeacon(req, res);
      return;
    }

    if (pathname.startsWith('/assets/')) {
      const publicRoot = path.join(ROOT, 'frontend', 'public');
      const file = path.join(publicRoot, pathname);
      if (!isInsideRoot(publicRoot, file)) return send(res, 403, 'forbidden');
      return serveFile(res, file, true);
    }
    if (pathname.startsWith('/site-admin-assets/')) {
      const relative = pathname.slice('/site-admin-assets/'.length);
      const publicRoot = path.join(ROOT, 'admin-server', 'public');
      const file = path.join(publicRoot, relative);
      if (!isInsideRoot(publicRoot, file)) return send(res, 403, 'forbidden');
      return serveFile(res, file, false);
    }
    if (pathname.startsWith('/uploads/site-admin/')) {
      const file = path.join(ROOT, pathname);
      if (!isInsideRoot(UPLOAD_DIR, file)) return send(res, 403, 'forbidden');
      return serveFile(res, file, false);
    }
    if (pathname === '/openapi.json' && req.method === 'GET') {
      json(res, 200, createOpenApiDocument(PUBLIC_ORIGIN));
      return;
    }
    if (pathname === '/metrics' && req.method === 'GET') {
      if (!ADMIN_METRICS_BEARER_TOKEN || req.headers.authorization !== `Bearer ${ADMIN_METRICS_BEARER_TOKEN}`) {
        send(res, 404, 'not found');
        return;
      }
      send(res, 200, await adminMetrics(), { 'Content-Type': 'text/plain; version=0.0.4; charset=utf-8', 'Cache-Control': 'no-store' });
      return;
    }
    // 会话已被撤销/过期时立即失效（改密、删除管理员、主动下线都会走到这里）。
    if (parseCookies(req)[SESSION_COOKIE]) {
      const session = verifySession(req);
      const active = session ? await platformStore.isAdminSessionActive(session.sid).catch(() => false) : false;
      if (!session || !active) {
        clearSessionCookie(res);
        if (pathname.startsWith('/site-admin-api/')) {
          json(res, 401, { error: { message: '登录已失效，请重新登录。' } });
          return;
        }
        if (pathname.startsWith('/site-admin/')) {
          redirect(res, `/site-admin/login?returnTo=${encodeURIComponent(req.url || '/site-admin/')}`);
          return;
        }
      }
    }
    // 状态变更接口的来源校验（抗 CSRF 纵深防御）。
    if (!adminOriginAllowed(req, pathname)) {
      const client = resolveAdminClient(req);
      void recordSystemEvent({
        category: 'security',
        level: 'warning',
        action: 'security.origin_rejected',
        message: `来源不允许的写请求：${pathname}`,
        method: req.method || '',
        path: pathname,
        ip: client.ip,
        ipSource: client.source,
        userAgent: String(req.headers['user-agent'] || ''),
        statusCode: 403,
        detail: { origin: String(req.headers.origin || '') }
      });
      json(res, 403, { error: { message: '请求来源不被允许。' } });
      return;
    }
    if (await handleAuth(req, res, pathname)) return;
    if (pathname.startsWith('/site-admin-api/') && await handleAdminApi(req, res, pathname)) return;
    if (pathname === '/admin-2fa' || pathname === '/admin-2fa/') {
      // 旧的登录入口统一指向 Arco 控制台的登录页，保留 returnTo。
      const requested = url.searchParams.get('returnTo') || '/site-admin/';
      const returnTo = requested.startsWith('/site-admin/') && !requested.startsWith('//') ? requested : '/site-admin/';
      return redirect(res, `/site-admin/login?returnTo=${encodeURIComponent(returnTo)}`);
    }
    if (pathname === '/site-admin/assets' || pathname.startsWith('/site-admin/assets/')) {
      const assetsRoot = path.join(ADMIN_WEB_DIST, 'assets');
      const relative = pathname.replace(/^\/site-admin\/assets\/?/, '');
      const file = path.join(assetsRoot, relative);
      if (!relative || !isInsideRoot(assetsRoot, file)) return send(res, 404, 'not found');
      return serveFile(res, file, true);
    }
    if (pathname === '/site-admin' || pathname === '/site-admin/') {
      return serveAdminWeb(res);
    }
    // 旧版后台页面全部下线：统一重定向到 Arco 控制台的对应路由，登录由前端守卫处理。
    if (pathname === '/site-admin/content' || pathname === '/site-admin/content/') {
      return redirect(res, '/site-admin/content/site');
    }
    if (pathname === '/site-admin/content/editor' || pathname === '/site-admin/content/editor/') {
      const section = url.searchParams.get('section') || '';
      return redirect(res, section === 'media' ? '/site-admin/content/media' : '/site-admin/content/site');
    }
    if (pathname === '/site-admin/translations' || pathname === '/site-admin/translations/') {
      return redirect(res, '/site-admin/content/translations');
    }
    if (pathname === '/site-admin/settings' || pathname === '/site-admin/settings/') {
      return redirect(res, '/site-admin/system/general');
    }
    if (pathname === '/site-admin/media' || pathname === '/site-admin/media/') {
      return redirect(res, '/site-admin/content/media');
    }
    if (pathname === '/site-admin/api-docs' || pathname === '/site-admin/api-docs/') {
      if (!verifySession(req)) return redirect(res, '/admin-2fa?returnTo=' + encodeURIComponent(req.url || '/site-admin/api-docs'));
      return serveFile(res, path.join(ROOT, 'admin-server', 'public', 'api-docs.html'), false);
    }
    if (pathname === '/site-admin/help') {
      send(res, 200, '<!doctype html><meta charset="utf-8"><title>帮助文档</title><body style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,PingFang SC,Microsoft YaHei,sans-serif;padding:48px;line-height:1.8"><h1>帮助文档</h1><p>登录后进入官网后台，左侧选择模块，右侧修改文字、图片、视频、SEO、产品、品类和咨询记录。保存当前模块后点击发布官网按钮，前台静态页面会重建。</p><p><a href="/admin-2fa">返回登录</a></p></body>', { 'Content-Type': 'text/html; charset=utf-8' });
      return;
    }
    if (pathname === '/site-admin/privacy') {
      send(res, 200, '<!doctype html><meta charset="utf-8"><title>隐私政策</title><body style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,PingFang SC,Microsoft YaHei,sans-serif;padding:48px;line-height:1.8"><h1>隐私政策</h1><p>星渡官网后台仅用于管理员维护官网内容。系统会保存管理员邮箱、加密后的密码、2FA 密钥、媒体文件与客户提交的咨询信息。</p><p><a href="/admin-2fa">返回登录</a></p></body>', { 'Content-Type': 'text/html; charset=utf-8' });
      return;
    }
    if (pathname === '/healthz') {
      json(res, 200, { ok: true, service: 'astreva-admin-server' });
      return;
    }
    if (pathname === '/') {
      redirect(res, '/site-admin/');
      return;
    }
    // SPA 深链接（/site-admin/workspace、/site-admin/login 等）一律回落到控制台入口，
    // 未登录时由前端守卫跳转登录页。
    if (pathname.startsWith('/site-admin/')) {
      return serveAdminWeb(res);
    }
    // 未知路径：命中危险特征就记一条 danger 日志，否则不落库以免噪声。
    const danger = detectDanger(pathname, String(req.headers['user-agent'] || ''));
    if (danger) {
      const client = resolveAdminClient(req);
      void recordSystemEvent({
        category: 'security',
        level: 'danger',
        action: 'security.suspicious_request',
        message: `${danger}：${pathname}`,
        method: req.method || '',
        path: pathname,
        ip: client.ip,
        ipSource: client.source,
        country: countryFromRequest(req),
        userAgent: String(req.headers['user-agent'] || ''),
        statusCode: 404,
        detail: { danger, query: url.search || '' }
      });
    }
    send(res, 404, 'not found');
  } catch (error) {
    console.error(error);
    json(res, 500, { error: { message: error.message || 'server error' } });
  }
});

await ensureDirs();
const initialContentRecord = await readContentRecord();
const initialTranslations = new Map((await translationService.list()).map((item) => [item.locale, item]));
const resumeLocales = translationService.locales.filter((locale) => {
  const item = initialTranslations.get(locale);
  return !item || item.sourceRevision !== initialContentRecord.revision || ['pending', 'running'].includes(item.status);
});
if (resumeLocales.length) await translationService.schedule(initialContentRecord.content, initialContentRecord.revision, resumeLocales);
void platformStore.pruneAdminSessions().catch(() => {});
void platformStore.purgeSystemEvents({}).catch(() => {});
setInterval(() => {
  void platformStore.pruneAdminSessions().catch(() => {});
  void platformStore.purgeSystemEvents({}).catch(() => {});
}, 24 * 3600 * 1000).unref?.();

server.listen(PORT, HOST, () => {
  console.log(`Astreva admin server listening on http://${HOST}:${PORT}`);
  console.log(`Login: http://${HOST}:${PORT}/admin-2fa`);
  console.log(`Unified admin: http://${HOST}:${PORT}/site-admin/`);
  void recordSystemEvent({
    category: 'system',
    level: 'info',
    action: 'system.started',
    message: `控制台服务已启动（${HOST}:${PORT}）`,
    detail: { node: process.version, platform: process.platform, pid: process.pid }
  });
});
