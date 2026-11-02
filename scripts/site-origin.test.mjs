import assert from 'node:assert/strict';
import test from 'node:test';
import { resolveSiteOrigin } from './site-origin.mjs';

test('部署域名优先，避免本地 .env 地址进入 canonical 与 sitemap', () => {
  assert.equal(resolveSiteOrigin({ SITE_DOMAIN: 'www.astreva.test', PUBLIC_SITE_URL: 'http://127.0.0.1:4321' }), 'https://www.astreva.test');
  assert.equal(resolveSiteOrigin({ PUBLIC_SITE_URL: 'http://127.0.0.1:4321/' }), 'http://127.0.0.1:4321');
});

test('后台保存的域名优先于部署环境域名', () => {
  assert.equal(resolveSiteOrigin({ SITE_ORIGIN_OVERRIDE: 'https://new.astreva.com', SITE_DOMAIN: 'old.astreva.com', PUBLIC_SITE_URL: 'https://old.astreva.com' }), 'https://new.astreva.com');
});

test('示例域名和带路径的网址不能用于正式 SEO', () => {
  assert.throws(() => resolveSiteOrigin({ PUBLIC_SITE_URL: 'https://www.example.com' }), /示例域名/);
  assert.throws(() => resolveSiteOrigin({ PUBLIC_SITE_URL: 'https://astreva.test/path' }), /纯 HTTP\/HTTPS 域名/);
});
