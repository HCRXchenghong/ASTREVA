import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import sharp from 'sharp';
import { createPlatformStore } from './platform-store.mjs';
import { createMediaStore, mediaObjectKeys } from './media-store.mjs';

async function loadEnvironment() {
  const text = await fs.readFile(path.join(process.cwd(), '.env'), 'utf8');
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    const key = trimmed.slice(0, index).trim();
    if (!process.env[key]) process.env[key] = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
  }
}

test('MinIO/R2 真实端到端：变体上传、搜索、稳定替换、预签名 PUT、HeadObject 确认与全量删除', { skip: process.env.RUN_MEDIA_S3_E2E !== 'true', timeout: 60_000 }, async () => {
  await loadEnvironment();
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-s3-media-'));
  const platformStore = createPlatformStore({
    driver: 'postgres', databaseUrl: process.env.DATABASE_URL, encryptionKey: process.env.CMS_DATA_ENCRYPTION_KEY,
    files: {}, logger: { info() {}, error() {} }
  });
  await platformStore.init();
  const store = createMediaStore({
    driver: 's3', uploadDir: root, publicOrigin: 'http://127.0.0.1:1337', platformStore,
    directUploadSecret: 'integration-direct-upload-secret', imageWidths: '200,400', imageQuality: 80,
    s3: {
      endpoint: process.env.S3_ENDPOINT, region: process.env.S3_REGION, bucket: process.env.S3_BUCKET,
      accessKeyId: process.env.S3_ACCESS_KEY_ID, secretAccessKey: process.env.S3_SECRET_ACCESS_KEY,
      forcePathStyle: process.env.S3_FORCE_PATH_STYLE, prefix: process.env.ADMIN_S3_KEY_PREFIX,
      publicBaseUrl: process.env.S3_PUBLIC_BASE_URL
    }
  });
  const category = `E2E-${Date.now()}`;
  const createdIds = [];
  try {
    await platformStore.createMediaCategory(category);
    const first = path.join(root, 'first.png');
    const second = path.join(root, 'second.png');
    await sharp({ create: { width: 640, height: 360, channels: 4, background: '#1677df' } }).png().toFile(first);
    await sharp({ create: { width: 360, height: 640, channels: 4, background: '#ef7a34' } }).png().toFile(second);
    const media = await store.uploadSmartImage({ temporaryPath: first, originalName: 'e2e-smart.png', category, preserveOriginal: true });
    createdIds.push(media.id);
    assert.equal(media.variants.webp.length, 2);
    for (const url of [media.url, ...media.variants.webp.map((item) => item.url), media.variants.original.url]) assert.equal((await fetch(url)).status, 200, url);
    assert.equal((await platformStore.searchMedia({ q: 'e2e-smart', category, type: 'image' })).total, 1);

    const replaced = await store.replace(media.id, { temporaryPath: second, originalName: 'e2e-replaced.png', mode: 'smart', category, preserveOriginal: true });
    assert.equal(replaced.url, media.url);
    assert.equal(replaced.width, 360);
    assert.equal((await fetch(replaced.url)).status, 200);

    const pdf = Buffer.from('%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n');
    const authorization = await store.createPresignedUpload({ name: 'e2e-document.pdf', mime: 'application/pdf', size: pdf.length, category });
    const put = await fetch(authorization.uploadUrl, { method: 'PUT', headers: authorization.headers, body: pdf });
    assert.equal(put.ok, true);
    const direct = await store.confirmPresignedUpload(authorization.token);
    createdIds.push(direct.id);
    assert.equal(direct.sourceType, 'direct');
    assert.equal((await fetch(direct.url)).status, 200);

    const forgedAuthorization = await store.createPresignedUpload({ name: 'forged.png', mime: 'image/png', size: pdf.length, category });
    const forgedPut = await fetch(forgedAuthorization.uploadUrl, { method: 'PUT', headers: forgedAuthorization.headers, body: pdf });
    assert.equal(forgedPut.ok, true);
    await assert.rejects(() => store.confirmPresignedUpload(forgedAuthorization.token), /魔数/);
    const forgedPublicUrl = `${process.env.S3_PUBLIC_BASE_URL.replace(/\/$/, '')}/${forgedAuthorization.key.split('/').map(encodeURIComponent).join('/')}`;
    assert.equal((await fetch(forgedPublicUrl)).status, 404);

    const stats = await platformStore.mediaStats();
    assert.ok(stats.total >= 2);
    assert.equal((await platformStore.listMediaCategories()).find((item) => item.name === category).count, 2);

    const smartKeys = mediaObjectKeys(await platformStore.getMedia(media.id));
    await store.remove(media.id); createdIds.shift();
    await store.remove(direct.id); createdIds.shift();
    assert.equal((await fetch(replaced.url)).status, 404);
    assert.ok(smartKeys.length >= 3);
    const categoryDelete = await platformStore.deleteMediaCategory(category);
    assert.equal(categoryDelete.reassigned, 0);
  } finally {
    for (const id of createdIds) await store.remove(id).catch(() => {});
    await platformStore.deleteMediaCategory(category).catch(() => {});
    await platformStore.close();
  }
});
