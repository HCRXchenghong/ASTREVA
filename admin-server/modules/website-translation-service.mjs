import { sanitizeCmsContent } from './html-sanitize.mjs';

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
// 控制输入长度，避免少数长文案把 40 条批次的输出挤到模型上限之外。
const MAX_BATCH_INPUT_CHARS = 6000;
const SINGLE_ENTRY_ATTEMPTS = 3;

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

// 部分调用方只注入了 error 日志，这里做一次安全兜底，避免告警把主流程打断。
function logWarn(logger, message) {
  if (logger && typeof logger.warn === 'function') logger.warn(message);
  else console.warn(message);
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
    logWarn(logger, `[网站翻译] 上游抖动（${lastError.message}），${RETRY_DELAYS_MS[attempt]}ms 后重试 ${url}`);
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
  // 每种语言的 SEO 由 SEO 能力直接生成，不能把中文 SEO 混进普通翻译任务。
  if (/(?:^|\.)(?:defaultSeo|seo)(?:\.|$)/i.test(path)) return false;
  const key = path.split('.').at(-1) || '';
  if (/^(id|slug|categorySlug|locale|href|url|src|path|phone|email|icon|logoUrl|serviceBridgeUrl)$/i.test(key)) return false;
  if (/(image|video|media|asset|cover|poster|thumbnail|background|download|endpoint|key|icon)$/i.test(key)) return false;
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

const MEDIA_FIELD_KEYS = new Set([
  'mediaItems', 'galleryMediaItems', 'videoMediaItems', 'videoModalBackgroundMediaItems',
  'coverImage', 'backgroundImage', 'backgroundVideo', 'videoUrl', 'videoImage',
  'videoPosterImage', 'videoModalBackgroundImage', 'galleryImages', 'galleryVideos',
  'heroVideo', 'image', 'images', 'poster', 'logoUrl'
]);

function sameValue(left, right) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function mediaAltChanged(before, after) {
  if (!Array.isArray(before) || !Array.isArray(after)) return false;
  const previous = new Map(before.map((item) => [`${item?.type}:${item?.url}`, item?.alt || '']));
  return after.some((item) => {
    const id = `${item?.type}:${item?.url}`;
    if (previous.has(id)) return previous.get(id) !== (item?.alt || '');
    return /[\u3400-\u9fff]/u.test(item?.alt || '');
  });
}

export function isMediaOnlyContentChange(before, after) {
  let changed = false;
  function visit(left, right, key = '') {
    if (sameValue(left, right)) return true;
    if (MEDIA_FIELD_KEYS.has(key)) {
      changed = true;
      return !/MediaItems$|^mediaItems$/.test(key) || !mediaAltChanged(left, right);
    }
    if (Array.isArray(left) || Array.isArray(right)) {
      if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false;
      return left.every((item, index) => visit(item, right[index]));
    }
    if (left && right && typeof left === 'object' && typeof right === 'object') {
      return [...new Set([...Object.keys(left), ...Object.keys(right)])]
        .every((child) => visit(left[child], right[child], child));
    }
    return false;
  }
  const visibleText = (content) => collectTranslatableEntries(content)
    .filter((entry) => !/(?:^|\.)(?:mediaItems|galleryMediaItems|videoMediaItems|videoModalBackgroundMediaItems)\.\d+\.alt$/.test(entry.id));
  const sourceTextUnchanged = sameValue(visibleText(before), visibleText(after));
  return sourceTextUnchanged && visit(before, after) && changed;
}

function rebaseMediaFields(translated, before, after) {
  if (!translated || typeof translated !== 'object' || !before || !after || typeof before !== 'object' || typeof after !== 'object') return;
  for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) {
    if (MEDIA_FIELD_KEYS.has(key)) {
      if (sameValue(before[key], after[key])) continue;
      if (!(key in after)) { delete translated[key]; continue; }
      const next = structuredClone(after[key]);
      if (/MediaItems$|^mediaItems$/.test(key) && Array.isArray(next) && Array.isArray(translated[key])) {
        const localizedAlt = new Map(translated[key].map((item) => [`${item?.type}:${item?.url}`, item?.alt || '']));
        for (const item of next) {
          const id = `${item?.type}:${item?.url}`;
          if (localizedAlt.has(id)) item.alt = localizedAlt.get(id);
        }
      }
      translated[key] = next;
    } else if (Array.isArray(after[key]) && Array.isArray(before[key]) && Array.isArray(translated[key])) {
      after[key].forEach((item, index) => rebaseMediaFields(translated[key][index], before[key][index], item));
    } else {
      rebaseMediaFields(translated[key], before[key], after[key]);
    }
  }
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

// 品牌在中文站显示为“星渡ASTREVA”；目标语言站统一使用拉丁品牌名。
// 上游偶尔会在已翻译的句子和独立生成的 SEO 中保留这段中文，发布前统一修正。
export function normalizeLocalizedBrandTerms(content) {
  function visit(value) {
    if (typeof value === 'string') {
      return value.replace(/星渡\s*ASTREVA/gi, 'ASTREVA').replace(/Click咔哒(?:锁扣)?/g, 'Click');
    }
    if (Array.isArray(value)) return value.map(visit);
    if (value && typeof value === 'object') {
      for (const [key, item] of Object.entries(value)) value[key] = visit(item);
    }
    return value;
  }
  return visit(content);
}

function localeLabel(locale) {
  return locale === 'ar' ? '阿拉伯语' : '英语';
}

function promptFor(locale, entries) {
  return [
    `将下面的官网文案从简体中文专业翻译为${localeLabel(locale)}。`,
    '你必须只返回 JSON 数组，每项仅包含 id 和 text，顺序、id 和条目数必须与输入一致。',
    '品牌“星渡ASTREVA”或“星渡 ASTREVA”在目标语言中统一写 ASTREVA；“Click咔哒锁扣”写 Click。保留产品型号、{category} 等占位符、URL、HTML 和换行结构；不要解释，不要 Markdown 代码块。',
    locale === 'ar' ? '阿拉伯语使用现代标准阿拉伯语，保持商业官网语气。' : '英语使用自然、准确的国际商务网站语气。',
    JSON.stringify(entries)
  ].join('\n');
}

function responseError(message) {
  return Object.assign(new Error(message), { code: 'TRANSLATION_RESPONSE_INVALID' });
}

function isBatchTooLarge(error) {
  const message = String(error?.message || '');
  return /^HTTP 413\b/.test(message) || /^HTTP (400|422):.*(?:context|token|too large|length|size)/i.test(message);
}

function makeBatches(entries, maxSize) {
  const batches = [];
  let batch = [];
  let chars = 0;
  for (const entry of entries) {
    const size = entry.id.length + entry.text.length;
    if (batch.length && (batch.length >= maxSize || chars + size > MAX_BATCH_INPUT_CHARS)) {
      batches.push(batch);
      batch = [];
      chars = 0;
    }
    batch.push(entry);
    chars += size;
  }
  if (batch.length) batches.push(batch);
  return batches;
}

export function createWebsiteTranslationService({ platformStore, fetchImpl = fetch, logger = console, resolveProvider = null, onAvailabilityChange = null }) {
  let stored = null;
  const queued = new Map();
  const latestRevision = new Map();
  const latestJob = new Map();
  const progressByLocale = new Map();
  let draining = false;
  let generateLocaleSeo = null;

  function notifyAvailability(change) {
    if (!onAvailabilityChange) return;
    Promise.resolve().then(() => onAvailabilityChange(change)).catch((error) => {
      logger.error(`[网站翻译] 自动更新前台失败：${error.message}`);
    });
  }

  /**
   * 解析控制台里的能力绑定。命中时只覆盖上游 baseUrl/apiType/model/apiKey，
   * 批量大小、超时、语言对、提示词等本地业务参数保持不变；解析为 null 时完全走原逻辑。
   */
  async function effectiveUpstream(capability, config, apiKey) {
    if (typeof resolveProvider !== 'function') return { config, apiKey };
    let provider = null;
    try {
      provider = await resolveProvider(capability);
    } catch (error) {
      logWarn(logger, `[AI 供应商] 解析 ${capability} 能力失败，沿用本地配置：${error?.message || error}`);
      return { config, apiKey };
    }
    if (!provider) return { config, apiKey };
    // 防御性检查：翻译/SEO 只接受文本协议，且地址与密钥必须完整。
    if (!provider.baseUrl || !provider.apiKey || !API_TYPES.has(provider.apiType)) {
      logWarn(logger, `[AI 供应商] ${capability} 能力绑定配置不完整或协议不受支持，沿用本地配置。`);
      return { config, apiKey };
    }
    return {
      config: {
        ...config,
        baseUrl: provider.baseUrl,
        apiType: provider.apiType,
        model: provider.model || config.model
      },
      apiKey: provider.apiKey
    };
  }

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
    let translated;
    try { translated = parseJsonArray(extractText(payload, config.apiType)); }
    catch (error) { throw responseError(error.message); }
    const requested = new Set(batch.map((item) => item.id));
    const result = new Map();
    for (const item of translated) {
      if (requested.has(item?.id) && typeof item.text === 'string' && item.text.trim()) {
        result.set(item.id, item.text);
      }
    }
    return result;
  }

  async function translateBatch(config, apiKey, locale, batch, onResult, isCurrent, singleAttempts = 0) {
    if (!isCurrent()) return;
    let result;
    try {
      result = await runSerializedBatch(() => requestBatch(config, apiKey, locale, batch));
    } catch (error) {
      if (error.code !== 'TRANSLATION_RESPONSE_INVALID' && !isBatchTooLarge(error)) throw error;
      logWarn(logger, `[网站翻译] ${locale} 批次未返回有效译文（${error.message}），缩小批次重试。`);
      result = new Map();
    }
    if (!isCurrent()) return;
    if (result.size) await onResult(result);
    const missing = batch.filter((entry) => !result.has(entry.id));
    if (!missing.length || !isCurrent()) return;
    if (missing.length === 1) {
      if (singleAttempts + 1 >= SINGLE_ENTRY_ATTEMPTS) {
        throw responseError(`翻译结果多次缺少字段 ${missing[0].id}。`);
      }
      logWarn(logger, `[网站翻译] ${locale} 单条文案 ${missing[0].id} 未返回有效译文，重试。`);
      await translateBatch(config, apiKey, locale, missing, onResult, isCurrent, singleAttempts + 1);
      return;
    }
    if (missing.length < batch.length) {
      await translateBatch(config, apiKey, locale, missing, onResult, isCurrent);
      return;
    }
    const middle = Math.floor(missing.length / 2);
    await translateBatch(config, apiKey, locale, missing.slice(0, middle), onResult, isCurrent);
    await translateBatch(config, apiKey, locale, missing.slice(middle), onResult, isCurrent);
  }

  async function generateJson(prompt, options = {}) {
    const current = await loadSetting();
    if (!current.config.enabled) throw Object.assign(new Error('网站 AI 已停用，请先在网站翻译设置中启用。'), { code: 'AI_UNCONFIGURED' });
    // AI SEO 传入 capability=seo，只有真正走 SEO 生成时才解析 seo 绑定。
    const upstream = await effectiveUpstream(
      String(options.capability || 'translation'),
      current.config,
      String(current.secrets?.apiKey || '')
    );
    const config = upstream.config;
    const apiKey = upstream.apiKey;
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

  async function translate(job) {
    const { locale, content, sourceRevision } = job;
    const isCurrent = () => latestRevision.get(locale) === sourceRevision && latestJob.get(locale) === job.token;
    const current = await loadSetting();
    if (!current.config.enabled) throw Object.assign(new Error('自动翻译已停用。'), { code: 'TRANSLATION_UNCONFIGURED' });
    // 控制台已把翻译能力绑定到供应商时，使用该上游（含协议/模型/密钥）；否则完全沿用本地配置。
    const upstream = await effectiveUpstream('translation', current.config, String(current.secrets?.apiKey || ''));
    const config = upstream.config;
    const apiKey = upstream.apiKey;
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
    if (!isCurrent()) return;
    await platformStore.saveContentTranslation(locale, { content: translatedContent, sourceRevision, status: 'running', lastError: '', startedAt, finishedAt: null });
    let done = resumed;
    const onResult = async (result) => {
      if (!isCurrent()) return;
      for (const [path, value] of result) setAtPath(translatedContent, path, value);
      done += result.size;
      progressByLocale.set(locale, { done, total, updatedAt: Date.now() });
      await platformStore.saveContentTranslation(locale, { content: translatedContent, sourceRevision, status: 'running', lastError: '', startedAt, finishedAt: null });
    };
    const batches = makeBatches(remaining, config.batchSize);
    for (const [index, batch] of batches.entries()) {
      if (index > 0) await new Promise((resolve) => setTimeout(resolve, BATCH_GAP_MS));
      if (!isCurrent()) return;
      await translateBatch(config, apiKey, locale, batch, onResult, isCurrent);
    }
    if (!isCurrent()) return;
    normalizeLocalizedBrandTerms(translatedContent);
    if (generateLocaleSeo) {
      const seoResult = await generateLocaleSeo(translatedContent, locale);
      if (!isCurrent()) return;
      const localizedContent = seoResult?.content;
      if (!localizedContent || typeof localizedContent !== 'object') {
        throw new Error(`AI SEO 没有返回 ${locale} 的官网内容。`);
      }
      Object.keys(translatedContent).forEach((key) => delete translatedContent[key]);
      Object.assign(translatedContent, localizedContent);
      translatedContent.site.locale = locale;
    }
    normalizeLocalizedBrandTerms(translatedContent);
    // 翻译上游是外部输入：它返回的 HTML/链接同样必须过白名单与协议校验，
    // 否则 en/ar 页面会成为绕过 CMS 写入消毒的 XSS 通道。
    const safeTranslatedContent = sanitizeCmsContent(translatedContent);
    progressByLocale.set(locale, { done: total, total, updatedAt: Date.now() });
    await platformStore.saveContentTranslation(locale, {
      content: safeTranslatedContent,
      sourceRevision,
      status: 'ready',
      lastError: '',
      startedAt,
      finishedAt: new Date().toISOString()
    });
    notifyAvailability({ locales: [locale], status: 'ready', sourceRevision });
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
      await translate(job);
    } catch (error) {
      if (latestRevision.get(job.locale) !== job.sourceRevision || latestJob.get(job.locale) !== job.token) return;
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
          if (latestRevision.get(job.locale) !== job.sourceRevision || latestJob.get(job.locale) !== job.token) return;
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
    const scheduledLocales = locales.filter((item) => TARGET_LOCALES.includes(item));
    for (const locale of scheduledLocales) {
      const previous = existing.get(locale);
      const checkpoint = previous?.sourceRevision === sourceRevision && previous.status !== 'ready' ? previous : null;
      const token = Symbol(locale);
      latestRevision.set(locale, sourceRevision);
      latestJob.set(locale, token);
      progressByLocale.set(locale, { done: 0, total: progressTotal, updatedAt: Date.now() });
      await platformStore.saveContentTranslation(locale, {
        content: checkpoint?.content || null,
        sourceRevision,
        status: 'pending',
        lastError: '',
        startedAt: checkpoint?.startedAt || null,
        finishedAt: null
      });
      queued.set(locale, { locale, content: structuredClone(content), sourceRevision, token });
    }
    if (scheduledLocales.length) notifyAvailability({ locales: scheduledLocales, status: 'pending', sourceRevision });
    queueMicrotask(drain);
  }

  async function carryForwardMediaOnly(before, after, previousRevision, sourceRevision) {
    if (!isMediaOnlyContentChange(before, after)) return false;
    const existing = await platformStore.listContentTranslations();
    if (!TARGET_LOCALES.every((locale) => existing.some((item) => item.locale === locale && item.status === 'ready' && item.sourceRevision === previousRevision && item.content))) return false;
    for (const locale of TARGET_LOCALES) {
      const previous = existing.find((item) => item.locale === locale);
      const content = structuredClone(previous.content);
      rebaseMediaFields(content, before, after);
      await platformStore.saveContentTranslation(locale, { ...previous, content, sourceRevision, status: 'ready' });
    }
    notifyAvailability({ locales: TARGET_LOCALES, status: 'ready', sourceRevision });
    return true;
  }

  return {
    locales: TARGET_LOCALES,
    publicSettings,
    saveSettings,
    requestModels,
    generateJson,
    setLocaleSeoGenerator(generator) {
      generateLocaleSeo = typeof generator === 'function' ? generator : null;
    },
    schedule,
    carryForwardMediaOnly,
    list: async () => (await platformStore.listContentTranslations()).map((item) => {
      const progress = progressByLocale.get(item.locale);
      return { ...item, progress: progress ? { ...progress } : null };
    }),
    read: (locale) => platformStore.readContentTranslation(locale)
  };
}
