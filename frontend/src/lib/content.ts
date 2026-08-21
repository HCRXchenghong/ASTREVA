import { fallbackContent } from '../data/fallback';
import type { Product, SiteContent, StaticPage } from './types';

const CONTENT_API_URL = (process.env.PUBLIC_ADMIN_URL || 'http://127.0.0.1:1337').replace(/\/$/, '');
const CONTENT_API_TOKEN = process.env.CONTENT_API_TOKEN || '';
const CONTENT_BRIDGE_URL = normalizeServiceBridgeUrl(
  process.env.PUBLIC_SERVICEBRIDGE_URL || 'http://127.0.0.1:5173'
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
        normalizeServiceBridgeUrl(content.site.serviceBridgeUrl) ||
        CONTENT_BRIDGE_URL ||
        fallbackContent.site.serviceBridgeUrl
    }
  };
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
  if (!CONTENT_API_URL) return normalizeContent(fallbackContent);

  try {
    return normalizeContent(await requestContent());
  } catch (error) {
    console.warn('[content] Admin content unavailable, using fallback content:', error);
    return normalizeContent(fallbackContent);
  }
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

export function staticPage(content: SiteContent, slug: StaticPage['slug']) {
  return content.staticPages.find((page) => page.slug === slug) ?? fallbackContent.staticPages.find((page) => page.slug === slug)!;
}
