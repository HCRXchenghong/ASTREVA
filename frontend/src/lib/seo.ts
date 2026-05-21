import type { Category, FAQ, Product, SEO, SiteSettings } from './types';

export function pageTitle(seo: SEO | undefined, site: SiteSettings) {
  return seo?.title || site.defaultSeo.title;
}

export function pageDescription(seo: SEO | undefined, site: SiteSettings) {
  return seo?.description || site.defaultSeo.description;
}

export function absoluteUrl(path = '/') {
  const site = (process.env.PUBLIC_SITE_URL || 'https://www.example.com').replace(/\/$/, '');
  return `${site}${path.startsWith('/') ? path : `/${path}`}`;
}

function absoluteAssetUrl(url: string) {
  if (!url) return '';
  if (/^https?:\/\//i.test(url)) return url;
  return absoluteUrl(url);
}

export function organizationJsonLd(site: SiteSettings) {
  return {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    name: site.brandName,
    url: absoluteUrl('/'),
    logo: absoluteAssetUrl(site.logoUrl),
    email: site.email,
    telephone: site.phone,
    sameAs: Object.values(site.socialLinks).filter(Boolean)
  };
}

export function websiteJsonLd(site: SiteSettings) {
  return {
    '@context': 'https://schema.org',
    '@type': 'WebSite',
    name: site.brandName,
    url: absoluteUrl('/')
  };
}

export function breadcrumbJsonLd(items: Array<{ name: string; url: string }>) {
  return {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: items.map((item, index) => ({
      '@type': 'ListItem',
      position: index + 1,
      name: item.name,
      item: absoluteUrl(item.url)
    }))
  };
}

export function productJsonLd(product: Product, category: Category) {
  return {
    '@context': 'https://schema.org',
    '@type': 'Product',
    name: product.name,
    image: product.coverImage,
    description: product.summary,
    category: category.name,
    brand: {
      '@type': 'Brand',
      name: 'ASTREVA'
    }
  };
}

export function faqJsonLd(faqs: FAQ[]) {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqs.map((faq) => ({
      '@type': 'Question',
      name: faq.question,
      acceptedAnswer: {
        '@type': 'Answer',
        text: faq.answer
      }
    }))
  };
}

export function contactPageJsonLd(site: SiteSettings) {
  return {
    '@context': 'https://schema.org',
    '@type': 'ContactPage',
    url: absoluteUrl('/support/'),
    mainEntity: {
      '@type': 'Organization',
      name: site.brandName,
      telephone: site.phone,
      email: site.email
    }
  };
}
