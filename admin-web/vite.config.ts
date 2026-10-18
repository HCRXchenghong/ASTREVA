import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { vitePluginForArco } from '@arco-plugins/vite-vue';
import path from 'node:path';

// 后台 SPA 与 admin-server 同源：生产由 /site-admin/ 提供，开发时把 API 代理到 1337。
export default defineConfig({
  base: '/site-admin/',
  plugins: [
    vue(),
    vitePluginForArco({
      style: 'less',
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/site-admin-api': 'http://127.0.0.1:1337',
      '/site-admin-auth': 'http://127.0.0.1:1337',
      '/openapi.json': 'http://127.0.0.1:1337',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
  },
});
