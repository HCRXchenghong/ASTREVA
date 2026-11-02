import assert from 'node:assert/strict';
import test from 'node:test';
import { collectTranslatableEntries, createWebsiteTranslationService, isMediaOnlyContentChange, normalizeLocalizedBrandTerms } from './website-translation-service.mjs';
import { recommendedWebsiteLocale } from './website-translation-router.mjs';

function fakeStore() {
  let setting = null;
  const translations = new Map();
  return {
    get rawSetting() { return setting; },
    readRuntimeSetting: async () => setting,
    saveRuntimeSetting: async (_key, value) => {
      setting = structuredClone({ ...value, updatedAt: new Date().toISOString() });
      return structuredClone(setting);
    },
    listContentTranslations: async () => [...translations.values()].map((item) => structuredClone(item)),
    readContentTranslation: async (locale) => structuredClone(translations.get(locale) || null),
    saveContentTranslation: async (locale, value) => {
      const record = { locale, ...structuredClone(value), updatedAt: new Date().toISOString() };
      translations.set(locale, record);
      return structuredClone(record);
    }
  };
}

async function waitForStatus(service, locale, wanted, timeoutMs = 2000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const record = await service.read(locale);
    if (record?.status === wanted) return record;
    if (record?.status === 'failed' && wanted !== 'failed') throw new Error(record.lastError);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error(`${locale} 未达到 ${wanted} 状态`);
}

function translationResponse(items) {
  return new Response(JSON.stringify({ choices: [{ message: { content: JSON.stringify(items) } }] }), { status: 200 });
}

test('只收集可见文案，不会翻译 URL、Slug、ID 和媒体地址', () => {
  const entries = collectTranslatableEntries({
    title: '建筑排水方案', slug: '不应翻译', id: '中文-id',
    href: '/中文路径', coverImage: 'https://example.com/中文.jpg', descriptionIcon: '工',
    seo: { title: '独立 SEO 标题', description: '独立 SEO 描述', keywords: '核心关键词' },
    site: { defaultSeo: { title: '全站默认 SEO', description: '默认文案内容', keywords: '默认关键词' } },
    nested: { description: '专业服务' }
  });
  assert.deepEqual(entries, [
    { id: 'title', text: '建筑排水方案' },
    { id: 'nested.description', text: '专业服务' }
  ]);
});

test('目标语言发布前统一品牌和 Click 术语，保留功能图标', () => {
  const content = {
    site: { brandName: '星渡ASTREVA' },
    home: { hero: { title: '星渡 ASTREVA' }, seo: { title: '星渡ASTREVA | Drainage' } },
    support: { description: 'Click咔哒锁扣 guide', secondary: 'Click咔哒 locking system', icon: '工' }
  };
  normalizeLocalizedBrandTerms(content);
  assert.equal(content.site.brandName, 'ASTREVA');
  assert.equal(content.home.hero.title, 'ASTREVA');
  assert.equal(content.home.seo.title, 'ASTREVA | Drainage');
  assert.equal(content.support.description, 'Click guide');
  assert.equal(content.support.secondary, 'Click locking system');
  assert.equal(content.support.icon, '工');
});

