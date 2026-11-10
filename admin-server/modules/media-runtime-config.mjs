const DEFAULT_IMAGE_MODELS = 'gpt-image-1,dall-e-3,dall-e-2';
const DEFAULT_MEDIA_MIME_TYPES = [
  'image/jpeg', 'image/png', 'image/webp', 'image/gif', 'image/avif',
  'video/mp4', 'video/webm', 'video/quicktime', 'application/pdf', 'application/zip',
  'application/acad', 'application/dxf', 'image/vnd.dwg'
];

function clean(value, max = 512) {
  return String(value ?? '').trim().slice(0, max);
}

function bool(value, fallback = false) {
  if (value === true || value === 'true') return true;
  if (value === false || value === 'false') return false;
  return fallback;
}

function boundedNumber(value, fallback, min, max) {
  const number = Number(value);
  return Number.isFinite(number) ? Math.max(min, Math.min(number, max)) : fallback;
}

function urlValue(value, label, { optional = false } = {}) {
  const text = clean(value, 2048).replace(/\/$/, '');
  if (!text && optional) return '';
  let url;
  try { url = new URL(text); } catch { throw new Error(`${label}不是有效 URL。`); }
  const loopback = ['127.0.0.1', 'localhost', '::1'].includes(url.hostname);
  if (url.protocol !== 'https:' && !(loopback && url.protocol === 'http:')) {
    throw new Error(`${label}必须使用 HTTPS（本机测试除外）。`);
  }
  if (url.username || url.password || url.search || url.hash) throw new Error(`${label}不能包含账号、密码、查询参数或锚点。`);
  return url.toString().replace(/\/$/, '');
}

function commaList(value, fallback) {
  const list = [...new Set(clean(value, 4096).split(',').map((item) => item.trim()).filter(Boolean))];
  return (list.length ? list : fallback).join(',');
}

function secretMask(value) {
  const text = String(value || '');
  return text ? `••••${text.slice(-4)}` : '';
}

export function mediaRuntimeConfigFromEnv(env = process.env) {
  return {
    storage: {
      driver: env.ADMIN_MEDIA_DRIVER === 'local' ? 'local' : 's3',
      endpoint: clean(env.S3_ENDPOINT, 2048),
      region: clean(env.S3_REGION || 'us-east-1', 100),
      bucket: clean(env.S3_BUCKET, 255),
      accessKeyId: clean(env.S3_ACCESS_KEY_ID, 1024),
      secretAccessKey: clean(env.S3_SECRET_ACCESS_KEY, 4096),
      sessionToken: clean(env.S3_SESSION_TOKEN, 8192),
      forcePathStyle: bool(env.S3_FORCE_PATH_STYLE, false),
      prefix: clean(env.ADMIN_S3_KEY_PREFIX || env.S3_KEY_PREFIX || 'site-admin', 512),
      publicBaseUrl: clean(env.S3_PUBLIC_BASE_URL, 2048),
      serverSideEncryption: clean(env.ADMIN_S3_SERVER_SIDE_ENCRYPTION, 100)
    },
    image: {
      variantWidths: clean(env.ADMIN_IMAGE_VARIANT_WIDTHS || '400,800,1200,1600', 200),
      webpQuality: boundedNumber(env.ADMIN_IMAGE_WEBP_QUALITY, 82, 40, 100),
      keepOriginal: bool(env.ADMIN_IMAGE_KEEP_ORIGINAL, true),
      maxUploadBytes: boundedNumber(env.ADMIN_UPLOAD_MAX_BYTES, 200 * 1024 * 1024, 1, 2 * 1024 * 1024 * 1024),
      allowedMimeTypes: commaList(env.ADMIN_MEDIA_ALLOWED_MIME_TYPES, DEFAULT_MEDIA_MIME_TYPES)
    },
    aiImage: {
      apiKey: clean(env.ADMIN_OPENAI_IMAGE_API_KEY || env.OPENAI_API_KEY, 8192),
      baseUrl: clean(env.ADMIN_OPENAI_IMAGE_BASE_URL || 'https://api.openai.com/v1', 2048),
      defaultModel: clean(env.ADMIN_OPENAI_IMAGE_MODEL || 'gpt-image-1', 200),
      allowedHosts: clean(env.ADMIN_AI_ALLOWED_HOSTS || 'api.openai.com', 4096),
      downloadHosts: clean(env.ADMIN_AI_IMAGE_DOWNLOAD_HOSTS || 'api.openai.com,oaidalleapiprodscus.blob.core.windows.net', 4096),
      configuredModels: clean(env.ADMIN_AI_IMAGE_MODELS || DEFAULT_IMAGE_MODELS, 4096),
      requestTimeoutMs: boundedNumber(env.ADMIN_AI_REQUEST_TIMEOUT_MS, 180_000, 10_000, 600_000)
    }
  };
}

