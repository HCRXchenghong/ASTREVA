import test from 'node:test';
import assert from 'node:assert/strict';
import { createAuthRouter } from './auth-router.mjs';

function makeHarness({ setupToken = 'setup-token-0123456789abcdef' } = {}) {
  let store = { users: [] };
  const calls = [];
  const router = createAuthRouter({
    readUsers: async () => store,
    saveUsers: async (next) => { store = next; calls.push('saveUsers'); },
    bodyJson: async (req) => req.body,
    json: (res, status, payload) => { res.statusCode = status; res.payload = payload; },
    generateTotpSecret: () => 'JBSWY3DPEHPK3PXP',
    verifyTotp: () => true,
    otpAuthUrl: () => 'otpauth://totp/test',
    setSessionCookie: async () => { calls.push('setSessionCookie'); },
    clearSessionCookie: () => {},
    verifySession: () => null,
    auditEvent: async () => {},
    resolveClient: () => ({ ip: '203.0.113.9', source: 'direct' }),
    setupToken
  });
  return { router, calls, getStore: () => store, setStore: (next) => { store = next; } };
}

function request(body) {
  return { method: 'POST', headers: { 'user-agent': 'test' }, url: '/', body };
}

test('初始化令牌缺失时拒绝 setup/start', async () => {
  const { router } = makeHarness();
  const res = {};
  await router(request({ email: 'a@b.com', password: 'Password-123' }), res, '/site-admin-auth/setup/start');
  assert.equal(res.statusCode, 403);
});

test('已有管理员时旧 setup 挑战不能覆盖账号', async () => {
  const { router, getStore, setStore } = makeHarness();
  const started = {};
  await router(request({ email: 'attacker@example.com', password: 'Password-123', setupToken: 'setup-token-0123456789abcdef' }), started, '/site-admin-auth/setup/start');
  assert.equal(started.statusCode, 200);
  const challengeId = started.payload.challengeId;

  // 管理员在挑战有效期内完成了自己的初始化。
  setStore({ users: [{ id: 'owner', email: 'owner@example.com', passwordHash: 'x', totpSecret: 'JBSWY3DPEHPK3PXP' }] });
  const verified = {};
  await router(request({ challengeId, code: '123456', setupToken: 'setup-token-0123456789abcdef' }), verified, '/site-admin-auth/setup/verify');
  assert.equal(verified.statusCode, 409);
  assert.equal(getStore().users[0].email, 'owner@example.com');
});
