import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import sharp from 'sharp';
import { createMediaStore, mediaObjectKeys, mediaReferenceUrls } from './media-store.mjs';
import { createMediaAiService } from './media-ai-service.mjs';

function memoryPlatformStore({ failSave = false } = {}) {
  const records = new Map();
  return {
    driver: 'postgres', records,
    async listMedia() { return [...records.values()]; },
    async getMedia(id) { return records.get(id) || null; },
    async saveMedia(record) { if (failSave) throw new Error('模拟数据库失败'); records.set(record.id, structuredClone(record)); },
    async deleteMedia(id) { return records.delete(id); }
  };
}

async function fixture(root, name, width, height, color = '#1677df') {
  const filename = path.join(root, name);
  await sharp({ create: { width, height, channels: 4, background: color } }).png().toFile(filename);
  return filename;
}

function localStore(root, platformStore) {
  return createMediaStore({
    driver: 'local', uploadDir: path.join(root, 'uploads'), publicOrigin: 'http://admin.test', platformStore,
    imageWidths: '100,200,400', imageQuality: 80, maxUploadBytes: 10 * 1024 * 1024, directUploadSecret: 'test-direct-secret'
  });
}

test('智能上传生成 WebP 主图、多尺寸变体和原图，删除时清理全部对象', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-smart-media-'));
  const platformStore = memoryPlatformStore();
  const store = localStore(root, platformStore);
  const source = await fixture(root, 'fixture.png', 320, 180);
  const media = await store.uploadSmartImage({ temporaryPath: source, originalName: '产品场景.png', category: '产品图', preserveOriginal: true });

  assert.equal(media.mime, 'image/webp');
  assert.equal(media.width, 320);
  assert.equal(media.height, 180);
  assert.equal(media.category, '产品图');
  assert.deepEqual(media.variants.webp.map((item) => item.width), [100, 200]);
  assert.ok(media.variants.original.url.endsWith('/original.png'));
  assert.equal(media.checksumSha256.length, 64);
  for (const key of mediaObjectKeys(media)) assert.equal(existsSync(path.join(root, 'uploads', key.replace('uploads/site-admin/', ''))), true, key);
  assert.equal(mediaReferenceUrls(media).length, 4);

  await store.remove(media.id);
  assert.equal(await platformStore.getMedia(media.id), null);
  for (const key of mediaObjectKeys(media)) assert.equal(existsSync(path.join(root, 'uploads', key.replace('uploads/site-admin/', ''))), false, key);
});

test('稳定替换保留主 URL，并更新尺寸、校验和变体', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-replace-media-'));
  const platformStore = memoryPlatformStore();
  const store = localStore(root, platformStore);
  const first = await fixture(root, 'first.png', 320, 180);
  const second = await fixture(root, 'second.png', 180, 360, '#ee7834');
  const original = await store.uploadSmartImage({ temporaryPath: first, originalName: 'first.png', category: '首页' });
  const replaced = await store.replace(original.id, { temporaryPath: second, originalName: 'second.png', mode: 'smart', category: '首页', preserveOriginal: true });

  assert.equal(replaced.key, original.key);
  assert.equal(replaced.url, original.url);
  assert.equal(replaced.width, 180);
  assert.equal(replaced.height, 360);
  assert.equal(replaced.sourceType, 'replacement');
  assert.notEqual(replaced.checksumSha256, original.checksumSha256);
  assert.deepEqual(replaced.variants.webp.map((item) => item.width), [100]);
});

test('SVG 和伪造扩展名会被内容校验拒绝，数据库失败会回滚对象', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-invalid-media-'));
  const svg = path.join(root, 'attack.png');
  await fs.writeFile(svg, '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>');
  const normalStore = localStore(root, memoryPlatformStore());
  await assert.rejects(() => normalStore.uploadOriginal({ temporaryPath: svg, originalName: 'attack.png' }), /SVG/);

  const valid = await fixture(root, 'valid.png', 300, 200);
  const failingRoot = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-rollback-media-'));
  const failingStore = localStore(failingRoot, memoryPlatformStore({ failSave: true }));
  await assert.rejects(() => failingStore.uploadSmartImage({ temporaryPath: valid, originalName: 'valid.png' }), /模拟数据库失败/);
  const files = await fs.readdir(path.join(failingRoot, 'uploads', 'images'), { recursive: true }).catch(() => []);
  assert.equal(files.filter((name) => /\.[a-z]+$/i.test(name)).length, 0);
});

test('OpenAI Images API 模拟真实请求链支持直接入库，以及临时预览后确认入库', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-ai-media-'));
  const platformStore = memoryPlatformStore();
  const store = localStore(root, platformStore);
  const image = await sharp({ create: { width: 256, height: 256, channels: 4, background: '#2755aa' } }).png().toBuffer();
  const calls = [];
  const service = createMediaAiService({
    mediaStore: store, apiKey: 'test-key', baseUrl: 'http://127.0.0.1:9999/v1', allowedHosts: '127.0.0.1', configuredModels: 'gpt-image-1',
    fetchImpl: async (url, options = {}) => {
      calls.push({ url: String(url), options });
      if (String(url).endsWith('/models')) return Response.json({ data: [{ id: 'gpt-image-1' }] });
      return Response.json({ data: [{ b64_json: image.toString('base64'), revised_prompt: '已优化提示词' }] });
    }
  });
  assert.deepEqual((await service.models({ refresh: true })).models, ['gpt-image-1']);
  const media = await service.generate({ prompt: '一张工业排水系统产品图', model: 'gpt-image-1', category: 'AI 生成' });
  assert.equal(calls.length, 2);
  assert.equal(media.sourceType, 'ai');
  assert.equal(media.category, 'AI 生成');
  assert.equal(media.metadata.ai.model, 'gpt-image-1');
  assert.equal(media.metadata.ai.revisedPrompt, '已优化提示词');
  assert.equal(await platformStore.getMedia(media.id) != null, true);

  const beforePreviewCount = platformStore.records.size;
  const preview = await service.preview({ prompt: '一张待确认的工业产品图', model: 'gpt-image-1', size: '1024x1024' });
  assert.match(preview.dataUrl, /^data:image\/png;base64,/);
  assert.equal(platformStore.records.size, beforePreviewCount, '生成临时结果时不得提前入库');
  const committed = await service.commitPreview({ previewId: preview.id, category: 'AI 待确认' });
  assert.equal(committed.category, 'AI 待确认');
  assert.equal(platformStore.records.size, beforePreviewCount + 1);
  await assert.rejects(() => service.commitPreview({ previewId: preview.id }), /不存在或已过期/);
});
