import assert from 'node:assert/strict';
import test from 'node:test';
import { createWebsiteSeoRouter } from './website-seo-router.mjs';

test('官网域名只能由已登录管理员修改，变更后自动排队重建', async () => {
  let domain = '';
  let authenticated = false;
  let rebuilds = 0;
  const responses = [];
  const router = createWebsiteSeoRouter({
    verifySession: () => authenticated ? { userId: 'admin-1' } : null,
    json: (_res, status, payload) => { responses.push({ status, payload }); },
    bodyJson: async () => ({ domain: 'www.astreva.com' }),
    auditEvent: async () => {},
    siteDomainService: {
      read: async () => ({ domain, effectiveOrigin: domain ? `https://${domain}` : 'http://127.0.0.1:4321' }),
      save: async (value) => {
        const changed = domain !== value;
        domain = value;
        return { domain, effectiveOrigin: `https://${domain}`, changed };
      }
    },
    publishService: { requestPublish: async () => { rebuilds += 1; return { jobId: 'job-1' }; } }
  });
  assert.equal(await router({ method: 'PUT' }, {}, '/site-admin-api/seo/site-domain'), true);
  assert.equal(responses.at(-1).status, 401);
  authenticated = true;
  await router({ method: 'PUT' }, {}, '/site-admin-api/seo/site-domain');
  assert.equal(responses.at(-1).status, 200);
  assert.equal(responses.at(-1).payload.data.effectiveOrigin, 'https://www.astreva.com');
  assert.equal(rebuilds, 1);
  await router({ method: 'PUT' }, {}, '/site-admin-api/seo/site-domain');
  assert.equal(rebuilds, 1);
  await router({ method: 'GET' }, {}, '/site-admin-api/seo/site-domain');
  assert.equal(responses.at(-1).payload.data.domain, 'www.astreva.com');
});
