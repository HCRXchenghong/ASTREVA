import type { APIRoute } from 'astro';

export const GET: APIRoute = () => {
  const site = (process.env.PUBLIC_SITE_URL || 'http://127.0.0.1:4321').replace(/\/$/, '');
  const agents = [
    '*',
    'Googlebot',
    'Bingbot',
    'Baiduspider',
    '360Spider',
    'Sogou web spider',
    'YandexBot',
    'DuckAssistBot',
    'Applebot',
    'GPTBot',
    'OAI-SearchBot',
    'ChatGPT-User',
    'ClaudeBot',
    'PerplexityBot',
    'Google-Extended',
    'Applebot-Extended',
    'CCBot',
    'Bytespider',
    'cohere-ai',
    'YouBot',
    'meta-externalagent'
  ];
  // Draft preview must never be crawled, even if the URL leaks.
  const rules = agents.map((agent) => `User-agent: ${agent}\nAllow: /\nDisallow: /__preview/`).join('\n\n');
  const host = new URL(site).host;
  return new Response(`${rules}\n\nHost: ${host}\nSitemap: ${site}/sitemap-index.xml\n`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' }
  });
};
