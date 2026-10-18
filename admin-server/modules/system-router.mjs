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
  verifySession,
  auditEvent,
  storagePaths = [],
  serviceBridgeStatus,
  rebuildStatus,
  storageHealth,
  aiCapability,
  translationSettings,
  eventRecorder
}) {
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
            detail: { referrer: visit.referrer, visitorId: visit.visitorId, sessionId: visit.sessionId, isBot: visit.isBot, host: visit.host }
          }));
        }
        const merged = [...events.map((event) => ({ ...event, source: 'event' })), ...visits]
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
        const event = await platformStore.systemEventById(id);
        if (!event) {
          json(res, 404, { error: { message: '日志不存在或已过期。' } });
          return true;
        }
        json(res, 200, { data: { ...event, source: 'event' } });
        return true;
      }
      if (pathname === '/site-admin-api/system/events/purge' && req.method === 'POST') {
        const result = await platformStore.purgeSystemEvents({});
        await auditEvent?.(req, 'system.events.purged', 'system-events', '', result);
        json(res, 200, { data: result });
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
