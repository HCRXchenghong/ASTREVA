import crypto from 'node:crypto';

const DEFAULT_MODELS = ['gpt-image-1', 'dall-e-3', 'dall-e-2'];
const ALLOWED_SIZES = new Set(['auto', '256x256', '512x512', '1024x1024', '1024x1536', '1536x1024', '1792x1024', '1024x1792']);
const ALLOWED_QUALITIES = new Set(['auto', 'low', 'medium', 'high', 'standard', 'hd']);

function commaList(value, fallback = []) {
  const items = String(value || '').split(',').map((item) => item.trim()).filter(Boolean);
  return items.length ? items : fallback;
}

function endpointUrl(baseUrl, pathname) {
  const base = new URL(String(baseUrl || 'https://api.openai.com/v1').replace(/\/$/, '') + '/');
  return new URL(pathname.replace(/^\//, ''), base);
}

function assertEndpointAllowed(url, allowedHosts) {
  if (!allowedHosts.has(url.hostname)) throw new Error(`AI 接口主机 ${url.hostname} 未加入 ADMIN_AI_ALLOWED_HOSTS 白名单。`);
  const loopback = ['127.0.0.1', 'localhost', '::1'].includes(url.hostname);
  if (url.protocol !== 'https:' && !(loopback && url.protocol === 'http:')) throw new Error('AI 接口必须使用 HTTPS（本机测试除外）。');
}

async function responseBytes(response, limit) {
  const declared = Number(response.headers.get('content-length') || 0);
  if (declared > limit) throw new Error('AI 图片响应超过安全大小限制。');
  const bytes = Buffer.from(await response.arrayBuffer());
  if (bytes.length > limit) throw new Error('AI 图片响应超过安全大小限制。');
  return bytes;
}

export function createMediaAiService({
  mediaStore,
  apiKey = process.env.ADMIN_OPENAI_IMAGE_API_KEY || process.env.OPENAI_API_KEY || '',
  baseUrl = process.env.ADMIN_OPENAI_IMAGE_BASE_URL || 'https://api.openai.com/v1',
  defaultModel = process.env.ADMIN_OPENAI_IMAGE_MODEL || 'gpt-image-1',
  allowedHosts = process.env.ADMIN_AI_ALLOWED_HOSTS || 'api.openai.com',
  downloadHosts = process.env.ADMIN_AI_IMAGE_DOWNLOAD_HOSTS || 'api.openai.com,oaidalleapiprodscus.blob.core.windows.net',
  configuredModels = process.env.ADMIN_AI_IMAGE_MODELS || '',
  requestTimeoutMs = process.env.ADMIN_AI_REQUEST_TIMEOUT_MS || 180_000,
  fetchImpl = fetch
}) {
  const hostAllowlist = new Set(commaList(allowedHosts, ['api.openai.com']));
  const downloadAllowlist = new Set(commaList(downloadHosts, ['api.openai.com', 'oaidalleapiprodscus.blob.core.windows.net']));
  const modelAllowlist = new Set(commaList(configuredModels, DEFAULT_MODELS));
  modelAllowlist.add(defaultModel);
  let modelCache = null;
  const previews = new Map();
  const previewTtlMs = 15 * 60 * 1000;

  function capability() {
    return { configured: Boolean(apiKey), defaultModel, models: [...modelAllowlist] };
  }

  async function models({ refresh = false } = {}) {
    if (!apiKey) return capability();
    if (!refresh && modelCache?.expiresAt > Date.now()) return { configured: true, defaultModel, models: modelCache.models };
    const url = endpointUrl(baseUrl, 'models');
    assertEndpointAllowed(url, hostAllowlist);
    const response = await fetchImpl(url, {
      headers: { Authorization: `Bearer ${apiKey}`, Accept: 'application/json' },
      signal: AbortSignal.timeout(20_000)
    });
    if (!response.ok) throw new Error(`同步 AI 模型失败（HTTP ${response.status}）。`);
    const payload = await response.json();
    const discovered = (Array.isArray(payload.data) ? payload.data : [])
      .map((item) => String(item?.id || '')).filter((id) => /image|dall-e/i.test(id));
    const available = [...new Set([...modelAllowlist].filter((id) => discovered.includes(id)).concat(discovered))];
    modelCache = { models: available.length ? available : [...modelAllowlist], expiresAt: Date.now() + 10 * 60 * 1000 };
    return { configured: true, defaultModel, models: modelCache.models };
  }

  function prunePreviews() {
    const now = Date.now();
    for (const [id, item] of previews) if (item.expiresAtMs <= now) previews.delete(id);
    while (previews.size > 12) previews.delete(previews.keys().next().value);
  }

  async function generateBytes({ prompt, model = defaultModel, size = '1024x1024', quality = 'auto' }) {
    if (!apiKey) throw new Error('未配置 ADMIN_OPENAI_IMAGE_API_KEY，AI 生图暂不可用。');
    const cleanPrompt = String(prompt || '').trim();
    if (!cleanPrompt || cleanPrompt.length > 4000) throw new Error('生图提示词必须在 1–4000 字之间。');
    if (!modelAllowlist.has(model) && !modelCache?.models.includes(model)) throw new Error('所选 AI 模型未在后台允许列表中。');
    if (!ALLOWED_SIZES.has(size)) throw new Error('所选生图尺寸不受支持。');
    if (!ALLOWED_QUALITIES.has(quality)) throw new Error('所选生图质量不受支持。');
    const url = endpointUrl(baseUrl, 'images/generations');
    assertEndpointAllowed(url, hostAllowlist);
    const request = { model, prompt: cleanPrompt, n: 1, size };
    if (quality !== 'auto') request.quality = quality;
    if (/dall-e/i.test(model)) request.response_format = 'b64_json';
    else request.output_format = 'png';
    const response = await fetchImpl(url, {
      method: 'POST',
      headers: { Authorization: `Bearer ${apiKey}`, 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(request),
      signal: AbortSignal.timeout(Math.max(10_000, Math.min(Number(requestTimeoutMs) || 180_000, 600_000)))
    });
    const responseText = await response.text();
    if (responseText.length > 90 * 1024 * 1024) throw new Error('AI 接口响应超过安全限制。');
    let payload;
    try { payload = JSON.parse(responseText); } catch { throw new Error(`AI 接口返回了无效 JSON（HTTP ${response.status}）。`); }
    if (!response.ok) throw new Error(payload.error?.message || `AI 生图失败（HTTP ${response.status}）。`);
    const result = payload.data?.[0];
    let image;
    if (result?.b64_json) {
      image = Buffer.from(result.b64_json, 'base64');
      if (!image.length || image.length > 64 * 1024 * 1024) throw new Error('AI 返回的图片数据无效或过大。');
    } else if (result?.url) {
      const imageUrl = new URL(result.url);
      if (imageUrl.protocol !== 'https:' || !downloadAllowlist.has(imageUrl.hostname)) throw new Error('AI 返回的图片地址不在下载白名单中。');
      const imageResponse = await fetchImpl(imageUrl, { signal: AbortSignal.timeout(60_000), redirect: 'error' });
      if (!imageResponse.ok) throw new Error(`下载 AI 图片失败（HTTP ${imageResponse.status}）。`);
      image = await responseBytes(imageResponse, 64 * 1024 * 1024);
    } else {
      throw new Error('AI 接口未返回可入库的图片。');
    }
    return {
      image,
      name: `ai-${new Date().toISOString().replace(/[:.]/g, '-')}.png`,
      metadata: { ai: { prompt: cleanPrompt, model, size, quality, revisedPrompt: result.revised_prompt || '' } }
    };
  }

  async function generate({ category = '未分类', preserveOriginal = true, ...options }) {
    const generated = await generateBytes(options);
    return mediaStore.uploadSmartImage({
      buffer: generated.image,
      originalName: generated.name,
      category,
      preserveOriginal,
      sourceType: 'ai',
      metadata: generated.metadata
    });
  }

  async function preview(options) {
    prunePreviews();
    const generated = await generateBytes(options);
    const id = crypto.randomUUID();
    const expiresAtMs = Date.now() + previewTtlMs;
    previews.set(id, { ...generated, expiresAtMs });
    return {
      id,
      dataUrl: `data:image/png;base64,${generated.image.toString('base64')}`,
      bytes: generated.image.length,
      expiresAt: new Date(expiresAtMs).toISOString(),
      ...generated.metadata.ai
    };
  }

  async function commitPreview({ previewId, category = '未分类', preserveOriginal = true }) {
    prunePreviews();
    const generated = previews.get(String(previewId || ''));
    if (!generated) throw new Error('AI 临时结果不存在或已过期，请重新生成。');
    const media = await mediaStore.uploadSmartImage({
      buffer: generated.image,
      originalName: generated.name,
      category,
      preserveOriginal,
      sourceType: 'ai',
      metadata: generated.metadata
    });
    previews.delete(String(previewId));
    return media;
  }

  function discardPreview(previewId) {
    prunePreviews();
    return previews.delete(String(previewId || ''));
  }

  return { capability, models, generate, preview, commitPreview, discardPreview };
}
