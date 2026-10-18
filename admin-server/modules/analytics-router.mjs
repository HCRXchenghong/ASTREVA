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
  visitorActivityForwarder = null
}) {
  const rateWindows = new Map();

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
        const data = await platformStore.analyticsSummary({
          tz: url.searchParams.get('tz') || timezoneFromRequest(req),
          month: url.searchParams.get('month') || ''
        });
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
