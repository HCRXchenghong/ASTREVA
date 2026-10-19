import fs from 'node:fs/promises';
import os from 'node:os';

function bytes(value) {
  const size = Number(value);
  return Number.isFinite(size) && size >= 0 ? size : 0;
}

async function statfsInfo(target) {
  try {
    const info = await fs.statfs(target);
    const total = bytes(info.blocks) * bytes(info.bsize);
    const free = bytes(info.bavail) * bytes(info.bsize);
    return { path: target, totalBytes: total, freeBytes: free, usedBytes: Math.max(total - free, 0) };
  } catch (error) {
    return { path: target, totalBytes: 0, freeBytes: 0, usedBytes: 0, error: error.message };
  }
}

async function readCgroupFile(candidates) {
  for (const file of candidates) {
    try {
      const raw = await fs.readFile(file, 'utf8');
      const value = Number(String(raw).trim());
      if (Number.isFinite(value) && value > 0) return value;
    } catch {
      /* 继续尝试下一个路径 */
    }
  }
  return 0;
}

function cpuSnapshot() {
  const cpus = os.cpus();
  let idle = 0;
  let total = 0;
  for (const cpu of cpus) {
    for (const [key, value] of Object.entries(cpu.times)) {
      total += value;
      if (key === 'idle') idle += value;
    }
  }
  return { idle, total, cores: cpus.length };
}

