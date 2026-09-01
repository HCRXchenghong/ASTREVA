const SETTING_KEY = 'website-translation';
const TARGET_LOCALES = ['en', 'ar'];
const API_TYPES = new Set(['chat_completions', 'responses', 'anthropic_messages']);

const RETRYABLE_STATUS = new Set([408, 429, 500, 502, 503, 504]);
// 批次内快速失败：上游网关偶发 502/504 时只做短重试，长不可用窗口交给任务级自动重排。
const RETRY_DELAYS_MS = [1500, 5000, 15000, 30000, 60000];
// 任务级重排退避：扛过上游账号池数分钟的临时排除窗口（实测网关 "no available accounts" 返回 502）。
const JOB_REQUEUE_DELAYS_MS = [60000, 120000, 240000];
// 连续批次之间短暂停顿，降低上游网关在高频请求下返回 502 的概率。
const BATCH_GAP_MS = 2500;

// 上游账号池容易被并发打限流：同一时刻最多一个批量翻译请求在途，多语言按批次交替进行。
let batchChain = Promise.resolve();
function runSerializedBatch(task) {
  const run = batchChain.then(() => task());
  batchChain = run.then(() => undefined, () => undefined);
  return run;
}

const DEFAULT_CONFIG = Object.freeze({
  enabled: true,
  apiType: 'chat_completions',
  baseUrl: 'https://api.openai.com/v1',
  model: '',
  configuredModels: [],
  batchSize: 40,
  timeoutMs: 120000
});

function cleanUrl(value) {
  const raw = String(value || '').trim().replace(/\/+$/, '');
  let url;
  try { url = new URL(raw); }
  catch { throw new Error('翻译上游地址不是有效 URL。'); }
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('翻译上游仅支持 HTTP/HTTPS。');
  return raw;
}

function normalizeConfig(value = {}) {
  const apiType = API_TYPES.has(value.apiType) ? value.apiType : DEFAULT_CONFIG.apiType;
  return {
    enabled: value.enabled !== false,
    apiType,
    baseUrl: cleanUrl(value.baseUrl || DEFAULT_CONFIG.baseUrl),
    model: String(value.model || '').trim().slice(0, 200),
    configuredModels: [...new Set((Array.isArray(value.configuredModels) ? value.configuredModels : [])
      .map((item) => String(item || '').trim()).filter(Boolean))].slice(0, 500),
    batchSize: Math.max(5, Math.min(Number(value.batchSize) || DEFAULT_CONFIG.batchSize, 100)),
    timeoutMs: Math.max(10000, Math.min(Number(value.timeoutMs) || DEFAULT_CONFIG.timeoutMs, 600000))
  };
}

function maskSecret(value) {
  const secret = String(value || '');
  if (!secret) return '';
  if (secret.length <= 8) return `${secret.slice(0, 2)}***`;
  return `${secret.slice(0, 4)}***${secret.slice(-4)}`;
}

function apiEndpoint(baseUrl, suffix) {
  const clean = cleanUrl(baseUrl)
    .replace(/\/(chat\/completions|responses|messages|models)$/i, '')
    .replace(/\/+$/, '');
  return `${clean}/${suffix}`;
}

// 上游网关偶发 401 剥离、502/504 与连接超时，这里做有限重试，避免一次抖动就判翻译失败。
async function fetchWithRetry(fetchImpl, url, options, timeoutMs, logger) {
  let lastError = null;
  for (let attempt = 0; attempt <= RETRY_DELAYS_MS.length; attempt++) {
    try {
      const response = await fetchImpl(url, { ...options, signal: AbortSignal.timeout(timeoutMs) });
      if (!RETRYABLE_STATUS.has(response.status) || attempt === RETRY_DELAYS_MS.length) return response;
      lastError = new Error(`HTTP ${response.status}`);
    } catch (error) {
      if (attempt === RETRY_DELAYS_MS.length) throw error;
      lastError = error;
    }
    logger.warn(`[网站翻译] 上游抖动（${lastError.message}），${RETRY_DELAYS_MS[attempt]}ms 后重试 ${url}`);
    await new Promise((resolve) => setTimeout(resolve, RETRY_DELAYS_MS[attempt]));
  }
  throw lastError;
}

