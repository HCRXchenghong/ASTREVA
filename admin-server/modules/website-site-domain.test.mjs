import assert from 'node:assert/strict';
import test from 'node:test';
import { createWebsiteSiteDomain, normalizeWebsiteDomain } from './website-site-domain.mjs';

test('只允许真实纯域名，国际化域名规范化', () => {
  assert.equal(normalizeWebsiteDomain('WWW.ASTREVA.COM'), 'www.astreva.com');
  assert.equal(normalizeWebsiteDomain('例子.中国'), 'xn--fsqu00a.xn--fiqs8s');
  for (const value of ['https://www.astreva.com', 'www.astreva.com/path', 'www.astreva.com:443', 'localhost', '127.0.0.1', 'www.example.com', 'bad_domain.com']) {
    assert.throws(() => normalizeWebsiteDomain(value), undefined, value);
  }
});

test('后台域名保存在运行时设置，重建服务重启后仍能读取', async () => {
  let setting = null;
  const platformStore = {
    readRuntimeSetting: async () => setting,
    saveRuntimeSetting: async (_key, value) => { setting = { ...structuredClone(value), updatedAt: '2026-10-22T00:00:00Z' }; }
  };
  const first = createWebsiteSiteDomain({ platformStore, fallbackOrigin: 'http://127.0.0.1:4321' });
  assert.equal((await first.read()).effectiveOrigin, 'http://127.0.0.1:4321');
  assert.equal((await first.save('www.astreva.com')).changed, true);
  assert.equal((await first.save('www.astreva.com')).changed, false);
  const restarted = createWebsiteSiteDomain({ platformStore, fallbackOrigin: 'https://old.astreva.com' });
  assert.deepEqual(await restarted.read(), {
    domain: 'www.astreva.com',
    effectiveOrigin: 'https://www.astreva.com',
    source: 'admin',
    updatedAt: '2026-10-22T00:00:00Z'
  });
  await restarted.save('');
  assert.equal((await restarted.read()).effectiveOrigin, 'https://old.astreva.com');
});
