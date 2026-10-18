import type { Product, SiteContent, StaticPageForSlug, StaticPageSlug } from './types';

// CONTENT_API_URL 仅供静态构建进程访问内网后台，避免把容器内部地址注入浏览器代码。
const CONTENT_API_URL = (process.env.CONTENT_API_URL || process.env.PUBLIC_ADMIN_URL || 'http://127.0.0.1:1337').replace(/\/$/, '');
export const PUBLIC_CONTENT_API_URL = (process.env.PUBLIC_ADMIN_URL || CONTENT_API_URL).replace(/\/$/, '');
const CONTENT_API_TOKEN = process.env.CONTENT_API_TOKEN || '';
const CONTENT_BRIDGE_URL = normalizeServiceBridgeUrl(
  process.env.PUBLIC_SERVICEBRIDGE_URL || ''
);

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

async function requestContent(): Promise<SiteContent> {
  const headers: Record<string, string> = {};
  if (CONTENT_API_TOKEN) headers.Authorization = `Bearer ${CONTENT_API_TOKEN}`;
  const response = await fetch(`${CONTENT_API_URL}/site-admin-api/public-content`, { headers });
  if (!response.ok) throw new Error(`Content API request failed: ${response.status}`);
  const payload = await response.json();
  if (!payload?.data?.site || !payload?.data?.home) throw new Error('Content API returned an invalid payload');
  return payload.data as SiteContent;
}

export async function getContent(): Promise<SiteContent> {
  return normalizeContent(await requestContent());
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