function headersFor(apiType, apiKey) {
  if (apiType === 'anthropic_messages') {
    return { 'Content-Type': 'application/json', 'x-api-key': apiKey, 'anthropic-version': '2023-06-01' };
  }
  // 同时携带 Authorization 与 x-api-key：部分上游网关会剥离 Authorization 头，
  // 其鉴权提示明确接受 x-api-key，双通道可避免此类 401。
  return { 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}`, 'x-api-key': apiKey };
}

function extractText(payload, apiType) {
  if (apiType === 'chat_completions') {
    const content = payload?.choices?.[0]?.message?.content;
    if (Array.isArray(content)) return content.map((item) => item?.text || '').join('');
    return String(content || '');
  }
  if (apiType === 'responses') {
    if (payload?.output_text) return String(payload.output_text);
    return (payload?.output || []).flatMap((item) => item?.content || []).map((item) => item?.text || item?.output_text || '').join('');
  }
  return (payload?.content || []).map((item) => item?.text || '').join('');
}

function parseJsonArray(text) {
  const cleaned = String(text || '').trim().replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/i, '');
  try {
    const value = JSON.parse(cleaned);
    if (Array.isArray(value)) return value;
  } catch {}
  const start = cleaned.indexOf('[');
  const end = cleaned.lastIndexOf(']');
  if (start >= 0 && end > start) {
    const value = JSON.parse(cleaned.slice(start, end + 1));
    if (Array.isArray(value)) return value;
  }
  throw new Error('翻译上游没有返回约定的 JSON 数组。');
}

function parseJsonObject(text) {
  const cleaned = String(text || '').trim().replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/i, '');
  try {
    const value = JSON.parse(cleaned);
    if (value && typeof value === 'object' && !Array.isArray(value)) return value;
  } catch {}
  const start = cleaned.indexOf('{');
  const end = cleaned.lastIndexOf('}');
  if (start >= 0 && end > start) {
    const value = JSON.parse(cleaned.slice(start, end + 1));
    if (value && typeof value === 'object' && !Array.isArray(value)) return value;
  }
  throw new Error('AI 上游没有返回约定的 JSON 对象。');
}

function shouldTranslate(path, value) {
  if (!/[\u3400-\u9fff]/u.test(value)) return false;
  const key = path.split('.').at(-1) || '';
  if (/^(id|slug|categorySlug|locale|href|url|src|path|phone|email|icon|logoUrl|serviceBridgeUrl)$/i.test(key)) return false;
  if (/(image|video|media|asset|cover|poster|thumbnail|background|download|endpoint|key)$/i.test(key)) return false;
  if (/^(?:https?:|mailto:|tel:|\/|#)/i.test(value.trim())) return false;
  return true;
}

export function collectTranslatableEntries(content) {
  const entries = [];
  function visit(value, path = '') {
    if (Array.isArray(value)) {
      value.forEach((item, index) => visit(item, path ? `${path}.${index}` : String(index)));
      return;
    }
    if (value && typeof value === 'object') {
      Object.entries(value).forEach(([key, item]) => visit(item, path ? `${path}.${key}` : key));
      return;
    }
    if (typeof value === 'string' && shouldTranslate(path, value)) entries.push({ id: path, text: value });
  }
  visit(content);
  return entries;
}

function setAtPath(target, path, value) {
  const keys = path.split('.');
  let cursor = target;
  for (const key of keys.slice(0, -1)) cursor = cursor[key];
  cursor[keys.at(-1)] = value;
}

function getAtPath(source, path) {
  const keys = path.split('.');
  let cursor = source;
  for (const key of keys) {
    if (cursor == null || typeof cursor !== 'object') return undefined;
    cursor = cursor[key];
  }
  return cursor;
}

function localeLabel(locale) {
  return locale === 'ar' ? '阿拉伯语' : '英语';
}

function promptFor(locale, entries) {
  return [
    `将下面的官网文案从简体中文专业翻译为${localeLabel(locale)}。`,
    '你必须只返回 JSON 数组，每项仅包含 id 和 text，顺序、id 和条目数必须与输入一致。',
    '保留 ASTREVA 等品牌名、产品型号、{category} 等占位符、URL、HTML 和换行结构；不要解释，不要 Markdown 代码块。',
    locale === 'ar' ? '阿拉伯语使用现代标准阿拉伯语，保持商业官网语气。' : '英语使用自然、准确的国际商务网站语气。',
    JSON.stringify(entries)
  ].join('\n');
}

export function createWebsiteTranslationService({ platformStore, fetchImpl = fetch, logger = console }) {
  let stored = null;
  const queued = new Map();
  const latestRevision = new Map();
  const progressByLocale = new Map();
  let draining = false;

  async function loadSetting() {
    if (stored) return stored;
    const value = await platformStore.readRuntimeSetting(SETTING_KEY);
    stored = value || { config: normalizeConfig(DEFAULT_CONFIG), secrets: {}, updatedAt: null };
    if (!value) stored = await platformStore.saveRuntimeSetting(SETTING_KEY, stored);
    stored.config = normalizeConfig(stored.config);
    return stored;
  }

  async function publicSettings() {
    const value = await loadSetting();
    const apiKey = String(value.secrets?.apiKey || '');
    return {
      ...value.config,
      apiKeyConfigured: Boolean(apiKey),
      apiKeyMasked: maskSecret(apiKey),
      updatedAt: value.updatedAt || null
    };
  }

  async function saveSettings(payload = {}) {
    const current = await loadSetting();
    const config = normalizeConfig({ ...current.config, ...payload });
    const apiKeyInput = String(payload.apiKey || '').trim();
    const apiKey = payload.clearApiKey === true ? '' : (apiKeyInput || current.secrets?.apiKey || '');
    stored = await platformStore.saveRuntimeSetting(SETTING_KEY, { config, secrets: { apiKey } });
    stored.config = normalizeConfig(stored.config);
    return publicSettings();
  }

  async function requestModels(payload = {}) {
    const current = await loadSetting();
    const config = normalizeConfig({ ...current.config, ...payload });
    const apiKey = String(payload.apiKey || '').trim() || current.secrets?.apiKey || '';
    if (!apiKey) throw new Error('请先输入并保存 API Key。');
    const response = await fetchWithRetry(fetchImpl, apiEndpoint(config.baseUrl, 'models'), {
      headers: headersFor(config.apiType, apiKey)
    }, config.timeoutMs, logger);
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const detail = body?.error?.message || body?.message || '';
      throw new Error(detail ? `HTTP ${response.status}: ${detail}` : `获取模型失败（HTTP ${response.status}）`);
    }
    const raw = Array.isArray(body?.data) ? body.data : (Array.isArray(body?.models) ? body.models : []);
    const models = [...new Set(raw.map((item) => String(item?.id || item?.name || item || '').trim()).filter(Boolean))].sort();
    if (!models.length) throw new Error('上游返回成功，但没有可用模型。');
    return models;
  }

  async function requestBatch(config, apiKey, locale, batch) {
    const prompt = promptFor(locale, batch);
    let suffix;
    let body;
    if (config.apiType === 'chat_completions') {
      suffix = 'chat/completions';
      body = { model: config.model, temperature: 0.2, messages: [{ role: 'system', content: '你是严格的商业网站本地化引擎。' }, { role: 'user', content: prompt }] };
    } else if (config.apiType === 'responses') {
      suffix = 'responses';
      body = { model: config.model, temperature: 0.2, reasoning: { effort: 'low' }, input: prompt };
    } else {
      suffix = 'messages';
      body = { model: config.model, temperature: 0.2, max_tokens: 16000, system: '你是严格的商业网站本地化引擎。', messages: [{ role: 'user', content: prompt }] };
    }
    const response = await fetchWithRetry(fetchImpl, apiEndpoint(config.baseUrl, suffix), {
      method: 'POST',
      headers: headersFor(config.apiType, apiKey),
      body: JSON.stringify(body)
    }, config.timeoutMs, logger);
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
      const detail = payload?.error?.message || payload?.message || '';
      throw new Error(detail ? `HTTP ${response.status}: ${detail}` : `翻译失败（HTTP ${response.status}）`);
    }
    const translated = parseJsonArray(extractText(payload, config.apiType));
    if (translated.length !== batch.length) throw new Error(`翻译上游返回 ${translated.length} 项，预期 ${batch.length} 项。`);
    const result = new Map(translated.map((item) => [String(item?.id || ''), String(item?.text || '')]));
    for (const item of batch) {
      if (!result.get(item.id)?.trim()) throw new Error(`翻译结果缺少字段 ${item.id}。`);
    }
    return result;
  }

  async function generateJson(prompt, options = {}) {
    const current = await loadSetting();
    const config = current.config;
    const apiKey = String(current.secrets?.apiKey || '');
    if (!config.enabled) throw Object.assign(new Error('网站 AI 已停用，请先在网站翻译设置中启用。'), { code: 'AI_UNCONFIGURED' });
    if (!apiKey || !config.model) {
      throw Object.assign(new Error('网站 AI 未完整配置：请先在网站翻译设置中保存 API Key 和模型。'), { code: 'AI_UNCONFIGURED' });
    }
    const system = String(options.system || '你是严格的商业网站内容引擎。');
    const temperature = Math.max(0, Math.min(Number(options.temperature) || 0.2, 1));
    const maxTokens = Math.max(1000, Math.min(Number(options.maxTokens) || 16000, 32000));
    let suffix;
    let body;
    if (config.apiType === 'chat_completions') {
      suffix = 'chat/completions';
      body = { model: config.model, temperature, max_tokens: maxTokens, messages: [{ role: 'system', content: system }, { role: 'user', content: String(prompt || '') }] };
    } else if (config.apiType === 'responses') {
      suffix = 'responses';
      body = { model: config.model, temperature, max_output_tokens: maxTokens, input: `${system}\n\n${String(prompt || '')}` };
    } else {
      suffix = 'messages';
      body = { model: config.model, temperature, max_tokens: maxTokens, system, messages: [{ role: 'user', content: String(prompt || '') }] };
    }
    return runSerializedBatch(async () => {
      const response = await fetchWithRetry(fetchImpl, apiEndpoint(config.baseUrl, suffix), {
        method: 'POST',
        headers: headersFor(config.apiType, apiKey),
        body: JSON.stringify(body)
      }, config.timeoutMs, logger);
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) {
        const detail = payload?.error?.message || payload?.message || '';
        throw Object.assign(
          new Error(detail ? `HTTP ${response.status}: ${detail}` : `AI 生成失败（HTTP ${response.status}）`),
          { code: 'AI_UPSTREAM_ERROR' }
        );
      }
      const text = extractText(payload, config.apiType);
      return options.type === 'object' ? parseJsonObject(text) : parseJsonArray(text);
    });
  }

  async function translate(locale, content, sourceRevision) {
    const current = await loadSetting();
    const config = current.config;
    const apiKey = String(current.secrets?.apiKey || '');
    if (!config.enabled) throw Object.assign(new Error('自动翻译已停用。'), { code: 'TRANSLATION_UNCONFIGURED' });
    if (!apiKey || !config.model) throw Object.assign(new Error('翻译上游未完整配置：需要 API Key 和模型。'), { code: 'TRANSLATION_UNCONFIGURED' });
    const entries = collectTranslatableEntries(content);
    const total = entries.length;
    const previous = await platformStore.readContentTranslation(locale);
    const translatedContent = structuredClone(content);
    translatedContent.site.locale = locale;
    const checkpointable = Boolean(
      previous &&
      previous.sourceRevision === sourceRevision &&
      previous.content && typeof previous.content === 'object'
    );
    let resumed = 0;
    const needsTranslation = (entry) => {
      if (!checkpointable) return true;
      const prevValue = getAtPath(previous.content, entry.id);
      return !(typeof prevValue === 'string' && prevValue.trim() && prevValue !== entry.text);
    };
    if (checkpointable) {
      for (const entry of entries) {
        if (!needsTranslation(entry)) {
          setAtPath(translatedContent, entry.id, getAtPath(previous.content, entry.id));
          resumed += 1;
        }
      }
      logger.info('[网站翻译] ' + locale + ' 断点续译：已完成 ' + resumed + '/' + total + ' 条。');
    }
    const remaining = checkpointable ? entries.filter(needsTranslation) : entries;
    const startedAt = (checkpointable && previous.startedAt) || new Date().toISOString();
    progressByLocale.set(locale, { done: resumed, total, updatedAt: Date.now() });
    await platformStore.saveContentTranslation(locale, { content: translatedContent, sourceRevision, status: 'running', lastError: '', startedAt, finishedAt: null });
    for (let index = 0; index < remaining.length; index += config.batchSize) {
      const batch = remaining.slice(index, index + config.batchSize);
      if (index > 0) await new Promise((resolve) => setTimeout(resolve, BATCH_GAP_MS));
      if (latestRevision.get(locale) !== sourceRevision) return;
      const result = await runSerializedBatch(() => requestBatch(config, apiKey, locale, batch));
      for (const [path, value] of result) setAtPath(translatedContent, path, value);
      progressByLocale.set(locale, { done: resumed + Math.min(index + config.batchSize, remaining.length), total, updatedAt: Date.now() });
      await platformStore.saveContentTranslation(locale, { content: translatedContent, sourceRevision, status: 'running', lastError: '', startedAt, finishedAt: null }).catch(() => {});
    }
    if (latestRevision.get(locale) !== sourceRevision) return;
    progressByLocale.set(locale, { done: total, total, updatedAt: Date.now() });
    await platformStore.saveContentTranslation(locale, {
      content: translatedContent,
      sourceRevision,
      status: 'ready',
      lastError: '',
      startedAt,
      finishedAt: new Date().toISOString()
    });
  }

  function isRetryableUpstreamError(error) {
    if (error?.code === 'TRANSLATION_UNCONFIGURED') return false;
    const message = String(error?.message || '');
    if (/HTTP (408|429|500|502|503|504)/.test(message)) return true;
    if (/temporarily unavailable|service unavailable|bad gateway|gateway timeout|no available accounts|overloaded|internal server error/i.test(message)) return true;
    return /fetch failed|timeout|aborted|network|ECONNRESET|ETIMEDOUT|EAI_AGAIN/i.test(message);
  }

  async function runJob(job) {
    try {
      await translate(job.locale, job.content, job.sourceRevision);
    } catch (error) {
      if (latestRevision.get(job.locale) !== job.sourceRevision) return;
      const attempts = Number(job.attempts || 0);
      if (isRetryableUpstreamError(error) && attempts < JOB_REQUEUE_DELAYS_MS.length) {
        const delay = JOB_REQUEUE_DELAYS_MS[attempts];
        logger.warn('[网站翻译] ' + job.locale + ' 上游暂不可用（' + error.message + '），' + delay + 'ms 后自动重排（第 ' + (attempts + 1) + ' 次）。');
        const existingProgress = progressByLocale.get(job.locale);
        if (existingProgress) progressByLocale.set(job.locale, { ...existingProgress, updatedAt: Date.now() });
        const existingRecord = await platformStore.readContentTranslation(job.locale).catch(() => null);
        await platformStore.saveContentTranslation(job.locale, {
          content: existingRecord?.content || null,
          sourceRevision: job.sourceRevision,
          status: 'pending',
          lastError: '',
          startedAt: existingRecord?.startedAt || null,
          finishedAt: null
        }).catch(() => {});
        setTimeout(() => {
          if (latestRevision.get(job.locale) !== job.sourceRevision) return;
          queued.set(job.locale, { ...job, attempts: attempts + 1 });
          queueMicrotask(drain);
        }, delay);
        return;
      }
      progressByLocale.delete(job.locale);
      const previous = await platformStore.readContentTranslation(job.locale);
      await platformStore.saveContentTranslation(job.locale, {
        ...previous,
        sourceRevision: job.sourceRevision,
        status: error.code === 'TRANSLATION_UNCONFIGURED' ? 'unconfigured' : 'failed',
        lastError: error.message || '未知翻译错误',
        finishedAt: new Date().toISOString()
      });
      logger.error(`[网站翻译] ${job.locale} 失败：${error.message}`);
    }
  }

  async function drain() {
    if (draining) return;
    draining = true;
    try {
      const jobs = [...queued.values()];
      queued.clear();
      await Promise.all(jobs.map((job) => runJob(job)));
    } finally {
      draining = false;
      if (queued.size) queueMicrotask(drain);
    }
  }

  async function schedule(content, sourceRevision, locales = TARGET_LOCALES) {
    const existing = new Map((await platformStore.listContentTranslations()).map((item) => [item.locale, item]));
    const progressTotal = collectTranslatableEntries(content).length;
    for (const locale of locales.filter((item) => TARGET_LOCALES.includes(item))) {
      latestRevision.set(locale, sourceRevision);
      progressByLocale.set(locale, { done: 0, total: progressTotal, updatedAt: Date.now() });
      await platformStore.saveContentTranslation(locale, {
        content: null,
        sourceRevision,
        status: 'pending',
        lastError: '',
        startedAt: null,
        finishedAt: null
      });
      queued.set(locale, { locale, content: structuredClone(content), sourceRevision });
    }
    queueMicrotask(drain);
  }

  return {
    locales: TARGET_LOCALES,
    publicSettings,
    saveSettings,
    requestModels,
    generateJson,
    schedule,
    list: async () => (await platformStore.listContentTranslations()).map((item) => {
      const progress = progressByLocale.get(item.locale);
      return { ...item, progress: progress ? { ...progress } : null };
    }),
    read: (locale) => platformStore.readContentTranslation(locale)
  };
}
