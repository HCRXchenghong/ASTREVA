import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import bcrypt from 'bcryptjs';
import pg from 'pg';
import QRCode from 'qrcode';

// 用法：node scripts/reset-admin.mjs <新邮箱> <新密码>
// 用途：忘记后台账号密码时，重置管理员邮箱/密码并重新生成 2FA 密钥（含登录自测与二维码）。
const [NEW_EMAIL, NEW_PASSWORD] = process.argv.slice(2);
if (!NEW_EMAIL || !NEW_PASSWORD) {
  console.error('用法：node scripts/reset-admin.mjs <新邮箱> <新密码>');
  process.exit(1);
}
const rootDir = process.cwd();
const envText = fs.readFileSync(path.join(rootDir, '.env'), 'utf8');
const cfg = {};
for (const line of envText.split('\n')) {
  const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/);
  if (m) cfg[m[1]] = m[2].replace(/^["']|["']$/g, '');
}

const DATABASE_URL = cfg.DATABASE_URL;
const KEY = cfg.CMS_DATA_ENCRYPTION_KEY;
if (!DATABASE_URL || !KEY) throw new Error('missing DATABASE_URL or CMS_DATA_ENCRYPTION_KEY in .env');

const key = crypto.createHash('sha256').update(KEY).digest();
function encrypt(value) {
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', key, iv);
  const ciphertext = Buffer.concat([cipher.update(String(value), 'utf8'), cipher.final()]);
  return `v1:${iv.toString('base64url')}:${cipher.getAuthTag().toString('base64url')}:${ciphertext.toString('base64url')}`;
}

const b32 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
function base32Encode(buffer) {
  let bits = 0, value = 0, out = '';
  for (const byte of buffer) {
    value = (value << 8) | byte; bits += 8;
    while (bits >= 5) { out += b32[(value >>> (bits - 5)) & 31]; bits -= 5; }
  }
  if (bits > 0) out += b32[(value << (5 - bits)) & 31];
  return out;
}
function base32Decode(secret) {
  const clean = String(secret).toUpperCase().replace(/=+$/g, '').replace(/\s+/g, '');
  let bits = 0, value = 0; const bytes = [];
  for (const ch of clean) {
    const idx = b32.indexOf(ch);
    if (idx === -1) throw new Error('bad secret');
    value = (value << 5) | idx; bits += 5;
    if (bits >= 8) { bytes.push((value >>> (bits - 8)) & 255); bits -= 8; }
  }
  return Buffer.from(bytes);
}
function generateTotp(secret, counter) {
  const k = base32Decode(secret);
  const buffer = Buffer.alloc(8);
  buffer.writeUInt32BE(Math.floor(counter / 0x100000000), 0);
  buffer.writeUInt32BE(counter & 0xffffffff, 4);
  const hmac = crypto.createHmac('sha1', k).update(buffer).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const binary = ((hmac[offset] & 0x7f) << 24) | ((hmac[offset + 1] & 0xff) << 16) | ((hmac[offset + 2] & 0xff) << 8) | (hmac[offset + 3] & 0xff);
  return String(binary % 1000000).padStart(6, '0');
}

const secret = base32Encode(crypto.randomBytes(20));
const passwordHash = await bcrypt.hash(NEW_PASSWORD, 12);
const encryptedSecret = encrypt(secret);

const client = new pg.Client({ connectionString: DATABASE_URL });
await client.connect();
const res = await client.query(
  "UPDATE site_admin_users SET email = $1, password_hash = $2, totp_secret = $3, updated_at = now() " +
  "WHERE id = (SELECT id FROM site_admin_users ORDER BY created_at ASC LIMIT 1) " +
  "RETURNING id, email",
  [NEW_EMAIL, passwordHash, encryptedSecret]
);
if (!res.rows.length) throw new Error('no admin user row found');
// 重置密码/2FA 后必须撤销所有已签发会话；否则旧 Cookie 仍可继续操作。
await client.query('UPDATE site_admin_sessions SET revoked_at = now() WHERE revoked_at IS NULL');
await client.end();

const base = 'http://127.0.0.1:1337';
const start = await (await fetch(base + '/site-admin-auth/login/start', {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email: NEW_EMAIL, password: NEW_PASSWORD })
})).json();
const code = generateTotp(secret, Math.floor(Date.now() / 30000));
const verify = await (await fetch(base + '/site-admin-auth/login/verify', {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ challengeId: start.challengeId, code })
})).json();

const otpauth = `otpauth://totp/${encodeURIComponent('星渡官网账号:' + NEW_EMAIL)}?secret=${secret}&issuer=${encodeURIComponent('星渡官网账号')}&algorithm=SHA1&digits=6&period=30`;
const qrPath = path.join(rootDir, '.local-logs', 'admin-2fa-reset.png');
await QRCode.toFile(qrPath, otpauth, { margin: 1, width: 320 });

console.log(JSON.stringify({
  updated: res.rows[0],
  email: NEW_EMAIL,
  password: NEW_PASSWORD,
  loginStartStatus: start.challengeId ? 'ok' : start,
  loginVerifyStatus: verify,
  secret,
  otpauth,
  qrPath
}, null, 2));
