import assert from 'node:assert/strict';
import test from 'node:test';
import { createAIProviderClient } from './ai-provider-client.mjs';

// 只记录告警，避免测试输出被预期内的失败信息淹没。
function silentLogger() {
  const messages = [];
  return {
    messages,
    warn(message) { messages.push(String(message)); },
    error() {},
    info() {}
  };
}

function fakeBridge({ bindings = [], secret = null, secretError = null, bindingsError = null, onCall = null } = {}) {
  const calls = [];
  const adminJson = async (pathname) => {
    calls.push(pathname);
    onCall?.(pathname);
    if (pathname === '/api/admin/ai-bindings') {
      if (bindingsError) throw bindingsError;
      return { bindings };
    }
    if (/^\/api\/admin\/ai-providers\/[^/]+\/secret$/.test(pathname)) {
      if (secretError) throw secretError;
      return secret;
    }
    throw new Error(`测试桩收到未预期路径：${pathname}`);
  };
  return { adminJson, calls };
}

function usableSecret(overrides = {}) {
  return {
    service_only: true,
    id: 'provider-1',
    name: '主文本供应商',
    kind: 'text',
    api_type: 'chat_completions',
    base_url: 'https://api.provider.example/v1',
    api_key: 'sk-provider-secret',
    model: 'provider-default-model',
    models: ['provider-default-model'],
    allowed_hosts: 'api.provider.example',
    enabled: true,
    ...overrides
  };
}

test('a. 有绑定且供应商可用时返回正确配置，绑定模型优先于供应商默认模型', async () => {
  const bridge = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1', model: 'bound-model' }],
    secret: usableSecret()
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  assert.deepEqual(await client.resolve('translation'), {
    baseUrl: 'https://api.provider.example/v1',
    apiKey: 'sk-provider-secret',
    model: 'bound-model',
    apiType: 'chat_completions',
    providerId: 'provider-1',
    allowedHosts: 'api.provider.example'
  });
  assert.deepEqual(bridge.calls, [
    '/api/admin/ai-bindings',
    '/api/admin/ai-providers/provider-1/secret'
  ]);
});

test('a2. 绑定未指定模型时回退供应商默认模型；seo 能力同样可解析', async () => {
  const bridge = fakeBridge({
    bindings: [{ capability: 'seo', provider_id: 'provider-1' }],
    secret: usableSecret({ api_type: 'responses' })
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  const resolved = await client.resolve('seo');
  assert.equal(resolved.model, 'provider-default-model');
  assert.equal(resolved.apiType, 'responses');
});

test('a3. image 能力接受生图协议，并返回生图模型', async () => {
  const bridge = fakeBridge({
    bindings: [{ capability: 'image', provider_id: 'provider-image', model: 'gpt-image-1' }],
    secret: usableSecret({
      id: 'provider-image',
      api_type: 'images',
      base_url: 'https://images.provider.example/v1',
      api_key: 'sk-image-secret',
      allowed_hosts: 'images.provider.example'
    })
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  assert.deepEqual(await client.resolve('image'), {
    baseUrl: 'https://images.provider.example/v1',
    apiKey: 'sk-image-secret',
    model: 'gpt-image-1',
    apiType: 'images',
    providerId: 'provider-image',
    allowedHosts: 'images.provider.example'
  });
});

test('b. 绑定指向被禁用供应商时返回 null', async () => {
  const logger = silentLogger();
  const bridge = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secret: usableSecret({ enabled: false })
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger });
  assert.equal(await client.resolve('translation'), null);
  assert.equal(logger.messages.length, 1);
  assert.match(logger.messages[0], /已停用/);
});

test('b2. 协议与能力不匹配时返回 null（文本能力不能用生图协议）', async () => {
  const logger = silentLogger();
  const bridge = fakeBridge({
    bindings: [{ capability: 'seo', provider_id: 'provider-1' }],
    secret: usableSecret({ api_type: 'images' })
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger });
  assert.equal(await client.resolve('seo'), null);
  assert.match(logger.messages[0], /协议/);
});

test('c. adminJson 抛错（Go 不可达）时返回 null、不抛出，且连续调用只告警一次', async () => {
  const logger = silentLogger();
  let calls = 0;
  const client = createAIProviderClient({
    adminJson: async () => {
      calls += 1;
      throw new Error('connect ECONNREFUSED 127.0.0.1:8080');
    },
    logger
  });
  // 并行和串行都不应抛出。
  const results = await Promise.all([client.resolve('translation'), client.resolve('translation')]);
  assert.deepEqual(results, [null, null]);
  assert.equal(await client.resolve('translation'), null);
  // 失败结果同样按 TTL 缓存，且告警只出现一次。
  assert.equal(calls, 1);
  assert.equal(logger.messages.length, 1);
  assert.match(logger.messages[0], /读取能力绑定失败/);
  assert.equal(logger.messages[0].includes('sk-provider-secret'), false);
});

test('c2. 读取 secret 失败时返回 null 且告警一次', async () => {
  const logger = silentLogger();
  const bridge = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secretError: new Error('客服引擎请求失败（HTTP 502）')
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger });
  assert.equal(await client.resolve('translation'), null);
  assert.equal(await client.resolve('translation'), null);
  assert.equal(logger.messages.length, 1);
  assert.match(logger.messages[0], /provider-1/);
});

