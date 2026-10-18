import type { APIRoute } from 'astro';
import { getContent } from '../lib/content';
import { absoluteUrl } from '../lib/seo';

export const GET: APIRoute = async () => {
  const content = await getContent();
  const site = absoluteUrl('/').replace(/\/$/, '');
  const url = (path: string) => absoluteUrl(path);
  const labels = content.site.aiDiscovery;
  const staticLabels = new Map(content.staticPages.map((page) => [page.slug, page.title || page.hero?.title || page.slug]));
  const lines = [
    `# ${content.site.brandName}`,
    '',
    `> ${content.site.defaultSeo.description || content.site.brandTagline}`,
    '',
    labels.introduction,
    '',
    `## ${labels.corePagesTitle}`,
    '',
    `- [${labels.homeLabel}](${url('/')}): ${content.home.seo.description}`,
    `- [${labels.productsLabel}](${url('/products/')}): ${content.productCenter.seo.description}`,
    `- [${labels.solutionsLabel}](${url('/solutions/')}): ${staticLabels.get('solutions') || labels.solutionsLabel}`,
    `- [${labels.supportLabel}](${url('/support/')}): ${content.support.seo.description}`,
    `- [${labels.aboutLabel}](${url('/about/')}): ${staticLabels.get('about') || labels.aboutLabel}`,
    `- [${labels.faqLabel}](${url('/support/faq/')}): ${staticLabels.get('faq') || labels.faqLabel}`,
    '',
    `## ${labels.categoriesTitle}`,
    '',
    ...content.categories
      .slice()
      .sort((a, b) => a.sortOrder - b.sortOrder)
      .map((category) => `- [${category.name}](${url(`/products/${category.slug}/`)}): ${category.description}`),
    '',
    `## ${labels.productsTitle}`,
    '',
    ...content.products
      .slice()
      .sort((a, b) => a.sortOrder - b.sortOrder)
      .map((product) => `- [${product.name}](${url(`/products/${product.categorySlug}/${product.slug}/`)}): ${product.summary}`),
    '',
    `## ${labels.contactTitle}`,
    '',
    `- ${labels.websiteLabel}: ${site}`,
    `- ${labels.emailLabel}: ${content.site.email}`,
    `- Sitemap: ${url('/sitemap-index.xml')}`,
    `- Robots: ${url('/robots.txt')}`,
    '',
    labels.citationNotice
  ];
  return new Response(`${lines.join('\n')}\n`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Cache-Control': 'public, max-age=3600' }
  });
};
