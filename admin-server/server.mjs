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
import { createAIProviderClient } from './modules/ai-provider-client.mjs';
import { createWebsiteTranslationService } from './modules/website-translation-service.mjs';
import { createWebsiteTranslationRouter } from './modules/website-translation-router.mjs';
import { createWebsiteSeoService } from './modules/website-seo-service.mjs';
import { createWebsiteSiteDomain } from './modules/website-site-domain.mjs';
import { createWebsiteAutoSeo, needsAutomaticSeo } from './modules/website-auto-seo.mjs';
import { createAnalyticsRouter, createVisitorActivityForwarder, detectDanger } from './modules/analytics-router.mjs';
import { createSystemRouter } from './modules/system-router.mjs';
import { sanitizeCmsContent } from './modules/html-sanitize.mjs';
import { createWebsiteSeoRouter } from './modules/website-seo-router.mjs';
import { geolocationFromRequest } from './modules/request-geo.mjs';
import { validateContentMedia } from './modules/content-media-validation.mjs';

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
// 控制台的公网地址（反向代理后 Host 可能与后端不同），用于同源判定。
const PUBLIC_ADMIN_URL = process.env.PUBLIC_ADMIN_URL || process.env.PUBLIC_ADMIN_ORIGIN || '';

const REBUILD_URL = process.env.FRONTEND_REBUILD_WEBHOOK_URL || 'http://127.0.0.1:8787/rebuild';
const SERVICEBRIDGE_API_URL = normalizeServiceBridgeUrl(process.env.SERVICEBRIDGE_API_URL || 'http://127.0.0.1:8080');
const SERVICEBRIDGE_ADMIN_ACCOUNT = process.env.SERVICEBRIDGE_ADMIN_ACCOUNT || 'superadmin';
const SERVICEBRIDGE_ADMIN_PASSWORD = process.env.SERVICEBRIDGE_ADMIN_PASSWORD || '';
if (!SERVICEBRIDGE_ADMIN_PASSWORD) {
  console.warn('[安全] 未配置 SERVICEBRIDGE_ADMIN_PASSWORD：统一后台将无法代理客服引擎管理接口。请设置强随机值。');
}
const SESSION_COOKIE = 'astreva_admin_session';
const CSRF_COOKIE = 'astreva_admin_csrf';
const PREVIEW_COOKIE = 'astreva_preview_access';
const PREVIEW_ACCESS_TTL_MS = 10 * 60 * 1000;
const SECURITY_SETTINGS_KEY = 'security';
const ISSUER = process.env.ADMIN_2FA_ISSUER || '星渡官网账号';
const SESSION_TTL_MS = Number(process.env.ADMIN_2FA_SESSION_TTL_MS || 12 * 60 * 60 * 1000);
// 生产环境必须提供强随机会话密钥，避免使用仓库里的示例值被伪造管理员会话。
const IS_PRODUCTION = (process.env.APP_ENV || process.env.NODE_ENV || '').toLowerCase() === 'production';
const SESSION_SECRET =
  process.env.ADMIN_2FA_SESSION_SECRET ||
  process.env.ADMIN_SESSION_SECRET ||
  // 本地未配置时使用进程内随机值，而不是公开的示例常量；重启后旧会话失效。
  (IS_PRODUCTION ? 'change-me-astreva-admin-session-secret' : crypto.randomBytes(32).toString('base64url'));
const SESSION_COOKIE_SECURE = process.env.ADMIN_SESSION_COOKIE_SECURE === 'true' || PUBLIC_ORIGIN.startsWith('https://');
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
// 单机商业化部署允许显式启用本地卷媒体；多机/对象存储仍推荐 s3。
const ADMIN_ALLOW_LOCAL_MEDIA = process.env.ADMIN_ALLOW_LOCAL_MEDIA === 'true';
const ADMIN_METRICS_BEARER_TOKEN = process.env.ADMIN_METRICS_BEARER_TOKEN || '';
const ADMIN_TRUST_PROXY = process.env.ADMIN_TRUST_PROXY === 'true';
const ADMIN_CORS_ALLOWED_ORIGINS = new Set(
  String(process.env.ADMIN_CORS_ALLOWED_ORIGINS || SITE_ORIGIN)
    .split(',')
    .map((value) => value.trim().replace(/\/$/, ''))
    .filter(Boolean)
);
// 后台“写”接口的 Origin 白名单必须与公开站点的读/提交白名单分离。
// 默认只允许同源（后台自身域名）；ADMIN_WRITE_ALLOWED_ORIGINS 仅给确有需要的独立管理端使用。
const ADMIN_WRITE_ALLOWED_ORIGINS = new Set(
  String(process.env.ADMIN_WRITE_ALLOWED_ORIGINS || '')
    .split(',')
    .map((value) => value.trim().replace(/\/$/, ''))
    .filter(Boolean)
);
let configuredSiteOrigin = '';
function publicSiteOriginAllowed(origin) {
  return ADMIN_CORS_ALLOWED_ORIGINS.has(origin) || Boolean(configuredSiteOrigin && origin === configuredSiteOrigin);
}

