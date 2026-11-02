import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import { loadEnv } from 'vite';
import { resolveSiteOrigin } from '../scripts/site-origin.mjs';

const env = loadEnv(process.env.NODE_ENV || 'development', process.cwd(), '');
const site = resolveSiteOrigin({ ...env, ...process.env });
process.env.PUBLIC_SITE_URL = site;

const adminOrigin = (() => {
  try { return new URL(process.env.PUBLIC_ADMIN_URL || env.PUBLIC_ADMIN_URL || site).origin; } catch { /* fall back to site origin */ return site; }
})();
const serviceOrigin = (() => {
  try { return new URL(process.env.PUBLIC_SERVICEBRIDGE_URL || env.PUBLIC_SERVICEBRIDGE_URL || 'https://service.invalid').origin; } catch { /* fall back to a harmless placeholder */ return 'https://service.invalid'; }
})();

export default defineConfig({
  site,
  output: 'static',
  security: {
    csp: {
      directives: [
        "default-src 'self'",
        `connect-src 'self' ${adminOrigin} ${serviceOrigin}`,
        "img-src 'self' data: blob: https: http:",
        "media-src 'self' blob: https: http:",
        "font-src 'self' data:",
        `frame-src 'self' https://www.youtube-nocookie.com ${serviceOrigin}`,
        "object-src 'none'",
        "base-uri 'self'",
        "form-action 'self'"
      ],
      scriptDirective: {
        resources: [{ resource: "'none'", kind: 'attribute' }]
      },
      styleDirective: {
        resources: [{ resource: "'none'", kind: 'attribute' }]
      }
    }
  },
  integrations: [
    react(),
    sitemap({
      i18n: {
        defaultLocale: 'zh-CN',
        locales: {
          'zh-CN': 'zh-CN',
          en: 'en',
          ar: 'ar'
        }
      },
      filter: (page) => !/\/404\/?$/.test(page)
    })
  ],
  // Tailwind CSS v4 走 Vite 插件，不再需要 postcss/tailwind 配置文件。
  vite: {
    plugins: [tailwindcss()]
  }
});
