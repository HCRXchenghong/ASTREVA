import client from './client';

// 官网内容编辑器的站点后台接口封装。
// 全部走 /site-admin-api/*，与 admin-server 同源，管理员会话 Cookie 由浏览器自动携带。
// 这里的请求形状与 admin-server/modules/content-router.mjs、media-router.mjs、
// publish-router.mjs、website-seo-router.mjs、leads-router.mjs 完全一致。

export interface MediaItem {
  id: string;
  name: string;
  url: string;
  type: 'image' | 'video' | 'file' | string;
  mime?: string;
  size?: number;
  category?: string;
  sourceType?: string;
  width?: number | null;
  height?: number | null;
  createdAt?: string;
  variants?: Record<string, any>;
  [key: string]: any;
}

export interface FallbackVersion {
  id: string;
  name: string;
  createdAt?: string;
  updatedAt?: string;
  active?: boolean;
}

export interface SiteContent {
  site: Record<string, any>;
  home: Record<string, any>;
  productCenter: Record<string, any>;
  support: Record<string, any>;
  categories: Record<string, any>[];
  products: Record<string, any>[];
  faqs: Record<string, any>[];
  staticPages: Record<string, any>[];
  [key: string]: any;
}

export interface ContentBootstrap {
  content: SiteContent;
  contentRevision: number;
  leads: Record<string, any>[];
  media: MediaItem[];
  fallbackVersions: FallbackVersion[];
  fallbackActiveId: string;
  counts: {
    categories: number;
    products: number;
    faqs: number;
    leads: number;
    newLeads: number;
  };
  generatedAt: string;
}

export interface BootstrapPayload {
  data: ContentBootstrap;
  meta: { previewUrl?: string; [key: string]: any };
}

export interface RebuildState {
  running: boolean;
  queued: boolean;
  ok?: boolean;
  url?: string;
  lastBuild?: {
    status?: string;
    exitCode?: number;
    finishedAt?: string;
    [key: string]: any;
  } | null;
  message?: string;
}

export interface SiteStatus {
  preview: { url: string; draftUrl: string; [key: string]: any };
  rebuild: RebuildState;
}

/** GET /site-admin-api/bootstrap */
export const fetchContentBootstrap = () =>
  client.get<BootstrapPayload>('/site-admin-api/bootstrap').then((r) => r.data);

/**
 * POST /site-admin-api/save
 * section 取值与旧编辑器一致：content / site / home / productCenter / support /
 * category / product / faq / staticPage / lead。
 */
export const saveContentSection = (payload: {
  section: string;
  data: unknown;
  revision?: number;
}) =>
  client
    .post<BootstrapPayload>('/site-admin-api/save', payload)
    .then((r) => r.data);

/** GET /site-admin-api/status */
export const fetchSiteStatus = () =>
  client.get<{ data: SiteStatus }>('/site-admin-api/status').then((r) => r.data.data);

/** POST /site-admin-api/publish?source=current|fallback */
export const requestPublish = (source: 'current' | 'fallback' = 'current') =>
  client
    .post(`/site-admin-api/publish?source=${encodeURIComponent(source)}`)
    .then((r) => r.data);

/** POST /site-admin-api/preview —— 草稿预览构建，不落库、不影响线上 */
export const requestDraftPreview = (content: unknown) =>
  client
    .post('/site-admin-api/preview', { content })
    .then((r) => r.data);

// 预览文件在官网域名下由 Caddy forward_auth 校验；先领取一个短时 HttpOnly 预览 Cookie。
export const grantPreviewAccess = () =>
  client.get('/site-admin-api/preview-grant').then((r) => r.data);

// ---------- 降级版本 ----------
/** GET /site-admin-api/fallback-versions */
export const fetchFallbackVersions = () =>
  client.get('/site-admin-api/fallback-versions').then((r) => r.data.data);

/** POST /site-admin-api/fallback-versions */
export const createFallbackVersion = (name: string) =>
  client
    .post('/site-admin-api/fallback-versions', { name })
    .then((r) => r.data.data);

/** POST /site-admin-api/fallback-versions/{id}/activate */
export const activateFallbackVersion = (id: string) =>
  client
    .post(`/site-admin-api/fallback-versions/${encodeURIComponent(id)}/activate`)
    .then((r) => r.data.data);

/** DELETE /site-admin-api/fallback-versions/{id} */
export const deleteFallbackVersion = (id: string) =>
  client
    .delete(`/site-admin-api/fallback-versions/${encodeURIComponent(id)}`)
    .then((r) => r.data.data);

// ---------- AI SEO ----------
/** POST /site-admin-api/seo/ai-generate */
export const generateSeo = (revision: number) =>
  client
    .post<BootstrapPayload>('/site-admin-api/seo/ai-generate', { revision })
    .then((r) => r.data);

// ---------- 媒体库 ----------
/** GET /site-admin-api/media */
export const fetchMediaList = (params: {
  q?: string;
  category?: string;
  type?: string;
  page?: number;
  pageSize?: number;
} = {}) =>
  client
    .get<{ data: MediaItem[]; meta: Record<string, any> }>('/site-admin-api/media', {
      params: { pageSize: 100, ...params },
    })
    .then((r) => r.data);

/** POST /site-admin-api/media/upload（multipart，字段名 file） */
export const uploadMedia = (file: File, fields: Record<string, string> = {}) => {
  const form = new FormData();
  form.append('file', file);
  Object.entries(fields).forEach(([key, value]) => form.append(key, value));
  return client
    .post<{ data: MediaItem }>('/site-admin-api/media/upload', form)
    .then((r) => r.data.data);
};

/** DELETE /site-admin-api/media/{id} */
export const deleteMedia = (id: string) =>
  client.delete(`/site-admin-api/media/${encodeURIComponent(id)}`).then((r) => r.data);

/** GET /site-admin-api/media/{id}/references */
export const fetchMediaReferences = (id: string) =>
  client
    .get<{ data: { references: string[] } }>(
      `/site-admin-api/media/${encodeURIComponent(id)}/references`
    )
    .then((r) => r.data.data.references || []);

// ---------- 客户咨询 ----------
/** PATCH /site-admin-api/leads/{id}（可选的单条更新入口） */
export const updateLead = (
  id: string,
  payload: { status?: string; priority?: string; internalNotes?: string }
) =>
  client
    .patch(`/site-admin-api/leads/${encodeURIComponent(id)}`, payload)
    .then((r) => r.data.data);

// ---------- 审计（可选） ----------
/** GET /site-admin-api/audit-events */
export const fetchAuditEvents = (limit = 50) =>
  client
    .get('/site-admin-api/audit-events', { params: { limit } })
    .then((r) => r.data.data || []);