if (process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS !== 'true' && CMS_STORE_DRIVER !== 'postgres') {
  throw new Error('后台运行必须使用 CMS_STORE_DRIVER=postgres；旧 JSON 仅可通过 npm run cms:cutover 导入。');
}
if (process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS !== 'true' && ADMIN_MEDIA_DRIVER !== 's3' && !ADMIN_ALLOW_LOCAL_MEDIA) {
  throw new Error('后台运行必须使用 ADMIN_MEDIA_DRIVER=s3；单机部署可显式设置 ADMIN_ALLOW_LOCAL_MEDIA=true 使用本地卷。');
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

// 首次初始化必须持有一次性令牌，避免公网部署窗口被扫描器抢占。
const ENV_SETUP_TOKEN = String(process.env.ADMIN_SETUP_TOKEN || '').trim();
let ADMIN_SETUP_TOKEN = ENV_SETUP_TOKEN;
const initialUsers = await platformStore.readUsers().catch(() => ({ users: [] }));
// 仅测试进程允许固定令牌，生产环境永远走随机/显式配置。
if (!ADMIN_SETUP_TOKEN && process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS === 'true') {
  ADMIN_SETUP_TOKEN = 'test-setup-token';
}
if (IS_PRODUCTION && initialUsers.users.length === 0 && !ENV_SETUP_TOKEN) {
  throw new Error('生产环境首次启动必须显式设置 ADMIN_SETUP_TOKEN（建议 openssl rand -base64 36），不能把初始化令牌写进日志。');
}
if (!ADMIN_SETUP_TOKEN && initialUsers.users.length === 0) {
  // 仅本地/测试：没有显式令牌时生成一次性随机值并打印，方便本机首次初始化。
  ADMIN_SETUP_TOKEN = crypto.randomBytes(24).toString('base64url');
  console.log('');
  console.log('============================================================');
  console.log('[安全] 首次初始化令牌（仅本次启动有效，初始化完成后不再需要）：');
  console.log(`[安全] ${ADMIN_SETUP_TOKEN}`);
  console.log('[安全] 在后台登录页选择“首次初始化”并粘贴该令牌；不要发到公开渠道。');
  console.log('============================================================');
  console.log('');
}
if (IS_PRODUCTION && ADMIN_SETUP_TOKEN && (ADMIN_SETUP_TOKEN.length < 24 || /change-me|changeme|placeholder/i.test(ADMIN_SETUP_TOKEN))) {
  throw new Error('生产环境 ADMIN_SETUP_TOKEN 必须至少 24 位且不能是示例值。');
}

// 客服引擎代理必须先于 AI 消费方创建：AI 供应商解析与后台管理共用同一条受信管理员通道。
const serviceBridgeProxy = createServiceBridgeProxy({
  apiUrl: SERVICEBRIDGE_API_URL,
  adminAccount: SERVICEBRIDGE_ADMIN_ACCOUNT,
  adminPassword: SERVICEBRIDGE_ADMIN_PASSWORD,
  internalToken: process.env.INTERNAL_API_TOKEN || '',
  publicOrigin: PUBLIC_ORIGIN,
  bodyRaw,
  bodyJson,
  json,
  send,
  verifySession,
  // 控制台保存供应商/能力绑定后立即让本地缓存失效，下一次请求即读取新配置。
  onConfigChange: () => aiProviderClient.invalidate()
});
// 网站翻译 / AI SEO / AI 生图统一从客服引擎控制台解析供应商；解析失败返回 null，消费方沿用本地配置。
const aiProviderClient = createAIProviderClient({ adminJson: serviceBridgeProxy.adminJson });
const resolveAiProvider = (capability) => aiProviderClient.resolve(capability);

const translationService = createWebsiteTranslationService({
  platformStore,
  resolveProvider: resolveAiProvider,
  onAvailabilityChange: () => scheduleTranslationRebuild()
});
const seoService = createWebsiteSeoService({ aiService: translationService });
const siteDomainService = createWebsiteSiteDomain({
  platformStore,
  fallbackOrigin: SITE_ORIGIN,
  onChange: (setting) => { configuredSiteOrigin = setting.domain ? setting.effectiveOrigin : ''; }
});
const initialSiteDomain = await siteDomainService.read();
configuredSiteOrigin = initialSiteDomain.domain ? initialSiteDomain.effectiveOrigin : '';
// 英语、阿拉伯语正文翻译完成后，使用 SEO 能力在各自语言的页面内容上独立生成元数据。
// 翻译记录只有在本步骤成功后才会标记 ready 并进入静态站构建流程。
translationService.setLocaleSeoGenerator((content, locale) => seoService.generateLocale(content, locale));
const autoSeo = createWebsiteAutoSeo({ platformStore, readContentRecord, seoService, translationService });
const publishService = createPublishService({
  rebuildUrl: REBUILD_URL,
  rebuildSecret: process.env.REBUILD_WEBHOOK_SECRET || '',
  siteOrigin: SITE_ORIGIN,
  platformStore
});
let translationRebuildTimer = null;
let translationRebuildRunning = false;
let translationRebuildQueued = false;
let translationRebuildFailures = 0;

function scheduleTranslationRebuild() {
  translationRebuildQueued = true;
  if (translationRebuildTimer || translationRebuildRunning) return;
  translationRebuildTimer = setTimeout(runTranslationRebuild, 250);
}

async function runTranslationRebuild() {
  translationRebuildTimer = null;
  if (!translationRebuildQueued || translationRebuildRunning) return;
  translationRebuildQueued = false;
  translationRebuildRunning = true;
  try {
    await publishService.requestAutomaticRebuild();
    translationRebuildFailures = 0;
  } catch (error) {
    translationRebuildFailures += 1;
    const delay = Math.min(5000 * 2 ** Math.min(translationRebuildFailures - 1, 4), 60000);
    console.error(`[网站翻译] 前台自动重建失败，${delay / 1000} 秒后重试：${error.message}`);
    translationRebuildQueued = true;
    translationRebuildTimer = setTimeout(runTranslationRebuild, delay);
  } finally {
    translationRebuildRunning = false;
    if (translationRebuildQueued && !translationRebuildTimer) {
      translationRebuildTimer = setTimeout(runTranslationRebuild, 250);
    }
  }
}
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
    requestTimeoutMs: config.aiImage.requestTimeoutMs,
    // 每次生图请求都会先尝试 image 能力绑定（走 TTL 缓存），未绑定则完全走本地配置。
    resolveProvider: resolveAiProvider
  });
}

