import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import { loadEnv } from 'vite';

const env = loadEnv(process.env.NODE_ENV || 'development', process.cwd(), '');
const site = process.env.PUBLIC_SITE_URL || env.PUBLIC_SITE_URL || 'https://www.example.com';

export default defineConfig({
  site,
  output: 'static',
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
