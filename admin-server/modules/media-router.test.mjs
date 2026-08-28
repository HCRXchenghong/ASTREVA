import assert from 'node:assert/strict';
import test from 'node:test';
import { createMediaRouter } from './media-router.mjs';

function router(overrides = {}) {
  const records = new Map([
    ['used', { id: 'used', name: '已引用.png', url: 'https://cdn.test/used.webp', key: 'used', variants: {} }],
    ['free', { id: 'free', name: '未引用.png', url: 'https://cdn.test/free.webp', key: 'free', variants: {} }]
  ]);
  const audits = [];
  const handler = createMediaRouter({
    upload: async () => null, replace: async () => null, search: async () => ({ items: [], total: 0, page: 1, pageSize: 24, pages: 0 }),
    get: async (id) => records.get(id) || null,
    remove: async (id) => { const item = records.get(id); records.delete(id); return item; },
    references: async (url) => url.includes('/used.') ? ['home.hero.backgroundImage'] : [],
    referenceUrls: (media) => [media.url], stats: async () => ({}), storageHealth: async () => ({ ok: true }),
    listCategories: async () => [], createCategory: async () => ({}), deleteCategory: async () => ({}),
    createPresignedUpload: async () => ({}), confirmPresignedUpload: async () => ({}), aiModels: async () => ({}), aiGenerate: async () => ({}),
    aiPreview: async () => ({}), aiCommitPreview: async () => ({}), aiDiscardPreview: () => true, runtimeStatus: async () => ({}),
    bodyJson: async (req) => req.body || {}, json: (res, status, payload) => Object.assign(res, { status, payload }),
    verifySession: () => true, auditEvent: async (...args) => audits.push(args), ...overrides
  });
  return { handler, records, audits };
}

test('批量删除会精确阻止被引用素材，并继续删除未引用素材', async () => {
  const { handler, records, audits } = router();
  const req = { method: 'POST', url: '/site-admin-api/media/batch-delete', body: { ids: ['used', 'free'] } };
  const res = {};
  assert.equal(await handler(req, res, '/site-admin-api/media/batch-delete'), true);
  assert.equal(res.status, 200);
  assert.deepEqual(res.payload.data.deleted.map((item) => item.id), ['free']);
  assert.deepEqual(res.payload.data.blocked, [{ id: 'used', name: '已引用.png', references: ['home.hero.backgroundImage'] }]);
  assert.equal(records.has('used'), true);
  assert.equal(records.has('free'), false);
  assert.equal(audits[0][1], 'media.batch-deleted');
});

test('媒体管理全部路由在未登录时先返回 401', async () => {
  const { handler } = router({ verifySession: () => false });
  for (const [method, pathname] of [
    ['GET', '/site-admin-api/media'], ['GET', '/site-admin-api/media/stats'], ['POST', '/site-admin-api/media/categories'],
    ['POST', '/site-admin-api/media/batch-delete'], ['POST', '/site-admin-api/media/presign'], ['POST', '/site-admin-api/media/confirm'],
    ['GET', '/site-admin-api/media/ai-models'], ['POST', '/site-admin-api/media/ai-generate'], ['POST', '/site-admin-api/media/ai-preview'],
    ['POST', '/site-admin-api/media/ai-preview/abc/commit'], ['DELETE', '/site-admin-api/media/ai-preview/abc'],
    ['GET', '/site-admin-api/media/runtime'], ['PUT', '/site-admin-api/media/abc/replace']
  ]) {
    const res = {};
    await handler({ method, url: pathname }, res, pathname);
    assert.equal(res.status, 401, `${method} ${pathname}`);
  }
});
