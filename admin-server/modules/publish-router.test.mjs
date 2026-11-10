import assert from 'node:assert/strict';
import test from 'node:test';
import { createPublishRouter } from './publish-router.mjs';

test('草稿预览路由使用注入的 bodyJson 读取内容并返回 202', async () => {
  let status = 0;
  let payload = null;
  const router = createPublishRouter({
    platformStore: {},
    publishService: { requestPreview: async (content) => ({ ok: true, content }) },
    verifySession: () => ({ userId: 'admin-test' }),
    auditEvent: async () => {},
    bodyJson: async () => ({ content: { site: { brandName: 'ASTREVA' }, home: {} } }),
    json: (_res, code, data) => { status = code; payload = data; },
    publicOrigin: 'http://127.0.0.1:1337'
  });
  const req = { method: 'POST', url: '/site-admin-api/preview', headers: {} };
  const handled = await router(req, {}, '/site-admin-api/preview');
  assert.equal(handled, true);
  assert.equal(status, 202);
  assert.equal(payload.data.ok, true);
});
