import type { Product, SiteContent, StaticPageForSlug, StaticPageSlug } from './types';
import { DEFAULT_LOCALE, type SiteLocale } from './i18n';

// CONTENT_API_URL 仅供静态构建进程访问内网后台，避免把容器内部地址注入浏览器代码。
const CONTENT_API_URL = (process.env.CONTENT_API_URL || process.env.PUBLIC_ADMIN_URL || 'http://127.0.0.1:1337').replace(/\/$/, '');
export const PUBLIC_CONTENT_API_URL = (process.env.PUBLIC_ADMIN_URL || CONTENT_API_URL).replace(/\/$/, '');
const CONTENT_API_TOKEN = process.env.CONTENT_API_TOKEN || '';
const CONTENT_BRIDGE_URL = normalizeServiceBridgeUrl(
  process.env.PUBLIC_SERVICEBRIDGE_URL || ''
);

type ContentApiErrorPayload = {
  error?: { message?: string };
  meta?: { status?: string; sourceRevision?: number; translationRevision?: number };
};

/**
 * Raised when a locale has no ready translation yet (content API answers 409).
 * The static build treats this as "skip the route", never as "fall back to
 * Chinese", so the caller can tell a missing translation from a real failure.
 */
export class TranslationNotReadyError extends Error {
  readonly locale: SiteLocale;
  readonly status: number;
  readonly sourceRevision: number | null;
  readonly translationStatus: string;

  constructor(
    locale: SiteLocale,
    message: string,
    options: { status?: number; sourceRevision?: number | null; translationStatus?: string } = {}
  ) {
    super(message);
    this.name = 'TranslationNotReadyError';
    this.locale = locale;
    this.status = options.status ?? 409;
    this.sourceRevision = options.sourceRevision ?? null;
    this.translationStatus = options.translationStatus ?? 'pending';
  }
}

function normalizeServiceBridgeUrl(value?: string) {
  const candidate = `${value || ''}`.trim().replace(/\/$/, '');
  if (!candidate) return '';
  try {
    const url = new URL(candidate);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString().replace(/\/$/, '') : '';
  } catch {
    return '';
  }
}

function normalizeContent(content: SiteContent): SiteContent {
  return {
    ...content,
    site: {
      ...content.site,
      serviceBridgeUrl:
        CONTENT_BRIDGE_URL ||
        normalizeServiceBridgeUrl(content.site.serviceBridgeUrl)
    }
  };
}

function isSiteContent(value: unknown): value is SiteContent {
  const content = value as SiteContent | null;
  return Boolean(
    content &&
    typeof content === 'object' &&
    content.site &&
    content.home &&
    content.productCenter &&
    content.support &&
    Array.isArray(content.categories) &&
    Array.isArray(content.products) &&
    Array.isArray(content.faqs) &&
    Array.isArray(content.staticPages)
  );
}

async function requestContent(locale: SiteLocale): Promise<SiteContent> {
  const headers: Record<string, string> = {};
  if (CONTENT_API_TOKEN) headers.Authorization = `Bearer ${CONTENT_API_TOKEN}`;
  const response = await fetch(
    `${CONTENT_API_URL}/site-admin-api/public-content?locale=${encodeURIComponent(locale)}`,
    { headers }
  );
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as ContentApiErrorPayload | null;
    if (response.status === 409) {
      throw new TranslationNotReadyError(
        locale,
        payload?.error?.message || `Translation for ${locale} is not ready yet.`,
        {
          status: response.status,
          sourceRevision: typeof payload?.meta?.sourceRevision === 'number' ? payload.meta.sourceRevision : null,
          translationStatus: payload?.meta?.status || 'pending'
        }
      );
    }
    throw new Error(`Content API request failed: ${response.status}`);
  }
  const payload = await response.json();
  if (!payload?.data?.site || !payload?.data?.home) throw new Error('Content API returned an invalid payload');
  return payload.data as SiteContent;
}

/** Load the content model for one locale; en/ar surface 409 as TranslationNotReadyError. */
export async function getContent(locale: SiteLocale = DEFAULT_LOCALE): Promise<SiteContent> {
  return normalizeContent(await requestContent(locale));
}

const translationAvailability = new Map<SiteLocale, Promise<boolean>>();

/**
 * Build-time probe: does this locale have a ready translation?
 * zh-CN is always available. A 409 means "skip the routes for this language";
 * any other failure is re-thrown so a broken content API still fails the build.
 */
export function hasTranslation(locale: SiteLocale): Promise<boolean> {
  if (locale === DEFAULT_LOCALE) return Promise.resolve(true);
  const cached = translationAvailability.get(locale);
  if (cached) return cached;
  const pending = getContent(locale).then(
    () => true,
    (error: unknown) => {
      if (error instanceof TranslationNotReadyError) return false;
      translationAvailability.delete(locale);
      throw error;
    }
  );
  translationAvailability.set(locale, pending);
  return pending;
}

/** Locales with ready content, zh-CN first. */
export async function getAvailableLocales(): Promise<SiteLocale[]> {
  const available: SiteLocale[] = [DEFAULT_LOCALE];
  for (const locale of ['en', 'ar'] as const) {
    if (await hasTranslation(locale)) available.push(locale);
  }
  return available;
}

export function productsForCategory(content: SiteContent, categorySlug: string) {
  return content.products
    .filter((product) => product.categorySlug === categorySlug)
    .sort((a, b) => a.sortOrder - b.sortOrder);
}

export function featuredProducts(content: SiteContent, limit = 3) {
  const featured = content.products.filter((product) => product.isFeatured);
  return (featured.length ? featured : content.products).sort((a, b) => a.sortOrder - b.sortOrder).slice(0, limit);
}

export function staticPage<TSlug extends StaticPageSlug>(content: SiteContent, slug: TSlug): StaticPageForSlug<TSlug> {
  const page = content.staticPages.find((item) => item.slug === slug);
  if (!page) throw new Error(`站点内容缺少静态页面：${slug}`);
  return page as StaticPageForSlug<TSlug>;
}