export function persistedMediaRuntimeConfig(config) {
  return {
    config: {
      storage: { ...config.storage, accessKeyId: undefined, secretAccessKey: undefined, sessionToken: undefined },
      image: { ...config.image },
      aiImage: { ...config.aiImage, apiKey: undefined }
    },
    secrets: {
      storageAccessKeyId: config.storage.accessKeyId || '',
      storageSecretAccessKey: config.storage.secretAccessKey || '',
      storageSessionToken: config.storage.sessionToken || '',
      aiImageApiKey: config.aiImage.apiKey || ''
    }
  };
}

export function hydrateMediaRuntimeConfig(stored, fallback = mediaRuntimeConfigFromEnv()) {
  if (!stored) return structuredClone(fallback);
  return {
    storage: {
      ...fallback.storage,
      ...(stored.config?.storage || {}),
      // 存储驱动是部署级开关（本地卷 / S3），不能由旧的运行时配置覆盖环境变量。
      driver: fallback.storage.driver,
      accessKeyId: stored.secrets?.storageAccessKeyId ?? fallback.storage.accessKeyId,
      secretAccessKey: stored.secrets?.storageSecretAccessKey ?? fallback.storage.secretAccessKey,
      sessionToken: stored.secrets?.storageSessionToken ?? fallback.storage.sessionToken
    },
    image: { ...fallback.image, ...(stored.config?.image || {}) },
    aiImage: {
      ...fallback.aiImage,
      ...(stored.config?.aiImage || {}),
      apiKey: stored.secrets?.aiImageApiKey ?? fallback.aiImage.apiKey
    }
  };
}

export function normalizeStorageUpdate(payload, current) {
  const next = {
    ...current,
    driver: 's3',
    endpoint: urlValue(payload.endpoint ?? current.endpoint, 'S3 Endpoint'),
    region: clean(payload.region ?? current.region, 100) || 'us-east-1',
    bucket: clean(payload.bucket ?? current.bucket, 255),
    forcePathStyle: bool(payload.forcePathStyle, current.forcePathStyle),
    prefix: clean(payload.prefix ?? current.prefix, 512).replace(/^\/+|\/+$/g, ''),
    publicBaseUrl: urlValue(payload.publicBaseUrl ?? current.publicBaseUrl, 'Public Base URL'),
    serverSideEncryption: clean(payload.serverSideEncryption ?? current.serverSideEncryption, 100),
    accessKeyId: clean(payload.accessKeyId, 1024) || current.accessKeyId,
    secretAccessKey: clean(payload.secretAccessKey, 4096) || current.secretAccessKey,
    sessionToken: payload.clearSessionToken ? '' : (clean(payload.sessionToken, 8192) || current.sessionToken)
  };
  if (!next.bucket) throw new Error('Bucket 不能为空。');
  if (!next.accessKeyId || !next.secretAccessKey) throw new Error('Access Key ID 和 Secret Access Key 必须配置完整。');
  return next;
}

