import test from 'node:test';
import assert from 'node:assert/strict';
import { createPublishService } from './publish-service.mjs';

test('previewToken 从构建服务读取本次预览 token', async (t) => {
  const token = 'a'.repeat(32);
  t.mock.method(globalThis, 'fetch', async (url) => {
    assert.match(String(url), /\/last-build$/);
    return {
      ok: true,
      async json() { return { lastBuild: { previewToken: token } }; }
    };
  });
  const service = createPublishService({
    rebuildUrl: 'http://rebuild.invalid/rebuild',
    rebuildSecret: '',
    siteOrigin: 'https://www.example.com',
    platformStore: {}
  });
  assert.equal(await service.previewToken(), token);
});
