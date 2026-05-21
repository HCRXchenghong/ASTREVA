import http from 'node:http';
import { createReadStream, existsSync, readFileSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import crypto from 'node:crypto';
import vm from 'node:vm';
import bcrypt from 'bcryptjs';
import formidable from 'formidable';
import QRCode from 'qrcode';
import ts from 'typescript';

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

const DATA_DIR = path.join(ROOT, 'data');
const CONTENT_FILE = path.join(DATA_DIR, 'site-content.json');
const USERS_FILE = path.join(DATA_DIR, 'admin-users.json');
const LEADS_FILE = path.join(DATA_DIR, 'leads.json');
const UPLOAD_DIR = path.join(ROOT, 'uploads', 'site-admin');
const PORT = Number(process.env.PORT || 1337);
const HOST = process.env.HOST || '127.0.0.1';
const PUBLIC_ORIGIN = process.env.PUBLIC_ADMIN_ORIGIN || `http://${HOST}:${PORT}`;
const SITE_ORIGIN = process.env.PUBLIC_SITE_URL || process.env.FRONTEND_SITE_ORIGIN || 'http://127.0.0.1:4321';
const REBUILD_URL = process.env.FRONTEND_REBUILD_WEBHOOK_URL || 'http://127.0.0.1:8787/rebuild';
const SESSION_COOKIE = 'astreva_admin_session';
const ISSUER = process.env.ADMIN_2FA_ISSUER || '星渡官网账号';
const SESSION_TTL_MS = Number(process.env.ADMIN_2FA_SESSION_TTL_MS || 12 * 60 * 60 * 1000);
const SESSION_SECRET =
  process.env.ADMIN_2FA_SESSION_SECRET ||
  process.env.ADMIN_SESSION_SECRET ||
  'change-me-astreva-admin-session-secret';

const pendingSetups = new Map();
const pendingLogins = new Map();

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

async function loadFallbackContent() {
  const source = await fs.readFile(path.join(ROOT, 'frontend', 'src', 'data', 'fallback.ts'), 'utf8');
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2020,
      esModuleInterop: true
    }
  }).outputText;
  const sandbox = {
    exports: {},
    process: {
      env: {
        ...process.env,
        PUBLIC_SERVICEBRIDGE_URL: process.env.PUBLIC_SERVICEBRIDGE_URL || 'http://127.0.0.1:5173'
      }
    }
  };
  vm.runInNewContext(compiled, sandbox, { filename: 'fallback.ts' });
  return structuredClone(sandbox.exports.fallbackContent);
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
  content.site.serviceBridgeUrl ||= process.env.PUBLIC_SERVICEBRIDGE_URL || 'http://127.0.0.1:5173';
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

async function readContent() {
  await ensureDirs();
  const fallback = await loadFallbackContent();
  if (!existsSync(CONTENT_FILE)) {
    await writeJson(CONTENT_FILE, fallback);
  }
  const content = normalizeContent(mergeDefaults(await readJson(CONTENT_FILE, fallback), fallback));
  for (const page of fallback.staticPages || []) {
    const index = content.staticPages.findIndex((item) => item.slug === page.slug);
    if (index >= 0) content.staticPages[index] = mergeDefaults(content.staticPages[index], page);
    else content.staticPages.push(structuredClone(page));
  }
  return content;
}

async function saveContent(content) {
  await writeJson(CONTENT_FILE, normalizeContent(content));
}

async function readUsers() {
  return readJson(USERS_FILE, { users: [] });
}

async function saveUsers(users) {
  await writeJson(USERS_FILE, users);
}

async function readLeads() {
  return readJson(LEADS_FILE, { leads: [] });
}

async function saveLeads(leads) {
  await writeJson(LEADS_FILE, leads);
}

