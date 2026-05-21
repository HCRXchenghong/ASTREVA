import type { APIRoute } from 'astro';

export const GET: APIRoute = () => {
  const site = (process.env.PUBLIC_SITE_URL || 'https://www.example.com').replace(/\/$/, '');
  return new Response(`User-agent: *\nAllow: /\nSitemap: ${site}/sitemap-index.xml\n`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' }
  });
};
