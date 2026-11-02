import crypto from 'node:crypto';

const LEAD_BODY_LIMIT = 64 * 1024;
const RATE_WINDOW_MS = Number(process.env.ADMIN_LEAD_RATE_WINDOW_MS || 10 * 60 * 1000);
const RATE_MAX = Number(process.env.ADMIN_LEAD_RATE_MAX || 5);
const DUPLICATE_WINDOW_MS = Number(process.env.ADMIN_LEAD_DUPLICATE_WINDOW_MS || 5 * 60 * 1000);
const ALLOWED_STATUSES = new Set(['新咨询', '处理中', '已联系', '已关闭']);
const ALLOWED_PRIORITIES = new Set(['低', '普通', '高', '紧急']);

function cleanText(value, maxLength) {
  return String(value || '').trim().replace(/\u0000/g, '').slice(0, maxLength);
}

function validEmail(value) {
  return !value || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
}

function validPhone(value) {
  return !value || /^[+\d][\d\s().-]{5,30}$/.test(value);
}

function safeHttpUrl(value, maxLength = 1000) {
  const candidate = cleanText(value, maxLength);
  if (!candidate) return '';
  try {
    const url = new URL(candidate);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString().slice(0, maxLength) : '';
  } catch {
    return '';
  }
}

function leadFingerprint(data) {
  return crypto.createHash('sha256').update([
    String(data.phone || '').replace(/\s+/g, ''),
    String(data.email || '').toLowerCase(),
    String(data.message || '').toLowerCase(),
    String(data.sourcePage || '')
  ].join('|')).digest('hex');
}

function normalizeLeadInput(data = {}) {
  const lead = {
    name: cleanText(data.name, 120),
    companyName: cleanText(data.companyName, 160),
    phone: cleanText(data.phone, 32),
    email: cleanText(data.email, 254).toLowerCase(),
    message: cleanText(data.message, 5000),
    sourcePage: cleanText(data.sourcePage, 300),
    sourceUrl: safeHttpUrl(data.sourceUrl),
    referrer: safeHttpUrl(data.referrer),
    utm: Object.fromEntries(['source', 'medium', 'campaign', 'term', 'content'].map((key) => [key, cleanText(data.utm?.[key], 200)]))
  };
  lead.contactInfo = [lead.phone, lead.email].filter(Boolean).join(' / ');
  if (!lead.name) throw new Error('请填写姓名。');
  if (!lead.phone && !lead.email) throw new Error('请至少填写手机号或邮箱。');
  if (!validEmail(lead.email)) throw new Error('邮箱格式不正确。');
  if (!validPhone(lead.phone)) throw new Error('手机号格式不正确。');
  return lead;
}

/** 官网咨询单提交与后台跟进逻辑。 */
export function createLeadsModule({ readLeads, saveLeads, bodyJson, json, verifySession, auditEvent, requestIp = () => '' }) {
  const rateWindows = new Map();

  function checkRateLimit(req) {
    const now = Date.now();
    const key = requestIp(req) || 'unknown';
    const current = rateWindows.get(key);
    const bucket = !current || current.resetAt <= now ? { count: 0, resetAt: now + RATE_WINDOW_MS } : current;
    bucket.count += 1;
    rateWindows.set(key, bucket);
    if (rateWindows.size > 5000) {
      for (const [entryKey, value] of rateWindows) if (value.resetAt <= now) rateWindows.delete(entryKey);
    }
    return { allowed: bucket.count <= RATE_MAX, retryAfter: Math.max(1, Math.ceil((bucket.resetAt - now) / 1000)) };
  }

  async function saveLead(data = {}) {
    if (!data.id) throw new Error('咨询单 ID 不能为空。');
    const store = await readLeads();
    const index = store.leads.findIndex((item) => item.id === data.id);
    if (index < 0) throw new Error('咨询单不存在或已被删除。');
    const current = store.leads[index];
    const status = data.status == null ? current.status : cleanText(data.status, 20);
    const priority = data.priority == null ? current.priority : cleanText(data.priority, 20);
    if (!ALLOWED_STATUSES.has(status)) throw new Error('咨询单状态无效。');
    if (!ALLOWED_PRIORITIES.has(priority)) throw new Error('咨询单优先级无效。');
    store.leads[index] = {
      ...current,
      status,
      priority,
      internalNotes: data.internalNotes == null ? current.internalNotes : cleanText(data.internalNotes, 5000),
      updatedAt: new Date().toISOString()
    };
    await saveLeads(store);
    return store.leads[index];
  }

  async function handle(req, res, pathname) {
    if (pathname === '/site-admin-api/leads' && req.method === 'POST') {
      const rate = checkRateLimit(req);
      if (!rate.allowed) {
        res.setHeader('Retry-After', String(rate.retryAfter));
        json(res, 429, { error: { message: '提交过于频繁，请稍后再试。' } });
        return true;
      }
      try {
        const body = await bodyJson(req, LEAD_BODY_LIMIT);
        const data = body.data || body;
        // 蜜罐字段有值时静默成功，不污染 CRM 数据。
        if (String(data.website || '').trim()) {
          json(res, 200, { ok: true });
          return true;
        }
        const normalized = normalizeLeadInput(data);
        const fingerprint = leadFingerprint(normalized);
        const store = await readLeads();
        const duplicate = store.leads.find((item) => {
          const createdAt = new Date(item.submittedAt || 0).getTime();
          return Date.now() - createdAt <= DUPLICATE_WINDOW_MS && leadFingerprint(item) === fingerprint;
        });
        if (duplicate) {
          // 只确认重复，不回传已有咨询单内容（避免内部备注/PII 被重复提交者读取）。
          json(res, 200, { ok: true, data: { id: duplicate.id, status: duplicate.status }, meta: { duplicate: true } });
          return true;
        }
        const now = new Date().toISOString();
        const lead = {
          id: `lead_${crypto.randomUUID()}`,
          ...normalized,
          status: '新咨询',
          priority: '普通',
          internalNotes: '',
          submittedAt: now,
          updatedAt: now
        };
        store.leads.unshift(lead);
        await saveLeads(store);
        await auditEvent(req, 'lead.created', 'lead', lead.id, { sourcePage: lead.sourcePage });
        json(res, 201, { data: lead });
      } catch (error) {
        json(res, 400, { error: { message: error.message || '提交咨询单失败。' } });
      }
      return true;
    }

    const match = pathname.match(/^\/site-admin-api\/leads\/([^/]+)$/);
    if (!match || req.method !== 'PATCH') return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }
    try {
      const lead = await saveLead({ ...(await bodyJson(req, LEAD_BODY_LIMIT)), id: match[1] });
      await auditEvent(req, 'lead.updated', 'lead', lead.id, { status: lead.status, priority: lead.priority });
      json(res, 200, { data: lead });
    } catch (error) {
      json(res, 400, { error: { message: error.message || '更新咨询单失败。' } });
    }
    return true;
  }

  return { handle, saveLead };
}
