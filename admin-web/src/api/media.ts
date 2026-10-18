import client from './client';

// 统一后台站点接口：/site-admin-api/media/*（同源 Cookie 鉴权，2FA 会话由浏览器自动携带）。
// 返回结构以 admin-server/modules/media-router.mjs 为准。

const BASE = '/site-admin-api/media';

/** 素材记录（字段与 platform-store 的 site_media 行一致）。 */
export interface MediaVariant {
  width: number;
  height: number;
  key: string;
  url: string;
  size: number;
  mime: string;
}

export interface MediaItem {
  id: string;
  name: string;
  key: string;
  url: string;
  mime: string;
  type: 'image' | 'video' | 'file' | string;
  size: number;
  storageDriver?: string;
  category?: string;
  sourceType?: string;
  originalKey?: string;
  width?: number | null;
  height?: number | null;
  checksumSha256?: string;
  variants?: { webp?: MediaVariant[]; original?: Omit<MediaVariant, 'width' | 'height'> };
  metadata?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
}

export interface MediaMeta {
  total: number;
  page: number;
  pageSize: number;
  pages: number;
}

export interface MediaCategory {
  name: string;
  count: number;
}

export interface MediaStorageHealth {
  driver?: string;
  ok?: boolean;
  bucket?: string;
  endpoint?: string;
  prefix?: string;
  message?: string;
}

export interface MediaStats {
  total?: number;
  images?: number;
  videos?: number;
  files?: number;
  categories?: number;
  totalBytes?: number;
  storedBytes?: number;
  storage?: MediaStorageHealth;
}

export interface MediaRuntime {
  service?: {
    ok?: boolean;
    pid?: number;
    node?: string;
    platform?: string;
    startedAt?: string;
    uptimeSeconds?: number;
  };
  database?: { ok?: boolean; driver?: string };
  storage?: MediaStorageHealth;
  processing?: { ok?: boolean; engine?: string };
  cpu?: {
    cores?: number;
    model?: string;
    loadAverage?: number[];
    usagePercent?: number;
    processUsagePercent?: number;
  };
  memory?: {
    rss?: number;
    heapUsed?: number;
    heapTotal?: number;
    systemTotal?: number;
    systemFree?: number;
  };
  media?: MediaStats;
  disk?: { available?: boolean; total?: number; free?: number; used?: number };
  generatedAt?: string;
}

export interface AiCapability {
  configured?: boolean;
  defaultModel?: string;
  models?: string[];
}

export interface AiPreviewItem {
  id: string;
  dataUrl: string;
  bytes: number;
  expiresAt: string;
  size?: string;
  quality?: string;
  model?: string;
  name?: string;
}

export interface MediaSettings {
  config?: {
    storage?: Record<string, any>;
    image?: Record<string, any>;
    aiImage?: Record<string, any>;
  };
  storageHealth?: MediaStorageHealth;
  aiImage?: AiCapability;
}

export interface MediaListQuery {
  q?: string;
  category?: string;
  type?: string;
  page?: number;
  pageSize?: number;
}

export interface MediaUploadOptions {
  file: File;
  mode?: 'smart' | 'original' | 'auto';
  category?: string;
  preserveOriginal?: boolean;
}

// ---------- 列表 / 统计 / 分类 ----------
export const fetchMediaList = (params: MediaListQuery) =>
  client
    .get<{ data: MediaItem[]; meta: MediaMeta }>(BASE, { params })
    .then((r) => r.data);

export const fetchMediaStats = () =>
  client.get<{ data: MediaStats }>(`${BASE}/stats`).then((r) => r.data);

export const fetchMediaCategories = () =>
  client.get<{ data: MediaCategory[] }>(`${BASE}/categories`).then((r) => r.data);

export const createMediaCategory = (name: string) =>
  client.post<{ data: MediaCategory }>(`${BASE}/categories`, { name }).then((r) => r.data);

export const deleteMediaCategory = (name: string) =>
  client
    .delete<{ data: { name: string; reassigned: number } }>(
      `${BASE}/categories/${encodeURIComponent(name)}`
    )
    .then((r) => r.data);

// ---------- 上传 / 替换 ----------
function uploadForm(options: MediaUploadOptions): FormData {
  const form = new FormData();
  form.append('file', options.file);
  form.append('mode', options.mode || 'smart');
  form.append('category', options.category || '未分类');
  form.append('preserveOriginal', String(options.preserveOriginal !== false));
  return form;
}

/** 后台上传（multipart）。大文件请使用 presign + confirm 直传。 */
export const uploadMedia = (options: MediaUploadOptions) =>
  client
    .post<{ data: MediaItem }>(`${BASE}/upload`, uploadForm(options), { timeout: 0 })
    .then((r) => r.data);

