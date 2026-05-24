import { fallbackContent } from '../data/fallback';
import type { Category, FAQ, HomePage, Product, ProductCenterPage, SiteContent, SiteSettings, StaticPage, SupportPage } from './types';

const CONTENT_API_URL = (process.env.PUBLIC_ADMIN_URL || '').replace(/\/$/, '');
const CONTENT_API_TOKEN = process.env.CONTENT_API_TOKEN || '';
const CONTENT_BRIDGE_URL = (process.env.PUBLIC_SERVICEBRIDGE_URL || '').replace(/\/$/, '');

function normalizeServiceBridgeUrl(value?: string) {
  const candidate = `${value || ''}`.trim().replace(/\/$/, '');
  if (!candidate) return '';
  const lower = candidate.toLowerCase();
  if (lower === '127.0.0.1' || lower === 'localhost' || lower === '::1' || lower.startsWith('127.0.0.1:') || lower.startsWith('localhost:') || lower.startsWith('[::1]') || lower.startsWith('[::1]:')) {
    return '';
  }
  if (!candidate.includes('://')) return candidate;
  try {
    const url = new URL(candidate);
    const host = String(url.hostname || '').toLowerCase();
    if (host === '127.0.0.1' || host === 'localhost' || host === '::1') return '';
    return candidate;
  } catch {
    return '';
  }
}

type ApiEntity = Record<string, any>;

function entity<T extends ApiEntity>(value: any): T | null {
  if (!value) return null;
  const raw = value.data ?? value;
  if (!raw) return null;
  const attrs = raw.attributes ?? raw;
  return { id: raw.id ?? attrs.id ?? raw.documentId, ...attrs } as T;
}

function entities<T extends ApiEntity>(value: any): T[] {
  const raw = Array.isArray(value?.data) ? value.data : Array.isArray(value) ? value : [];
  return raw.map((item) => entity<T>(item)).filter(Boolean) as T[];
}

