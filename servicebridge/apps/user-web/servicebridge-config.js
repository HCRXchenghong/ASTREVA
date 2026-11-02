// 本地开发默认允许的父页面（生产环境由 Caddy 覆盖为 https://<SITE_DOMAIN>）。
window.__SERVICEBRIDGE_ALLOWED_PARENT__ = [
  'http://127.0.0.1:4321',
  'http://localhost:4321'
];