/** 稳定替换：保持旧 URL / 对象 Key 不变。 */
export const replaceMedia = (id: string, options: MediaUploadOptions) =>
  client
    .post<{ data: MediaItem }>(
      `${BASE}/${encodeURIComponent(id)}/replace`,
      uploadForm(options),
      { timeout: 0 }
    )
    .then((r) => r.data);

// ---------- 预签名直传 ----------
export interface PresignResult {
  uploadUrl: string;
  method: string;
  headers: Record<string, string>;
  expiresAt: string;
  token: string;
  key: string;
}

export const presignMediaUpload = (payload: {
  name: string;
  mime: string;
  mimeType?: string;
  size: number;
  category?: string;
  mode?: string;
  preserveOriginal?: boolean;
}) => client.post<{ data: PresignResult }>(`${BASE}/presign`, payload).then((r) => r.data);

export const confirmMediaUpload = (token: string) =>
  client.post<{ data: MediaItem }>(`${BASE}/confirm`, { token }).then((r) => r.data);

/** 浏览器直接 PUT 到 R2；失败时给出明确的 CORS 提示。 */
export async function putToStorage(direct: PresignResult, file: File): Promise<void> {
  let response: Response;
  try {
    response = await fetch(direct.uploadUrl, {
      method: direct.method || 'PUT',
      headers: direct.headers || {},
      body: file,
    });
  } catch {
    throw new Error('浏览器无法直连对象存储（网络或 CORS 被拒绝）。请检查 Bucket 的 CORS AllowedOrigins 是否包含当前后台域名。');
  }
  if (!response.ok) {
    throw new Error(
      `R2 直传失败（HTTP ${response.status}）。请检查 Bucket CORS 是否允许 PUT 以及 AllowedHeaders 是否包含 Content-Type。`
    );
  }
}

// ---------- 详情 / 引用 / 删除 ----------
export const fetchMedia = (id: string) =>
  client.get<{ data: MediaItem }>(`${BASE}/${encodeURIComponent(id)}`).then((r) => r.data);

export const fetchMediaReferences = (id: string) =>
  client
    .get<{ data: { media: MediaItem; references: string[] } }>(
      `${BASE}/${encodeURIComponent(id)}/references`
    )
    .then((r) => r.data);

export const deleteMedia = (id: string) =>
  client.delete<{ data: MediaItem }>(`${BASE}/${encodeURIComponent(id)}`).then((r) => r.data);

export interface BatchDeleteResult {
  deleted: MediaItem[];
  blocked: { id: string; name: string; references: string[] }[];
  failed: { id: string; message: string }[];
}

export const batchDeleteMedia = (ids: string[]) =>
  client
    .post<{ data: BatchDeleteResult }>(`${BASE}/batch-delete`, { ids })
    .then((r) => r.data);

// ---------- 运行状态 / 设置（只读） ----------
export const fetchMediaRuntime = () =>
  client.get<{ data: MediaRuntime }>(`${BASE}/runtime`).then((r) => r.data);

/** 只读运行配置；写入仍由旧系统设置页负责。 */
export const fetchMediaSettings = () =>
  client
    .get<{ data: MediaSettings }>('/site-admin-api/system/media-settings')
    .then((r) => r.data);

// ---------- AI 生图 ----------
export const fetchAiModels = (refresh = false) =>
  client
    .get<{ data: AiCapability }>(`${BASE}/ai-models`, {
      params: refresh ? { refresh: 'true' } : undefined,
    })
    .then((r) => r.data);

export const aiGenerate = (payload: {
  prompt: string;
  size?: string;
  quality?: string;
  model?: string;
  category?: string;
}) => client.post<{ data: MediaItem }>(`${BASE}/ai-generate`, payload).then((r) => r.data);

/** 生成临时结果（15 分钟有效，不自动入库）。 */
export const aiPreview = (payload: {
  prompt: string;
  size?: string;
  quality?: string;
  model?: string;
}) => client.post<{ data: AiPreviewItem }>(`${BASE}/ai-preview`, payload).then((r) => r.data);

export const aiCommitPreview = (
  id: string,
  payload: { category?: string; preserveOriginal?: boolean }
) =>
  client
    .post<{ data: MediaItem }>(`${BASE}/ai-preview/${encodeURIComponent(id)}/commit`, payload)
    .then((r) => r.data);

export const aiDiscardPreview = (id: string) =>
  client
    .delete<{ data: { id: string; discarded: boolean } }>(
      `${BASE}/ai-preview/${encodeURIComponent(id)}`
    )
    .then((r) => r.data);
