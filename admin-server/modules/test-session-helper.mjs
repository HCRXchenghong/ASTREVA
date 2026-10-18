import crypto from 'node:crypto';

const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';

function base32Decode(input) {
  const clean = String(input || '').replace(/=+$/, '').replace(/\s+/g, '').toUpperCase();
  let bits = 0;
  let value = 0;
  const output = [];
  for (const char of clean) {
    const index = BASE32_ALPHABET.indexOf(char);
    if (index === -1) continue;
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      output.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(output);
}

/** RFC 6238 TOTP（SHA-1 / 6 位 / 30 秒），用于测试里走真实的 2FA 流程。 */
export function totpCode(base32Secret, atMs = Date.now()) {
  const key = base32Decode(base32Secret);
  const counter = Math.floor(atMs / 30000);
  const buffer = Buffer.alloc(8);
  buffer.writeUInt32BE(Math.floor(counter / 2 ** 32), 0);
  buffer.writeUInt32BE(counter >>> 0, 4);
  const digest = crypto.createHmac('sha1', key).update(buffer).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const binary =
    ((digest[offset] & 0x7f) << 24) |
    ((digest[offset + 1] & 0xff) << 16) |
    ((digest[offset + 2] & 0xff) << 8) |
    (digest[offset + 3] & 0xff);
  return String(binary % 1000000).padStart(6, '0');
}

function cookieValueFromResponse(response) {
  const raw = response.headers.get('set-cookie') || '';
  const pair = raw.split(';')[0] || '';
  return pair.includes('=') ? pair.slice(pair.indexOf('=') + 1) : '';
}

/**
 * 通过真实的「创建管理员 + 绑定 2FA」或「登录 + 2FA」流程拿到可用的会话 Cookie。
 * 会话现在带 sid 并在服务端校验，测试不能再伪造 Cookie。
 */
export async function createAdminSession(base, options = {}) {
  const email = options.email || `admin-${crypto.randomUUID().slice(0, 8)}@test.local`;
  const password = options.password || 'Test-Password-123';

  const setupStart = await fetch(`${base}/site-admin-auth/setup/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password })
  });
  if (setupStart.ok) {
    const challenge = await setupStart.json();
    const verify = await fetch(`${base}/site-admin-auth/setup/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ challengeId: challenge.challengeId, code: totpCode(challenge.secret) })
    });
    if (!verify.ok) throw new Error(`2FA 绑定失败：${verify.status} ${await verify.text()}`);
    const cookieValue = cookieValueFromResponse(verify);
    if (!cookieValue) throw new Error('2FA 绑定成功但没有返回会话 Cookie');
    return { cookieValue, cookieHeader: `astreva_admin_session=${cookieValue}`, email, password, secret: challenge.secret };
  }

  // 已存在管理员：走登录流程（需要调用方提供 secret）
  if (!options.secret) {
    throw new Error(`创建管理员失败（${setupStart.status}），且未提供用于登录的 2FA secret`);
  }
  const loginStart = await fetch(`${base}/site-admin-auth/login/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password })
  });
  if (!loginStart.ok) throw new Error(`登录失败：${loginStart.status} ${await loginStart.text()}`);
  const challenge = await loginStart.json();
  const verify = await fetch(`${base}/site-admin-auth/login/verify`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ challengeId: challenge.challengeId, code: totpCode(options.secret) })
  });
  if (!verify.ok) throw new Error(`2FA 验证失败：${verify.status} ${await verify.text()}`);
  const cookieValue = cookieValueFromResponse(verify);
  return { cookieValue, cookieHeader: `astreva_admin_session=${cookieValue}`, email, password, secret: options.secret };
}

/** Playwright：把上面拿到的会话写进浏览器上下文。 */
export async function applySessionCookie(context, base, session) {
  await context.addCookies([
    { name: 'astreva_admin_session', value: session.cookieValue, url: base, httpOnly: true, sameSite: 'Lax' }
  ]);
}
