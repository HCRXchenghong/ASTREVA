/**
 * AI 供应商解析客户端。
 *
 * 统一后台的 AI 消费方（网站翻译、AI SEO、AI 生图）通过本模块复用客服引擎控制台里
 * 配置的「AI 多供应商 + 能力绑定」，不再各自维护一套独立的上游配置。
 *
 * 数据来源是客服引擎的服务间接口（由 servicebridge-proxy 以受信管理员身份调用）：
 *   GET /api/admin/ai-bindings
 *   GET /api/admin/ai-providers/{id}/secret
 *
 * 约定：
 * - 返回 null 表示「没有可用绑定 / 读取失败」，调用方必须保持既有配置不变；
 * - 结果按能力缓存 cacheTtlMs，翻译/SEO 热路径不会每次都访问 Go；
 * - 任何异常都不向外抛出，只 logger.warn 一次，避免上游抖动时刷屏；
 * - api_key 只在内存中传递，绝不写入日志或响应。
 */

// 文本类能力可用的上游协议；image 能力只接受生图协议。
const TEXT_API_TYPES = new Set(['chat_completions', 'responses', 'anthropic_messages']);
const IMAGE_API_TYPE = 'images';
// 单次读取服务间接口的超时：客服引擎不可达时尽快回退本地配置，避免拖住翻译/生图请求。
const ADMIN_JSON_TIMEOUT_MS = 5000;

function clean(value) {
  return String(value ?? '').trim();
}

function warnFunction(logger) {
  if (logger && typeof logger.warn === 'function') return logger.warn.bind(logger);
  return console.warn.bind(console);
}

function warnOnce(warned, warn, key, message) {
  if (warned.has(key)) return;
  warned.add(key);
  warn(message);
}

/**
 * 判断目标主机是否在供应商白名单内，语义与 Go 侧 domain.HostAllowed 保持一致：
 * 空白名单表示不限制；支持 `*.example.com` 通配；条目可带端口，带端口时必须匹配。
 */
