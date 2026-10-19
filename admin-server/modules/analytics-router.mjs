import crypto from 'node:crypto';
import { parseUserAgent } from './user-agent.mjs';

const BEACON_BODY_LIMIT = 8 * 1024;
const BEACON_RATE_WINDOW_MS = 60 * 1000;
const BEACON_RATE_MAX = Number(process.env.ADMIN_ANALYTICS_RATE_MAX || 120);
const ALLOWED_EVENTS = new Set(['pageview', 'chat_open', 'lead_submit']);

// 官网埋点 → 客服引擎访客画像的服务间通道：只有配置了 INTERNAL_API_TOKEN 才会启用。
const VISITOR_ACTIVITY_PATH = '/api/internal/visitor-activity';
const VISITOR_ACTIVITY_TIMEOUT_MS = 2000;
const VISITOR_ACTIVITY_LOG_CAP = 200;

// 数据看板「今日询盘」来自客服引擎（conversations/messages），不在 CMS 库。
// 口径（用户已确认）：询盘 = 当天有访客消息的会话数；同一会话当天多条消息只计 1 次；
// 与 site_leads（官网表单提交）是两件事，绝不能混用。
const ENGINE_DASHBOARD_PATH = '/api/internal/analytics/dashboard';
const ENGINE_LOGIN_PATH = '/api/admin/login';
const ENGINE_CONVERSATIONS_PATH = '/api/admin/conversations';
const ENGINE_DASHBOARD_TIMEOUT_MS = 2500;
const ENGINE_INQUIRY_DEFINITION =
  '询盘=当天有访客消息的会话数；同一会话当天多条消息只计 1 次；不含官网表单提交。';

/**
 * 访客活跃转发的 fire-and-forget 发送器。
 *
 * - 仅在同时配置了 `SERVICEBRIDGE_API_URL` 与 `INTERNAL_API_TOKEN` 时启用；
 *   token 缺失时 `forward()` 直接跳过（不抛错、不发请求），符合「未配置即完全静默」。
 * - `forward()` 返回的 Promise **永不 reject**（内部已吞掉全部异常），调用方拿到后
 *   可以直接忽略；埋点响应因此在任何情况下都不会被阻塞或改写。
 * - 失败（网络错误 / 超时 / 非 2xx）只按「错误特征」去重后记一条 debug 日志，
 *   避免上游长时间不可用时刷屏。
 */