test('d. 按能力缓存：连续两次只读一次 Go，invalidate() 后重新读取', async () => {
  let bindingsCalls = 0;
  let secretCalls = 0;
  const bridge = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secret: usableSecret(),
    onCall: (pathname) => {
      if (pathname === '/api/admin/ai-bindings') bindingsCalls += 1;
      else secretCalls += 1;
    }
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  assert.ok(await client.resolve('translation'));
  assert.ok(await client.resolve('translation'));
  assert.equal(bindingsCalls, 1);
  assert.equal(secretCalls, 1);

  client.invalidate();
  assert.ok(await client.resolve('translation'));
  assert.equal(bindingsCalls, 2);
  assert.equal(secretCalls, 2);
});

test('d2. 不同能力分别缓存，未绑定能力返回 null 且不读取 secret', async () => {
  const bridge = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secret: usableSecret()
  });
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger: silentLogger() });
  assert.ok(await client.resolve('translation'));
  assert.equal(await client.resolve('image'), null);
  assert.deepEqual(bridge.calls, [
    '/api/admin/ai-bindings',
    '/api/admin/ai-providers/provider-1/secret',
    '/api/admin/ai-bindings'
  ]);
});

for (const [name, overrides] of [
  ['api_key 为空', { api_key: '' }],
  ['api_key 只有空白', { api_key: '   ' }],
  ['base_url 为空', { base_url: '' }],
  ['base_url 不是有效 URL', { base_url: 'not-a-url' }],
  ['base_url 不是 HTTP(S)', { base_url: 'file:///etc/passwd' }]
]) {
  test(`e. ${name} 时返回 null`, async () => {
    const logger = silentLogger();
    const bridge = fakeBridge({
      bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
      secret: usableSecret(overrides)
    });
    const client = createAIProviderClient({ adminJson: bridge.adminJson, logger });
    assert.equal(await client.resolve('translation'), null);
    assert.equal(logger.messages.length, 1);
  });
}

test('e2. 空白名单表示不限制，但白名单不匹配时防御性返回 null', async () => {
  const allowed = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secret: usableSecret({ allowed_hosts: '' })
  });
  assert.ok(await createAIProviderClient({ adminJson: allowed.adminJson, logger: silentLogger() }).resolve('translation'));

  const denied = fakeBridge({
    bindings: [{ capability: 'translation', provider_id: 'provider-1' }],
    secret: usableSecret({ allowed_hosts: 'other.example.com' })
  });
  const logger = silentLogger();
  assert.equal(await createAIProviderClient({ adminJson: denied.adminJson, logger }).resolve('translation'), null);
  assert.match(logger.messages[0], /白名单/);
});

test('未绑定任何能力时返回 null，不读取 secret', async () => {
  const bridge = fakeBridge({ bindings: [] });
  const logger = silentLogger();
  const client = createAIProviderClient({ adminJson: bridge.adminJson, logger });
  assert.equal(await client.resolve('translation'), null);
  assert.deepEqual(bridge.calls, ['/api/admin/ai-bindings']);
  assert.equal(logger.messages.length, 0);
});

test('缺少 adminJson 通道时在构造阶段明确报错', () => {
  assert.throws(() => createAIProviderClient({}), /adminJson/);
});
