import crypto from 'node:crypto';
import bcrypt from 'bcryptjs';
import QRCode from 'qrcode';

/** 后台账号密码、TOTP 与会话路由。HTTP 层通过依赖注入提供，不依赖内容或媒体模块。 */
export function createAuthRouter({
  readUsers,
  saveUsers,
  bodyJson,
  json,
  generateTotpSecret,
  verifyTotp,
  otpAuthUrl,
  setSessionCookie,
  clearSessionCookie,
  verifySession,
  auditEvent
}) {
  const pendingSetups = new Map();
  const pendingLogins = new Map();

  return async function handleAuth(req, res, pathname) {
    if (pathname === '/site-admin-auth/status' && req.method === 'GET') {
      const users = await readUsers();
      json(res, 200, { hasAdmin: users.users.length > 0, authenticated: Boolean(verifySession(req)) });
      return true;
    }
    if (pathname === '/site-admin-auth/setup/start' && req.method === 'POST') {
      const users = await readUsers();
      if (users.users.length) {
        json(res, 409, { error: { message: '管理员账号已存在，请直接登录。' } });
        return true;
      }
      const body = await bodyJson(req);
      const email = String(body.email || '').trim().toLowerCase();
      const password = String(body.password || '');
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        json(res, 400, { error: { message: '请输入有效邮箱。' } });
        return true;
      }
      if (password.length < 8) {
        json(res, 400, { error: { message: '密码至少 8 位。' } });
        return true;
      }
      const secret = generateTotpSecret();
      const challengeId = crypto.randomUUID();
      pendingSetups.set(challengeId, { email, passwordHash: await bcrypt.hash(password, 12), secret, exp: Date.now() + 10 * 60 * 1000 });
      json(res, 200, { challengeId, secret, qr: await QRCode.toDataURL(otpAuthUrl(email, secret), { margin: 1, width: 240 }) });
      return true;
    }
    if (pathname === '/site-admin-auth/setup/verify' && req.method === 'POST') {
      const body = await bodyJson(req);
      const challenge = pendingSetups.get(body.challengeId);
      if (!challenge || challenge.exp < Date.now()) {
        json(res, 400, { error: { message: '绑定已过期，请重新开始。' } });
        return true;
      }
      if (!verifyTotp(challenge.secret, body.code)) {
        json(res, 400, { error: { message: '2FA 验证码不正确。' } });
        return true;
      }
      const user = { id: crypto.randomUUID(), email: challenge.email, passwordHash: challenge.passwordHash, totpSecret: challenge.secret, createdAt: new Date().toISOString() };
      await saveUsers({ users: [user] });
      pendingSetups.delete(body.challengeId);
      setSessionCookie(res, user);
      await auditEvent(req, 'auth.setup.completed', 'admin-user', user.id, { email: user.email }, user);
      json(res, 200, { ok: true });
      return true;
    }
    if (pathname === '/site-admin-auth/login/start' && req.method === 'POST') {
      const body = await bodyJson(req);
      const users = await readUsers();
      const user = users.users.find((item) => item.email === String(body.email || '').trim().toLowerCase());
      if (!user || !(await bcrypt.compare(String(body.password || ''), user.passwordHash))) {
        json(res, 401, { error: { message: '邮箱或密码错误。' } });
        return true;
      }
      const challengeId = crypto.randomUUID();
      pendingLogins.set(challengeId, { userId: user.id, exp: Date.now() + 10 * 60 * 1000 });
      json(res, 200, { challengeId });
      return true;
    }
    if (pathname === '/site-admin-auth/login/verify' && req.method === 'POST') {
      const body = await bodyJson(req);
      const challenge = pendingLogins.get(body.challengeId);
      if (!challenge || challenge.exp < Date.now()) {
        json(res, 400, { error: { message: '登录验证已过期，请重新登录。' } });
        return true;
      }
      const users = await readUsers();
      const user = users.users.find((item) => item.id === challenge.userId);
      if (!user || !verifyTotp(user.totpSecret, body.code)) {
        json(res, 400, { error: { message: '2FA 验证码不正确。' } });
        return true;
      }
      pendingLogins.delete(body.challengeId);
      setSessionCookie(res, user);
      await auditEvent(req, 'auth.login.completed', 'admin-user', user.id, { email: user.email }, user);
      json(res, 200, { ok: true });
      return true;
    }
    if (pathname === '/site-admin-auth/logout' && req.method === 'POST') {
      await auditEvent(req, 'auth.logout', 'admin-user');
      clearSessionCookie(res);
      json(res, 200, { ok: true });
      return true;
    }
    return false;
  };
}
