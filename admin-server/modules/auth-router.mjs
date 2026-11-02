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
  auditEvent,
  recordSystemEvent = () => {},
  resolveClient = () => ({ ip: '', source: '' }),
  platformStore = null,
  setupToken = ''
}) {
  const pendingSetups = new Map();
  const pendingLogins = new Map();
  const SETUP_PENDING_LIMIT = 50;

  // 登录失败计数与 2FA 重放保护（内存态，重启即清零）
  const LOGIN_WINDOW_MS = 15 * 60 * 1000;
  const LOGIN_MAX_FAILURES = Number(process.env.ADMIN_LOGIN_MAX_FAILURES || 10);
  const CHALLENGE_MAX_ATTEMPTS = Number(process.env.ADMIN_LOGIN_MAX_2FA_ATTEMPTS || 5);
  const failureBuckets = new Map();
  const usedTotpCodes = new Map();
  const setupBuckets = new Map();

  function setupBucket(req) {
    const key = resolveClient(req).ip || 'unknown';
    const now = Date.now();
    const current = setupBuckets.get(key);
    const bucket = !current || current.resetAt <= now ? { count: 0, resetAt: now + LOGIN_WINDOW_MS } : current;
    setupBuckets.set(key, bucket);
    if (setupBuckets.size > 5000) {
      for (const [entryKey, value] of setupBuckets) if (value.resetAt <= now) setupBuckets.delete(entryKey);
    }
    return bucket;
  }

  function setupRateLimited(req) {
    const bucket = setupBucket(req);
    return bucket.count >= LOGIN_MAX_FAILURES ? bucket : null;
  }

  function noteSetupAttempt(req) {
    const bucket = setupBucket(req);
    bucket.count += 1;
    return bucket;
  }

  function timingSafeStringEqual(left, right) {
    const a = Buffer.from(String(left || ''));
    const b = Buffer.from(String(right || ''));
    return a.length === b.length && a.length > 0 && crypto.timingSafeEqual(a, b);
  }

  function validSetupToken(body) {
    if (!setupToken) return false;
    return timingSafeStringEqual(body?.setupToken || body?.setup_token || '', setupToken);
  }

  function bucketFor(key) {
    const now = Date.now();
    const current = failureBuckets.get(key);
    const bucket = !current || current.resetAt <= now ? { count: 0, resetAt: now + LOGIN_WINDOW_MS } : current;
    failureBuckets.set(key, bucket);
    if (failureBuckets.size > 5000) {
      for (const [entryKey, value] of failureBuckets) if (value.resetAt <= now) failureBuckets.delete(entryKey);
    }
    return bucket;
  }

  function lockedOut(req, email) {
    const client = resolveClient(req);
    const bucket = bucketFor(`${client.ip}|${String(email || '').toLowerCase()}`);
    return bucket.count >= LOGIN_MAX_FAILURES ? bucket : null;
  }

  function noteFailure(req, email) {
    const client = resolveClient(req);
    const key = `${client.ip}|${String(email || '').toLowerCase()}`;
    const bucket = bucketFor(key);
    bucket.count += 1;
    return bucket;
  }

  function clearFailures(req, email) {
    const client = resolveClient(req);
    failureBuckets.delete(`${client.ip}|${String(email || '').toLowerCase()}`);
  }

  function totpReplayed(secret, code) {
    const normalized = String(code || '').replace(/\D/g, '');
    const counter = Math.floor(Date.now() / 30000);
    for (const window of [-1, 0, 1]) {
      const key = `${secret}:${counter + window}`;
      if (usedTotpCodes.get(key) === normalized) return true;
    }
    return false;
  }

  function rememberTotp(secret, code) {
    const normalized = String(code || '').replace(/\D/g, '');
    const counter = Math.floor(Date.now() / 30000);
    usedTotpCodes.set(`${secret}:${counter}`, normalized);
    const cutoff = counter - 5;
    for (const key of usedTotpCodes.keys()) {
      const value = Number(key.split(':').pop());
      if (Number.isFinite(value) && value < cutoff) usedTotpCodes.delete(key);
    }
  }

  async function logAuthEvent(req, level, action, message, detail = {}) {
    const client = resolveClient(req);
    await recordSystemEvent({
      category: 'auth',
      level,
      action,
      message,
      ip: client.ip,
      ipSource: client.source,
      method: req.method || '',
      path: req.url || '',
      userAgent: String(req.headers['user-agent'] || ''),
      statusCode: detail.statusCode,
      detail
    });
  }

  return async function handleAuth(req, res, pathname) {
    if (pathname === '/site-admin-auth/status' && req.method === 'GET') {
      const users = await readUsers();
      json(res, 200, {
        hasAdmin: users.users.length > 0,
        authenticated: Boolean(verifySession(req)),
        setupRequired: users.users.length === 0,
        setupTokenRequired: users.users.length === 0 && Boolean(setupToken)
      });
      return true;
    }
    if (pathname === '/site-admin-auth/setup/start' && req.method === 'POST') {
      const limited = setupRateLimited(req);
      if (limited) {
        res.setHeader('Retry-After', String(Math.max(1, Math.ceil((limited.resetAt - Date.now()) / 1000))));
        json(res, 429, { error: { message: '初始化尝试过于频繁，请稍后再试。' } });
        return true;
      }
      const body = await bodyJson(req);
      if (!validSetupToken(body)) {
        noteSetupAttempt(req);
        await logAuthEvent(req, 'warning', 'auth.setup.token_rejected', '初始化令牌不正确或缺失', { statusCode: 403 });
        json(res, 403, { error: { message: '初始化令牌不正确。请查看 admin-server 启动日志中的一次性令牌。' } });
        return true;
      }
      const users = await readUsers();
      if (users.users.length) {
        json(res, 409, { error: { message: '管理员账号已存在，请直接登录。' } });
        return true;
      }
      if (pendingSetups.size >= SETUP_PENDING_LIMIT) {
        json(res, 429, { error: { message: '待完成的初始化请求过多，请稍后再试。' } });
        return true;
      }
      const email = String(body.email || '').trim().toLowerCase();
      const password = String(body.password || '');
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        json(res, 400, { error: { message: '请输入有效邮箱。' } });
        return true;
      }
      if (password.length < 12) {
        json(res, 400, { error: { message: '密码至少 12 位。' } });
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
      if (!validSetupToken(body)) {
        noteSetupAttempt(req);
        await logAuthEvent(req, 'warning', 'auth.setup.token_rejected', '初始化令牌不正确或缺失', { statusCode: 403 });
        json(res, 403, { error: { message: '初始化令牌不正确。' } });
        return true;
      }
      // 关键：挑战创建后如果管理员已经存在，绝不能用旧挑战覆盖现有账号。
      const currentUsers = await readUsers();
      if (currentUsers.users.length) {
        pendingSetups.clear();
        json(res, 409, { error: { message: '管理员账号已存在，初始化已关闭。请直接登录。' } });
        return true;
      }
      const challenge = pendingSetups.get(body.challengeId);
      if (!challenge || challenge.exp < Date.now()) {
        json(res, 400, { error: { message: '绑定已过期，请重新开始。' } });
        return true;
      }
      challenge.attempts = (challenge.attempts || 0) + 1;
      if (challenge.attempts > CHALLENGE_MAX_ATTEMPTS) {
        pendingSetups.delete(body.challengeId);
        await logAuthEvent(req, 'danger', 'auth.setup.locked', '2FA 绑定尝试次数过多，已作废本次挑战', { email: challenge.email, attempts: challenge.attempts });
        json(res, 429, { error: { message: '尝试次数过多，请重新开始绑定。' } });
        return true;
      }
      if (totpReplayed(challenge.secret, body.code) || !verifyTotp(challenge.secret, body.code)) {
        await logAuthEvent(req, 'warning', 'auth.setup.failed', '2FA 绑定验证码不正确', { email: challenge.email, attempts: challenge.attempts });
        json(res, 400, { error: { message: '2FA 验证码不正确。' } });
        return true;
      }
      rememberTotp(challenge.secret, body.code);
      const user = { id: crypto.randomUUID(), email: challenge.email, passwordHash: challenge.passwordHash, totpSecret: challenge.secret, createdAt: new Date().toISOString() };
      await saveUsers({ users: [user] });
      pendingSetups.clear();
      await setSessionCookie(res, user, req);
      await auditEvent(req, 'auth.setup.completed', 'admin-user', user.id, { email: user.email }, user);
      json(res, 200, { ok: true });
      return true;
    }
    if (pathname === '/site-admin-auth/login/start' && req.method === 'POST') {
      const body = await bodyJson(req);
      const users = await readUsers();
      const email = String(body.email || '').trim().toLowerCase();
      const locked = lockedOut(req, email);
      if (locked) {
        res.setHeader('Retry-After', String(Math.max(1, Math.ceil((locked.resetAt - Date.now()) / 1000))));
        await logAuthEvent(req, 'danger', 'auth.login.locked', `登录失败次数过多，账号/IP 已被临时锁定：${email}`, { email, failures: locked.count, statusCode: 429 });
        json(res, 429, { error: { message: '登录失败次数过多，请稍后再试。' } });
        return true;
      }
      const user = users.users.find((item) => item.email === email);
      if (!user || !(await bcrypt.compare(String(body.password || ''), user.passwordHash))) {
        const bucket = noteFailure(req, email);
        await logAuthEvent(req, bucket.count >= LOGIN_MAX_FAILURES ? 'danger' : 'warning', 'auth.login.failed', `登录失败：${email || '(空邮箱)'}`, { email, failures: bucket.count, statusCode: 401 });
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
      challenge.attempts = (challenge.attempts || 0) + 1;
      const user = users.users.find((item) => item.id === challenge.userId);
      if (challenge.attempts > CHALLENGE_MAX_ATTEMPTS) {
        pendingLogins.delete(body.challengeId);
        await logAuthEvent(req, 'danger', 'auth.2fa.locked', '2FA 尝试次数过多，已作废本次登录', { userId: challenge.userId, attempts: challenge.attempts });
        json(res, 429, { error: { message: '尝试次数过多，请重新登录。' } });
        return true;
      }
      if (!user || totpReplayed(user.totpSecret, body.code) || !verifyTotp(user.totpSecret, body.code)) {
        await logAuthEvent(req, 'warning', 'auth.2fa.failed', '2FA 验证码不正确', { userId: challenge.userId, attempts: challenge.attempts });
        json(res, 400, { error: { message: '2FA 验证码不正确。' } });
        return true;
      }
      rememberTotp(user.totpSecret, body.code);
      pendingLogins.delete(body.challengeId);
      clearFailures(req, user.email);
      await setSessionCookie(res, user, req);
      await auditEvent(req, 'auth.login.completed', 'admin-user', user.id, { email: user.email }, user);
      json(res, 200, { ok: true });
      return true;
    }
    if (pathname === '/site-admin-auth/password' && req.method === 'POST') {
      const session = verifySession(req);
      if (!session?.userId) {
        json(res, 401, { error: { message: '请先登录。' } });
        return true;
      }
      const body = await bodyJson(req);
      const users = await readUsers();
      const user = users.users.find((item) => item.id === session.userId);
      if (!user || !(await bcrypt.compare(String(body.currentPassword || ''), user.passwordHash))) {
        json(res, 400, { error: { message: '当前密码不正确。' } });
        return true;
      }
      if (totpReplayed(user.totpSecret, body.code) || !verifyTotp(user.totpSecret, body.code)) {
        json(res, 400, { error: { message: '2FA 验证码不正确。' } });
        return true;
      }
      const newPassword = String(body.newPassword || '');
      if (newPassword.length < 12) {
        json(res, 400, { error: { message: '新密码至少 12 位。' } });
        return true;
      }
      rememberTotp(user.totpSecret, body.code);
      const updated = { ...user, passwordHash: await bcrypt.hash(newPassword, 12) };
      await saveUsers({ users: [updated] });
      await setSessionCookie(res, updated, req);
      await auditEvent(req, 'auth.password.changed', 'admin-user', user.id, {}, updated);
      json(res, 200, { ok: true });
      return true;
    }
    if (pathname === '/site-admin-auth/logout' && req.method === 'POST') {
      const session = verifySession(req);
      if (session?.sid && platformStore) await platformStore.revokeAdminSession(session.sid).catch(() => {});
      await auditEvent(req, 'auth.logout', 'admin-user');
      clearSessionCookie(res);
      json(res, 200, { ok: true });
      return true;
    }
    return false;
  };
}
