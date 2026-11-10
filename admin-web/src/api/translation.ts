import client from './client';

// 网站翻译接口（统一后台 SPA 与 admin-server 同源，管理员 2FA Cookie 会话）。
// 对应后端 modules/website-translation-router.mjs，网站翻译页面接口。

export type TranslationApiType =
  | 'chat_completions'
  | 'responses'
  | 'anthropic_messages';

export type TranslationLocale = 'en' | 'ar';

export type TranslationStatus =
  | 'pending'
  | 'running'
  | 'ready'
  | 'failed'
  | 'unconfigured';

export interface TranslationSourceState {
  locale: string;
  revision: number;
  status: string;
  updatedAt: string | null;
}

export interface TranslationProgress {
  done: number;
  total: number;
  updatedAt?: number | null;
}

export interface TranslationLocaleState {
  locale: string;
  status: string;
  sourceRevision: number;
  stale: boolean;
  lastError: string;
  progress: TranslationProgress | null;
  updatedAt: string | null;
}

export interface TranslationSettings {
  enabled: boolean;
  apiType: TranslationApiType | string;
  baseUrl: string;
  model: string;
  batchSize: number;
  timeoutMs: number;
  apiKeyConfigured: boolean;
  apiKeyMasked: string;
  configuredModels: string[];
  updatedAt?: string | null;
}

export interface TranslationOverview {
  source: TranslationSourceState;
  settings: TranslationSettings;
  translations: TranslationLocaleState[];
}

export interface TranslationSettingsPayload {
  enabled: boolean;
  apiType: string;
  baseUrl: string;
  apiKey?: string;
  model: string;
  batchSize: number;
  timeoutMs: number;
  configuredModels: string[];
  clearApiKey?: boolean;
}

export interface TranslationModelsResult {
  models: string[];
}

const base = '/site-admin-api/translations';

/** 读取源语言修订、上游配置与三语版本状态。 */
export const fetchTranslations = (): Promise<TranslationOverview> =>
  client.get(base).then((r) => r.data.data);

/** 保存翻译上游配置；apiKey 留空表示保留现有 Key，clearApiKey 为 true 时清除。 */
export const saveTranslationSettings = (
  payload: TranslationSettingsPayload
): Promise<TranslationSettings> =>
  client.put(`${base}/settings`, payload).then((r) => r.data.data);

/** 从上游拉取可用模型；apiKey 留空时后端回退已保存的 Key。 */
export const fetchTranslationModels = (
  payload: TranslationSettingsPayload
): Promise<TranslationModelsResult> =>
  client.post(`${base}/models`, payload).then((r) => r.data.data);

/** 提交重译任务；不传 locale 时重译英语与阿拉伯语。 */
export const requestRetranslation = (
  locale?: TranslationLocale
): Promise<{ accepted: boolean; locales: string[]; sourceRevision: number }> =>
  client
    .post(`${base}/retranslate`, locale ? { locale } : {})
    .then((r) => r.data.data);
