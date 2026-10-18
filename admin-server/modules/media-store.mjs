import {
  DeleteObjectCommand,
  GetObjectCommand,
  HeadBucketCommand,
  HeadObjectCommand,
  PutObjectCommand,
  S3Client
} from '@aws-sdk/client-s3';
import { getSignedUrl } from '@aws-sdk/s3-request-presigner';
import { createReadStream, existsSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import sharp from 'sharp';

const DEFAULT_CATEGORY = '未分类';
const DIRECT_MIME_TYPES = new Set([
  'image/jpeg', 'image/png', 'image/webp', 'image/gif', 'image/avif',
  'video/mp4', 'video/webm', 'video/quicktime', 'application/pdf', 'application/zip',
  'application/acad', 'application/dxf', 'image/vnd.dwg'
]);

function cleanPrefix(value = '') {
  return String(value).trim().replace(/^\/+|\/+$/g, '');
}

function joinPublicUrl(base, key) {
  return `${String(base).replace(/\/$/, '')}/${key.split('/').map(encodeURIComponent).join('/')}`;
}

function normalizeCategory(value) {
  return String(value || DEFAULT_CATEGORY).trim().replace(/\s+/g, ' ').slice(0, 80) || DEFAULT_CATEGORY;
}

function safeName(value, fallback = 'asset') {
  const extension = path.extname(String(value || '')).toLowerCase().replace(/[^.a-z0-9]/g, '').slice(0, 10);
  const base = path.basename(String(value || ''), path.extname(String(value || '')))
    .toLowerCase().replace(/[^a-z0-9_-]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 70) || fallback;
  return `${base}${extension}`;
}

function mimeExtension(mime) {
  return ({
    'image/jpeg': '.jpg', 'image/png': '.png', 'image/webp': '.webp', 'image/gif': '.gif', 'image/avif': '.avif',
    'video/mp4': '.mp4', 'video/webm': '.webm', 'video/quicktime': '.mov', 'application/pdf': '.pdf',
    'application/zip': '.zip', 'application/acad': '.dwg', 'image/vnd.dwg': '.dwg', 'application/dxf': '.dxf'
  })[mime] || '';
}

function mediaTypeFromMime(mime) {
  if (mime.startsWith('image/')) return 'image';
  if (mime.startsWith('video/')) return 'video';
  return 'file';
}

function sha256Buffer(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex');
}

async function sha256File(filename) {
  const hash = crypto.createHash('sha256');
  for await (const chunk of createReadStream(filename)) hash.update(chunk);
  return hash.digest('hex');
}

function looksLikeSvg(buffer) {
  const text = buffer.subarray(0, 4096).toString('utf8').replace(/^\uFEFF/, '').trimStart().toLowerCase();
  return text.startsWith('<svg') || text.startsWith('<?xml') && text.includes('<svg');
}

function detectMagic(buffer) {
  if (!Buffer.isBuffer(buffer) || buffer.length < 4) return null;
  if (looksLikeSvg(buffer)) throw new Error('为避免脚本注入，媒体库不接受 SVG，请转为 PNG 或 WebP。');
  if (buffer[0] === 0xff && buffer[1] === 0xd8 && buffer[2] === 0xff) return 'image/jpeg';
  if (buffer.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return 'image/png';
  if (buffer.subarray(0, 4).toString('ascii') === 'RIFF' && buffer.subarray(8, 12).toString('ascii') === 'WEBP') return 'image/webp';
  if (['GIF87a', 'GIF89a'].includes(buffer.subarray(0, 6).toString('ascii'))) return 'image/gif';
  if (buffer.subarray(4, 12).toString('ascii').includes('ftypavif') || buffer.subarray(4, 12).toString('ascii').includes('ftypavis')) return 'image/avif';
  if (buffer[0] === 0x1a && buffer[1] === 0x45 && buffer[2] === 0xdf && buffer[3] === 0xa3) return 'video/webm';
  if (buffer.subarray(4, 8).toString('ascii') === 'ftyp') return buffer.subarray(8, 12).toString('ascii') === 'qt  ' ? 'video/quicktime' : 'video/mp4';
  if (buffer.subarray(0, 5).toString('ascii') === '%PDF-') return 'application/pdf';
  if (buffer[0] === 0x50 && buffer[1] === 0x4b && [0x03, 0x05, 0x07].includes(buffer[2])) return 'application/zip';
  if (/^AC10\d{2}/.test(buffer.subarray(0, 8).toString('ascii'))) return 'application/acad';
  const text = buffer.subarray(0, 4096).toString('utf8').toUpperCase();
  if (text.includes('SECTION') && (text.includes('HEADER') || text.includes('ENTITIES'))) return 'application/dxf';
  return null;
}

async function readPrefix(filename, length = 128 * 1024) {
  const handle = await fs.open(filename, 'r');
  try {
    const buffer = Buffer.alloc(length);
    const { bytesRead } = await handle.read(buffer, 0, length, 0);
    return buffer.subarray(0, bytesRead);
  } finally {
    await handle.close();
  }
}

async function detectFile(filename, allowedTypes = DIRECT_MIME_TYPES) {
  const prefix = await readPrefix(filename);
  const mime = detectMagic(prefix);
  if (!mime || !DIRECT_MIME_TYPES.has(mime) || !allowedTypes.has(mime)) throw new Error('文件内容与后台允许的图片、视频或文档格式不匹配。');
  let width = null;
  let height = null;
  if (mime.startsWith('image/')) {
    const info = await sharp(filename, { limitInputPixels: Number(process.env.ADMIN_IMAGE_MAX_PIXELS || 80_000_000) }).metadata();
    width = info.autoOrient?.width || info.width || null;
    height = info.autoOrient?.height || info.height || null;
  }
  return { mime, type: mediaTypeFromMime(mime), width, height };
}

function variantItems(record) {
  const webp = Array.isArray(record?.variants?.webp) ? record.variants.webp : [];
  return webp.filter((item) => item?.key);
}

export function mediaObjectKeys(record) {
  return [...new Set([record?.key, record?.originalKey, ...variantItems(record).map((item) => item.key)].filter(Boolean))];
}

export function mediaReferenceUrls(record) {
  const originalUrl = record?.variants?.original?.url;
  return [...new Set([record?.url, originalUrl, ...variantItems(record).map((item) => item.url)].filter(Boolean))];
}

/** S3/R2-compatible media driver. Local mode is retained only for isolated tests and legacy cutover. */
export function createMediaStore({
  driver = 'local', uploadDir, publicOrigin, platformStore, s3 = {},
  directUploadSecret = '', imageWidths, imageQuality, maxUploadBytes, allowedMimeTypes
}) {
  const local = driver === 'local';
  const prefix = cleanPrefix(s3.prefix || 'site-admin');
  const widths = [...new Set(String(imageWidths || process.env.ADMIN_IMAGE_VARIANT_WIDTHS || '400,800,1200,1600')
    .split(',').map((value) => Number.parseInt(value, 10)).filter((value) => value >= 100 && value <= 6000))].sort((a, b) => a - b);
  const webpQuality = Math.max(40, Math.min(Number(imageQuality || process.env.ADMIN_IMAGE_WEBP_QUALITY || 82), 100));
  const uploadLimit = Math.max(1, Number(maxUploadBytes || process.env.ADMIN_UPLOAD_MAX_BYTES || 200 * 1024 * 1024));
  const allowedTypes = new Set(String(allowedMimeTypes || [...DIRECT_MIME_TYPES].join(','))
    .split(',').map((value) => value.trim().toLowerCase()).filter((value) => DIRECT_MIME_TYPES.has(value)));
  const client = local ? null : new S3Client({
    region: s3.region || 'us-east-1', endpoint: s3.endpoint || undefined,
    forcePathStyle: s3.forcePathStyle === true || s3.forcePathStyle === 'true',
    credentials: s3.accessKeyId
      ? { accessKeyId: s3.accessKeyId, secretAccessKey: s3.secretAccessKey || '', sessionToken: s3.sessionToken || undefined }
      : undefined
  });

  function assertReady() {
    if (local) return;
    if (!s3.bucket || !s3.publicBaseUrl) throw new Error('ADMIN_MEDIA_DRIVER=s3 时必须配置 S3_BUCKET 和 S3_PUBLIC_BASE_URL。');
  }

  function objectKey(...parts) {
    const relative = parts.map(cleanPrefix).filter(Boolean).join('/');
    return local ? `uploads/site-admin/${relative}` : [prefix, relative].filter(Boolean).join('/');
  }

  function localPath(key) {
    return path.join(uploadDir, ...String(key).replace(/^uploads\/site-admin\//, '').split('/'));
  }

  function publicUrl(key) {
    if (local) return `${String(publicOrigin).replace(/\/$/, '')}/${key.split('/').map(encodeURIComponent).join('/')}`;
    return joinPublicUrl(s3.publicBaseUrl, key);
  }

  async function putFile(key, filename, mime) {
    if (local) {
      const target = localPath(key);
      await fs.mkdir(path.dirname(target), { recursive: true });
      await fs.copyFile(filename, target);
      return;
    }
    assertReady();
    await client.send(new PutObjectCommand({ Bucket: s3.bucket, Key: key, Body: createReadStream(filename), ContentType: mime, ServerSideEncryption: s3.serverSideEncryption || undefined }));
  }

  async function putBuffer(key, buffer, mime) {
    if (local) {
      const target = localPath(key);
      await fs.mkdir(path.dirname(target), { recursive: true });
      await fs.writeFile(target, buffer);
      return;
    }
    assertReady();
    await client.send(new PutObjectCommand({ Bucket: s3.bucket, Key: key, Body: buffer, ContentType: mime, ServerSideEncryption: s3.serverSideEncryption || undefined }));
  }

  async function deleteObject(key) {
    if (!key) return;
    if (local) {
      await fs.unlink(localPath(key)).catch((error) => { if (error.code !== 'ENOENT') throw error; });
      return;
    }
    assertReady();
    await client.send(new DeleteObjectCommand({ Bucket: s3.bucket, Key: key }));
  }

  async function deleteObjects(keys) {
    const failures = [];
    for (const key of [...new Set(keys.filter(Boolean))]) {
      try { await deleteObject(key); } catch (error) { failures.push({ key, error }); }
    }
    if (failures.length) throw new Error(`删除对象存储文件失败：${failures.map((item) => item.key).join('、')}`);
  }

  async function getStoredObject(key, range = '') {
    if (local) {
      try {
        const buffer = await fs.readFile(localPath(key));
        return { buffer: range ? buffer.subarray(0, 128 * 1024) : buffer, contentType: '' };
      } catch (error) {
        if (error.code === 'ENOENT') return null;
        throw error;
      }
    }
    try {
      const result = await client.send(new GetObjectCommand({ Bucket: s3.bucket, Key: key, Range: range || undefined }));
      return { buffer: Buffer.from(await result.Body.transformToByteArray()), contentType: result.ContentType || '' };
    } catch (error) {
      if (['NoSuchKey', 'NotFound'].includes(error.name) || error.$metadata?.httpStatusCode === 404) return null;
      throw error;
    }
  }

  async function listLocalMedia() {
    await fs.mkdir(uploadDir, { recursive: true });
    const entries = await fs.readdir(uploadDir, { withFileTypes: true }).catch(() => []);
    return Promise.all(entries.filter((entry) => entry.isFile()).map(async (entry) => {
      const absolute = path.join(uploadDir, entry.name);
      const stats = await fs.stat(absolute);
      return {
        id: `local_${entry.name}`, name: entry.name, key: `uploads/site-admin/${entry.name}`,
        url: publicUrl(`uploads/site-admin/${entry.name}`), path: `/uploads/site-admin/${entry.name}`,
        mime: '', type: 'file', size: stats.size, storageDriver: 'local', category: DEFAULT_CATEGORY,
        sourceType: 'migration', originalKey: '', width: null, height: null, checksumSha256: '', variants: {}, metadata: {},
        createdAt: stats.birthtime.toISOString(), updatedAt: stats.mtime.toISOString()
      };
    }));
  }

  async function list() {
    const fromDatabase = await platformStore.listMedia();
    if (fromDatabase.length || platformStore.driver === 'postgres') return fromDatabase;
    return (await listLocalMedia()).sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
  }

  async function buildSmartImage({ source, id, originalName, category, preserveOriginal = true, sourceType = 'upload', metadata = {}, fixedKey = '' }) {
    const sourcePrefix = Buffer.isBuffer(source) ? source.subarray(0, 128 * 1024) : await readPrefix(source);
    const magicMime = detectMagic(sourcePrefix);
    if (!magicMime?.startsWith('image/')) throw new Error('智能上传的文件内容不是可支持的位图。');
    if (!allowedTypes.has(magicMime)) throw new Error(`后台上传限制不允许 ${magicMime}。`);
    const inputMetadata = await sharp(source, { limitInputPixels: Number(process.env.ADMIN_IMAGE_MAX_PIXELS || 80_000_000), animated: false }).metadata();
    if (!['jpeg', 'png', 'webp', 'gif', 'avif'].includes(inputMetadata.format)) throw new Error('智能上传仅支持 JPEG、PNG、WebP、GIF 和 AVIF 位图。');
    const mainResult = await sharp(source, { limitInputPixels: Number(process.env.ADMIN_IMAGE_MAX_PIXELS || 80_000_000), animated: false })
      .rotate().webp({ quality: webpQuality }).toBuffer({ resolveWithObject: true });
    const directory = fixedKey ? path.posix.dirname(fixedKey) : objectKey('images', id);
    const key = fixedKey || `${directory}/main.webp`;
    const objects = [{ key, buffer: mainResult.data, mime: 'image/webp' }];
    const variantList = [];
    for (const width of widths.filter((value) => value < mainResult.info.width)) {
      const output = await sharp(source, { limitInputPixels: Number(process.env.ADMIN_IMAGE_MAX_PIXELS || 80_000_000), animated: false })
        .rotate().resize({ width, withoutEnlargement: true }).webp({ quality: webpQuality }).toBuffer({ resolveWithObject: true });
      const variantKey = `${directory}/w${width}.webp`;
      objects.push({ key: variantKey, buffer: output.data, mime: 'image/webp' });
      variantList.push({ width: output.info.width, height: output.info.height, key: variantKey, url: publicUrl(variantKey), size: output.data.length, mime: 'image/webp' });
    }
    let original = null;
    if (preserveOriginal) {
      const sourceBuffer = Buffer.isBuffer(source) ? source : await fs.readFile(source);
      const detectedMime = magicMime;
      const originalKey = `${directory}/original${mimeExtension(detectedMime || `image/${inputMetadata.format}`)}`;
      objects.push({ key: originalKey, buffer: sourceBuffer, mime: detectedMime || `image/${inputMetadata.format}` });
      original = { key: originalKey, url: publicUrl(originalKey), size: sourceBuffer.length, mime: detectedMime || `image/${inputMetadata.format}` };
    }
    const sourceChecksum = Buffer.isBuffer(source) ? sha256Buffer(source) : await sha256File(source);
    const now = new Date().toISOString();
    return {
      objects,
      record: {
        id, name: String(originalName || 'image').slice(0, 255), key, url: publicUrl(key), mime: 'image/webp', type: 'image',
        size: mainResult.data.length, storageDriver: local ? 'local' : 's3', category: normalizeCategory(category), sourceType,
        originalKey: original?.key || '', width: mainResult.info.width, height: mainResult.info.height,
        checksumSha256: sourceChecksum, variants: { webp: variantList, ...(original ? { original } : {}) },
        metadata: { ...metadata, originalFormat: inputMetadata.format, webpQuality, configuredWidths: widths }, createdAt: now, updatedAt: now
      }
    };
  }

  async function persistObjectsAndRecord(objects, record) {
    const written = [];
    try {
      for (const item of objects) {
        if (item.filename) await putFile(item.key, item.filename, item.mime);
        else await putBuffer(item.key, item.buffer, item.mime);
        written.push(item.key);
      }
      await platformStore.saveMedia(record);
      return record;
    } catch (error) {
      await deleteObjects(written).catch(() => {});
      throw error;
    }
  }

  async function uploadSmartImage({ temporaryPath, buffer, originalName, category, preserveOriginal = true, sourceType = 'upload', metadata = {} }) {
    const source = buffer || temporaryPath;
    if (!source) throw new Error('缺少要上传的图片。');
    const sourceSize = Buffer.isBuffer(source) ? source.length : (await fs.stat(source)).size;
    if (sourceSize > uploadLimit) throw new Error(`文件不能超过 ${Math.ceil(uploadLimit / 1024 / 1024)} MB。`);
    const id = crypto.randomUUID();
    const built = await buildSmartImage({ source, id, originalName, category, preserveOriginal, sourceType, metadata });
    return persistObjectsAndRecord(built.objects, built.record);
  }

  async function uploadOriginal({ temporaryPath, originalName, category, sourceType = 'upload', metadata = {} }) {
    const stats = await fs.stat(temporaryPath);
    if (stats.size > uploadLimit) throw new Error(`文件不能超过 ${Math.ceil(uploadLimit / 1024 / 1024)} MB。`);
    const detected = await detectFile(temporaryPath, allowedTypes);
    const id = crypto.randomUUID();
    const filename = safeName(originalName, 'asset').replace(/\.[^.]+$/, '') + mimeExtension(detected.mime);
    const key = objectKey('originals', id, filename);
    const now = new Date().toISOString();
    const record = {
      id, name: String(originalName || filename).slice(0, 255), key, url: publicUrl(key), mime: detected.mime, type: detected.type,
      size: stats.size, storageDriver: local ? 'local' : 's3', category: normalizeCategory(category), sourceType,
      originalKey: key, width: detected.width, height: detected.height, checksumSha256: await sha256File(temporaryPath), variants: {},
      metadata: { ...metadata, originalUpload: true }, createdAt: now, updatedAt: now
    };
    return persistObjectsAndRecord([{ key, filename: temporaryPath, mime: detected.mime }], record);
  }

  async function upload(options) {
    const extension = path.extname(String(options.originalName || options.filename || '')).toLowerCase();
    const likelyImage = ['.jpg', '.jpeg', '.png', '.webp', '.gif', '.avif', '.svg'].includes(extension);
    if (options.mode === 'smart' || options.mode !== 'original' && likelyImage) return uploadSmartImage(options);
    return uploadOriginal(options);
  }

  async function replace(id, { temporaryPath, originalName, mode = 'smart', preserveOriginal = true, category }) {
    const current = await platformStore.getMedia(id);
    if (!current) throw new Error('媒体文件不存在或已被删除。');
    let built;
    if (mode === 'smart') {
      built = await buildSmartImage({ source: temporaryPath, id: current.id, originalName: originalName || current.name,
        category: category || current.category, preserveOriginal, sourceType: 'replacement',
        metadata: { ...current.metadata, replacedAt: new Date().toISOString() }, fixedKey: current.key });
    } else {
      const detected = await detectFile(temporaryPath, allowedTypes);
      const stats = await fs.stat(temporaryPath);
      const buffer = await fs.readFile(temporaryPath);
      const now = new Date().toISOString();
      built = { objects: [{ key: current.key, buffer, mime: detected.mime }], record: {
        ...current, name: String(originalName || current.name).slice(0, 255), mime: detected.mime, type: detected.type,
        size: stats.size, category: normalizeCategory(category || current.category), sourceType: 'replacement', originalKey: current.key,
        width: detected.width, height: detected.height, checksumSha256: sha256Buffer(buffer), variants: {},
        metadata: { ...current.metadata, originalUpload: true, replacedAt: now }, updatedAt: now
      } };
    }
    const oldKeys = mediaObjectKeys(current);
    const newKeys = built.objects.map((item) => item.key);
    const backups = new Map();
    for (const key of oldKeys) {
      const stored = await getStoredObject(key);
      if (stored) backups.set(key, stored);
    }
    try {
      for (const item of built.objects) await putBuffer(item.key, item.buffer, item.mime);
      await platformStore.saveMedia(built.record);
      // The new database record is already durable here. Old, no-longer-referenced variants are best-effort cleanup;
      // a transient cleanup failure must not roll the stable URL back while leaving the database on the new version.
      await deleteObjects(oldKeys.filter((key) => !newKeys.includes(key))).catch(() => {});
      return built.record;
    } catch (error) {
      for (const key of newKeys.filter((item) => !backups.has(item))) await deleteObject(key).catch(() => {});
      for (const [key, backup] of backups) await putBuffer(key, backup.buffer, backup.contentType || current.mime || 'application/octet-stream').catch(() => {});
      throw error;
    }
  }

  async function remove(id) {
    const record = await platformStore.getMedia(id) || (await list()).find((item) => item.id === id);
    if (!record) throw new Error('媒体文件不存在或已被删除。');
    await deleteObjects(mediaObjectKeys(record));
    await platformStore.deleteMedia(record.id);
    return record;
  }

  function signDirectPayload(payload) {
    const encoded = Buffer.from(JSON.stringify(payload)).toString('base64url');
    const signature = crypto.createHmac('sha256', directUploadSecret).update(encoded).digest('base64url');
    return `${encoded}.${signature}`;
  }

  function verifyDirectToken(token) {
    if (!directUploadSecret) throw new Error('直传签名密钥未配置。');
    const [encoded, signature] = String(token || '').split('.');
    if (!encoded || !signature) throw new Error('直传凭证无效。');
    const expected = crypto.createHmac('sha256', directUploadSecret).update(encoded).digest('base64url');
    if (signature.length !== expected.length || !crypto.timingSafeEqual(Buffer.from(signature), Buffer.from(expected))) throw new Error('直传凭证无效。');
    const payload = JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8'));
    if (payload.exp < Date.now()) throw new Error('直传凭证已过期，请重新申请。');
    return payload;
  }

  async function createPresignedUpload({ name, mime, size, category }) {
    if (local) throw new Error('预签名直传仅在 S3/R2 存储模式下可用。');
    if (!directUploadSecret) throw new Error('直传签名密钥未配置。');
    assertReady();
    const contentType = String(mime || '').toLowerCase();
    const bytes = Number(size);
    if (!DIRECT_MIME_TYPES.has(contentType) || !allowedTypes.has(contentType) || contentType === 'image/svg+xml') throw new Error('该 Content-Type 不允许直传。');
    if (!Number.isSafeInteger(bytes) || bytes <= 0 || bytes > uploadLimit) throw new Error(`直传文件必须在 1 字节到 ${Math.ceil(uploadLimit / 1024 / 1024)} MB 之间。`);
    const id = crypto.randomUUID();
    const filename = safeName(name, 'asset').replace(/\.[^.]+$/, '') + mimeExtension(contentType);
    const key = objectKey('direct', id, filename);
    const exp = Date.now() + 10 * 60 * 1000;
    const payload = { id, key, name: String(name || filename).slice(0, 255), mime: contentType, size: bytes, category: normalizeCategory(category), exp };
    const uploadUrl = await getSignedUrl(client, new PutObjectCommand({ Bucket: s3.bucket, Key: key, ContentType: contentType }), { expiresIn: 600 });
    return { uploadUrl, method: 'PUT', headers: { 'Content-Type': contentType }, expiresAt: new Date(exp).toISOString(), token: signDirectPayload(payload), key };
  }

  async function confirmPresignedUpload(token) {
    const payload = verifyDirectToken(token);
    try {
      const head = await client.send(new HeadObjectCommand({ Bucket: s3.bucket, Key: payload.key }));
      if (Number(head.ContentLength) !== payload.size) throw new Error('直传对象大小与申请值不一致。');
      if (String(head.ContentType || '').split(';')[0].toLowerCase() !== payload.mime) throw new Error('直传对象的 Content-Type 与申请值不一致。');
      const sample = await getStoredObject(payload.key, 'bytes=0-131071');
      const detected = detectMagic(sample?.buffer || Buffer.alloc(0));
      const mimeMatches = detected === payload.mime || detected === 'application/acad' && payload.mime === 'image/vnd.dwg';
      if (!mimeMatches) throw new Error('直传对象的魔数与声明格式不一致。');
      const now = new Date().toISOString();
      const record = { id: payload.id, name: payload.name, key: payload.key, url: publicUrl(payload.key), mime: payload.mime,
        type: mediaTypeFromMime(payload.mime), size: payload.size, storageDriver: 's3', category: payload.category,
        sourceType: 'direct', originalKey: payload.key, width: null, height: null, checksumSha256: '', variants: {},
        metadata: { directUpload: true }, createdAt: now, updatedAt: now };
      await platformStore.saveMedia(record);
      return record;
    } catch (error) {
      await deleteObject(payload.key).catch(() => {});
      throw error;
    }
  }

  async function health() {
    if (local) return { driver: 'local', ok: true, bucket: '', endpoint: '' };
    try {
      assertReady();
      await client.send(new HeadBucketCommand({ Bucket: s3.bucket }));
      return { driver: 's3', ok: true, bucket: s3.bucket, endpoint: s3.endpoint || 'AWS/R2 default endpoint', prefix };
    } catch (error) {
      return { driver: 's3', ok: false, bucket: s3.bucket || '', endpoint: s3.endpoint || '', prefix, message: error.message };
    }
  }

  async function migrateLocalMedia() {
    if (local) throw new Error('当前已使用本地媒体存储，无需迁移。');
    assertReady();
    const records = await listLocalMedia();
    const migrated = [];
    for (const item of records) {
      const filename = path.basename(item.key);
      const source = path.join(uploadDir, filename);
      if (!existsSync(source)) continue;
      const key = objectKey('migration', filename);
      await putFile(key, source, item.mime || 'application/octet-stream');
      const record = { ...item, id: crypto.randomUUID(), key, url: publicUrl(key), storageDriver: 's3', updatedAt: new Date().toISOString() };
      await platformStore.saveMedia(record);
      migrated.push(record);
    }
    return migrated;
  }

  return {
    driver, list, upload, uploadSmartImage, uploadOriginal, replace, remove, migrateLocalMedia,
    createPresignedUpload, confirmPresignedUpload, health, referenceUrls: mediaReferenceUrls, objectKeys: mediaObjectKeys,
    close: () => client?.destroy()
  };
}
