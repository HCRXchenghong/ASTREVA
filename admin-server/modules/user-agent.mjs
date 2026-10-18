/**
 * 轻量 UA 解析：不引第三方依赖，够后台展示与危险识别用。
 * 只做能确定的事情，解析不出来就留空，不猜。
 */
const BOT_PATTERN =
  /bot|crawler|spider|crawling|slurp|bingpreview|facebookexternalhit|whatsapp|telegrambot|python-requests|curl\/|wget|go-http-client|okhttp|axios\/|headlesschrome|phantomjs|scrapy|nmap|masscan|sqlmap|nikto|nuclei|zgrab|semrush|ahrefs|mj12bot|dotbot|petalbot|yandex|baiduspider|bytespider/i;

export function parseUserAgent(rawUserAgent) {
  const ua = String(rawUserAgent || '').slice(0, 512);
  const result = {
    browser: '',
    browserVersion: '',
    os: '',
    device: 'desktop',
    isBot: BOT_PATTERN.test(ua),
  };
  if (!ua) return result;

  // 设备类型
  if (/ipad|tablet|playbook|silk/i.test(ua)) result.device = 'tablet';
  else if (/mobi|iphone|ipod|android.*mobile|windows phone/i.test(ua)) result.device = 'mobile';

  // 操作系统
  const osRules = [
    [/windows nt 10\.0/i, 'Windows 10/11'],
    [/windows nt 6\.3/i, 'Windows 8.1'],
    [/windows nt 6\.1/i, 'Windows 7'],
    [/windows/i, 'Windows'],
    [/iphone os ([\d_]+)/i, 'iOS'],
    [/ipad; cpu os ([\d_]+)/i, 'iPadOS'],
    [/android[ /]([\d.]+)/i, 'Android'],
    [/mac os x ([\d_]+)/i, 'macOS'],
    [/cros/i, 'ChromeOS'],
    [/ubuntu/i, 'Ubuntu'],
    [/linux/i, 'Linux'],
    [/freebsd/i, 'FreeBSD'],
  ];
  for (const [pattern, name] of osRules) {
    const match = ua.match(pattern);
    if (match) {
      result.os = name;
      const version = (match[1] || '').replace(/_/g, '.');
      if (version && /^(ios|ipados|android|macos)$/i.test(name)) {
        result.os = `${name} ${version}`;
      }
      break;
    }
  }

  // 浏览器（顺序很重要：Edge/Opera 的 UA 里也含 Chrome/Safari）
  const browserRules = [
    [/edg(?:e|ios|a)?\/([\d.]+)/i, 'Edge'],
    [/opr\/([\d.]+)|opera\/([\d.]+)/i, 'Opera'],
    [/micromessenger\/([\d.]+)/i, 'WeChat'],
    [/qqbrowser\/([\d.]+)/i, 'QQBrowser'],
    [/(?:ucbrowser|ucweb)\/([\d.]+)/i, 'UCBrowser'],
    [/firefox\/([\d.]+)/i, 'Firefox'],
    [/(?:chrome|crios)\/([\d.]+)/i, 'Chrome'],
    [/version\/([\d.]+).*safari/i, 'Safari'],
    [/safari\/([\d.]+)/i, 'Safari'],
    [/msie ([\d.]+)/i, 'Internet Explorer'],
  ];
  for (const [pattern, name] of browserRules) {
    const match = ua.match(pattern);
    if (match) {
      result.browser = name;
      result.browserVersion = (match[1] || match[2] || '').trim();
      break;
    }
  }
  if (!result.browser && result.isBot) result.browser = '爬虫/机器人';
  return result;
}

export function describeDevice(parsed) {
  if (!parsed) return '';
  const parts = [parsed.browser, parsed.os].filter(Boolean);
  return parts.join(' · ');
}
