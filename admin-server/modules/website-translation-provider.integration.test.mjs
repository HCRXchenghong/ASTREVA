import assert from 'node:assert/strict';
import test from 'node:test';
import http from 'node:http';
import { createAIProviderClient } from './ai-provider-client.mjs';
import { createWebsiteTranslationService } from './website-translation-service.mjs';

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

function silentLogger() {
  const messages = [];
  return {
    messages,
    warn(message) { messages.push(String(message)); },
    error(message) { messages.push(String(message)); },
    info() {}
  };
}

/**
 * 桩 AI 上游：真实监听本机端口，记录收到的请求并按翻译协议返回 JSON 数组。
 * 翻译服务通过真实 fetch 访问这里，用于验证“请求确实打到了绑定供应商的地址”。
 */
async function startStubUpstream(prefix) {
  const requests = [];
  const server = http.createServer((req, res) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      let body = null;
      try { body = raw ? JSON.parse(raw) : null; } catch { body = null; }
      requests.push({ method: req.method, url: req.url, headers: req.headers, body });

      if (String(req.url).endsWith('/models')) {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ data: [{ id: 'stub-model' }] }));
        return;
      }

      const prompt = body?.messages?.at(-1)?.content || body?.input || '';
      let entries = [];
      try { entries = JSON.parse(prompt.split('\n').at(-1)); } catch { entries = []; }
      const translated = entries.map((item) => ({ id: item.id, text: `${prefix}:${item.text}` }));
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify(translated) } }] }));
    });
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  return {
    requests,
    baseUrl: `http://127.0.0.1:${port}/v1`,
    close: () => new Promise((resolve) => {
      server.closeAllConnections?.();
      server.close(() => resolve());
    })
  };
}

