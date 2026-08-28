import assert from 'node:assert/strict';
import test from 'node:test';
import { collectTranslatableEntries, createWebsiteTranslationService } from './website-translation-service.mjs';
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

test('只收集可见文案，不会翻译 URL、Slug、ID 和媒体地址', () => {
  const entries = collectTranslatableEntries({
    title: '建筑排水方案', slug: '不应翻译', id: '中文-id',
    href: '/中文路径', coverImage: 'https://example.com/中文.jpg', nested: { description: '专业服务' }
  });
  assert.deepEqual(entries, [
    { id: 'title', text: '建筑排水方案' },
    { id: 'nested.description', text: '专业服务' }
  ]);
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
