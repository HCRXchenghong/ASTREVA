import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import sitemap from '@astrojs/sitemap';
import { loadEnv } from 'vite';

const env = loadEnv(process.env.NODE_ENV || 'development', process.cwd(), '');
const site = process.env.PUBLIC_SITE_URL || env.PUBLIC_SITE_URL || 'https://www.example.com';

export default defineConfig({
  site,
  output: 'static',
  integrations: [react(), sitemap()]
});