async function waitForReady(service, locale, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const record = await service.read(locale);
    if (record?.status === 'ready') return record;
    if (record?.status === 'failed' || record?.status === 'unconfigured') {
      throw new Error(`翻译未成功：${record.status} ${record.lastError || ''}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error('翻译未在超时时间内完成。');
}

function fakeBridge({ binding, secret }) {
  const calls = [];
  return {
    calls,
    adminJson: async (pathname) => {
      calls.push(pathname);
      if (pathname === '/api/admin/ai-bindings') {
        return { bindings: binding ? [binding] : [] };
      }
      if (/^\/api\/admin\/ai-providers\/[^/]+\/secret$/.test(pathname)) return secret;
      throw new Error(`测试桩收到未预期路径：${pathname}`);
    }
  };
}

test('集成：翻译能力绑定控制台供应商后，翻译请求打到绑定上游而不是本地配置地址', { timeout: 15000 }, async (t) => {
  const boundUpstream = await startStubUpstream('BOUND');
  const localUpstream = await startStubUpstream('LOCAL');
  t.after(async () => {
    await boundUpstream.close();
    await localUpstream.close();
  });

  const bridge = fakeBridge({
    binding: { capability: 'translation', provider_id: 'provider-1', model: 'bound-model' },
    secret: {
      service_only: true,
      id: 'provider-1',
      kind: 'text',
      enabled: true,
      api_type: 'chat_completions',
      base_url: boundUpstream.baseUrl,
      api_key: 'bound-secret',
      model: 'provider-default-model',
      allowed_hosts: '127.0.0.1'
    }
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  const store = fakeStore();
  const service = createWebsiteTranslationService({
    platformStore: store,
    logger: silentLogger(),
    resolveProvider: (capability) => client.resolve(capability)
  });

  // 本地仍保留一套完整的旧上游配置，用于证明绑定优先生效。
  await service.saveSettings({
    baseUrl: localUpstream.baseUrl,
    apiType: 'chat_completions',
    apiKey: 'local-secret',
    model: 'local-model'
  });
  await service.schedule({ site: { locale: 'zh-CN', title: '中文标题' } }, 1, ['en']);
  const record = await waitForReady(service, 'en');

  assert.equal(boundUpstream.requests.length, 1);
  assert.equal(localUpstream.requests.length, 0);
  const request = boundUpstream.requests[0];
  assert.equal(request.url, '/v1/chat/completions');
  assert.equal(request.headers.authorization, 'Bearer bound-secret');
  assert.equal(request.headers['x-api-key'], 'bound-secret');
  assert.equal(request.body.model, 'bound-model');
  assert.equal(record.content.site.title, 'BOUND:中文标题');
});

test('集成：没有可用绑定时请求回到原先配置的本地翻译上游', { timeout: 15000 }, async (t) => {
  const boundUpstream = await startStubUpstream('BOUND');
  const localUpstream = await startStubUpstream('LOCAL');
  t.after(async () => {
    await boundUpstream.close();
    await localUpstream.close();
  });

  const bridge = fakeBridge({ binding: null, secret: null });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  const store = fakeStore();
  const service = createWebsiteTranslationService({
    platformStore: store,
    logger: silentLogger(),
    resolveProvider: (capability) => client.resolve(capability)
  });

  await service.saveSettings({
    baseUrl: localUpstream.baseUrl,
    apiType: 'chat_completions',
    apiKey: 'local-secret',
    model: 'local-model'
  });
  await service.schedule({ site: { locale: 'zh-CN', title: '中文标题' } }, 2, ['en']);
  const record = await waitForReady(service, 'en');

  assert.equal(localUpstream.requests.length, 1);
  assert.equal(boundUpstream.requests.length, 0);
  const request = localUpstream.requests[0];
  assert.equal(request.url, '/v1/chat/completions');
  assert.equal(request.headers.authorization, 'Bearer local-secret');
  assert.equal(request.body.model, 'local-model');
  assert.equal(record.content.site.title, 'LOCAL:中文标题');
  // 只读取了绑定列表，没有指向任何供应商的 secret。
  assert.deepEqual(bridge.calls, ['/api/admin/ai-bindings']);
});

test('集成：Go 不可达时翻译服务继续使用本地配置且不抛错', { timeout: 15000 }, async (t) => {
  const localUpstream = await startStubUpstream('LOCAL');
  t.after(async () => { await localUpstream.close(); });

  const logger = silentLogger();
  const client = createAIProviderClient({
    adminJson: async () => { throw new Error('connect ECONNREFUSED 127.0.0.1:8080'); },
    logger
  });
  const service = createWebsiteTranslationService({
    platformStore: fakeStore(),
    logger,
    resolveProvider: (capability) => client.resolve(capability)
  });
  await service.saveSettings({
    baseUrl: localUpstream.baseUrl,
    apiType: 'chat_completions',
    apiKey: 'local-secret',
    model: 'local-model'
  });
  await service.schedule({ site: { locale: 'zh-CN', title: '降级标题' } }, 3, ['ar']);
  const record = await waitForReady(service, 'ar');

  assert.equal(localUpstream.requests.length, 1);
  assert.equal(record.content.site.title, 'LOCAL:降级标题');
  assert.ok(logger.messages.some((message) => /读取能力绑定失败/.test(message)));
});

test('集成：AI SEO 走 generateJson 时使用 seo 能力绑定的上游', { timeout: 15000 }, async (t) => {
  const boundUpstream = await startStubUpstream('SEO');
  t.after(async () => { await boundUpstream.close(); });

  const bridge = fakeBridge({
    binding: { capability: 'seo', provider_id: 'provider-seo', model: 'seo-model' },
    secret: {
      service_only: true,
      id: 'provider-seo',
      kind: 'text',
      enabled: true,
      api_type: 'chat_completions',
      base_url: boundUpstream.baseUrl,
      api_key: 'seo-secret',
      model: 'seo-default-model',
      allowed_hosts: '127.0.0.1'
    }
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  const service = createWebsiteTranslationService({
    platformStore: fakeStore(),
    logger: silentLogger(),
    resolveProvider: (capability) => client.resolve(capability)
  });
  // 仅配置本地上游地址（不配置 API Key），证明 SEO 生成完全依赖 seo 绑定。
  await service.saveSettings({
    baseUrl: 'https://local.seo.invalid/v1',
    apiType: 'chat_completions',
    model: 'local-model',
    clearApiKey: true
  });

  const result = await service.generateJson('返回一个 JSON 数组', { type: 'array', capability: 'seo' });
  assert.deepEqual(result, []);
  assert.equal(boundUpstream.requests.length, 1);
  assert.equal(boundUpstream.requests[0].url, '/v1/chat/completions');
  assert.equal(boundUpstream.requests[0].headers.authorization, 'Bearer seo-secret');
  assert.equal(boundUpstream.requests[0].body.model, 'seo-model');
});
