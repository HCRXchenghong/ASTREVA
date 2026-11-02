/* Locale switch/recommendation runtime (external so CSP can stay script-src 'self'). */
(() => {
  const script = document.currentScript || document.querySelector('script[data-astreva-language]');
  const dataset = (script && script.dataset) || {};
  const astrevaApiBase = dataset.apiBase || '';
  let astrevaLocales = [];
  let astrevaLocalePaths = {};
  try { astrevaLocales = JSON.parse(dataset.locales || '[]'); } catch { /* invalid dataset: use empty list */ }
  try { astrevaLocalePaths = JSON.parse(dataset.paths || '{}'); } catch { /* invalid dataset: use empty map */ }
  (() => {
    const supported = ['zh-CN', 'en', 'ar'];
    const storageKey = 'astreva-site-locale';
    const available = Array.isArray(astrevaLocales)
      ? astrevaLocales.filter((item) => supported.includes(item))
      : ['zh-CN'];
    const paths = astrevaLocalePaths && typeof astrevaLocalePaths === 'object' ? astrevaLocalePaths : {};
    const htmlLocale = document.documentElement.lang;
    const currentLocale = supported.includes(htmlLocale) ? htmlLocale : 'zh-CN';

    function storedLocale() {
      try {
        return localStorage.getItem(storageKey);
      } catch {
        return null;
      }
    }

    function remember(locale) {
      try {
        localStorage.setItem(storageKey, locale);
      } catch {
        /* ignore: storage unavailable (private mode) */
      }
    }

    function homeFor(locale) {
      if (typeof paths[locale] === 'string' && paths[locale]) return paths[locale];
      const currentPath = window.location.pathname;
      const basePath = currentPath.replace(/^\/(?:en|ar)(?=\/|$)/, '') || '/';
      const path = /^\/404\/?$/.test(basePath) ? '/' : basePath;
      return locale === 'zh-CN' ? path : `/${locale}${path}`;
    }

    // Language switching is a real navigation now: the URL decides the content,
    // so there is no DOM text replacement left to do here.
    async function switchToLocale(locale) {
      if (!supported.includes(locale)) locale = 'zh-CN';
      if (locale === currentLocale) return true;
      const target = homeFor(locale);
      if (!available.includes(locale)) {
        try {
          const response = await fetch(target, { method: 'HEAD', credentials: 'same-origin', cache: 'no-store' });
          if (!response.ok) return false;
        } catch {
          return false;
        }
      }
      remember(locale);
      window.location.href = target;
      return true;
    }

    window.AstrevaLanguage = {
      switch: switchToLocale,
      get: () => currentLocale
    };
    document.documentElement.dataset.astrevaLocale = currentLocale;
    window.dispatchEvent(new CustomEvent('astreva:locale', { detail: currentLocale }));

    const saved = storedLocale();
    if (supported.includes(saved)) return;

    // First visit: ask the backend for a recommendation, but only follow it when
    // the matching static route really exists (injected at build time).
    fetch(`${astrevaApiBase}/site-admin-api/public-locale`, { credentials: 'omit' })
      .then((response) => (response.ok ? response.json() : null))
      .then((payload) => {
        const recommended = payload?.data?.locale;
        if (!supported.includes(recommended) || !available.includes(recommended) || recommended === currentLocale) {
          remember(currentLocale);
          return;
        }
        remember(recommended);
        window.location.replace(homeFor(recommended));
      })
      .catch(() => remember(currentLocale));
  })();
})();