export function createVisitorActivityForwarder({
  apiUrl = '',
  token = '',
  timeoutMs = VISITOR_ACTIVITY_TIMEOUT_MS,
  logger = console
} = {}) {
  const base = String(apiUrl || '').trim().replace(/\/+$/, '');
  const internalToken = String(token || '').trim();
  const enabled = Boolean(base && internalToken);
  const loggedFailures = new Set();

  function logFailureOnce(key, message) {
    if (loggedFailures.has(key)) return;
    if (loggedFailures.size >= VISITOR_ACTIVITY_LOG_CAP) loggedFailures.clear();
    loggedFailures.add(key);
    // 只走 debug：控制台访问路径上不产生噪音。
    try {
      logger?.debug?.(`[visitor-activity] ${message}`);
    } catch {}
  }

  function forward(payload = {}) {
    if (!enabled) return Promise.resolve({ ok: false, skipped: 'disabled' });
    const visitorId = String(payload.visitorId ?? payload.visitor_id ?? '').trim();
    // 未同意 analytics 的匿名上报同样带服务端派生的 anon_*；空 visitor_id 才跳过。
    if (!visitorId) return Promise.resolve({ ok: false, skipped: 'no-visitor' });
    const path = String(payload.path || '/').trim() || '/';
    return (async () => {
      try {
        const response = await fetch(`${base}${VISITOR_ACTIVITY_PATH}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-Internal-Token': internalToken
          },
          body: JSON.stringify({ visitor_id: visitorId, path }),
          signal: AbortSignal.timeout(timeoutMs)
        });
        // 必须消费响应体，否则连接不会被释放。
        await response.text().catch(() => '');
        if (!response.ok) {
          logFailureOnce(`http-${response.status}`, `客服引擎返回 HTTP ${response.status}`);
          return { ok: false, status: response.status };
        }
        return { ok: true, status: response.status };
      } catch (error) {
        logFailureOnce(`error-${error?.name || 'Error'}`, `转发失败：${error?.message || error}`);
        return { ok: false, error: error?.message || String(error) };
      }
    })();
  }

  return { enabled, forward, timeoutMs };
}

/**
 * 匿名 UV 的根密钥。生产优先用独立配置的 `ADMIN_ANALYTICS_ANON_SALT`，
 * 未配置时依次回退到已有的服务端密钥，保证"当日盐"不是公开常量。
 */
const ANONYMOUS_SALT_SECRET = String(
  process.env.ADMIN_ANALYTICS_ANON_SALT ||
  process.env.ADMIN_2FA_SESSION_SECRET ||
  process.env.CMS_DATA_ENCRYPTION_KEY ||
  process.env.DATA_ENCRYPTION_KEY ||
  'astreva-anonymous-visitor-salt'
);
const ANONYMOUS_SALT_NAMESPACE = 'astreva-analytics-anonymous-visitor:v1';

/**
 * 匿名 UV 的 IP 归并（截断）：
 * - IPv4 只保留 /24（末段置 0）；
 * - IPv6 只保留 /64（前 4 个 hextet）；
 * - 顺手去掉端口、方括号与 zone id，避免同一地址因写法不同被算成多个访客。
 * 归并后的网段才是 HMAC 的输入；明文 IP 不会出现在 visitor_key 里。
 */
export function truncateIpForAnonymization(value) {
  let candidate = String(value || '').trim();
  if (!candidate) return 'unknown';
  // [2001:db8::1]:443 / 2001:db8::1%en0 / 203.0.113.5:8080
  candidate = candidate.replace(/^\[([^\]]+)\](?::\d+)?$/, '$1');
  candidate = candidate.replace(/%[0-9a-z._-]+$/i, '');
  const ipv4WithPort = candidate.match(/^(\d{1,3}(?:\.\d{1,3}){3}):\d+$/);
  if (ipv4WithPort) candidate = ipv4WithPort[1];
  // IPv4-mapped IPv6：按 IPv4 处理。
  candidate = candidate.replace(/^::ffff:/i, '');
  const ipv4 = candidate.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (ipv4) {
    const octets = ipv4.slice(1).map(Number);
    if (octets.every((item) => item >= 0 && item <= 255)) {
      return `${octets[0]}.${octets[1]}.${octets[2]}.0`;
    }
    return candidate;
  }
  if (candidate.includes(':')) {
    const hextets = ipv6Hextets(candidate);
    if (hextets) return hextets.slice(0, 4).join(':');
  }
  return candidate.slice(0, 64);
}

function ipv6Hextets(address) {
  const doubleColon = address.indexOf('::');
  const head = doubleColon >= 0 ? address.slice(0, doubleColon) : address;
  const tail = doubleColon >= 0 ? address.slice(doubleColon + 2) : '';
  const headParts = head ? head.split(':') : [];
  const tailParts = tail ? tail.split(':') : [];
  if (doubleColon < 0) return headParts.length === 8 ? headParts : null;
  const missing = 8 - headParts.length - tailParts.length;
  if (missing < 0) return null;
  return [...headParts, ...Array(missing).fill('0'), ...tailParts];
}

/** 当日轮换的盐：以统计日（看板时区）为输入对根密钥做一次 HMAC，跨天自然失效。 */
export function dailyAnonymousSalt(day, { secret = ANONYMOUS_SALT_SECRET } = {}) {
  return crypto.createHmac('sha256', secret).update(`${ANONYMOUS_SALT_NAMESPACE}:${day}`).digest();
}

/**
 * 匿名访客键：`anon_` + HMAC-SHA256(当日盐, 归并 IP) 的前 16 字节（hex）。
 * 同一 IP 同一天稳定、跨天不可关联；服务端不保存明文 IP 与 visitor_id 的映射。
 */
export function anonymousVisitorId(ip, { day = '', secret } = {}) {
  const salt = dailyAnonymousSalt(day || new Date().toISOString().slice(0, 10), { secret });
  const key = crypto.createHmac('sha256', salt).update(truncateIpForAnonymization(ip)).digest('hex').slice(0, 32);
  return `anon_${key}`;
}

function clean(value, maxLength) {
  return String(value || '').replace(/[\u0000-\u001f\u007f]/g, '').trim().slice(0, maxLength);
}

function safeReferrer(value) {
  const candidate = clean(value, 500);
  if (!candidate) return '';
  try {
    const url = new URL(candidate);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString().slice(0, 500) : '';
  } catch {
    return '';
  }
}

function safePath(value) {
  const candidate = clean(value, 300);
  if (!candidate.startsWith('/')) return '/';
  // 只保留路径本身：查询串与锚点不参与统计聚合。
  return candidate.split('#')[0].split('?')[0].slice(0, 300) || '/';
}

/**
 * 客服引擎看板询盘读取器。
 *
 * 询盘口径只在引擎侧成立（conversations/messages），admin-server 需要把它合并进
 * /site-admin-api/analytics/overview。读取顺序：
 * 1. 优先调用引擎的只读聚合接口 `/api/internal/analytics/dashboard`
 *    （服务间 X-Internal-Token，一次查询完成，口径注释见引擎 analytics_dashboard.go）；
 * 2. 引擎尚未升级（404/503）时退回既有的管理员接口，在 admin-server 侧聚合：
 *    `/api/admin/conversations` + `/api/conversations/{id}/messages`。
 *
 * 兼容路径会按 conversation.updated_at 先过滤（访客消息一定会刷新该字段），
 * 再分页读取消息，避免把整库消息全拉一遍。失败时返回 `{ ok:false }`，
 * 由调用方把指标显示为「未知」，绝不用 0 冒充「今天没有询盘」。
 */
export function createEngineDashboardClient({
  apiUrl = '',
  adminAccount = '',
  adminPassword = '',
  internalToken = '',
  timeoutMs = ENGINE_DASHBOARD_TIMEOUT_MS,
  fetchImpl = globalThis.fetch
} = {}) {
  let adminToken = { value: '', expiresAt: 0 };

  // 延迟读取 env：server.mjs 的 loadDotEnv() 在 import 之后才执行。
  function config() {
    return {
      base: String(apiUrl || process.env.SERVICEBRIDGE_API_URL || 'http://127.0.0.1:8080').trim().replace(/\/+$/, ''),
      account: String(adminAccount || process.env.SERVICEBRIDGE_ADMIN_ACCOUNT || 'superadmin').trim(),
      password: String(adminPassword || process.env.SERVICEBRIDGE_ADMIN_PASSWORD || '123456'),
      internalToken: String(internalToken || process.env.INTERNAL_API_TOKEN || '').trim()
    };
  }

  async function requestJson(target, options = {}) {
    const response = await fetchImpl(target, {
      ...options,
      headers: { Accept: 'application/json', ...(options.headers || {}) },
      signal: options.signal || AbortSignal.timeout(timeoutMs)
    });
    const text = await response.text().catch(() => '');
    let payload = {};
    try {
      payload = text ? JSON.parse(text) : {};
    } catch {
      payload = { message: text };
    }
    if (!response.ok) {
      const message =
        payload?.message ||
        payload?.error?.message ||
        (typeof payload?.error === 'string' ? payload.error : '') ||
        `HTTP ${response.status}`;
      const error = new Error(message);
      error.status = response.status;
      throw error;
    }
    return payload;
  }

  async function login(force = false) {
    const { base, account, password } = config();
    if (!force && adminToken.value && adminToken.expiresAt > Date.now()) return adminToken.value;
    const payload = await requestJson(`${base}${ENGINE_LOGIN_PATH}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ account, password })
    });
    if (!payload?.token) throw new Error('客服引擎未返回管理员令牌');
    adminToken = { value: payload.token, expiresAt: Date.now() + 11 * 60 * 60 * 1000 };
    return payload.token;
  }

  function adminGet(pathname, token) {
    return requestJson(`${config().base}${pathname}`, {
      headers: { Authorization: `Bearer ${token}` }
    });
  }

  function normalizeAggregate(payload) {
    const today = Number(payload?.today?.inquiries);
    const month = Number(payload?.month?.inquiries);
    if (!Number.isInteger(today) || today < 0 || !Number.isInteger(month) || month < 0) return null;
    return {
      ok: true,
      today,
      month,
      definition: String(payload?.definition || ENGINE_INQUIRY_DEFINITION),
      source: 'engine-aggregate'
    };
  }

  async function readAggregate({ tz, month }) {
    const { base, internalToken: token } = config();
    if (!token) return null;
    const query = new URLSearchParams();
    if (tz) query.set('tz', tz);
    if (month) query.set('month', month);
    const suffix = query.toString() ? `?${query}` : '';
    try {
      const payload = await requestJson(`${base}${ENGINE_DASHBOARD_PATH}${suffix}`, {
        headers: { 'X-Internal-Token': token }
      });
      return normalizeAggregate(payload);
    } catch {
      // 引擎尚未升级（404）、令牌不匹配（401/503）或暂时不可用：交给兼容聚合。
      return null;
    }
  }

  async function conversationInquiries(conversationId, token, boundaries) {
    let before = '';
    let today = false;
    let month = false;
    // 单会话最多翻 50 页（每页 100 条）；正常会话远小于这个量级。
    for (let page = 0; page < 50; page += 1) {
      const query = new URLSearchParams({ limit: '100' });
      if (before) query.set('before', before);
      const payload = await adminGet(
        `/api/conversations/${encodeURIComponent(conversationId)}/messages?${query}`,
        token
      );
      const messages = Array.isArray(payload?.messages) ? payload.messages : [];
      if (!messages.length) break;
      let oldest = Infinity;
      for (const message of messages) {
        if (message?.sender_type !== 'visitor') continue;
        const createdAt = Date.parse(message.created_at || '');
        if (!Number.isFinite(createdAt)) continue;
        if (createdAt < oldest) oldest = createdAt;
        if (createdAt >= boundaries.todayStart) today = true;
        if (createdAt >= boundaries.monthStart && createdAt < boundaries.monthEnd) month = true;
      }
      if (!payload?.has_more || !payload?.next_before) break;
      // 已经翻到早于本月首条消息的位置，再往前不可能有本月访客消息。
      if (oldest < boundaries.monthStart) break;
      before = payload.next_before;
    }
    return { today, month };
  }

  async function readViaAdminApi({ range }) {
    const todayStart = Date.parse(range?.todayStart || '');
    const monthStart = Date.parse(range?.monthStart || '');
    const monthEnd = Date.parse(range?.monthEnd || '');
    if (![todayStart, monthStart, monthEnd].every(Number.isFinite)) {
      return { ok: false, message: '缺少统计区间，无法聚合询盘。', definition: ENGINE_INQUIRY_DEFINITION };
    }
    const boundaries = { todayStart, monthStart, monthEnd };
    let token = await login(false);
    let payload;
    try {
      payload = await adminGet(ENGINE_CONVERSATIONS_PATH, token);
    } catch (error) {
      if (error?.status !== 401 && error?.status !== 403) throw error;
      token = await login(true);
      payload = await adminGet(ENGINE_CONVERSATIONS_PATH, token);
    }
    const conversations = Array.isArray(payload?.conversations) ? payload.conversations : [];
    // 访客消息会更新 conversation.updated_at：本月有访客消息的会话一定在
    // updated_at >= monthStart 的集合里，先过滤可显著减少消息请求数。
    const candidates = conversations.filter((conversation) => {
      const updatedAt = Date.parse(conversation?.updated_at || conversation?.updatedAt || '');
      return Number.isFinite(updatedAt) && updatedAt >= monthStart;
    });
    let today = 0;
    let month = 0;
    // 控制并发，避免看板轮询把引擎打满。
    for (let index = 0; index < candidates.length; index += 4) {
      const chunk = candidates.slice(index, index + 4);
      const results = await Promise.all(
        chunk.map((conversation) =>
          conversationInquiries(conversation?.id, token, boundaries).catch(() => ({ today: false, month: false }))
        )
      );
      for (const result of results) {
        if (result.today) today += 1;
        if (result.month) month += 1;
      }
    }
    return {
      ok: true,
      today,
      month,
      definition: ENGINE_INQUIRY_DEFINITION,
      // 兼容路径受引擎后台会话列表上限（当前 500 条）约束；
      // 引擎升级后优先走一次查询的聚合接口，不再有该限制。
      source: 'admin-api',
      ...(candidates.length >= 500 ? { truncated: true } : {})
    };
  }

  async function read({ tz = '', month = '', range = null } = {}) {
    try {
      const aggregate = await readAggregate({ tz, month });
      if (aggregate) return aggregate;
      return await readViaAdminApi({ range });
    } catch (error) {
      return {
        ok: false,
        message: `客服引擎询盘数据不可用：${error?.message || error}`,
        definition: ENGINE_INQUIRY_DEFINITION
      };
    }
  }

  return { read };
}

