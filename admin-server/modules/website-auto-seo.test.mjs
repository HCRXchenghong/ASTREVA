import assert from 'node:assert/strict';
import test from 'node:test';
import { createWebsiteAutoSeo, needsAutomaticSeo } from './website-auto-seo.mjs';

function content() {
  return {
    site: { brandName: 'ASTREVA', brandTagline: '排水系统', logoUrl: '/logo.png', defaultSeo: { title: '旧标题' } },
    home: { hero: { title: '排水产品', backgroundImage: '/hero.jpg' }, seo: { title: '旧标题' } },
    productCenter: { hero: { title: '产品中心' }, seo: { title: '旧标题' } },
    support: { hero: { title: '服务支持' }, seo: { title: '旧标题' } },
    categories: [], products: [], staticPages: [], faqs: []
  };
}

async function until(check) {
  for (let i = 0; i < 100; i += 1) {
    if (await check()) return;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  throw new Error('自动 SEO 任务未完成');
}

test('正文修改需要自动 SEO；SEO、图片修改不会循环触发', () => {
  const before = content();
  const copy = structuredClone(before);
  copy.home.hero.title = '新排水产品';
  assert.equal(needsAutomaticSeo(before, copy), true);
  const seo = structuredClone(before);
  seo.home.seo.title = 'AI 新标题';
  assert.equal(needsAutomaticSeo(before, seo), false);
  const media = structuredClone(before);
  media.home.hero.backgroundImage = '/new.jpg';
  assert.equal(needsAutomaticSeo(before, media), false);
});

test('自动 SEO 保存中文结果后排队英阿翻译，且状态可在重启后读取', async () => {
  let record = { content: content(), revision: 4 };
  let setting = null;
  const calls = [];
  const platformStore = {
    readRuntimeSetting: async () => setting,
    saveRuntimeSetting: async (_key, value) => { setting = structuredClone(value); },
    saveContent: async (next, { expectedRevision }) => {
      assert.equal(expectedRevision, record.revision);
      record = { content: structuredClone(next), revision: record.revision + 1 };
      return { revision: record.revision };
    }
  };
  const deps = {
    platformStore,
    readContentRecord: async () => structuredClone(record),
    seoService: { generate: async (source) => {
      const next = structuredClone(source);
      next.home.seo.title = 'AI SEO 标题';
      return { content: next, generatedCount: 4, generatedAt: '2026-10-22T00:00:00Z' };
    } },
    translationService: { schedule: async (_next, revision) => { calls.push(revision); } },
    logger: { error: () => {} }
  };
  const service = createWebsiteAutoSeo(deps);
  await service.enqueue(4);
  await until(async () => (await service.status())?.status === 'ready');
  assert.equal(record.content.home.seo.title, 'AI SEO 标题');
  assert.equal(record.revision, 5);
  assert.deepEqual(calls, [5]);
  assert.equal((await createWebsiteAutoSeo(deps).status()).resultRevision, 5);
});

test('AI 不可用时内容保持可用，并记录失败供后台重试', async () => {
  const record = { content: content(), revision: 9 };
  let setting = null;
  const calls = [];
  const service = createWebsiteAutoSeo({
    platformStore: {
      readRuntimeSetting: async () => setting,
      saveRuntimeSetting: async (_key, value) => { setting = structuredClone(value); },
      saveContent: async () => { throw new Error('不应保存'); }
    },
    readContentRecord: async () => record,
    seoService: { generate: async () => { throw new Error('AI unavailable'); } },
    translationService: { schedule: async (_content, revision) => { calls.push(revision); } },
    logger: { error: () => {} }
  });
  await service.enqueue(9);
  await until(async () => (await service.status())?.status === 'failed');
  assert.match((await service.status()).error, /AI unavailable/);
  assert.deepEqual(calls, [9]);
});