function hostAllowed(url, allowedHosts) {
  const entries = String(allowedHosts || '').split(',').map((item) => item.trim()).filter(Boolean);
  if (!entries.length) return true;
  const host = url.hostname.toLowerCase().replace(/\.$/, '');
  const port = url.port || (url.protocol === 'https:' ? '443' : '80');
  return entries.some((entry) => {
    let raw = entry.toLowerCase();
    const wildcard = raw.startsWith('*.') || raw.startsWith('.');
    raw = raw.replace(/^\*\./, '').replace(/^\./, '');
    // 与 Go 侧一致：白名单条目不允许出现协议、路径、查询串等。
    if (!raw || raw.includes('://') || /[ \t/?#@]/.test(raw)) return false;
    let entryHost = raw;
    let entryPort = '';
    if (raw.startsWith('[')) {
      const end = raw.indexOf(']');
      if (end < 0) return false;
      entryHost = raw.slice(1, end);
      entryPort = raw.slice(end + 1).replace(/^:/, '');
    } else {
      const matched = raw.match(/^(.*):(\d+)$/);
      if (matched) {
        entryHost = matched[1];
        entryPort = matched[2];
      }
    }
    entryHost = entryHost.replace(/\.$/, '');
    if (entryPort && entryPort !== port) return false;
    return wildcard ? host === entryHost || host.endsWith(`.${entryHost}`) : host === entryHost;
  });
}

function apiTypeCompatible(capability, apiType) {
  if (capability === 'image') return apiType === IMAGE_API_TYPE;
  return TEXT_API_TYPES.has(apiType);
}

export function createAIProviderClient({ adminJson, logger = console, cacheTtlMs = 60000 } = {}) {
  if (typeof adminJson !== 'function') throw new Error('AI 供应商客户端缺少 adminJson 通道。');
  const warn = warnFunction(logger);
  const ttlMs = Number.isFinite(Number(cacheTtlMs)) ? Math.max(0, Number(cacheTtlMs)) : 60000;
  // capability -> { value, expiresAt }
  const cache = new Map();
  // 同一能力的并发解析只打一次 Go。
  const pending = new Map();
  const warned = new Set();
  let generation = 0;

  function readCache(capability) {
    const entry = cache.get(capability);
    if (!entry || entry.expiresAt <= Date.now()) return undefined;
    return entry.value;
  }

  function writeCache(capability, value) {
    cache.set(capability, { value, expiresAt: Date.now() + ttlMs });
  }

  function clearWarnings(capability) {
    for (const key of warned) {
      if (key === 'bindings' || key.startsWith(`${capability}:`)) warned.delete(key);
    }
  }

  function reject(capability, key, message) {
    warnOnce(warned, warn, `${capability}:${key}`, message);
    return null;
  }

  async function load(capability) {
    let bindingsPayload;
    try {
      bindingsPayload = await adminJson('/api/admin/ai-bindings', { signal: AbortSignal.timeout(ADMIN_JSON_TIMEOUT_MS) });
    } catch (error) {
      return reject('bindings', 'read', `[AI 供应商] 读取能力绑定失败，暂停使用控制台供应商：${error?.message || error}`);
    }

    const bindings = Array.isArray(bindingsPayload?.bindings) ? bindingsPayload.bindings : [];
    const binding = bindings.find((item) => clean(item?.capability).toLowerCase() === capability);
    const providerId = clean(binding?.provider_id);
    // 未绑定（或显式解除绑定）：不是错误，调用方沿用本地配置。
    if (!binding || !providerId) return null;
    clearWarnings('bindings');

    let secret;
    try {
      secret = await adminJson(`/api/admin/ai-providers/${encodeURIComponent(providerId)}/secret`, {
        signal: AbortSignal.timeout(ADMIN_JSON_TIMEOUT_MS)
      });
    } catch (error) {
      return reject(capability, `secret:${providerId}`, `[AI 供应商] 读取供应商 ${providerId} 配置失败，沿用本地配置：${error?.message || error}`);
    }
    if (!secret || typeof secret !== 'object') {
      return reject(capability, `secret:${providerId}`, `[AI 供应商] 供应商 ${providerId} 未返回有效配置，沿用本地配置。`);
    }
    if (secret.enabled !== true) {
      return reject(capability, `disabled:${providerId}`, `[AI 供应商] 供应商 ${providerId} 已停用，${capability} 能力沿用本地配置。`);
    }

    const baseUrl = clean(secret.base_url);
    const apiKey = clean(secret.api_key);
    if (!baseUrl || !apiKey) {
      return reject(capability, `missing:${providerId}`, `[AI 供应商] 供应商 ${providerId} 缺少 base_url 或 api_key，${capability} 能力沿用本地配置。`);
    }

    let parsedUrl;
    try {
      parsedUrl = new URL(baseUrl);
    } catch {
      return reject(capability, `url:${providerId}`, `[AI 供应商] 供应商 ${providerId} 的 base_url 不是有效 URL，${capability} 能力沿用本地配置。`);
    }
    if (!['http:', 'https:'].includes(parsedUrl.protocol)) {
      return reject(capability, `url:${providerId}`, `[AI 供应商] 供应商 ${providerId} 的 base_url 仅支持 HTTP/HTTPS，${capability} 能力沿用本地配置。`);
    }

    const apiType = clean(secret.api_type);
    if (!apiTypeCompatible(capability, apiType)) {
      return reject(capability, `type:${providerId}`, `[AI 供应商] 供应商 ${providerId} 的协议 ${apiType || '(空)'} 不适用于 ${capability} 能力，沿用本地配置。`);
    }

    const allowedHosts = clean(secret.allowed_hosts);
    if (!hostAllowed(parsedUrl, allowedHosts)) {
      return reject(capability, `host:${providerId}`, `[AI 供应商] 供应商 ${providerId} 的目标主机不在 allowed_hosts 白名单内，${capability} 能力沿用本地配置。`);
    }

    clearWarnings(capability);
    return {
      baseUrl,
      apiKey,
      // 能力绑定的模型优先；为空时回退供应商默认模型（Go 的 secret 接口已做同样兜底）。
      model: clean(binding?.model) || clean(secret.model),
      apiType,
      providerId,
      allowedHosts
    };
  }

  async function loadAndCache(capability) {
    const startGeneration = generation;
    const value = await load(capability);
    // invalidate() 之后完成的旧请求不得写回缓存。
    if (startGeneration === generation) writeCache(capability, value);
    return value;
  }

  /** 解析某能力对应的上游；返回 null 表示调用方应保持既有配置。 */
  function resolve(capability) {
    const name = clean(capability).toLowerCase();
    if (!name) return Promise.resolve(null);
    const cached = readCache(name);
    if (cached !== undefined) return Promise.resolve(cached);
    if (pending.has(name)) return pending.get(name);

    let task;
    task = loadAndCache(name)
      .catch((error) => {
        // load 内部已捕获预期异常，这里兜底保证绝不向调用方抛出。
        warnOnce(warned, warn, `${name}:unexpected`, `[AI 供应商] 解析 ${name} 能力时发生意外错误，沿用本地配置：${error?.message || error}`);
        return null;
      })
      .finally(() => {
        if (pending.get(name) === task) pending.delete(name);
      });
    pending.set(name, task);
    return task;
  }

  /** 控制台的供应商/绑定保存后立即失效缓存，让下一次请求读到新配置。 */
  function invalidate() {
    generation += 1;
    cache.clear();
    pending.clear();
    warned.clear();
  }

  return { resolve, invalidate };
}