let mediaStore = configuredMediaStore(mediaRuntimeConfig);
let mediaAiService = configuredMediaAiService(mediaRuntimeConfig, mediaStore);

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
  const previous = await readContentRecord();
  const saved = await platformStore.saveContent(normalized, options);
  if (options.autoSeo !== false && needsAutomaticSeo(previous.content, normalized)) {
    try {
      await autoSeo.enqueue(saved.revision);
    } catch (error) {
      console.error(`[AI SEO] 自动任务排队失败，继续生成译文：${error.message}`);
      await translationService.schedule(normalized, saved.revision);
    }
    return saved;
  }
  const carried = await translationService.carryForwardMediaOnly(previous.content, normalized, previous.revision, saved.revision);
  if (!carried) await translationService.schedule(normalized, saved.revision);
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
  // 账号被删除/重建后，旧会话必须立即失效，避免旧 Cookie 继续访问。
  await platformStore.revokeAllAdminSessions().catch((error) => {
    console.error(`[auth] 撤销管理员会话失败：${error.message}`);
  });
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

async function createSession(user, req = null, csrf = '') {
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
    csrf: String(csrf || ''),
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

function timingSafeStringEqual(left, right) {
  const a = Buffer.from(String(left || ''));
  const b = Buffer.from(String(right || ''));
  return a.length === b.length && a.length > 0 && crypto.timingSafeEqual(a, b);
}

function cookieDomainForPreview() {
  const configured = String(process.env.PREVIEW_COOKIE_DOMAIN || '').trim().replace(/^\./, '');
  if (configured) return configured;
  let adminHost = '';
  let siteHost = '';
  try { adminHost = new URL(PUBLIC_ORIGIN).hostname.toLowerCase(); } catch {}
  try { siteHost = new URL(configuredSiteOrigin || SITE_ORIGIN).hostname.toLowerCase(); } catch {}
  if (!adminHost || !siteHost) return '';
  // IP / localhost 不设置 Domain，使用 host-only Cookie（同一主机不同端口仍会发送）。
  if (/^[0-9.]+$/.test(adminHost) || adminHost.includes(':') || !adminHost.includes('.')) return '';
  const adminParts = adminHost.split('.');
  const siteParts = siteHost.split('.');
  let labels = 0;
  while (
    labels < adminParts.length && labels < siteParts.length &&
    adminParts[adminParts.length - 1 - labels] === siteParts[siteParts.length - 1 - labels]
  ) labels += 1;
  if (labels < 2) return '';
  return `.${adminParts.slice(-labels).join('.')}`;
}

async function readSecuritySettings() {
  try {
    const record = await platformStore.readRuntimeSetting(SECURITY_SETTINGS_KEY);
    const candidate = Number(record?.config?.previewTtlMinutes);
    const minutes = Number.isFinite(candidate) ? Math.min(1440, Math.max(1, Math.round(candidate))) : 10;
    return { previewTtlMinutes: minutes };
  } catch {
    return { previewTtlMinutes: 10 };
  }
}

function setPreviewAccessCookie(res, ttlMs = PREVIEW_ACCESS_TTL_MS) {
  const ttl = Math.min(24 * 60 * 60 * 1000, Math.max(60 * 1000, Number(ttlMs) || PREVIEW_ACCESS_TTL_MS));
  const exp = Date.now() + ttl;
  const signature = sign(`preview:${exp}`);
  const secure = SESSION_COOKIE_SECURE ? ' Secure;' : '';
  const domain = cookieDomainForPreview();
  res.setHeader('Set-Cookie', `${PREVIEW_COOKIE}=${exp}.${signature}; Path=/; HttpOnly; SameSite=Lax;${secure} Max-Age=${Math.floor(ttl / 1000)}${domain ? `; Domain=${domain}` : ''}`);
}

function verifyPreviewAccess(req) {
  const value = String(parseCookies(req)[PREVIEW_COOKIE] || '');
  const [rawExp, signature] = value.split('.');
  const exp = Number(rawExp || 0);
  if (!Number.isFinite(exp) || exp <= Date.now() || !signature) return false;
  return timingSafeStringEqual(signature, sign(`preview:${exp}`));
}

function verifyCsrf(req, session = null) {
  const active = session || verifySession(req);
  const expected = String(active?.csrf || '');
  if (!expected) return false;
  const header = String(req.headers['x-csrf-token'] || '');
  const cookie = String(parseCookies(req)[CSRF_COOKIE] || '');
  return timingSafeStringEqual(header, expected) && timingSafeStringEqual(cookie, expected);
}

const CSRF_EXEMPT_WRITE_PATHS = new Set([
  '/site-admin-api/leads',
  '/site-admin-api/analytics/visit'
]);

function requiresCsrf(req, pathname) {
  if (process.env.ALLOW_LEGACY_STORAGE_FOR_TESTS === 'true') return false;
  if (!['POST', 'PATCH', 'PUT', 'DELETE'].includes(req.method || '')) return false;
  if (pathname === '/site-admin-auth/logout' || pathname === '/site-admin-auth/password') return true;
  return pathname.startsWith('/site-admin-api/') && !CSRF_EXEMPT_WRITE_PATHS.has(pathname);
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
  if (!publicSiteOriginAllowed(origin)) return false;
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
  if (!publicSiteOriginAllowed(origin)) return false;
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
  const csrf = crypto.randomBytes(32).toString('base64url');
  const token = await createSession(user, req, csrf);
  const maxAge = Math.floor(SESSION_TTL_MS / 1000);
  const secure = SESSION_COOKIE_SECURE ? ' Secure;' : '';
  res.setHeader('Set-Cookie', [
    `${SESSION_COOKIE}=${encodeURIComponent(token)}; Path=/; HttpOnly; SameSite=Strict;${secure} Max-Age=${maxAge}`,
    // CSRF 令牌必须能被同源 JS 读取，因此不设 HttpOnly；它不能单独用于认证。
    `${CSRF_COOKIE}=${encodeURIComponent(csrf)}; Path=/; SameSite=Strict;${secure} Max-Age=${maxAge}`
  ]);
}

function clearSessionCookie(res) {
  const secure = SESSION_COOKIE_SECURE ? ' Secure;' : '';
  res.setHeader('Set-Cookie', [
    `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Strict;${secure} Max-Age=0`,
    `${CSRF_COOKIE}=; Path=/; SameSite=Strict;${secure} Max-Age=0`
  ]);
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
  platformStore,
  setupToken: ADMIN_SETUP_TOKEN
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
const STATIC_PAGE_SLUGS = new Set(['about', 'about-manufacturing', 'about-quality', 'solutions', 'warranty', 'install', 'faq', 'privacy', 'agreement']);

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
  validateContentMedia(content);
}


async function saveSection(payload) {
  const content = await readContent();
  const section = String(payload.section || '');
  const data = payload.data || {};
  let saved = null;

  if (section === 'content') {
    // 全量保存也要先做富文本消毒，避免绕过下面的白名单清洗。
    const sanitizedData = sanitizeCmsContent(data);
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
        mediaItems: Array.isArray(data.mediaItems) ? data.mediaItems : existing?.mediaItems,
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
      const existing = data.id ? content.products.find((item) => item.id === data.id) : null;
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
        galleryVideos: Array.isArray(data.galleryVideos) ? data.galleryVideos : [],
        mediaItems: Array.isArray(data.mediaItems) ? data.mediaItems : existing?.mediaItems,
        galleryMediaItems: Array.isArray(data.galleryMediaItems) ? data.galleryMediaItems : existing?.galleryMediaItems,
        videoMediaItems: Array.isArray(data.videoMediaItems) ? data.videoMediaItems : existing?.videoMediaItems,
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
  const sanitized = sanitizeCmsContent(content);
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
  readSiteOrigin: async () => (await siteDomainService.read()).effectiveOrigin,
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
  autoSeo,
  siteDomainService,
  publishService,
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
  return geolocationFromRequest(req, { trustedProxy: ADMIN_TRUST_PROXY }).country;
}

function regionFromRequest(req) {
  return geolocationFromRequest(req, { trustedProxy: ADMIN_TRUST_PROXY }).region;
}

function cityFromRequest(req) {
  return geolocationFromRequest(req, { trustedProxy: ADMIN_TRUST_PROXY }).city;
}

function visitorTimezoneFromRequest(req) {
  return geolocationFromRequest(req, { trustedProxy: ADMIN_TRUST_PROXY }).timezone;
}

function analyticsTimezone() {
  return process.env.ADMIN_ANALYTICS_TIMEZONE || 'Asia/Shanghai';
}

function analyticsCors(req, res) {
  const origin = String(req.headers.origin || '').replace(/\/$/, '');
  if (!origin) return true;
  if (!publicSiteOriginAllowed(origin)) return false;
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

// 官网埋点 -> 客服引擎访客画像：服务间令牌未配置时整个转发静默跳过（见 createVisitorActivityForwarder）。
const visitorActivityForwarder = createVisitorActivityForwarder({
  apiUrl: SERVICEBRIDGE_API_URL,
  token: process.env.INTERNAL_API_TOKEN || ''
});

const analyticsModule = createAnalyticsRouter({
  platformStore,
  bodyJson,
  json,
  verifySession,
  applyPublicCors: analyticsCors,
  resolveClient: resolveAdminClient,
  countryFromRequest,
  regionFromRequest,
  cityFromRequest,
  visitorTimezoneFromRequest,
  timezoneFromRequest: analyticsTimezone,
  visitorActivityForwarder
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
    ['aboutManufacturing', '制造能力', '', '/about/manufacturing/#about-subpage-hero'],
    ['aboutQuality', '工程与品质', '', '/about/quality/#about-subpage-hero'],
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
    items: ['homeHero', 'homeProducts', 'homeAdvantages', 'homeTestimonials', 'homeCta', 'solutions', 'support', 'about', 'aboutManufacturing', 'aboutQuality', 'warranty', 'install', 'faqPage', 'privacy', 'agreement']
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

function isInsideRoot(root, file) {
  const base = path.resolve(root);
  const resolved = path.resolve(file);
  return resolved === base || resolved.startsWith(base + path.sep);
}

async function serveFile(res, file, cache = false) {
  const safeFile = path.normalize(file);
  let stat;
  try {
    stat = await fs.stat(safeFile);
  } catch {
    send(res, 404, 'not found');
    return;
  }
  // 目录必须在这里拦住：对目录 createReadStream 会抛 EISDIR，
  // 未处理的 'error' 事件会直接杀掉整个后台进程（请求 /site-admin-assets/ 即可触发）。
  if (!stat.isFile()) {
    send(res, 404, 'not found');
    return;
  }
  const ext = path.extname(safeFile).toLowerCase();
  res.writeHead(200, {
    'Content-Type': mimeTypes[ext] || 'application/octet-stream',
    'Cache-Control': cache ? 'public, max-age=31536000, immutable' : 'no-store'
  });
  const stream = createReadStream(safeFile);
  stream.on('error', () => {
    if (!res.headersSent) send(res, 500, 'read error');
    else res.destroy();
  });
  stream.pipe(res);
}

/** 所有响应统一安全头：CSP 按控制台/官网/客服三个来源收敛。 */
function applySecurityHeaders(res) {
  const connectSources = new Set(["'self'", SITE_ORIGIN, PUBLIC_ORIGIN]);
  if (configuredSiteOrigin) connectSources.add(configuredSiteOrigin);
  try {
    connectSources.add(new URL(SERVICEBRIDGE_API_URL).origin);
  } catch {}
  const frameSources = new Set(["'self'", SITE_ORIGIN, 'https://www.youtube-nocookie.com']);
  if (configuredSiteOrigin) frameSources.add(configuredSiteOrigin);
  const csp = [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self'",
    "style-src-attr 'unsafe-inline'",
    "img-src 'self' data: blob: https: http:",
    "media-src 'self' blob: https: http:",
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
  if (ADMIN_WRITE_ALLOWED_ORIGINS.has(origin)) return true;
  // 控制台自身的来源必须放行：admin-server 就是控制台的宿主，「同源」请求不可能是 CSRF。
  // 之前只比对 ADMIN_CORS_ALLOWED_ORIGINS（那是给官网跨域用的），一旦没把控制台地址写进去，
  // 浏览器里所有写操作（保存内容、上传素材、AI 设置、营业时间、推送、提醒…）都会 403。
  return isSameOriginRequest(req, origin);
}

/** 判断 Origin 是否与控制台自身同源（含反向代理的 X-Forwarded-Host 与配置的公网控制台地址）。 */
function isSameOriginRequest(req, origin) {
  let originHost = '';
  try {
    originHost = new URL(origin).host;
  } catch {
    return false;
  }
  if (!originHost) return false;
  const candidates = new Set();
  const forwardedHost = ADMIN_TRUST_PROXY
    ? String(req.headers['x-forwarded-host'] || '').split(',')[0].trim()
    : '';
  const host = String(req.headers.host || '').trim();
  if (host) candidates.add(host);
  if (forwardedHost) candidates.add(forwardedHost);
  try {
    candidates.add(new URL(PUBLIC_ADMIN_URL).host);
  } catch {}
  for (const candidate of candidates) {
    if (candidate && candidate === originHost) return true;
  }
  return false;
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
  if (pathname === '/site-admin-api/security-settings') {
    if (req.method === 'GET') {
      json(res, 200, { data: await readSecuritySettings() });
      return true;
    }
    if (req.method === 'PATCH') {
      const body = await bodyJson(req).catch(() => null);
      const candidate = Number(body?.preview_ttl_minutes);
      if (!Number.isFinite(candidate) || candidate < 1 || candidate > 1440) {
        json(res, 400, { error: { message: 'preview_ttl_minutes 必须是 1–1440 的整数。' } });
        return true;
      }
      const settings = { previewTtlMinutes: Math.round(candidate) };
      await platformStore.saveRuntimeSetting(SECURITY_SETTINGS_KEY, { config: settings });
      void recordSystemEvent({
        category: 'admin',
        level: 'info',
        action: 'security.settings',
        message: `更新安全策略：预览有效期 ${settings.previewTtlMinutes} 分钟`,
        method: req.method,
        path: pathname,
        ip: resolveAdminClient(req).ip,
        userAgent: String(req.headers['user-agent'] || ''),
        statusCode: 200
      });
      json(res, 200, { data: { previewTtlMinutes: settings.previewTtlMinutes } });
      return true;
    }
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
      // 必须相对 ADMIN_UPLOAD_DIR 解析，不能假定它等于 ROOT/uploads。
      const relative = pathname.slice('/uploads/site-admin/'.length);
      const file = path.join(UPLOAD_DIR, relative);
      if (!isInsideRoot(UPLOAD_DIR, file)) return send(res, 403, 'forbidden');
      return serveFile(res, file, false);
    }
    if (pathname === '/openapi.json' && req.method === 'GET') {
      if (!verifySession(req)) {
        json(res, 401, { error: { message: '请先登录后再查看后台 OpenAPI 文档。' } });
        return;
      }
      json(res, 200, createOpenApiDocument(PUBLIC_ORIGIN));
      return;
    }
    if (pathname === '/metrics' && req.method === 'GET') {
      if (!ADMIN_METRICS_BEARER_TOKEN || !timingSafeStringEqual(req.headers.authorization || '', `Bearer ${ADMIN_METRICS_BEARER_TOKEN}`)) {
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
    if (requiresCsrf(req, pathname) && !verifyCsrf(req)) {
      await recordSystemEvent({
        category: 'security',
        level: 'warning',
        action: 'security.csrf_rejected',
        message: `缺少或错误的 CSRF 令牌：${pathname}`,
        method: req.method || '',
        path: pathname,
        ip: resolveAdminClient(req).ip,
        userAgent: String(req.headers['user-agent'] || ''),
        statusCode: 403
      });
      json(res, 403, { error: { message: 'CSRF 校验失败，请刷新页面后重试。' } });
      return;
    }
    // Caddy forward_auth 专用：校验预览路径第一段是否为当前构建的随机 token。
    // 该接口只认 token，不依赖后台 Cookie（预览在官网域名下打开）。
    if (pathname === '/site-admin-api/preview-grant' && req.method === 'GET') {
      if (!verifySession(req)) {
        json(res, 401, { error: { message: '请先登录。' } });
        return;
      }
      const settings = await readSecuritySettings();
      setPreviewAccessCookie(res, settings.previewTtlMinutes * 60 * 1000);
      json(res, 200, { ok: true, preview_ttl_minutes: settings.previewTtlMinutes });
      return;
    }
    if (pathname === '/site-admin-api/preview-auth' && req.method === 'GET') {
      const previewPath = url.searchParams.get('path') || '';
      const match = previewPath.match(/^\/__preview\/([0-9a-f]{32})(?:\/|$)/);
      const expected = await publishService.previewToken().catch(() => '');
      if (!match || !expected || !timingSafeStringEqual(match[1], expected) || !verifyPreviewAccess(req)) {
        send(res, 401, 'preview unauthorized');
        return;
      }
      send(res, 204, '');
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
const initialAutoSeo = await autoSeo.status();
await autoSeo.resume();
if ([...initialTranslations.values()].some((item) => item.status === 'ready')) scheduleTranslationRebuild();
const translationSettings = await translationService.publicSettings();
const canRetryUnconfigured = translationSettings.enabled && (
  (translationSettings.apiKeyConfigured && Boolean(translationSettings.model)) ||
  Boolean(await resolveAiProvider('translation'))
);
const resumeLocales = translationService.locales.filter((locale) => {
  const item = initialTranslations.get(locale);
  return !item || item.sourceRevision !== initialContentRecord.revision ||
    ['pending', 'running', 'failed'].includes(item.status) ||
    (item.status === 'unconfigured' && canRetryUnconfigured);
});
if (resumeLocales.length && !(initialAutoSeo?.status === 'pending' && initialAutoSeo.sourceRevision === initialContentRecord.revision)) {
  await translationService.schedule(initialContentRecord.content, initialContentRecord.revision, resumeLocales);
}
void platformStore.pruneAdminSessions().catch(() => {});
void platformStore.purgeSystemEvents({}).catch(() => {});
setInterval(() => {
  void platformStore.pruneAdminSessions().catch(() => {});
  void platformStore.purgeSystemEvents({}).catch(() => {});
}, 24 * 3600 * 1000).unref?.();

// 限制慢请求/慢头攻击；大文件上传仍允许 5 分钟。
server.requestTimeout = 5 * 60 * 1000;
server.headersTimeout = 30 * 1000;
server.keepAliveTimeout = 5 * 1000;
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