export function normalizeAiImageUpdate(payload, current) {
  const baseUrl = urlValue(payload.baseUrl ?? current.baseUrl, 'AI Base URL');
  const allowedHosts = commaList(payload.allowedHosts ?? current.allowedHosts, ['api.openai.com']);
  if (!allowedHosts.split(',').includes(new URL(baseUrl).hostname)) throw new Error('AI Base URL 的主机必须同时加入请求主机白名单。');
  const next = {
    ...current,
    apiKey: payload.clearApiKey ? '' : (clean(payload.apiKey, 8192) || current.apiKey),
    baseUrl,
    defaultModel: clean(payload.defaultModel ?? current.defaultModel, 200),
    allowedHosts,
    downloadHosts: commaList(payload.downloadHosts ?? current.downloadHosts, ['api.openai.com', 'oaidalleapiprodscus.blob.core.windows.net']),
    configuredModels: commaList(payload.configuredModels ?? current.configuredModels, ['gpt-image-1', 'dall-e-3', 'dall-e-2']),
    requestTimeoutMs: boundedNumber(payload.requestTimeoutMs, current.requestTimeoutMs, 10_000, 600_000)
  };
  if (!next.defaultModel) throw new Error('默认生图模型不能为空。');
  if (!next.configuredModels.split(',').includes(next.defaultModel)) {
    next.configuredModels = `${next.defaultModel},${next.configuredModels}`;
  }
  return next;
}

export function normalizeImageUpdate(payload, current) {
  const widthValues = [...new Set(String(payload.variantWidths ?? current.variantWidths)
    .split(',').map((value) => Number.parseInt(value.trim(), 10)).filter(Number.isFinite))]
    .filter((value) => value >= 100 && value <= 6000).sort((a, b) => a - b);
  if (!widthValues.length || widthValues.length > 12) throw new Error('图片变体宽度必须填写 1–12 个 100–6000 之间的整数。');

  const requestedTypes = [...new Set(String(payload.allowedMimeTypes ?? current.allowedMimeTypes)
    .split(',').map((value) => value.trim().toLowerCase()).filter(Boolean))];
  const unsupported = requestedTypes.filter((value) => !DEFAULT_MEDIA_MIME_TYPES.includes(value));
  if (!requestedTypes.length) throw new Error('至少需要允许一种媒体 Content-Type。');
  if (unsupported.length) throw new Error(`暂不支持以下媒体 Content-Type：${unsupported.join('、')}。`);

  const requestedMaxBytes = payload.maxUploadMb === undefined
    ? payload.maxUploadBytes
    : Number(payload.maxUploadMb) * 1024 * 1024;
  return {
    ...current,
    variantWidths: widthValues.join(','),
    webpQuality: Math.round(boundedNumber(payload.webpQuality, current.webpQuality, 40, 100)),
    keepOriginal: bool(payload.keepOriginal, current.keepOriginal),
    maxUploadBytes: Math.round(boundedNumber(requestedMaxBytes, current.maxUploadBytes, 1024 * 1024, 2 * 1024 * 1024 * 1024)),
    allowedMimeTypes: requestedTypes.join(',')
  };
}

export function publicMediaRuntimeConfig(config) {
  return {
    storage: {
      ...config.storage,
      accessKeyId: '',
      secretAccessKey: '',
      sessionToken: '',
      accessKeyConfigured: Boolean(config.storage.accessKeyId),
      accessKeyMasked: secretMask(config.storage.accessKeyId),
      secretAccessKeyConfigured: Boolean(config.storage.secretAccessKey),
      sessionTokenConfigured: Boolean(config.storage.sessionToken)
    },
    image: { ...config.image },
    aiImage: {
      ...config.aiImage,
      apiKey: '',
      apiKeyConfigured: Boolean(config.aiImage.apiKey),
      apiKeyMasked: secretMask(config.aiImage.apiKey)
    }
  };
}

export function storageIdentity(config) {
  return [config.endpoint, config.bucket, config.prefix, config.publicBaseUrl].map((item) => String(item || '').replace(/\/$/, '')).join('|');
}