test('仅调整媒体时沿用已就绪译文，并同步图片顺序和新增图片', async () => {
  const store = fakeStore();
  const before = { site: { locale: 'zh-CN' }, home: { seo: { title: '中文 SEO' }, hero: { title: '排水系统', mediaItems: [{ type: 'image', url: '/one.webp', alt: '排水图片' }] } } };
  const after = structuredClone(before);
  after.home.hero.mediaItems = [
    { type: 'image', url: '/two.webp', alt: '' },
    { type: 'image', url: '/one.webp', alt: '排水图片' }
  ];
  for (const locale of ['en', 'ar']) {
    const content = structuredClone(before);
    content.site.locale = locale;
    content.home.hero.title = `${locale} title`;
    content.home.seo.title = `${locale} SEO`;
    content.home.hero.mediaItems[0].alt = `${locale} alt`;
    await store.saveContentTranslation(locale, { content, sourceRevision: 4, status: 'ready' });
  }
  const service = createWebsiteTranslationService({ platformStore: store, logger: { error() {} } });
  assert.equal(isMediaOnlyContentChange(before, after), true);
  assert.equal(await service.carryForwardMediaOnly(before, after, 4, 5), true);
  for (const locale of ['en', 'ar']) {
    const current = await store.readContentTranslation(locale);
    assert.equal(current.status, 'ready');
    assert.equal(current.sourceRevision, 5);
    assert.equal(current.content.home.hero.title, `${locale} title`);
    assert.equal(current.content.home.seo.title, `${locale} SEO`);
    assert.deepEqual(current.content.home.hero.mediaItems.map((item) => item.url), ['/two.webp', '/one.webp']);
    assert.equal(current.content.home.hero.mediaItems[1].alt, `${locale} alt`);
  }
  assert.equal(isMediaOnlyContentChange(before, { ...after, home: { hero: { ...after.home.hero, title: '新标题' } } }), false);
});