/**
 * 官网数据看板与系统日志（访问部分）。
 * - POST /site-admin-api/analytics/visit 公开埋点（管理员登录之外的唯一公开写入口）
 *   - 已同意 analytics：前端带 visitor_id/session_id，按真实标识计数 UV；
 *   - 未同意 analytics：前端只发 `anonymous:true`，服务端用「当日轮换的盐 + 归并 IP」
 *     派生 `anon_<16字节HMAC>` 作为 visitor_id。匿名 UV 只在当天去重、不含跨天识别；
 *     访问明细（IP/UA）依旧入库，供安全审计与危险识别。
 * - 其余接口都需要管理员会话
 */
export function createAnalyticsRouter({
  platformStore,
  bodyJson,
  json,
  verifySession,
  applyPublicCors,
  resolveClient,
  countryFromRequest,
  timezoneFromRequest,
  visitorActivityForwarder = null,
  engineDashboard = null
}) {
  const rateWindows = new Map();
  // 询盘来自客服引擎；注入点便于单测替换，未注入时用默认真实客户端。
  const inquiryReader = engineDashboard || createEngineDashboardClient();

  /** 统计日按看板时区切分，与 analyticsSummary 的 today 口径一致。 */
  function anonymousDayKey() {
    try {
      const tz = timezoneFromRequest?.() || 'Asia/Shanghai';
      return new Intl.DateTimeFormat('en-CA', { timeZone: tz }).format(new Date());
    } catch {
      return new Date().toISOString().slice(0, 10);
    }
  }

  function allowBeacon(req) {
    const key = resolveClient(req)?.ip || 'unknown';
    const now = Date.now();
    const current = rateWindows.get(key);
    const bucket = !current || current.resetAt <= now ? { count: 0, resetAt: now + BEACON_RATE_WINDOW_MS } : current;
    bucket.count += 1;
    rateWindows.set(key, bucket);
    if (rateWindows.size > 10000) {
      for (const [entryKey, value] of rateWindows) if (value.resetAt <= now) rateWindows.delete(entryKey);
    }
    return bucket.count <= BEACON_RATE_MAX;
  }

  async function handleBeacon(req, res) {
    if (!applyPublicCors(req, res)) {
      json(res, 403, { error: { message: '该网站来源不允许上报访问数据。' } });
      return true;
    }
    if (req.method === 'OPTIONS') {
      res.writeHead(204);
      res.end();
      return true;
    }
    if (req.method !== 'POST') {
      json(res, 405, { error: { message: '该接口仅支持 POST。' } });
      return true;
    }
    if (!allowBeacon(req)) {
      json(res, 429, { error: { message: '上报过于频繁。' } });
      return true;
    }
    try {
      const body = await bodyJson(req, BEACON_BODY_LIMIT);
      const payload = body?.data || body || {};
      const userAgent = clean(req.headers['user-agent'], 512);
      const parsed = parseUserAgent(userAgent);
      const ipInfo = resolveClient(req);
      // 未同意 analytics 的访客：前端只发匿名 pageview，不携带任何本地标识。
      // 服务端用「当日盐 + 归并 IP」派生匿名 visitor_key，仅用于当日 UV 去重；
      // 访问明细（含 IP/UA）仍按原样入库，供安全审计与危险识别使用。
      const anonymous = payload.anonymous === true;
      const visitorId = anonymous
        ? anonymousVisitorId(ipInfo.ip, { day: anonymousDayKey() })
        : clean(payload.visitor_id, 64);
      const sessionId = anonymous ? '' : clean(payload.session_id, 64);
      const targetPath = safePath(payload.path);
      await platformStore.recordVisit({
        path: targetPath,
        host: clean(payload.host || req.headers.host, 200),
        referrer: safeReferrer(payload.referrer),
        ip: ipInfo.ip,
        ipSource: ipInfo.source,
        country: countryFromRequest(req),
        userAgent,
        browser: parsed.browser,
        browserVersion: parsed.browserVersion,
        os: parsed.os,
        device: parsed.device,
        visitorId,
        sessionId,
        anonymous,
        eventType: ALLOWED_EVENTS.has(payload.event_type) ? payload.event_type : 'pageview',
        isBot: parsed.isBot
      });
      json(res, 202, { ok: true });
      // 埋点响应已经发出：转发是 fire-and-forget，超时/失败/未配置都不得影响它。
      // 匿名上报的 visitorId 是服务端派生的 anon_*，与真实 id 一样进入访客画像。
      if (visitorId) {
        try {
          visitorActivityForwarder?.forward?.({ visitorId, path: targetPath });
        } catch {}
      }
    } catch (error) {
      json(res, 400, { error: { message: error.message || '上报失败。' } });
    }
    return true;
  }

  async function handleAdmin(req, res, pathname, url) {
    if (!pathname.startsWith('/site-admin-api/analytics')) return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    try {
      if (pathname === '/site-admin-api/analytics/overview' && req.method === 'GET') {
        const tz = url.searchParams.get('tz') || timezoneFromRequest(req);
        const month = url.searchParams.get('month') || '';
        const data = await platformStore.analyticsSummary({ tz, month });
        // 询盘口径在客服引擎（有访客消息的会话数），与 CMS 的 leads（表单提交）分开。
        // 引擎不可用时把 inquiries 置为 null，由前端显示「未知」，而不是 0。
        const inquiries = await inquiryReader.read({ tz, month, range: data?.range });
        data.today = { ...data.today, inquiries: inquiries?.ok ? inquiries.today : null };
        data.month = { ...data.month, inquiries: inquiries?.ok ? inquiries.month : null };
        data.inquiry = {
          ok: Boolean(inquiries?.ok),
          definition: inquiries?.definition || ENGINE_INQUIRY_DEFINITION,
          source: inquiries?.source || 'engine',
          ...(inquiries?.ok
            ? (inquiries.truncated ? { truncated: true } : {})
            : { message: inquiries?.message || '客服引擎未返回询盘数据。' })
        };
        json(res, 200, { data });
        return true;
      }
      if (pathname === '/site-admin-api/analytics/visits' && req.method === 'GET') {
        const visits = await platformStore.listVisits({
          limit: url.searchParams.get('limit') || 100,
          ip: url.searchParams.get('ip') || '',
          visitorId: url.searchParams.get('visitor_id') || '',
          path: url.searchParams.get('path') || '',
          includeBots: url.searchParams.get('include_bots') === '1',
          since: url.searchParams.get('since') || ''
        });
        json(res, 200, { data: visits });
        return true;
      }
      if (pathname === '/site-admin-api/analytics/visitor' && req.method === 'GET') {
        const visitorId = url.searchParams.get('visitor_id') || '';
        if (!visitorId) {
          json(res, 400, { error: { message: '缺少 visitor_id。' } });
          return true;
        }
        const visits = await platformStore.listVisits({ visitorId, limit: 200, includeBots: true });
        json(res, 200, { data: visits });
        return true;
      }
      json(res, 404, { error: { message: '未知的分析接口。' } });
    } catch (error) {
      json(res, 500, { error: { message: error.message || '分析接口失败。' } });
    }
    return true;
  }

  return { handleBeacon, handleAdmin };
}

/** 危险请求特征识别：命中即写入系统日志并标红。 */
const DANGER_PATTERNS = [
  { pattern: /\.\.|%2e%2e|%252e/i, label: '目录穿越尝试' },
  { pattern: /\/(\.env|\.git|wp-admin|wp-login|phpmyadmin|adminer|\.aws|\.ssh|config\.php|backup)/i, label: '敏感路径探测' },
  { pattern: /(union\s+select|sleep\(|benchmark\(|or\s+1=1|information_schema|xp_cmdshell)/i, label: 'SQL 注入特征' },
  { pattern: /(<script|onerror=|javascript:|%3cscript)/i, label: 'XSS 特征' },
  { pattern: /\/(etc\/passwd|proc\/self|windows\/win\.ini)/i, label: '系统文件探测' },
  { pattern: /(sqlmap|nikto|nmap|masscan|acunetix|nessus|w3af)/i, label: '扫描器特征' }
];

export function detectDanger(pathname, userAgent = '') {
  const target = `${pathname} ${userAgent}`;
  for (const item of DANGER_PATTERNS) {
    if (item.pattern.test(target)) return item.label;
  }
  return '';
}

export function buildEventId() {
  return `evt_${crypto.randomUUID()}`;
}
