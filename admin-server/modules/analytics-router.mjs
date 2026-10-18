import crypto from 'node:crypto';
import { parseUserAgent } from './user-agent.mjs';

const BEACON_BODY_LIMIT = 8 * 1024;
const BEACON_RATE_WINDOW_MS = 60 * 1000;
const BEACON_RATE_MAX = Number(process.env.ADMIN_ANALYTICS_RATE_MAX || 120);
const ALLOWED_EVENTS = new Set(['pageview', 'chat_open', 'lead_submit']);

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
  timezoneFromRequest
}) {
  const rateWindows = new Map();

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
      await platformStore.recordVisit({
        path: safePath(payload.path),
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
        visitorId: clean(payload.visitor_id, 64),
        sessionId: clean(payload.session_id, 64),
        eventType: ALLOWED_EVENTS.has(payload.event_type) ? payload.event_type : 'pageview',
        isBot: parsed.isBot
      });
      json(res, 202, { ok: true });
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