/** 服务器与依赖服务健康度：控制台数据看板用。 */
export function createSystemRouter({
  platformStore,
  json,
  bodyJson,
  verifySession,
  auditEvent,
  storagePaths = [],
  serviceBridgeStatus,
  rebuildStatus,
  storageHealth,
  aiCapability,
  translationSettings,
  serviceBridgeAudit,
  eventRecorder
}) {
  /** 客服引擎的审计事件映射成系统日志条目（服务器与业务统一展示）。 */
  function auditCategoryFor(action) {
    const value = String(action || '');
    if (/login|logout|password|token|session/i.test(value)) return 'auth';
    if (/api|key|openapi/i.test(value)) return 'api';
    if (/disable|delete|reset|revoke|kick|force/i.test(value)) return 'security';
    if (/conversation|message|transfer|close|rating|agent/i.test(value)) return 'admin';
    return 'server';
  }

  function auditLevelFor(action) {
    const value = String(action || '');
    if (/invalid|blocked|denied|rejected|rate_limit|brute/i.test(value)) return 'danger';
    if (/disable|delete|reset|revoke|kick|force|failed/i.test(value)) return 'warning';
    return 'info';
  }

  async function bridgeAuditEntries(limit) {
    if (typeof serviceBridgeAudit !== 'function') return [];
    try {
      const payload = await serviceBridgeAudit();
      const list = Array.isArray(payload) ? payload : payload?.events || payload?.data || [];
      return list.slice(0, limit).map((event) => ({
        id: `sbaudit_${event.id || `${event.created_at}_${event.action}`}`,
        source: 'servicebridge',
        occurredAt: event.created_at || new Date().toISOString(),
        category: auditCategoryFor(event.action),
        level: auditLevelFor(event.action),
        action: event.action || '',
        message: event.description || event.resource || '',
        actorKind: event.actor_kind || '',
        actorId: event.actor_id || '',
        ip: event.ip_address || '',
        ipSource: '',
        method: '',
        path: event.resource || '',
        statusCode: null,
        userAgent: event.user_agent || '',
        browser: '',
        os: '',
        device: '',
        detail: { resource: event.resource || '', resourceId: event.resource_id || '' }
      }));
    } catch {
      return [];
    }
  }

  async function sampleCpu() {
    const start = cpuSnapshot();
    await new Promise((resolve) => setTimeout(resolve, 150));
    const end = cpuSnapshot();
    const idleDelta = end.idle - start.idle;
    const totalDelta = end.total - start.total;
    const usage = totalDelta > 0 ? Math.max(0, Math.min(1, 1 - idleDelta / totalDelta)) : 0;
    const load = os.loadavg();
    return {
      usage,
      cores: end.cores,
      load1: Number(load[0]?.toFixed(2) || 0),
      load5: Number(load[1]?.toFixed(2) || 0),
      load15: Number(load[2]?.toFixed(2) || 0)
    };
  }

  async function memoryInfo() {
    const total = bytes(os.totalmem());
    const free = bytes(os.freemem());
    const processUsage = process.memoryUsage();
    const cgroupUsed = await readCgroupFile(['/sys/fs/cgroup/memory.current', '/sys/fs/cgroup/memory/memory.usage_in_bytes']);
    const cgroupLimit = await readCgroupFile(['/sys/fs/cgroup/memory.max', '/sys/fs/cgroup/memory/memory.limit_in_bytes']);
    const limit = cgroupLimit > 0 && cgroupLimit < total * 4 ? cgroupLimit : total;
    const used = cgroupUsed > 0 ? cgroupUsed : total - free;
    return {
      totalBytes: limit,
      usedBytes: Math.min(used, limit),
      freeBytes: Math.max(limit - used, 0),
      systemTotalBytes: total,
      systemFreeBytes: free,
      process: { rss: processUsage.rss, heapUsed: processUsage.heapUsed, heapTotal: processUsage.heapTotal, external: processUsage.external }
    };
  }

  async function probeService(label, run) {
    const startedAt = Date.now();
    try {
      const detail = await run();
      return { label, ok: true, latencyMs: Date.now() - startedAt, detail: detail ?? null };
    } catch (error) {
      return { label, ok: false, latencyMs: Date.now() - startedAt, message: error?.message || String(error) };
    }
  }

  /** 日志保留策略：事件默认 180 天（等保≥6 个月），访问明细默认 30 天。 */
  function retentionPolicy(override = {}) {
    const envEventDays = Number(process.env.ADMIN_LOG_EVENT_RETENTION_DAYS || 180);
    const envVisitDays = Number(process.env.ADMIN_LOG_VISIT_RETENTION_DAYS || 30);
    const pick = (value, fallback, min, max) => {
      const parsed = Number(value);
      if (!Number.isFinite(parsed)) return fallback;
      return Math.min(Math.max(Math.trunc(parsed), min), max);
    };
    return {
      eventDays: pick(override.event_days ?? override.eventDays, envEventDays, 30, 3650),
      visitDays: pick(override.visit_days ?? override.visitDays, envVisitDays, 1, 365)
    };
  }

  async function readJsonBody(req) {
    if (typeof bodyJson !== 'function') return {};
    try {
      const payload = await bodyJson(req, 8 * 1024);
      return payload?.data && typeof payload.data === 'object' ? payload.data : payload || {};
    } catch {
      return {};
    }
  }

  async function healthPayload() {
    const [cpu, memory, db, disks] = await Promise.all([
      sampleCpu(),
      memoryInfo(),
      platformStore.dbHealth().catch((error) => ({ driver: 'unknown', error: error.message })),
      Promise.all(storagePaths.map((item) => statfsInfo(item.path).then((info) => ({ ...info, label: item.label }))))
    ]);

    const services = await Promise.all([
      probeService('控制台服务', async () => ({ ok: true })),
      probeService('客服引擎', async () => serviceBridgeStatus?.() ?? null),
      probeService('构建服务', async () => rebuildStatus?.() ?? null),
      probeService('对象存储', async () => storageHealth?.() ?? null),
      probeService('AI 生图', async () => aiCapability?.() ?? null),
      probeService('网站翻译', async () => translationSettings?.() ?? null)
    ]);

    return {
      server: {
        hostname: os.hostname(),
        platform: `${os.platform()} ${os.release()}`,
        uptimeSeconds: Math.round(os.uptime()),
        processUptimeSeconds: Math.round(process.uptime()),
        nodeVersion: process.version,
        time: new Date().toISOString()
      },
      cpu,
      memory,
      disks,
      database: db,
      services,
      generatedAt: new Date().toISOString()
    };
  }

  async function handle(req, res, pathname, url) {
    if (!pathname.startsWith('/site-admin-api/system/')) return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    try {
      if (pathname === '/site-admin-api/system/health' && req.method === 'GET') {
        json(res, 200, { data: await healthPayload() });
        return true;
      }
      if (pathname === '/site-admin-api/system/events' && req.method === 'GET') {
        const includeVisits = url.searchParams.get('include_visits') !== '0';
        const limit = url.searchParams.get('limit') || 100;
        const filters = {
          level: url.searchParams.get('level') || '',
          category: url.searchParams.get('category') || '',
          ip: url.searchParams.get('ip') || '',
          query: url.searchParams.get('q') || '',
          since: url.searchParams.get('since') || '',
          limit
        };
        const events = await platformStore.listSystemEvents(filters);
        let visits = [];
        if (includeVisits) {
          visits = (await platformStore.listVisits({
            limit,
            ip: filters.ip,
            path: '',
            includeBots: url.searchParams.get('include_bots') === '1',
            since: filters.since
          })).map((visit) => ({
            id: `visit_${visit.id}`,
            source: 'visit',
            occurredAt: visit.occurredAt,
            category: 'visit',
            level: 'info',
            action: visit.eventType === 'chat_open' ? 'chat.open' : visit.eventType === 'lead_submit' ? 'lead.submit' : 'page.view',
            message: `${visit.path || '/'}${visit.country ? ` · ${visit.country}` : ''}`,
            ip: visit.ip,
            ipSource: visit.ipSource,
            country: visit.country,
            method: 'GET',
            path: visit.path,
            userAgent: visit.userAgent,
            browser: visit.browser,
            os: visit.os,
            device: visit.device,
            detail: { referrer: visit.referrer, visitorId: visit.visitorId, sessionId: visit.sessionId, anonymous: visit.anonymous === true, isBot: visit.isBot, host: visit.host }
          }));
        }
        const bridge = url.searchParams.get('include_servicebridge') === '0' ? [] : await bridgeAuditEntries(Math.min(Number(limit) || 100, 200));
        const merged = [...events.map((event) => ({ ...event, source: 'event' })), ...visits, ...bridge]
          .filter((item) => {
            if (filters.level && item.level !== filters.level) return false;
            if (filters.category && item.category !== filters.category) return false;
            if (filters.query) {
              const needle = filters.query.toLowerCase();
              const haystack = [item.action, item.message, item.path, item.actorEmail, item.ip, item.browser, item.os].join(' ').toLowerCase();
              if (!haystack.includes(needle)) return false;
            }
            return true;
          })
          .sort((a, b) => new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime())
          .slice(0, Math.min(Math.max(Number(limit) || 100, 1), 500));
        json(res, 200, { data: merged });
        return true;
      }
      const detailMatch = pathname.match(/^\/site-admin-api\/system\/events\/([^/]+)$/);
      if (detailMatch && req.method === 'GET') {
        const id = decodeURIComponent(detailMatch[1]);
        if (id.startsWith('visit_')) {
          const found = await platformStore.visitById(id.slice('visit_'.length));
          if (!found) {
            json(res, 404, { error: { message: '访问记录不存在或已过期。' } });
            return true;
          }
          json(res, 200, { data: { ...found, source: 'visit', category: 'visit', level: 'info' } });
          return true;
        }
        // 客服引擎的审计日志只存在于引擎库里（列表里 id 形如 sbaudit_xxx），
        // 早先这里直接落到本地事件表查询，所以点开引擎行永远 404「日志不存在或已过期」。
        // 现在按同一份列表精确回查该条目，找不到才是真的过期。
        if (id.startsWith('sbaudit_')) {
          const entry = (await bridgeAuditEntries(200)).find((item) => item.id === id);
          if (!entry) {
            json(res, 404, { error: { message: '该审计日志已超出可读取范围（引擎侧只保留最近 200 条）。' } });
            return true;
          }
          json(res, 200, { data: entry });
          return true;
        }
        const event = await platformStore.systemEventById(id);
        if (!event) {
          json(res, 404, { error: { message: '日志不存在或已过期。' } });
          return true;
        }
        json(res, 200, { data: { ...event, source: 'event' } });
        return true;
      }
      if (pathname === '/site-admin-api/system/retention' && req.method === 'GET') {
        json(res, 200, { data: retentionPolicy() });
        return true;
      }
      if (pathname === '/site-admin-api/system/events/purge' && req.method === 'POST') {
        // 保留天数可由请求覆盖（等保要求留存期明确、可审计），否则用环境变量默认值。
        const body = await readJsonBody(req);
        const policy = retentionPolicy(body);
        const result = await platformStore.purgeSystemEvents({
          eventDays: policy.eventDays,
          visitDays: policy.visitDays
        });
        await auditEvent?.(req, 'system.events.purged', 'system-events', '', { ...result, ...policy });
        json(res, 200, { data: { ...result, ...policy } });
        return true;
      }
      json(res, 404, { error: { message: '未知的系统接口。' } });
    } catch (error) {
      json(res, 500, { error: { message: error.message || '系统接口失败。' } });
    }
    return true;
  }

  return { handle, healthPayload, record: eventRecorder };
}