function imageUrl(value: any): string {
  if (!value) return '';
  if (typeof value === 'string') return value;
  const file = entity<any>(value);
  const url = file?.url || value?.url || '';
  if (!url) return '';
  if (/^https?:\/\//i.test(url)) return url;
  return `${CONTENT_API_URL}${url}`;
}

function mediaUrls(value: any): string[] {
  return entities<any>(value).map((item) => imageUrl(item)).filter(Boolean);
}

function unique<T>(items: T[]): T[] {
  return [...new Set(items.filter(Boolean))];
}

function uniqueDownloads(items: Array<{ label: string; url: string }>) {
  const seen = new Set<string>();
  return items.filter((item) => {
    if (!item.url || seen.has(item.url)) return false;
    seen.add(item.url);
    return true;
  });
}

function json<T>(value: any, fallback: T): T {
  if (value == null) return fallback;
  if (typeof value === 'string') {
    try {
      return JSON.parse(value);
    } catch {
      return fallback;
    }
  }
  return value as T;
}

function textExcerpt(value = '') {
  return String(value)
    .replace(/<\s*br\s*\/?>/gi, ' ')
    .replace(/<[^>]+>/g, ' ')
    .replace(/&nbsp;/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 150);
}

function meaningfulText(...values: Array<string | undefined>) {
  return values.map((value) => String(value || '').trim()).find((value) => value.length >= 10) || values.find(Boolean) || '';
}

function seoWithFallback(value: any, fallback: SEO, descriptionFallback = ''): SEO {
  const next = { ...fallback, ...json(value, fallback) };
  next.title = meaningfulText(next.title, fallback.title);
  next.description = meaningfulText(next.description, descriptionFallback, fallback.description, fallback.title);
  next.image = next.image || fallback.image;
  next.keywords = next.keywords || fallback.keywords;
  return next;
}

async function request(path: string) {
  if (!CONTENT_API_URL) throw new Error('Content API is not configured');
  const headers: Record<string, string> = {};
  if (CONTENT_API_TOKEN) headers.Authorization = `Bearer ${CONTENT_API_TOKEN}`;
  const response = await fetch(`${CONTENT_API_URL}${path}`, {
    headers
  });
  if (!response.ok) {
    throw new Error(`Content API request failed: ${path} ${response.status}`);
  }
  return response.json();
}

function mapSiteSetting(raw: any): SiteSettings {
  const item = entity<any>(raw);
  if (!item) return fallbackContent.site;
  return {
    ...fallbackContent.site,
    brandName: item.brandName || fallbackContent.site.brandName,
    brandTagline: item.brandTagline || fallbackContent.site.brandTagline,
    logoUrl: imageUrl(item.logo) || item.logoUrl || fallbackContent.site.logoUrl,
    phone: item.phone || fallbackContent.site.phone,
    email: item.email || fallbackContent.site.email,
    copyright: item.copyright || fallbackContent.site.copyright,
    serviceBridgeUrl:
      normalizeServiceBridgeUrl(item.serviceBridgeUrl) ||
      normalizeServiceBridgeUrl(CONTENT_BRIDGE_URL) ||
      fallbackContent.site.serviceBridgeUrl,
    defaultSeo: seoWithFallback(item.defaultSeo, fallbackContent.site.defaultSeo),
    socialLinks: json(item.socialLinks, fallbackContent.site.socialLinks),
    cookie: { ...fallbackContent.site.cookie, ...json(item.cookie, fallbackContent.site.cookie) },
    navigation: { ...fallbackContent.site.navigation, ...json(item.navigation, fallbackContent.site.navigation) },
    footer: { ...fallbackContent.site.footer, ...json(item.footer, fallbackContent.site.footer) },
    chat: { ...fallbackContent.site.chat, ...json(item.chat, fallbackContent.site.chat) }
  };
}

function mapHomePage(raw: any): HomePage {
  const item = entity<any>(raw);
  if (!item) return fallbackContent.home;
  const hero = { ...fallbackContent.home.hero, ...json(item.hero, fallbackContent.home.hero) };
  const productIntro = { ...fallbackContent.home.productIntro, ...json(item.productIntro, fallbackContent.home.productIntro) };
  const cta = { ...fallbackContent.home.cta, ...json(item.cta, fallbackContent.home.cta) };
  return {
    seo: seoWithFallback(item.seo, fallbackContent.home.seo, hero.subtitle || hero.title),
    hero: {
      ...hero,
      backgroundImage: imageUrl(item.heroBackgroundImage) || hero.backgroundImage,
      backgroundVideo: imageUrl(item.heroBackgroundVideo) || hero.backgroundVideo
    },
    partnerLabel: item.partnerLabel || fallbackContent.home.partnerLabel,
    featuredProducts: { ...fallbackContent.home.featuredProducts, ...json(item.featuredProducts, fallbackContent.home.featuredProducts) },
    partners: json(item.partners, fallbackContent.home.partners),
    productIntro: {
      ...productIntro,
      image: imageUrl(item.productIntroImage) || productIntro.image
    },
    advantagesIntro: { ...fallbackContent.home.advantagesIntro, ...json(item.advantagesIntro, fallbackContent.home.advantagesIntro) },
    advantages: json(item.advantages, fallbackContent.home.advantages),
    testimonialsTitle: item.testimonialsTitle || fallbackContent.home.testimonialsTitle,
    testimonials: json(item.testimonials, fallbackContent.home.testimonials),
    cta: {
      ...cta,
      backgroundImage: imageUrl(item.ctaBackgroundImage) || cta.backgroundImage
    }
  };
}

function mapProductCenterPage(raw: any): ProductCenterPage {
  const item = entity<any>(raw);
  if (!item) return fallbackContent.productCenter;
  const hero = { ...fallbackContent.productCenter.hero, ...json(item.hero, fallbackContent.productCenter.hero) };
  return {
    seo: seoWithFallback(item.seo, fallbackContent.productCenter.seo, hero.description || hero.title),
    hero: {
      ...hero,
      backgroundImage: imageUrl(item.heroBackgroundImage) || hero.backgroundImage,
      backgroundVideo: imageUrl(item.heroBackgroundVideo) || hero.backgroundVideo
    },
    intro: { ...fallbackContent.productCenter.intro, ...json(item.intro, fallbackContent.productCenter.intro) },
    categoryPage: { ...fallbackContent.productCenter.categoryPage, ...json(item.categoryPage, fallbackContent.productCenter.categoryPage) },
    detailPage: { ...fallbackContent.productCenter.detailPage, ...json(item.detailPage, fallbackContent.productCenter.detailPage) }
  };
}

function mapCategory(item: any): Category {
  return {
    id: String(item.documentId || item.id || item.slug),
    name: item.name,
    slug: item.slug,
    description: item.description || '',
    coverImage: imageUrl(item.coverMedia) || imageUrl(item.coverImage) || item.coverImageUrl || '',
    heroVideo: imageUrl(item.heroBackgroundVideo) || imageUrl(item.heroVideo) || item.heroVideo || '',
    sortOrder: Number(item.sortOrder || 0),
    seo: seoWithFallback(item.seo, {
      title: `${item.name} | 星渡ASTREVA`,
      description: item.description || ''
    }, item.description)
  };
}

function mapProduct(item: any): Product {
  const category = entity<any>(item.category);
  const galleryMedia = mediaUrls(item.galleryMedia);
  const galleryImages = json<string[]>(item.galleryImages, []);
  const downloadFiles = entities<any>(item.downloadFiles)
    .map((file) => ({
      label: file.name || file.alternativeText || '下载文件',
      url: imageUrl(file)
    }))
    .filter((file) => file.url);
  const jsonDownloads = json<Array<{ label: string; url: string }>>(item.downloads, []);
  return {
    id: String(item.documentId || item.id || item.slug),
    name: item.name,
    slug: item.slug,
    categorySlug: category?.slug || item.categorySlug || '',
    coverImage: imageUrl(item.coverMedia) || imageUrl(item.coverImage) || item.coverImageUrl || '',
    galleryImages: unique([...galleryMedia, ...galleryImages]),
    videoUrl: imageUrl(item.videoFile) || item.videoUrl || '',
    summary: item.summary || '',
    features: json(item.features, []),
    specifications: json(item.specifications, []),
    downloads: uniqueDownloads([...downloadFiles, ...jsonDownloads]),
    body: item.body || '',
    isFeatured: Boolean(item.isFeatured),
    sortOrder: Number(item.sortOrder || 0),
    seo: seoWithFallback(item.seo, {
      title: `${item.name} | 星渡ASTREVA`,
      description: item.summary || ''
    }, item.summary)
  };
}

function mapSupportPage(raw: any): SupportPage {
  const item = entity<any>(raw);
  if (!item) return fallbackContent.support;
  const hero = { ...fallbackContent.support.hero, ...json(item.hero, fallbackContent.support.hero) };
  return {
    seo: seoWithFallback(item.seo, fallbackContent.support.seo, hero.description || hero.title),
    hero: {
      ...hero,
      backgroundImage: imageUrl(item.heroBackgroundImage) || hero.backgroundImage,
      backgroundVideo: imageUrl(item.heroBackgroundVideo) || hero.backgroundVideo
    },
    services: json(item.services, fallbackContent.support.services),
    contact: {
      ...fallbackContent.support.contact,
      ...json(item.contact, fallbackContent.support.contact),
      form: {
        ...fallbackContent.support.contact.form,
        ...json(item.contact, fallbackContent.support.contact).form
      }
    }
  };
}

function mapFAQ(item: any): FAQ {
  return {
    id: String(item.documentId || item.id || item.question),
    question: item.question,
    answer: item.answer,
    sortOrder: Number(item.sortOrder || 0),
    enabled: item.enabled !== false
  };
}

function mapStaticPage(item: any): StaticPage {
  const fallback = fallbackContent.staticPages.find((page) => page.slug === item.slug);
  const hero = { ...(fallback?.hero || {}), ...json(item.hero, fallback?.hero || undefined) };
  return {
    slug: item.slug,
    title: item.title,
    hero: {
      ...hero,
      backgroundImage: imageUrl(item.heroBackgroundImage) || hero?.backgroundImage,
      backgroundVideo: imageUrl(item.heroBackgroundVideo) || hero?.backgroundVideo
    },
    body: item.body || '',
    sections: json(item.sections, fallback?.sections || {}),
    seo: seoWithFallback(item.seo, fallback?.seo || {
      title: `${item.title} | 星渡ASTREVA`,
      description: item.title
    }, hero.description || textExcerpt(item.body) || item.title)
  };
}

export async function getContent(): Promise<SiteContent> {
  if (!CONTENT_API_URL) {
    return fallbackContent;
  }

  try {
    const payload = await request('/site-admin-api/public-content');
    if (payload?.data?.site && payload?.data?.home) {
      return payload.data as SiteContent;
    }
  } catch (error) {
    console.warn('[content] Self-hosted admin content unavailable, using fallback content:', error);
    return fallbackContent;
  }

  try {
    const [site, home, productCenter, categories, products, support, faqs, staticPages] = await Promise.all([
      request('/api/site-setting?populate=logo'),
      request('/api/home-page?populate[0]=heroBackgroundImage&populate[1]=heroBackgroundVideo&populate[2]=productIntroImage&populate[3]=ctaBackgroundImage'),
      request('/api/product-center-page?populate[0]=heroBackgroundImage&populate[1]=heroBackgroundVideo'),
      request('/api/categories?populate[0]=coverMedia&populate[1]=heroBackgroundVideo&sort=sortOrder:asc&pagination[pageSize]=100'),
      request('/api/products?populate[0]=category&populate[1]=coverMedia&populate[2]=galleryMedia&populate[3]=downloadFiles&populate[4]=videoFile&sort=sortOrder:asc&pagination[pageSize]=500'),
      request('/api/support-page?populate[0]=heroBackgroundImage&populate[1]=heroBackgroundVideo'),
      request('/api/faqs?filters[enabled][$eq]=true&sort=sortOrder:asc&pagination[pageSize]=200'),
      request('/api/static-pages?populate[0]=heroBackgroundImage&populate[1]=heroBackgroundVideo&pagination[pageSize]=50')
    ]);

    const mappedCategories = entities<any>(categories).map(mapCategory).filter((item) => item.slug);
    const mappedProducts = entities<any>(products).map(mapProduct).filter((item) => item.slug && item.categorySlug);

    return {
      site: mapSiteSetting(site),
      home: mapHomePage(home),
      productCenter: mapProductCenterPage(productCenter),
      categories: mappedCategories.length ? mappedCategories : fallbackContent.categories,
      products: mappedProducts.length ? mappedProducts : fallbackContent.products,
      support: mapSupportPage(support),
      faqs: entities<any>(faqs).map(mapFAQ).filter((item) => item.enabled),
      staticPages: entities<any>(staticPages).map(mapStaticPage)
    };
  } catch (error) {
    console.warn('[content] Falling back to seed content:', error);
    return fallbackContent;
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