test('译文先完成，再按目标语言独立生成 SEO，并随同该语言内容发布', async () => {
  const store = fakeStore();
  const service = createWebsiteTranslationService({
    platformStore: store,
    fetchImpl: async (_url, options = {}) => {
      const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
      return translationResponse(entries.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
    },
    logger: { warn() {}, info() {}, error() {} }
  });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  const seoCalls = [];
  service.setLocaleSeoGenerator(async (content, locale) => {
    seoCalls.push({ locale, titleBeforeSeo: content.home.seo.title, pageTitle: content.home.hero.title });
    const localized = structuredClone(content);
    localized.home.seo = { title: `独立 ${locale} SEO 标题`, description: `独立生成的 ${locale} 页面摘要，内容与搜索语言匹配`, keywords: `primary-${locale}, related-${locale}` };
    return { content: localized };
  });
  await service.schedule({
    site: { locale: 'zh-CN', title: '中文官网' },
    home: { hero: { title: '首页标题' }, seo: { title: '中文 SEO 标题', description: '中文 SEO 描述文本', keywords: '中文关键词' } }
  }, 9, ['en']);

  const ready = await waitForStatus(service, 'en', 'ready');
  assert.deepEqual(seoCalls, [{ locale: 'en', titleBeforeSeo: '中文 SEO 标题', pageTitle: 'EN:首页标题' }]);
  assert.equal(ready.content.site.locale, 'en');
  assert.equal(ready.content.home.seo.title, '独立 en SEO 标题');
  assert.match(ready.content.home.seo.description, /en 页面摘要/);
});

test('目标语言 SEO 生成失败时，该语言不会被标记为已就绪', async () => {
  const store = fakeStore();
  const availability = [];
  const service = createWebsiteTranslationService({
    platformStore: store,
    fetchImpl: async (_url, options = {}) => {
      const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
      return translationResponse(entries.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
    },
    logger: { warn() {}, info() {}, error() {} },
    onAvailabilityChange: (change) => availability.push(change)
  });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  service.setLocaleSeoGenerator(async () => { throw new Error('seo upstream unavailable'); });
  await service.schedule({ site: { locale: 'zh-CN', title: '中文官网' } }, 10, ['en']);
  const failed = await waitForStatus(service, 'en', 'failed');
  assert.match(failed.lastError, /seo upstream unavailable/);
  assert.equal(availability.some((change) => change.status === 'ready'), false);
});

test('翻译配置不回显 Key，并能用真实协议形状生成同修订号译文', async () => {
  const store = fakeStore();
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    calls.push({ url, options });
    if (String(url).endsWith('/models')) return new Response(JSON.stringify({ data: [{ id: 'translation-live' }] }), { status: 200 });
    const request = JSON.parse(options.body);
    const prompt = request.messages.at(-1).content;
    const entries = JSON.parse(prompt.split('\n').at(-1));
    const translated = entries.map((item) => ({ id: item.id, text: `EN:${item.text}` }));
    return new Response(JSON.stringify({ choices: [{ message: { content: JSON.stringify(translated) } }] }), { status: 200 });
  };
  const service = createWebsiteTranslationService({ platformStore: store, fetchImpl, logger: { error() {} } });
  const settings = await service.saveSettings({ baseUrl: 'https://translator.example/v1', apiType: 'chat_completions', apiKey: 'secret-api-key', model: 'translation-live' });
  assert.equal(settings.apiKeyConfigured, true);
  assert.equal('apiKey' in settings, false);
  assert.equal(store.rawSetting.secrets.apiKey, 'secret-api-key');
  assert.deepEqual(await service.requestModels(), ['translation-live']);

  const source = {
    site: { locale: 'zh-CN', title: '中文官网', logoUrl: 'https://cdn.example/logo.png' },
    home: { hero: { title: '首页标题' } }
  };
  await service.schedule(source, 7, ['en']);
  const deadline = Date.now() + 2000;
  let translation;
  while (Date.now() < deadline) {
    translation = await service.read('en');
    if (translation?.status === 'ready') break;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  assert.equal(translation.status, 'ready');
  assert.equal(translation.sourceRevision, 7);
  assert.equal(translation.content.site.locale, 'en');
  assert.equal(translation.content.site.title, 'EN:中文官网');
  assert.equal(translation.content.site.logoUrl, 'https://cdn.example/logo.png');
  assert.ok(calls.some((call) => String(call.url).endsWith('/chat/completions')));
});

test('批次漏掉一条时只补译缺失字段并完成任务', async () => {
  const store = fakeStore();
  const requests = [];
  const fetchImpl = async (_url, options) => {
    const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
    requests.push(entries.map((item) => item.id));
    const returned = requests.length === 1 ? entries.filter((item) => item.id !== 'products.3.name') : entries;
    return translationResponse(returned.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
  };
  const service = createWebsiteTranslationService({ platformStore: store, fetchImpl, logger: { warn() {}, info() {}, error() {} } });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  const source = { site: { locale: 'zh-CN', title: '中文官网' }, products: [{ name: '产品甲' }, { name: '产品乙' }, { name: '产品丙' }, { name: '景观级雨水收集器' }] };
  await service.schedule(source, 1, ['en']);
  const record = await waitForStatus(service, 'en', 'ready');
  assert.deepEqual(requests[1], ['products.3.name']);
  assert.equal(record.content.products[3].name, 'EN:景观级雨水收集器');
  assert.equal(record.content.site.title, 'EN:中文官网');
});

test('响应损坏时缩小批次重试，重译保留同修订号的断点', async () => {
  const store = fakeStore();
  const requests = [];
  let broken = true;
  const fetchImpl = async (_url, options) => {
    const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
    requests.push(entries.map((item) => item.id));
    if (broken && entries.length > 1) return new Response(JSON.stringify({ choices: [{ message: { content: '[{"id":' } }] }), { status: 200 });
    if (broken && entries[0].id === 'products.1.name') return translationResponse([]);
    return translationResponse(entries.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
  };
  const service = createWebsiteTranslationService({ platformStore: store, fetchImpl, logger: { warn() {}, info() {}, error() {} } });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  const source = { site: { locale: 'zh-CN', title: '中文官网' }, products: [{ name: '产品甲' }, { name: '产品乙' }] };
  await service.schedule(source, 2, ['en']);
  const failed = await waitForStatus(service, 'en', 'failed');
  assert.equal(failed.content.site.title, 'EN:中文官网');
  assert.equal(failed.content.products[0].name, 'EN:产品甲');
  broken = false;
  requests.length = 0;
  await service.schedule(source, 2, ['en']);
  const ready = await waitForStatus(service, 'en', 'ready');
  assert.deepEqual(requests, [['products.1.name']]);
  assert.equal(ready.content.products[1].name, 'EN:产品乙');
});

test('上游拒绝过大的批次时拆分请求继续翻译', async () => {
  const requests = [];
  const fetchImpl = async (_url, options) => {
    const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
    requests.push(entries.length);
    if (entries.length > 1) return new Response(JSON.stringify({ error: { message: 'context length exceeded' } }), { status: 413 });
    return translationResponse(entries.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
  };
  const service = createWebsiteTranslationService({ platformStore: fakeStore(), fetchImpl, logger: { warn() {}, info() {}, error() {} } });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  await service.schedule({ site: { locale: 'zh-CN', title: '官网' }, home: { title: '首页' } }, 3, ['en']);
  const record = await waitForStatus(service, 'en', 'ready');
  assert.deepEqual(requests, [2, 1, 1]);
  assert.equal(record.content.home.title, 'EN:首页');
});

test('译文从待翻译到就绪时通知前台重建', async () => {
  const changes = [];
  const fetchImpl = async (_url, options) => {
    const entries = JSON.parse(JSON.parse(options.body).messages.at(-1).content.split('\n').at(-1));
    return translationResponse(entries.map((item) => ({ id: item.id, text: `EN:${item.text}` })));
  };
  const service = createWebsiteTranslationService({
    platformStore: fakeStore(),
    fetchImpl,
    logger: { warn() {}, info() {}, error() {} },
    onAvailabilityChange: (change) => { changes.push(change); }
  });
  await service.saveSettings({ apiKey: 'key', model: 'model' });
  await service.schedule({ site: { locale: 'zh-CN', title: '中文官网' } }, 4, ['en']);
  await waitForStatus(service, 'en', 'ready');
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(changes, [
    { locales: ['en'], status: 'pending', sourceRevision: 4 },
    { locales: ['en'], status: 'ready', sourceRevision: 4 }
  ]);
});

for (const apiType of ['responses', 'anthropic_messages']) {
  test(`${apiType} 协议的请求地址、认证头和返回内容均能真实适配`, async () => {
    const store = fakeStore();
    let captured;
    const fetchImpl = async (url, options) => {
      captured = { url: String(url), options, body: JSON.parse(options.body) };
      const prompt = apiType === 'responses' ? captured.body.input : captured.body.messages[0].content;
      const entries = JSON.parse(prompt.split('\n').at(-1));
      const text = JSON.stringify(entries.map((item) => ({ id: item.id, text: `T:${item.text}` })));
      const payload = apiType === 'responses'
        ? { output: [{ content: [{ type: 'output_text', text }] }] }
        : { content: [{ type: 'text', text }] };
      return new Response(JSON.stringify(payload), { status: 200 });
    };
    const service = createWebsiteTranslationService({ platformStore: store, fetchImpl, logger: { error() {} } });
    await service.saveSettings({ baseUrl: 'https://translator.example/v1', apiType, apiKey: 'protocol-secret', model: 'protocol-model' });
    await service.schedule({ site: { locale: 'zh-CN', title: '测试网站' } }, 3, ['ar']);
    const deadline = Date.now() + 2000;
    let translation;
    while (Date.now() < deadline) {
      translation = await service.read('ar');
      if (translation?.status === 'ready') break;
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
    assert.equal(translation.status, 'ready');
    assert.equal(translation.content.site.title, 'T:测试网站');
    assert.ok(captured.url.endsWith(apiType === 'responses' ? '/responses' : '/messages'));
    if (apiType === 'responses') assert.equal(captured.options.headers.Authorization, 'Bearer protocol-secret');
    else {
      assert.equal(captured.options.headers['x-api-key'], 'protocol-secret');
      assert.equal(captured.options.headers['anthropic-version'], '2023-06-01');
    }
  });
}

test('IP 国家头优先于浏览器语言，无国家头时才使用 Accept-Language', () => {
  assert.equal(recommendedWebsiteLocale({ headers: { 'cf-ipcountry': 'AE', 'accept-language': 'zh-CN' } }).locale, 'ar');
  assert.equal(recommendedWebsiteLocale({ headers: { 'cf-ipcountry': 'CN', 'accept-language': 'en' } }).locale, 'zh-CN');
  assert.equal(recommendedWebsiteLocale({ headers: { 'accept-language': 'ar-SA,ar;q=0.9' } }).locale, 'ar');
  assert.equal(recommendedWebsiteLocale({ headers: { 'accept-language': 'en-US,en;q=0.9' } }).locale, 'en');
});
