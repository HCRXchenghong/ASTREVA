export function resolveSiteOrigin(env = process.env) {
  const domain = String(env.SITE_DOMAIN || '').trim().replace(/^https?:\/\//i, '').replace(/\/+$/, '');
  const configured = String(env.SITE_ORIGIN_OVERRIDE || '').trim() || (domain ? `https://${domain}` : String(env.PUBLIC_SITE_URL || '').trim());
  const origin = configured || 'http://127.0.0.1:4321';
  let url;
  try { url = new URL(origin); }
  catch { throw new Error('PUBLIC_SITE_URL 或 SITE_DOMAIN 不是有效网址。'); }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('官网地址必须是纯 HTTP/HTTPS 域名，不能带路径、账号或查询参数。');
  }
  if (url.hostname === 'example.com' || url.hostname.endsWith('.example.com')) {
    throw new Error('官网地址仍是 example.com 示例域名；部署时请填写 SITE_DOMAIN。');
  }
  return url.origin;
}
