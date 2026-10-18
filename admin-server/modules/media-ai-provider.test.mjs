import assert from 'node:assert/strict';
import test from 'node:test';
import { createMediaAiService } from './media-ai-service.mjs';

function fakeMediaStore() {
  return {
    uploaded: [],
    async uploadSmartImage(payload) {
      this.uploaded.push(payload);
      return { id: `media-${this.uploaded.length}`, ...payload, url: `https://cdn.example/${this.uploaded.length}.png` };
    }
  };
}

function fakeFetch(calls, imageBytes = Buffer.from('fake-png-bytes')) {
  return async (url, options = {}) => {
    calls.push({ url: String(url), options, body: options.body ? JSON.parse(options.body) : null });
    if (String(url).endsWith('/models')) {
      return Response.json({ data: [{ id: 'bound-image-model' }, { id: 'local-image-model' }] });
    }
    return Response.json({ data: [{ b64_json: imageBytes.toString('base64') }] });
  };
}

const PROVIDER = Object.freeze({
  baseUrl: 'https://bound.example/v1',
  apiKey: 'bound-image-key',
  model: 'bound-image-model',
  apiType: 'images',
  providerId: 'provider-image',
  allowedHosts: 'bound.example'
});

test('AI 生图：image 能力绑定时覆盖上游地址/密钥/模型，生图参数保持不变', async () => {
  const calls = [];
  const store = fakeMediaStore();
  const service = createMediaAiService({
    mediaStore: store,
    apiKey: 'local-image-key',
    baseUrl: 'https://local.example/v1',
    defaultModel: 'local-image-model',
    allowedHosts: 'local.example',
    configuredModels: 'local-image-model',
    fetchImpl: fakeFetch(calls),
    resolveProvider: async (capability) => (capability === 'image' ? PROVIDER : null)
  });

  const capability = await service.models();
  assert.equal(capability.configured, true);
  assert.equal(capability.defaultModel, 'bound-image-model');
  assert.ok(capability.models.includes('bound-image-model'));
  assert.match(calls[0].url, /^https:\/\/bound\.example\/v1\/models$/);
  assert.equal(calls[0].options.headers.Authorization, 'Bearer bound-image-key');

  const preview = await service.preview({ prompt: '工业排水产品图', model: 'local-image-model', size: '1024x1024', quality: 'high' });
  assert.match(preview.dataUrl, /^data:image\/png;base64,/);
  const generateCall = calls.at(-1);
  assert.match(generateCall.url, /^https:\/\/bound\.example\/v1\/images\/generations$/);
  assert.equal(generateCall.options.headers.Authorization, 'Bearer bound-image-key');
  // 绑定模型覆盖调用方选择的本地模型，尺寸/质量等生图参数保持请求值。
  assert.equal(generateCall.body.model, 'bound-image-model');
  assert.equal(generateCall.body.size, '1024x1024');
  assert.equal(generateCall.body.quality, 'high');
});

test('AI 生图：没有可用绑定时完全沿用本地配置', async () => {
  const calls = [];
  const store = fakeMediaStore();
  const service = createMediaAiService({
    mediaStore: store,
    apiKey: 'local-image-key',
    baseUrl: 'https://local.example/v1',
    defaultModel: 'gpt-image-1',
    allowedHosts: 'local.example',
    configuredModels: 'gpt-image-1',
    fetchImpl: fakeFetch(calls),
    resolveProvider: async () => null
  });

  await service.models();
  assert.match(calls[0].url, /^https:\/\/local\.example\/v1\/models$/);
  assert.equal(calls[0].options.headers.Authorization, 'Bearer local-image-key');

  await service.generate({ prompt: '本地生图', model: 'gpt-image-1', category: 'AI' });
  const generateCall = calls.at(-1);
  assert.match(generateCall.url, /^https:\/\/local\.example\/v1\/images\/generations$/);
  assert.equal(generateCall.options.headers.Authorization, 'Bearer local-image-key');
  assert.equal(generateCall.body.model, 'gpt-image-1');
  assert.equal(store.uploaded.length, 1);
});

test('AI 生图：绑定协议不是 images 时防御性回退本地配置', async () => {
  const calls = [];
  const service = createMediaAiService({
    mediaStore: fakeMediaStore(),
    apiKey: 'local-image-key',
    baseUrl: 'https://local.example/v1',
    defaultModel: 'gpt-image-1',
    allowedHosts: 'local.example',
    configuredModels: 'gpt-image-1',
    fetchImpl: fakeFetch(calls),
    resolveProvider: async () => ({ ...PROVIDER, apiType: 'chat_completions' })
  });

  await service.models();
  assert.match(calls[0].url, /^https:\/\/local\.example\/v1\/models$/);
  assert.equal(calls[0].options.headers.Authorization, 'Bearer local-image-key');
});
