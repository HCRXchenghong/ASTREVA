import type { APIRoute } from 'astro';
import { getContent, hasTranslation } from '../lib/content';
import { localePath, PREFIXED_LOCALES, type SiteLocale } from '../lib/i18n';
import { absoluteUrl } from '../lib/seo';
import type { SiteContent } from '../lib/types';

function languageLabel(content: SiteContent, locale: SiteLocale) {
  const labels = content.site.localization;
  if (locale === 'en') return labels.englishLabel;
  if (locale === 'ar') return labels.arabicLabel;
  return labels.chineseLabel;
}

/** Per-language index built from that locale's own content model. */
function languageSection(content: SiteContent, locale: SiteLocale) {
  const labels = content.site.aiDiscovery;
  const url = (path: string) => absoluteUrl(localePath(locale, path));
  const staticLabels = new Map(
    content.staticPages.map((page) => [page.slug, page.title || page.hero?.title || page.slug])
  );
  return [
    `## ${languageLabel(content, locale)}`,
    '',
    `### ${labels.corePagesTitle}`,
    '',
    `- [${labels.homeLabel}](${url('/')}): ${content.home.seo.description}`,
    `- [${labels.productsLabel}](${url('/products/')}): ${content.productCenter.seo.description}`,
    `- [${labels.solutionsLabel}](${url('/solutions/')}): ${staticLabels.get('solutions') || labels.solutionsLabel}`,
    `- [${labels.supportLabel}](${url('/support/')}): ${content.support.seo.description}`,
    `- [${labels.aboutLabel}](${url('/about/')}): ${staticLabels.get('about') || labels.aboutLabel}`,
    `- [${labels.faqLabel}](${url('/support/faq/')}): ${staticLabels.get('faq') || labels.faqLabel}`,
    '',
    `### ${labels.categoriesTitle}`,
    '',
    ...content.categories
      .slice()
      .sort((a, b) => a.sortOrder - b.sortOrder)
      .map((category) => `- [${category.name}](${url(`/products/${category.slug}/`)}): ${category.description}`),
    '',
    `### ${labels.productsTitle}`,
    '',
    ...content.products
      .slice()
      .sort((a, b) => a.sortOrder - b.sortOrder)
      .map(
        (product) =>
          `- [${product.name}](${url(`/products/${product.categorySlug}/${product.slug}/`)}): ${product.summary}`
      ),
    '',
    `### ${labels.contactTitle}`,
    '',
    `- ${labels.websiteLabel}: ${url('/')}`,
    `- ${labels.emailLabel}: ${content.site.email}`,
    `- Sitemap: ${absoluteUrl('/sitemap-index.xml')}`,
    `- Robots: ${absoluteUrl('/robots.txt')}`
  ];
}

export const GET: APIRoute = async () => {
  const content = await getContent('zh-CN');
  const labels = content.site.aiDiscovery;
  const available: SiteLocale[] = ['zh-CN'];
  for (const locale of PREFIXED_LOCALES) {
    if (await hasTranslation(locale)) available.push(locale);
  }

  const lines = [
    `# ${content.site.brandName}`,
    '',
    `> ${content.site.defaultSeo.description || content.site.brandTagline}`,
    '',
    labels.introduction,
    '',
    `## ${content.site.localization.languageLabel}`,
    '',
    ...available.map((locale) => `- [${languageLabel(content, locale)}](${absoluteUrl(localePath(locale, '/'))})`),
    '',
    labels.citationNotice
  ];

  for (const locale of available) {
    const localized = locale === 'zh-CN' ? content : await getContent(locale);
    lines.push('', ...languageSection(localized, locale));
  }

  return new Response(`${lines.join('\n')}\n`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'public, max-age=3600' }
  });
};
