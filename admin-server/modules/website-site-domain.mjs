import { isIP } from 'node:net';

const SETTING_KEY = 'website-site-domain';

export function normalizeWebsiteDomain(value) {
  const raw = String(value ?? '').trim();
  if (!raw) return '';
  if (raw.length > 253 || /[\s/:?#@\\]/u.test(raw)) {
    throw new Error('请只填写官网域名，不要包含协议、端口、路径或空格。');
  }
  let hostname;
  try { hostname = new URL(`https://${raw}`).hostname.toLowerCase(); }
  catch { throw new Error('官网域名格式不正确。'); }
  if (hostname !== raw.toLowerCase() && !raw.toLowerCase().includes('xn--')) {
    // 国际化域名经过 URL 标准转换为 Punycode，仍然允许保存。
    if (/^[\x00-\x7f]+$/.test(raw)) throw new Error('官网域名格式不正确。');
  }
  if (!hostname.includes('.') || hostname === 'localhost' || isIP(hostname) || hostname === 'example.com' || hostname.endsWith('.example.com')) {
    throw new Error('请填写真实的公网域名，不能使用本地地址或 example.com 示例域名。');
  }
  const labels = hostname.split('.');
  if (labels.some((label) => !label || label.length > 63 || !/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)) || !/^[a-z]{2,63}$|^xn--[a-z0-9-]+$/.test(labels.at(-1))) {
    throw new Error('官网域名格式不正确。');
  }
  return hostname;
}

export function createWebsiteSiteDomain({ platformStore, fallbackOrigin, onChange = () => {} }) {
  async function read() {
    const setting = await platformStore.readRuntimeSetting(SETTING_KEY);
    const domain = normalizeWebsiteDomain(setting?.config?.domain || '');
    return {
      domain,
      effectiveOrigin: domain ? `https://${domain}` : fallbackOrigin,
      source: domain ? 'admin' : 'deployment',
      updatedAt: setting?.updatedAt || null
    };
  }

  async function save(value) {
    const domain = normalizeWebsiteDomain(value);
    const previous = await read();
    if (domain === previous.domain) return { ...previous, changed: false };
    await platformStore.saveRuntimeSetting(SETTING_KEY, { config: { domain } });
    const result = { ...await read(), changed: true };
    onChange(result);
    return result;
  }

  return { read, save };
}