function bodyJson(req) {
  return new Promise((resolve, reject) => {
    let raw = '';
    req.on('data', (chunk) => {
      raw += chunk;
      if (raw.length > 20 * 1024 * 1024) {
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

function createSession(user) {
  const payload = Buffer.from(JSON.stringify({
    userId: user.id,
    email: user.email,
    exp: Date.now() + SESSION_TTL_MS
  })).toString('base64url');
  return `${payload}.${sign(payload)}`;
}

function verifySession(req) {
  const token = parseCookies(req)[SESSION_COOKIE];
  if (!token || !token.includes('.')) return null;
  const [payload, signature] = token.split('.');
  if (signature !== sign(payload)) return null;
  try {
    const session = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'));
    if (!session.exp || session.exp < Date.now()) return null;
    return session;
  } catch {
    return null;
  }
}

function setSessionCookie(res, user) {
  const cookie = `${SESSION_COOKIE}=${encodeURIComponent(createSession(user))}; Path=/; HttpOnly; SameSite=Lax; Max-Age=${Math.floor(SESSION_TTL_MS / 1000)}`;
  res.setHeader('Set-Cookie', cookie);
}

function clearSessionCookie(res) {
  res.setHeader('Set-Cookie', `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`);
}

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

function mediaUrl(relativePath) {
  return `${PUBLIC_ORIGIN}${relativePath}`;
}

async function readMediaLibrary() {
  await ensureDirs();
  const entries = await fs.readdir(UPLOAD_DIR, { withFileTypes: true }).catch(() => []);
  const files = await Promise.all(entries.filter((entry) => entry.isFile()).map(async (entry) => {
    const absolute = path.join(UPLOAD_DIR, entry.name);
    const stats = await fs.stat(absolute);
    const ext = path.extname(entry.name).toLowerCase();
    const type = ['.mp4', '.webm', '.mov'].includes(ext)
      ? 'video'
      : ['.jpg', '.jpeg', '.png', '.webp', '.gif', '.svg'].includes(ext)
        ? 'image'
        : 'file';
    const relativePath = `/uploads/site-admin/${entry.name}`;
    return {
      name: entry.name,
      url: mediaUrl(relativePath),
      path: relativePath,
      type,
      size: stats.size,
      updatedAt: stats.mtime.toISOString()
    };
  }));
  return files.sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime());
}

async function getBootstrap() {
  const [content, leadsStore, media] = await Promise.all([readContent(), readLeads(), readMediaLibrary()]);
  const leads = leadsStore.leads || [];
  return {
    content,
    leads,
    media,
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

async function saveSection(payload) {
  const content = await readContent();
  const section = String(payload.section || '');
  const data = payload.data || {};
  let saved = null;

  if (section === 'site') {
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
      content.categories = content.categories.filter((item) => item.id !== data.id);
      content.products = content.products.filter((item) => item.categorySlug !== data.slug);
      saved = { deleted: true, id: data.id };
    } else {
      const category = {
        id: data.id || `cat_${crypto.randomUUID()}`,
        name: data.name || '未命名品类',
        slug: data.slug || slugify(data.name, 'category'),
        description: data.description || '',
        coverImage: data.coverImage || '',
        heroVideo: data.heroVideo || '',
        sortOrder: Number(data.sortOrder || 0),
        seo: data.seo || { title: `${data.name || '品类'} | 星渡ASTREVA`, description: data.description || '' }
      };
      const index = content.categories.findIndex((item) => item.id === category.id);
      if (index >= 0) content.categories[index] = category;
      else content.categories.push(category);
      saved = category;
    }
  } else if (section === 'product') {
    if (data._delete && data.id) {
      content.products = content.products.filter((item) => item.id !== data.id);
      saved = { deleted: true, id: data.id };
    } else {
      const product = {
        id: data.id || `prod_${crypto.randomUUID()}`,
        name: data.name || '未命名产品',
        slug: data.slug || slugify(data.name, 'product'),
        categorySlug: data.categorySlug || content.categories[0]?.slug || '',
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
      const faq = {
        id: data.id || `faq_${crypto.randomUUID()}`,
        question: data.question || '未填写问题',
        answer: data.answer || '',
        sortOrder: Number(data.sortOrder || 0),
        enabled: data.enabled !== false
      };
      const index = content.faqs.findIndex((item) => item.id === faq.id);
      if (index >= 0) content.faqs[index] = faq;
      else content.faqs.push(faq);
      saved = faq;
    }
  } else if (section === 'staticPage') {
    const page = getStaticPage(content, data.slug);
    Object.assign(page, data);
    saved = page;
  } else if (section === 'lead') {
    const store = await readLeads();
    const index = store.leads.findIndex((item) => item.id === data.id);
    if (index >= 0) {
      store.leads[index] = { ...store.leads[index], ...data, updatedAt: new Date().toISOString() };
      await saveLeads(store);
      return store.leads[index];
    }
  } else {
    throw new Error('未知的保存模块。');
  }

  await saveContent(content);
  return saved;
}

async function parseUpload(req) {
  const form = formidable({
    multiples: false,
    maxFileSize: 1024 * 1024 * 1024,
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
  const { files } = await parseUpload(req);
  const rawFile = files.file || files.files || Object.values(files)[0];
  const file = Array.isArray(rawFile) ? rawFile[0] : rawFile;
  if (!file) {
    json(res, 400, { error: { message: '请选择要上传的文件。' } });
    return;
  }
  const originalName = file.originalFilename || file.name || 'asset';
  const extension = path.extname(originalName).toLowerCase();
  const allowed = new Set(['.jpg', '.jpeg', '.png', '.webp', '.gif', '.svg', '.mp4', '.webm', '.mov', '.pdf', '.zip', '.dwg', '.dxf']);
  if (!allowed.has(extension)) {
    json(res, 400, { error: { message: '暂不支持该文件格式。' } });
    return;
  }
  const safeBaseName = path
    .basename(originalName, extension)
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60) || 'asset';
  const filename = `${Date.now()}-${crypto.randomUUID().slice(0, 8)}-${safeBaseName}${extension}`;
  const relativePath = `/uploads/site-admin/${filename}`;
  await fs.copyFile(file.filepath || file.path, path.join(UPLOAD_DIR, filename));
  json(res, 200, {
    data: {
      name: originalName,
      url: mediaUrl(relativePath),
      path: relativePath,
      mime: file.mimetype || file.type || '',
      size: file.size || 0
    }
  });
}

async function triggerRebuild() {
  const headers = {};
  if (process.env.REBUILD_WEBHOOK_SECRET) {
    headers.Authorization = `Bearer ${process.env.REBUILD_WEBHOOK_SECRET}`;
  }
  const response = await fetch(REBUILD_URL, { method: 'POST', headers });
  const body = await response.text();
  if (!response.ok) throw new Error(`重建服务返回 ${response.status}: ${body}`);
  try {
    return JSON.parse(body);
  } catch {
    return { ok: true, body };
  }
}

async function checkUrl(url) {
  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 3000);
    const response = await fetch(url, { signal: controller.signal });
    clearTimeout(timer);
    return { ok: response.ok, status: response.status, message: response.ok ? '可访问' : `HTTP ${response.status}` };
  } catch (error) {
    return { ok: false, status: null, message: error.name === 'AbortError' ? '连接超时' : error.message };
  }
}

async function statusPayload() {
  const rebuildStatusUrl = new URL('/last-build', REBUILD_URL).toString();
  const [preview, rebuildResponse] = await Promise.all([
    checkUrl(SITE_ORIGIN),
    fetch(rebuildStatusUrl).then((response) => response.ok ? response.json() : null).catch(() => null)
  ]);
  return {
    preview: { url: SITE_ORIGIN, ...preview },
    rebuild: {
      url: rebuildStatusUrl,
      ok: Boolean(rebuildResponse),
      running: Boolean(rebuildResponse?.running),
      queued: Boolean(rebuildResponse?.queued),
      lastBuild: rebuildResponse?.lastBuild || null,
      message: rebuildResponse ? '可访问' : '重建服务未启动'
    }
  };
}

function navMarkup(active = 'homeHero') {
  const items = [
    ['homeHero', '首页首屏', '⌂', '/#home-hero'],
    ['homeProducts', '首页产品区', '▦', '/#home-products'],
    ['homeAdvantages', '首页优势', '✦', '/#home-advantages'],
    ['homeTestimonials', '客户评价', '', '/#home-testimonials'],
    ['homeCta', '首页行动区', '', '/#home-cta'],
    ['productCenter', '产品中心', '□', '/products/#products-hero'],
    ['categories', '产品品类', '', '/products/#product-categories'],
    ['products', '产品管理', '', '/products/#product-categories'],
    ['solutions', '解决方案', '◇', '/solutions/#solutions-hero'],
    ['projects', '工程案例', '▣', '/projects/#projects-hero'],
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
    ['navigation', '导航文案', '', '/#home-hero'],
    ['footer', '页脚链接', '', '/#site-footer'],
    ['social', '社交方式', '', '/#site-footer'],
    ['cookie', 'Cookie 提示', '', '/#cookie-consent'],
    ['chat', '在线客服', '', '/#home-hero']
  ];
  return items.map(([id, label, icon, preview]) => {
    const child = !icon;
    return `<button class="${child ? 'nav-child' : 'nav-button'} ${id === active ? 'active' : ''}" type="button" data-nav="${id}" data-preview="${preview}">${child ? '' : `<span class="nav-icon">${escapeHtml(icon)}</span>`}<span>${escapeHtml(label)}</span></button>`;
  }).join('');
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
      if (data.authenticated) location.href = '/site-admin/';
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
          location.href = '/site-admin/';
        } else if (mode === 'login-verify') {
          await api('/site-admin-auth/login/verify', { challengeId, code: fd.get('code') });
          location.href = '/site-admin/';
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

function renderAdminPage(bootstrap) {
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
    .top-button:disabled, .save-current:disabled, .ghost:disabled, .danger:disabled { opacity: .58; cursor: not-allowed; filter: grayscale(.12); }
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
    .picker { display: grid; grid-template-columns: 150px 1fr; gap: 12px; } .select-list { display: grid; gap: 8px; align-content: start; }
    .select-list button { min-height: 38px; border-radius: 8px; border: 1px solid #d6e1ef; background: #fff; text-align: left; padding: 8px 10px; font-weight: 800; }
    .select-list button.active { border-color: #0666d8; color: #064b9f; background: #eef7ff; }
    .media-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; } .media-item { border: 1px solid #dbe5f1; border-radius: 10px; overflow: hidden; background: #fff; }
    .media-item img, .media-item video { width: 100%; aspect-ratio: 1.5; object-fit: cover; display: block; background: #eef3f8; } .media-item div { padding: 9px; font-size: 12px; color: #5f6f86; word-break: break-all; }
    .error-panel { position: fixed; left: 300px; right: 24px; bottom: 22px; z-index: 50; display: none; padding: 14px 16px; border: 1px solid #ffd1d1; border-radius: 10px; color: #941b1b; background: #fff4f4; font-weight: 800; box-shadow: 0 12px 30px rgba(100,20,20,.12); }
    .error-panel.show { display: block; }
  </style>
</head>
<body>
  <div class="app">
    <aside class="sidebar">
      <div class="brand"><img src="/assets/astreva-logo-transparent.png" alt="星渡" /><span>星渡官网后台</span></div>
      <nav class="nav" id="nav">${navMarkup()}</nav>
    </aside>
    <header class="topbar">
      <div class="status"><span class="saved" id="saved">✓ 已保存</span><span class="pending" id="dirty">● 待发布</span><span class="build" id="build">重建状态读取中</span></div>
      <div class="actions"><button class="top-button" id="previewOpen">预览官网</button><button class="top-button" id="saveTop">保存并更新前台</button><button class="top-button primary" id="publish">发布官网</button><span class="avatar">👤</span><strong>管理员</strong><button class="top-button" id="logout">退出</button></div>
    </header>
    <section class="preview-pane">
      <div class="device-tabs"><button class="active" data-device="desktop">▣ 桌面</button><button data-device="tablet">▯ 平板</button><button data-device="mobile">▯ 手机</button></div>
      <div class="preview-frame-wrap" id="frameWrap"><iframe id="preview" src="${SITE_ORIGIN}/"></iframe></div>
    </section>
    <main class="editor" id="editor"><h1>正在编辑：首页首屏</h1><div class="rule"></div><p>正在加载内容...</p></main>
  </div>
  <div class="error-panel" id="errorPanel"></div>
  <script>window.__BOOTSTRAP__=${safeJson(bootstrap)}; window.__SITE_ORIGIN__=${safeJson(SITE_ORIGIN)};</script>
  <script>
    const state = { data: window.__BOOTSTRAP__, content: window.__BOOTSTRAP__.content, active: 'homeHero', dirty: false, busy: false, selected: { category: 0, product: 0, faq: 0, staticPage: 'about', lead: 0 }, previewPath: '/#home-hero' };
    const editor = document.querySelector('#editor');
    const nav = document.querySelector('#nav');
    const preview = document.querySelector('#preview');
    const frameWrap = document.querySelector('#frameWrap');
    const errorPanel = document.querySelector('#errorPanel');
    const build = document.querySelector('#build');
    const saved = document.querySelector('#saved');
    const dirty = document.querySelector('#dirty');
    function showError(error) { errorPanel.textContent = error?.message || String(error); errorPanel.classList.add('show'); }
    function clearError() { errorPanel.textContent = ''; errorPanel.classList.remove('show'); }
    function syncBusy() { document.querySelectorAll('#saveTop,#publish,#saveCurrent,[data-delete]').forEach((button) => { button.disabled = state.busy; }); }
    window.onerror = (_m, _s, _l, _c, error) => showError(error || _m);
    window.onunhandledrejection = (event) => showError(event.reason || '后台运行错误');
    const esc = (v) => String(v ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#039;'}[c]));
    const byPath = (obj, path) => path.split('.').reduce((acc, key) => acc?.[key], obj);
    function setPath(obj, path, value) { const keys = path.split('.'); let cur = obj; keys.slice(0, -1).forEach((key) => { cur[key] ||= {}; cur = cur[key]; }); cur[keys.at(-1)] = value; markDirty(); }
    function markDirty() { state.dirty = true; saved.textContent = '未保存'; dirty.textContent = '● 待发布'; }
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
    function render() {
      nav.querySelectorAll('button').forEach((btn) => btn.classList.toggle('active', btn.dataset.nav === state.active));
      const c = state.content;
      if (state.active === 'homeHero') editor.innerHTML = title('首页首屏') + field('眉标', 'home.hero.eyebrow') + field('主标题', 'home.hero.title') + field('商标角标', 'home.hero.trademark') + text('副标题', 'home.hero.subtitle') + mediaField('背景图', 'home.hero.backgroundImage') + mediaField('背景视频', 'home.hero.backgroundVideo', 'video/*') + field('主按钮文字', 'home.hero.primaryCta') + field('主按钮链接', 'home.hero.primaryHref') + field('播放按钮文字', 'home.hero.secondaryCta') + field('播放视频链接', 'home.hero.videoUrl') + field('视频弹窗标题', 'home.hero.videoTitle') + field('视频占位提示', 'home.hero.videoPreviewLabel') + field('外部打开按钮文字', 'home.hero.videoOpenExternalLabel') + field('关闭按钮文字', 'home.hero.videoCloseLabel') + field('合作伙伴标签', 'home.partnerLabel') + field('SEO 标题', 'home.seo.title') + text('SEO 描述', 'home.seo.description') + field('SEO 关键词', 'home.seo.keywords') + field('SEO 分享图', 'home.seo.image') + saveButton() + '</div>';
      else if (state.active === 'homeProducts') editor.innerHTML = title('首页产品区') + field('产品区标题', 'home.featuredProducts.title') + text('产品区描述', 'home.featuredProducts.description') + field('产品卡片链接文字', 'home.featuredProducts.linkLabel') + field('产品卡片链接图标', 'home.featuredProducts.linkIcon') + listText('合作伙伴', 'home.partners') + field('产品介绍标题', 'home.productIntro.title') + text('产品介绍描述', 'home.productIntro.description', 5) + mediaField('产品介绍图片', 'home.productIntro.image', 'image/*') + listText('产品介绍要点', 'home.productIntro.bullets') + field('产品介绍要点图标', 'home.productIntro.bulletIcon') + field('技术规格按钮文字', 'home.productIntro.ctaLabel') + field('技术规格按钮链接', 'home.productIntro.ctaHref') + saveButton() + '</div>';
      else if (state.active === 'homeAdvantages') editor.innerHTML = title('首页优势') + field('优势区标题', 'home.advantagesIntro.title') + text('优势区描述', 'home.advantagesIntro.description') + objectList('优势卡片', 'home.advantages', [['title','标题'],['description','描述','textarea'],['icon','图标代号']]) + saveButton() + '</div>';
      else if (state.active === 'homeTestimonials') editor.innerHTML = title('客户评价') + field('区域标题', 'home.testimonialsTitle') + objectList('评价列表', 'home.testimonials', [['name','姓名'],['role','身份'],['text','评价内容','textarea']]) + saveButton() + '</div>';
      else if (state.active === 'homeCta') editor.innerHTML = title('首页行动区') + field('标题', 'home.cta.title') + text('描述', 'home.cta.description') + field('主按钮', 'home.cta.primaryLabel') + field('主按钮链接', 'home.cta.primaryHref') + field('次按钮', 'home.cta.secondaryLabel') + field('次按钮链接', 'home.cta.secondaryHref') + mediaField('背景图', 'home.cta.backgroundImage') + saveButton() + '</div>';
      else if (state.active === 'productCenter') editor.innerHTML = title('产品中心') + field('首屏标题', 'productCenter.hero.title') + text('首屏描述', 'productCenter.hero.description') + mediaField('首屏背景图', 'productCenter.hero.backgroundImage') + mediaField('首屏背景视频', 'productCenter.hero.backgroundVideo', 'video/*') + field('选择区标题', 'productCenter.intro.title') + text('选择区描述', 'productCenter.intro.description') + field('品类按钮文字', 'productCenter.intro.categoryCtaLabel') + field('品类按钮图标', 'productCenter.categoryPage.categoryCtaIcon') + field('品类页标题模板', 'productCenter.categoryPage.titleTemplate') + field('返回按钮文字', 'productCenter.categoryPage.backLabel') + field('返回按钮图标', 'productCenter.categoryPage.backIcon') + field('产品悬浮按钮文字', 'productCenter.categoryPage.productHoverLabel') + field('面包屑分隔符', 'productCenter.detailPage.breadcrumbSeparator') + field('详情页徽标', 'productCenter.detailPage.badgeLabel') + field('详情页徽标图标', 'productCenter.detailPage.badgeIcon') + field('卖点列表图标', 'productCenter.detailPage.featureIcon') + field('报价按钮文字', 'productCenter.detailPage.quoteButtonLabel') + field('报价按钮图标', 'productCenter.detailPage.quoteButtonIcon') + field('下载按钮图标', 'productCenter.detailPage.downloadButtonIcon') + field('规格区标题', 'productCenter.detailPage.specsTitle') + text('规格区描述', 'productCenter.detailPage.specsDescription') + field('规格左侧图标', 'productCenter.detailPage.specsPrimaryIcon') + field('规格左侧标题', 'productCenter.detailPage.specsPrimaryTitle') + field('说明右侧图标', 'productCenter.detailPage.descriptionIcon') + field('说明右侧标题', 'productCenter.detailPage.descriptionTitle') + field('图库标题', 'productCenter.detailPage.galleryTitle') + field('视频标题', 'productCenter.detailPage.videoTitle') + field('SEO 标题', 'productCenter.seo.title') + text('SEO 描述', 'productCenter.seo.description') + field('SEO 关键词', 'productCenter.seo.keywords') + field('SEO 分享图', 'productCenter.seo.image') + saveButton() + '</div>';
      else if (state.active === 'categories') renderCollection('产品品类', c.categories, 'category', ['name','slug','description','coverImage','sortOrder']);
      else if (state.active === 'products') renderProduct();
      else if (state.active === 'support') editor.innerHTML = title('服务支持') + field('首屏标题', 'support.hero.title') + text('首屏描述', 'support.hero.description') + mediaField('首屏背景图', 'support.hero.backgroundImage') + mediaField('首屏背景视频', 'support.hero.backgroundVideo', 'video/*') + objectList('服务卡片', 'support.services', [['title','标题'],['description','描述','textarea'],['icon','图标'],['href','链接'],['cta','按钮文字']]) + field('联系标题', 'support.contact.title') + text('联系说明', 'support.contact.description') + field('电话标签', 'support.contact.phoneLabel') + field('电话图标', 'support.contact.phoneIcon') + field('邮箱标签', 'support.contact.emailLabel') + field('邮箱图标', 'support.contact.emailIcon') + field('姓名 / 公司名标签', 'support.contact.form.nameCompanyLabel') + field('姓名 / 公司名占位', 'support.contact.form.nameCompanyPlaceholder') + field('手机号 / 邮箱号标签', 'support.contact.form.contactLabel') + field('手机号 / 邮箱号占位', 'support.contact.form.contactPlaceholder') + field('提交按钮', 'support.contact.form.submitLabel') + field('提交中按钮', 'support.contact.form.submittingLabel') + field('提交成功提示', 'support.contact.form.successMessage') + field('提交失败提示', 'support.contact.form.errorMessage') + field('接口未配置提示', 'support.contact.form.missingEndpointMessage') + field('SEO 标题', 'support.seo.title') + text('SEO 描述', 'support.seo.description') + field('SEO 关键词', 'support.seo.keywords') + field('SEO 分享图', 'support.seo.image') + saveButton() + '</div>';
      else if (state.active === 'faq') renderCollection('常见问题', c.faqs, 'faq', ['question','answer','sortOrder','enabled']);
      else if (['about','solutions','projects','warranty','install','privacy','agreement','faqPage'].includes(state.active)) renderStatic(state.active === 'faqPage' ? 'faq' : state.active);
      else if (state.active === 'media') renderMedia();
      else if (state.active === 'leads') renderLeads();
      else if (state.active === 'seo') editor.innerHTML = title('SEO 设置') + field('默认 SEO 标题', 'site.defaultSeo.title') + text('默认 SEO 描述', 'site.defaultSeo.description') + field('默认分享图', 'site.defaultSeo.image') + field('默认关键词', 'site.defaultSeo.keywords') + field('首页 SEO 标题', 'home.seo.title') + text('首页 SEO 描述', 'home.seo.description') + field('首页 SEO 分享图', 'home.seo.image') + field('产品中心 SEO 标题', 'productCenter.seo.title') + text('产品中心 SEO 描述', 'productCenter.seo.description') + field('产品中心 SEO 分享图', 'productCenter.seo.image') + field('服务页 SEO 标题', 'support.seo.title') + text('服务页 SEO 描述', 'support.seo.description') + field('服务页 SEO 分享图', 'support.seo.image') + saveButton() + '</div>';
      else if (state.active === 'site') editor.innerHTML = title('全站设置') + field('品牌名', 'site.brandName') + field('品牌标语', 'site.brandTagline') + mediaField('Logo', 'site.logoUrl') + field('电话', 'site.phone') + field('邮箱', 'site.email') + field('版权文案', 'site.copyright') + field('ServiceBridge 地址', 'site.serviceBridgeUrl') + saveButton() + '</div>';
      else if (state.active === 'navigation') editor.innerHTML = title('导航文案') + field('首页', 'site.navigation.homeLabel') + field('产品中心', 'site.navigation.productsLabel') + field('解决方案', 'site.navigation.solutionsLabel') + field('工程案例', 'site.navigation.projectsLabel') + field('服务支持', 'site.navigation.supportLabel') + field('关于我们', 'site.navigation.aboutLabel') + field('下拉提示', 'site.navigation.categoryPrompt') + field('详情入口', 'site.navigation.productDetailLabel') + field('移动端菜单无障碍标签', 'site.navigation.mobileMenuLabel') + saveButton() + '</div>';
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
        : slug === 'projects'
          ? '<div class="field"><label>列表标题</label><input data-static-key="sections.introTitle" value="'+esc(page.sections?.introTitle||'')+'"></div><div class="field"><label>列表描述</label><textarea data-static-key="sections.introDescription">'+esc(page.sections?.introDescription||'')+'</textarea></div><div class="grid2"><div class="field"><label>卡片按钮文字</label><input data-static-key="sections.ctaLabel" value="'+esc(page.sections?.ctaLabel||'查看完整结项报告')+'"></div><div class="field"><label>返回按钮文字</label><input data-static-key="sections.backLabel" value="'+esc(page.sections?.backLabel||'返回案例列表')+'"></div></div><div class="grid2"><div class="field"><label>面包屑文字</label><input data-static-key="sections.breadcrumbLabel" value="'+esc(page.sections?.breadcrumbLabel||'工程案例')+'"></div><div class="field"><label>详情报告文字</label><input data-static-key="sections.reportLabel" value="'+esc(page.sections?.reportLabel||'详情报告')+'"></div></div><div class="grid2"><div class="field"><label>详情第一节标题</label><input data-static-key="sections.detailIntroTitle" value="'+esc(page.sections?.detailIntroTitle||'01. 项目背景与设计参数')+'"></div><div class="field"><label>规格卡标题</label><input data-static-key="sections.specTitle" value="'+esc(page.sections?.specTitle||'技术规格清单 (Technical Specs)')+'"></div></div><div class="grid2"><div class="field"><label>施工章节标题</label><input data-static-key="sections.processTitle" value="'+esc(page.sections?.processTitle||'02. 施工与深化过程 (Construction Process)')+'"></div><div class="field"><label>评价章节标题</label><input data-static-key="sections.reviewTitle" value="'+esc(page.sections?.reviewTitle||'03. 交付实测与评价 (Review)')+'"></div></div><div class="field"><label>底部 CTA 标题</label><input data-static-key="sections.detailCtaTitle" value="'+esc(page.sections?.detailCtaTitle||'想了解相似项目在您的场景下如何落地？')+'"></div><div class="field"><label>底部 CTA 描述</label><textarea data-static-key="sections.detailCtaDescription">'+esc(page.sections?.detailCtaDescription||'我们的资深工程顾问可为您提供 1 对 1 技术咨询与 CAD 深化图纸。')+'</textarea></div><div class="grid2"><div class="field"><label>底部 CTA 按钮</label><input data-static-key="sections.detailPrimaryLabel" value="'+esc(page.sections?.detailPrimaryLabel||'咨询该案例专家')+'"></div><div class="field"><label>底部 CTA 链接</label><input data-static-key="sections.detailPrimaryHref" value="'+esc(page.sections?.detailPrimaryHref||'/support/')+'"></div></div>'+staticObjectList('筛选按钮', 'sections.filters', [['label','筛选名称']])+staticObjectList('工程案例列表', 'sections.cases', [['id','案例 ID'],['title','案例标题'],['location','地点'],['type','项目类型'],['year','年份'],['image','图片 URL','image'],['challenge','项目挑战','textarea'],['solution','星渡方案','textarea'],['results','项目结果','textarea'],['tags','标签，逗号或换行分隔','textarea'],['detailIntroExtra','详情补充段落','textarea'],['specs','技术参数，每行：名称：值','textarea'],['processOneVisual','施工图一文字或图片 URL','image'],['processOneTitle','施工步骤一标题'],['processOneDescription','施工步骤一说明','textarea'],['processTwoVisual','施工图二文字或图片 URL','image'],['processTwoTitle','施工步骤二标题'],['processTwoDescription','施工步骤二说明','textarea'],['reviewQuote','客户评价语','textarea'],['reviewAuthor','评价人'],['metrics','交付指标，每行：数值：说明','textarea']])
          : '';
      const iconFields = ['warranty','install'].includes(slug) ? '<div class="field"><label>页面图标</label><input data-static-key="sections.icon" value="'+esc(page.sections?.icon||'')+'"></div>' : '';
      editor.innerHTML = title(page.title || slug) + '<input data-static-key="slug" type="hidden" value="'+esc(page.slug)+'"><div class="field"><label>页面标题</label><input data-static-key="title" value="'+esc(page.title||'')+'"></div><div class="field"><label>首屏眉标</label><input data-static-key="hero.eyebrow" value="'+esc(page.hero?.eyebrow||'')+'"></div><div class="field"><label>首屏标题</label><input data-static-key="hero.title" value="'+esc(page.hero?.title||'')+'"></div><div class="field"><label>首屏描述</label><textarea data-static-key="hero.description">'+esc(page.hero?.description||'')+'</textarea></div>'+staticMediaField('首屏背景图', 'hero.backgroundImage', 'image/*')+staticMediaField('首屏背景视频', 'hero.backgroundVideo', 'video/*')+aboutFields+solutionFields+iconFields+'<div class="field"><label>正文内容</label><textarea data-static-key="body" rows="10">'+esc(page.body||'')+'</textarea><p class="small">可输入正文文本，换行会保留；需要加粗或小标题时也可以使用少量 HTML。</p></div><div class="field"><label>SEO 标题</label><input data-static-key="seo.title" value="'+esc(page.seo?.title||'')+'"></div><div class="field"><label>SEO 描述</label><textarea data-static-key="seo.description">'+esc(page.seo?.description||'')+'</textarea></div><div class="field"><label>SEO 关键词</label><input data-static-key="seo.keywords" value="'+esc(page.seo?.keywords||'')+'"></div><div class="field"><label>SEO 分享图</label><input data-static-key="seo.image" value="'+esc(page.seo?.image||'')+'"></div>'+saveButton()+'</div>';
    }
    function renderMedia() { editor.innerHTML = title('媒体库') + '<div class="field"><label>上传媒体</label><input type="file" data-upload-media accept="image/*,video/*,.pdf,.zip,.dwg,.dxf"></div><div class="media-grid">'+(state.data.media||[]).map(m=>'<div class="media-item">'+(m.type==='video'?'<video src="'+esc(m.url)+'" controls></video>':m.type==='image'?'<img src="'+esc(m.url)+'" alt="">':'<div style="height:130px;display:grid;place-items:center;background:#eef3f8">文件</div>')+'<div>'+esc(m.name)+'<br><button class="ghost" data-copy="'+esc(m.url)+'">复制地址</button></div></div>').join('')+'</div></div>'; }
    function renderLeads() { const leads = state.data.leads||[]; const rows = leads.map((lead,i)=>'<div class="card"><div class="card-head"><span>'+esc(lead.name||'未命名')+' · '+esc(lead.contactInfo||lead.phone||lead.email||'')+'</span><button class="ghost" data-select-lead="'+i+'">编辑</button></div><p class="small">'+esc(lead.email||lead.phone||'')+'<br>'+esc(lead.message||'')+'</p></div>').join(''); const lead = leads[state.selected.lead] || {}; editor.innerHTML = title('客户咨询') + rows + '<div class="rule"></div><input data-lead-key="id" type="hidden" value="'+esc(lead.id||'')+'"><div class="grid2"><div class="field"><label>状态</label><input data-lead-key="status" value="'+esc(lead.status||'新咨询')+'"></div><div class="field"><label>优先级</label><input data-lead-key="priority" value="'+esc(lead.priority||'普通')+'"></div></div><div class="field"><label>内部备注</label><textarea data-lead-key="internalNotes">'+esc(lead.internalNotes||'')+'</textarea></div>'+saveButton()+'</div>'; }
    function sectionForActive() { if (state.active.startsWith('home')) return ['home', state.content.home]; if (state.active === 'productCenter') return ['productCenter', state.content.productCenter]; if (state.active === 'support') return ['support', state.content.support]; if (['seo','site','navigation','footer','social','cookie','chat'].includes(state.active)) return ['site', state.content.site]; if (['about','solutions','projects','warranty','install','privacy','agreement','faqPage'].includes(state.active)) return ['staticPage', state.content.staticPages.find(p => p.slug === state.selected.staticPage)]; return [state.active, null]; }
    function collectItem(section) { const data = {}; editor.querySelectorAll('[data-item-section="'+section+'"]').forEach(el => { let value = el.type === 'checkbox' ? el.checked : el.value; if (['sortOrder'].includes(el.dataset.key)) value = Number(value||0); if (['galleryImages','features'].includes(el.dataset.key)) value = value.split('\\n').map(s=>s.trim()).filter(Boolean); if (el.dataset.key === 'specifications') value = value.split('\\n').map(s=>s.trim()).filter(Boolean).map(s=>{const [label,...rest]=s.split(/[：:]/); return {label:label||'', value:rest.join('：')||''};}); if (el.dataset.key === 'downloads') value = value.split('\\n').map(s=>s.trim()).filter(Boolean).map(s=>{const [label,...rest]=s.split(/[：:]/); return {label:label||'', url:rest.join(':')||''};}); setNested(data, el.dataset.key, value); }); return data; }
    function setNested(obj, path, value) { const keys = path.split('.'); let cur = obj; keys.slice(0,-1).forEach(k=>{cur[k] ||= {}; cur = cur[k];}); cur[keys.at(-1)] = value; }
    async function uploadAsset(file) { const fd = new FormData(); fd.append('file', file); const res = await fetch('/site-admin-api/upload', { method:'POST', body: fd }); const data = await res.json(); if (!res.ok) throw new Error(data.error?.message || '上传失败'); state.data.media = (await (await fetch('/site-admin-api/media')).json()).data; return data.data; }
    async function upload(file) { return (await uploadAsset(file)).url; }
    async function saveCurrent(options = {}) {
      if (state.busy) return;
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
        const res = await fetch('/site-admin-api/save', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ section, data }) });
        const payload = await res.json(); if (!res.ok) throw new Error(payload.error?.message || '保存失败');
        state.data = payload.data; state.content = payload.data.content; state.dirty = false; saved.textContent='✓ 已保存'; dirty.textContent='● 正在同步'; render();
        if (shouldPublish && section !== 'lead') await publishSite();
        else refreshPreview();
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
    async function publishSite() {
      build.textContent = '发布中';
      const res = await fetch('/site-admin-api/publish', { method:'POST' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error?.message || '发布失败');
      build.textContent = '构建中';
      await waitForBuild();
      dirty.textContent='● 已同步';
      refreshPreview();
    }
    editor.addEventListener('input', (event) => { const el = event.target; if (el.dataset.path) setPath(state.content, el.dataset.path, el.type==='checkbox'?el.checked:el.value); if (el.dataset.listPath) setPath(state.content, el.dataset.listPath, el.value.split('\\n').map(s=>s.trim()).filter(Boolean)); });
    editor.addEventListener('change', async (event) => { const el = event.target; if (el.dataset.uploadTarget && el.files[0]) setPath(state.content, el.dataset.uploadTarget, await upload(el.files[0])); if (el.dataset.itemUpload && el.files[0]) { const input = editor.querySelector('[data-key="'+el.dataset.itemUpload+'"]'); input.value = await upload(el.files[0]); input.dispatchEvent(new Event('input',{bubbles:true})); } if ('itemGalleryUpload' in el.dataset && el.files?.length) { const input = editor.querySelector('[data-key="galleryImages"]'); const urls=[]; for (const file of el.files) urls.push(await upload(file)); input.value = [input.value, ...urls].filter(Boolean).join('\\n'); input.dispatchEvent(new Event('input',{bubbles:true})); } if ('itemDownloadUpload' in el.dataset && el.files[0]) { const asset = await uploadAsset(el.files[0]); const input = editor.querySelector('[data-key="downloads"]'); input.value = [input.value, (asset.name || '下载资料') + '：' + asset.url].filter(Boolean).join('\\n'); input.dispatchEvent(new Event('input',{bubbles:true})); } if (el.dataset.staticUpload && el.files[0]) { const input = editor.querySelector('[data-static-key="'+el.dataset.staticUpload+'"]'); input.value = await upload(el.files[0]); input.dispatchEvent(new Event('input',{bubbles:true})); } if (el.dataset.staticObjectUploadKey && el.files[0]) { const page=selectedStaticPage(); const box=el.closest('[data-static-object-list]'); const arr=byPath(page,box.dataset.staticObjectList)||[]; const index=Number(el.dataset.staticObjectUploadIndex); arr[index] ||= {}; arr[index][el.dataset.staticObjectUploadKey]=await upload(el.files[0]); setNested(page,box.dataset.staticObjectList,arr); markDirty(); render(); } if ('uploadMedia' in el.dataset && el.files[0]) { await upload(el.files[0]); renderMedia(); } });
    editor.addEventListener('click', async (event) => { const btn = event.target.closest('button'); if (!btn) return; if (btn.id === 'saveCurrent') saveCurrent().catch(showError); if (btn.dataset.select) { state.selected[btn.dataset.select] = Number(btn.dataset.index); render(); const nextPreview = selectedPreview(btn.dataset.select); if (nextPreview) refreshPreview(nextPreview); } if (btn.dataset.selectLead) { state.selected.lead = Number(btn.dataset.selectLead); renderLeads(); refreshPreview('/support/#support-contact'); } if (btn.dataset.new) { const type=btn.dataset.new; if(type==='category') state.content.categories.push({id:'',name:'新产品品类',slug:'',description:'',coverImage:'',heroVideo:'',sortOrder:0,seo:{title:'',description:''}}); if(type==='product') state.content.products.push({id:'',name:'新产品',slug:'',categorySlug:state.content.categories[0]?.slug||'',coverImage:'',galleryImages:[],videoUrl:'',summary:'',features:[],specifications:[],downloads:[],body:'',isFeatured:false,sortOrder:0,seo:{title:'',description:''}}); state.selected[type] = state.content[type === 'category' ? 'categories' : 'products'].length - 1; render(); markDirty(); refreshPreview(previewForNav(state.active, state.previewPath)); } if (btn.dataset.delete) { if (state.busy) return; state.busy = true; clearError(); syncBusy(); try { const type=btn.dataset.delete; const data=collectItem(type); data._delete=true; await fetch('/site-admin-api/save',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({section:type,data})}); const boot = await (await fetch('/site-admin-api/bootstrap')).json(); state.data=boot.data; state.content=boot.data.content; state.selected[type]=0; render(); await publishSite(); } finally { state.busy = false; syncBusy(); } } if (btn.dataset.copy) navigator.clipboard?.writeText(btn.dataset.copy); if (btn.dataset.addPair) { const arr=byPath(state.content,btn.dataset.addPair)||[]; arr.push({label:'',href:''}); setPath(state.content,btn.dataset.addPair,arr); render(); } if (btn.dataset.removePair) { const box=btn.closest('[data-pair-list]'); const arr=byPath(state.content,box.dataset.pairList)||[]; arr.splice(Number(btn.dataset.removePair),1); setPath(state.content,box.dataset.pairList,arr); render(); } if (btn.dataset.addObject) { const arr=byPath(state.content,btn.dataset.addObject)||[]; arr.push({title:'',description:'',icon:''}); setPath(state.content,btn.dataset.addObject,arr); render(); } if (btn.dataset.removeObject) { const box=btn.closest('[data-object-list]'); const arr=byPath(state.content,box.dataset.objectList)||[]; arr.splice(Number(btn.dataset.removeObject),1); setPath(state.content,box.dataset.objectList,arr); render(); } if (btn.dataset.addStaticObject) { const page=selectedStaticPage(); const arr=byPath(page,btn.dataset.addStaticObject)||[]; arr.push({}); setNested(page,btn.dataset.addStaticObject,arr); markDirty(); render(); } if (btn.dataset.removeStaticObject) { const page=selectedStaticPage(); const box=btn.closest('[data-static-object-list]'); const arr=byPath(page,box.dataset.staticObjectList)||[]; arr.splice(Number(btn.dataset.removeStaticObject),1); setNested(page,box.dataset.staticObjectList,arr); markDirty(); render(); } });
    editor.addEventListener('input', (event) => { const el = event.target; const pairBox = el.closest('[data-pair-list]'); if (pairBox && el.dataset.pairIndex) { const arr=byPath(state.content,pairBox.dataset.pairList)||[]; arr[Number(el.dataset.pairIndex)] ||= {}; arr[Number(el.dataset.pairIndex)][el.dataset.pairKey]=el.value; markDirty(); } const objBox = el.closest('[data-object-list]'); if (objBox && el.dataset.objectIndex) { const arr=byPath(state.content,objBox.dataset.objectList)||[]; arr[Number(el.dataset.objectIndex)] ||= {}; arr[Number(el.dataset.objectIndex)][el.dataset.objectKey]=el.value; markDirty(); } const staticObjBox = el.closest('[data-static-object-list]'); if (staticObjBox && el.dataset.staticObjectIndex) { const page=selectedStaticPage(); const arr=byPath(page,staticObjBox.dataset.staticObjectList)||[]; arr[Number(el.dataset.staticObjectIndex)] ||= {}; arr[Number(el.dataset.staticObjectIndex)][el.dataset.staticObjectKey]=el.value; setNested(page,staticObjBox.dataset.staticObjectList,arr); markDirty(); } if (el.dataset.staticKey) { const page=state.content.staticPages.find(p=>p.slug===state.selected.staticPage); setNested(page, el.dataset.staticKey, el.value); markDirty(); } });
    nav.addEventListener('click', (event) => { const btn = event.target.closest('[data-nav]'); if (!btn) return; state.active = btn.dataset.nav; refreshPreview(previewForNav(btn.dataset.nav, btn.dataset.preview || '/')); render(); });
    document.querySelector('#saveTop').addEventListener('click', () => saveCurrent().catch(showError));
    document.querySelector('#previewOpen').addEventListener('click', () => window.open(buildPreviewUrl(state.previewPath || '/'), '_blank'));
    document.querySelector('#publish').addEventListener('click', () => saveCurrent().catch(showError));
    document.querySelector('#logout').addEventListener('click', async () => { await fetch('/site-admin-auth/logout',{method:'POST'}); location.href='/admin-2fa'; });
    document.querySelector('.device-tabs').addEventListener('click', (event) => { const btn=event.target.closest('[data-device]'); if(!btn) return; document.querySelectorAll('[data-device]').forEach(b=>b.classList.toggle('active',b===btn)); frameWrap.className='preview-frame-wrap '+(btn.dataset.device==='desktop'?'':btn.dataset.device); });
    render(); refreshPreview(state.previewPath); refreshStatus().catch(()=>{}); setInterval(() => refreshStatus().catch(()=>{}), 8000);
  </script>
</body>
</html>`;
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

async function handleAuth(req, res, pathname) {
  if (pathname === '/site-admin-auth/status' && req.method === 'GET') {
    const users = await readUsers();
    json(res, 200, { hasAdmin: users.users.length > 0, authenticated: Boolean(verifySession(req)) });
    return true;
  }
  if (pathname === '/site-admin-auth/setup/start' && req.method === 'POST') {
    const users = await readUsers();
    if (users.users.length) {
      json(res, 409, { error: { message: '管理员账号已存在，请直接登录。' } });
      return true;
    }
    const body = await bodyJson(req);
    const email = String(body.email || '').trim().toLowerCase();
    const password = String(body.password || '');
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      json(res, 400, { error: { message: '请输入有效邮箱。' } });
      return true;
    }
    if (password.length < 8) {
      json(res, 400, { error: { message: '密码至少 8 位。' } });
      return true;
    }
    const secret = generateTotpSecret();
    const challengeId = crypto.randomUUID();
    pendingSetups.set(challengeId, { email, passwordHash: await bcrypt.hash(password, 12), secret, exp: Date.now() + 10 * 60 * 1000 });
    json(res, 200, { challengeId, secret, qr: await QRCode.toDataURL(otpAuthUrl(email, secret), { margin: 1, width: 240 }) });
    return true;
  }
  if (pathname === '/site-admin-auth/setup/verify' && req.method === 'POST') {
    const body = await bodyJson(req);
    const challenge = pendingSetups.get(body.challengeId);
    if (!challenge || challenge.exp < Date.now()) {
      json(res, 400, { error: { message: '绑定已过期，请重新开始。' } });
      return true;
    }
    if (!verifyTotp(challenge.secret, body.code)) {
      json(res, 400, { error: { message: '2FA 验证码不正确。' } });
      return true;
    }
    const user = { id: crypto.randomUUID(), email: challenge.email, passwordHash: challenge.passwordHash, totpSecret: challenge.secret, createdAt: new Date().toISOString() };
    await saveUsers({ users: [user] });
    pendingSetups.delete(body.challengeId);
    setSessionCookie(res, user);
    json(res, 200, { ok: true });
    return true;
  }
  if (pathname === '/site-admin-auth/login/start' && req.method === 'POST') {
    const body = await bodyJson(req);
    const users = await readUsers();
    const user = users.users.find((item) => item.email === String(body.email || '').trim().toLowerCase());
    if (!user || !(await bcrypt.compare(String(body.password || ''), user.passwordHash))) {
      json(res, 401, { error: { message: '邮箱或密码错误。' } });
      return true;
    }
    const challengeId = crypto.randomUUID();
    pendingLogins.set(challengeId, { userId: user.id, exp: Date.now() + 10 * 60 * 1000 });
    json(res, 200, { challengeId });
    return true;
  }
  if (pathname === '/site-admin-auth/login/verify' && req.method === 'POST') {
    const body = await bodyJson(req);
    const challenge = pendingLogins.get(body.challengeId);
    if (!challenge || challenge.exp < Date.now()) {
      json(res, 400, { error: { message: '登录验证已过期，请重新登录。' } });
      return true;
    }
    const users = await readUsers();
    const user = users.users.find((item) => item.id === challenge.userId);
    if (!user || !verifyTotp(user.totpSecret, body.code)) {
      json(res, 400, { error: { message: '2FA 验证码不正确。' } });
      return true;
    }
    pendingLogins.delete(body.challengeId);
    setSessionCookie(res, user);
    json(res, 200, { ok: true });
    return true;
  }
  if (pathname === '/site-admin-auth/logout' && req.method === 'POST') {
    clearSessionCookie(res);
    json(res, 200, { ok: true });
    return true;
  }
  return false;
}

async function handleAdminApi(req, res, pathname) {
  if (pathname === '/site-admin-api/public-content' && req.method === 'GET') {
    json(res, 200, { data: await readContent(), meta: { generatedAt: new Date().toISOString() } });
    return true;
  }
  if (pathname === '/site-admin-api/leads' && req.method === 'POST') {
    const body = await bodyJson(req);
    const data = body.data || body;
    if (String(data.website || '').trim()) {
      json(res, 200, { ok: true });
      return true;
    }
    const store = await readLeads();
    const lead = {
      id: `lead_${crypto.randomUUID()}`,
      name: data.name || '',
      companyName: data.companyName || '',
      contactInfo: data.contactInfo || '',
      phone: data.phone || '',
      email: data.email || '',
      message: data.message || '',
      sourcePage: data.sourcePage || '',
      sourceUrl: data.sourceUrl || '',
      referrer: data.referrer || '',
      utm: data.utm || {},
      status: '新咨询',
      priority: '普通',
      internalNotes: '',
      submittedAt: data.submittedAt || new Date().toISOString()
    };
    store.leads.unshift(lead);
    await saveLeads(store);
    json(res, 201, { data: lead });
    return true;
  }
  if (!verifySession(req)) {
    json(res, 401, { error: { message: '请先登录。' } });
    return true;
  }
  if (pathname === '/site-admin-api/bootstrap' && req.method === 'GET') {
    json(res, 200, { data: await getBootstrap(), meta: { previewUrl: SITE_ORIGIN } });
    return true;
  }
  if (pathname === '/site-admin-api/save' && req.method === 'POST') {
    try {
      const saved = await saveSection(await bodyJson(req));
      json(res, 200, { data: await getBootstrap(), meta: { savedAt: new Date().toISOString(), saved } });
    } catch (error) {
      json(res, 400, { error: { message: error.message || '保存失败。' } });
    }
    return true;
  }
  if (pathname === '/site-admin-api/upload' && req.method === 'POST') {
    try {
      await handleUpload(req, res);
    } catch (error) {
      json(res, 400, { error: { message: error.message || '上传失败。' } });
    }
    return true;
  }
  if (pathname === '/site-admin-api/media' && req.method === 'GET') {
    json(res, 200, { data: await readMediaLibrary() });
    return true;
  }
  if (pathname === '/site-admin-api/status' && req.method === 'GET') {
    json(res, 200, { data: await statusPayload() });
    return true;
  }
  if (pathname === '/site-admin-api/publish' && req.method === 'POST') {
    try {
      json(res, 202, { data: { ok: true, rebuild: await triggerRebuild() } });
    } catch (error) {
      json(res, 503, { error: { message: `重建服务未启动或发布失败：${error.message}` } });
    }
    return true;
  }
  return false;
}

const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url || '/', PUBLIC_ORIGIN);
    const pathname = decodeURIComponent(url.pathname);

    if (pathname.startsWith('/assets/')) {
      const file = path.join(ROOT, 'frontend', 'public', pathname);
      return serveFile(res, file, true);
    }
    if (pathname.startsWith('/uploads/site-admin/')) {
      const file = path.join(ROOT, pathname);
      if (!file.startsWith(UPLOAD_DIR)) return send(res, 403, 'forbidden');
      return serveFile(res, file, false);
    }
    if (await handleAuth(req, res, pathname)) return;
    if (pathname.startsWith('/site-admin-api/') && await handleAdminApi(req, res, pathname)) return;
    if (pathname === '/admin-2fa' || pathname === '/admin-2fa/') {
      send(res, 200, renderLoginPage(), { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return;
    }
    if (pathname === '/site-admin' || pathname === '/site-admin/') {
      if (!verifySession(req)) return redirect(res, '/admin-2fa');
      const boot = await getBootstrap();
      send(res, 200, renderAdminPage(boot), { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return;
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
    send(res, 404, 'not found');
  } catch (error) {
    console.error(error);
    json(res, 500, { error: { message: error.message || 'server error' } });
  }
});

await ensureDirs();
await readContent();
server.listen(PORT, HOST, () => {
  console.log(`Astreva admin server listening on http://${HOST}:${PORT}`);
  console.log(`Login: http://${HOST}:${PORT}/admin-2fa`);
  console.log(`Site admin: http://${HOST}:${PORT}/site-admin/`);
});
