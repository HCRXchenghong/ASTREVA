const LOCALES = new Set(['en', 'ar']);

export function recommendedWebsiteLocale(req) {
  const country = String(req.headers['cf-ipcountry'] || req.headers['x-vercel-ip-country'] || '').trim().toUpperCase();
  const arabicCountries = new Set(['AE', 'SA', 'EG', 'DZ', 'BH', 'IQ', 'JO', 'KW', 'LB', 'LY', 'MA', 'OM', 'PS', 'QA', 'SD', 'SY', 'TN', 'YE', 'MR', 'SO', 'DJ', 'KM']);
  const chineseCountries = new Set(['CN', 'HK', 'MO', 'TW']);
  if (arabicCountries.has(country)) return { locale: 'ar', source: 'ip-country', country };
  if (chineseCountries.has(country)) return { locale: 'zh-CN', source: 'ip-country', country };
  if (country && country !== 'XX' && country !== 'T1') return { locale: 'en', source: 'ip-country', country };
  const accepted = String(req.headers['accept-language'] || '').toLowerCase();
  if (/\bar(?:-|,|;|$)/.test(accepted)) return { locale: 'ar', source: 'accept-language', country: '' };
  if (/\bzh(?:-|,|;|$)/.test(accepted)) return { locale: 'zh-CN', source: 'accept-language', country: '' };
  return { locale: 'en', source: accepted ? 'accept-language' : 'default', country: '' };
}

export function createWebsiteTranslationRouter({
  bodyJson,
  json,
  verifySession,
  auditEvent,
  translationService,
  readContentRecord,
  applyPublicCors = () => true
}) {
  return async function handleWebsiteTranslation(req, res, pathname) {
    if (pathname === '/site-admin-api/public-locale') {
      if (!applyPublicCors(req, res)) {
        json(res, 403, { error: { message: '当前来源不允许读取语言推荐。' } });
        return true;
      }
      if (req.method === 'OPTIONS') {
        res.writeHead(204);
        res.end();
        return true;
      }
      if (req.method === 'GET') {
        json(res, 200, { data: recommendedWebsiteLocale(req), meta: { supported: ['zh-CN', 'en', 'ar'] } });
        return true;
      }
    }

    const managed = new Set([
      '/site-admin-api/translations',
      '/site-admin-api/translations/settings',
      '/site-admin-api/translations/models',
      '/site-admin-api/translations/retranslate'
    ]);
    if (!managed.has(pathname)) return false;
    if (!verifySession(req)) {
      json(res, 401, { error: { message: '请先登录。' } });
      return true;
    }

    if (pathname === '/site-admin-api/translations' && req.method === 'GET') {
      const [source, settings, translations] = await Promise.all([
        readContentRecord(),
        translationService.publicSettings(),
        translationService.list()
      ]);
      const byLocale = new Map(translations.map((item) => [item.locale, item]));
      json(res, 200, {
        data: {
          source: { locale: 'zh-CN', revision: source.revision, status: 'ready', updatedAt: source.updatedAt },
          settings,
          translations: ['en', 'ar'].map((locale) => {
            const item = byLocale.get(locale) || { locale, sourceRevision: 0, status: 'pending', lastError: '', updatedAt: null };
            return { ...item, content: undefined, stale: item.sourceRevision !== source.revision || item.status !== 'ready' };
          })
        }
      });
      return true;
    }

    if (pathname === '/site-admin-api/translations/settings' && req.method === 'PUT') {
      try {
        const settings = await translationService.saveSettings(await bodyJson(req));
        if (settings.enabled && settings.apiKeyConfigured && settings.model) {
          const source = await readContentRecord();
          await translationService.schedule(source.content, source.revision, ['en', 'ar']);
        }
        await auditEvent(req, 'translation.settings.updated', 'website-translation', '', {
          apiType: settings.apiType,
          baseUrl: settings.baseUrl,
          model: settings.model,
          enabled: settings.enabled,
          apiKeyConfigured: settings.apiKeyConfigured
        });
        json(res, 200, { data: settings });
      } catch (error) {
        json(res, 400, { error: { message: error.message || '保存翻译配置失败。' } });
      }
      return true;
    }

    if (pathname === '/site-admin-api/translations/models' && req.method === 'POST') {
      try {
        const models = await translationService.requestModels(await bodyJson(req));
        json(res, 200, { data: { models } });
      } catch (error) {
        json(res, 502, { error: { message: error.message || '获取模型失败。' } });
      }
      return true;
    }

    if (pathname === '/site-admin-api/translations/retranslate' && req.method === 'POST') {
      try {
        const payload = await bodyJson(req);
        const locales = payload.locale ? [String(payload.locale)] : ['en', 'ar'];
        if (locales.some((locale) => !LOCALES.has(locale))) throw new Error('只能重译英语或阿拉伯语。');
        const source = await readContentRecord();
        await translationService.schedule(source.content, source.revision, locales);
        await auditEvent(req, 'translation.requested', 'website-translation', locales.join(','), { sourceRevision: source.revision });
        json(res, 202, { data: { accepted: true, locales, sourceRevision: source.revision } });
      } catch (error) {
        json(res, 400, { error: { message: error.message || '提交翻译任务失败。' } });
      }
      return true;
    }
    return false;
  };
}
