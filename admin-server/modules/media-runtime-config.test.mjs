import assert from 'node:assert/strict';
import test from 'node:test';
import {
  hydrateMediaRuntimeConfig,
  mediaRuntimeConfigFromEnv,
  normalizeAiImageUpdate,
  normalizeImageUpdate,
  normalizeStorageUpdate,
  persistedMediaRuntimeConfig,
  publicMediaRuntimeConfig,
  storageIdentity
} from './media-runtime-config.mjs';

function fixture() {
  return mediaRuntimeConfigFromEnv({
    S3_ENDPOINT: 'http://127.0.0.1:10006', S3_REGION: 'us-east-1', S3_BUCKET: 'media',
    S3_ACCESS_KEY_ID: 'access-secret-value', S3_SECRET_ACCESS_KEY: 'secret-secret-value',
    S3_FORCE_PATH_STYLE: 'true', S3_PUBLIC_BASE_URL: 'http://127.0.0.1:10006/media',
    ADMIN_S3_KEY_PREFIX: 'site-admin', ADMIN_OPENAI_IMAGE_API_KEY: 'image-api-secret',
    ADMIN_OPENAI_IMAGE_BASE_URL: 'https://api.openai.com/v1', ADMIN_OPENAI_IMAGE_MODEL: 'gpt-image-1'
  });
}

test('媒体运行配置将密钥与公开配置分离，并且页面只得到掩码', () => {
  const source = fixture();
  const stored = persistedMediaRuntimeConfig(source);
  assert.equal(JSON.stringify(stored.config).includes('secret-value'), false);
  assert.equal(stored.secrets.storageAccessKeyId, 'access-secret-value');
  assert.equal(stored.secrets.aiImageApiKey, 'image-api-secret');
  const hydrated = hydrateMediaRuntimeConfig(stored, mediaRuntimeConfigFromEnv({}));
  assert.equal(hydrated.storage.secretAccessKey, 'secret-secret-value');
  const visible = publicMediaRuntimeConfig(hydrated);
  assert.equal(visible.storage.accessKeyId, '');
  assert.equal(visible.storage.secretAccessKey, '');
  assert.equal(visible.aiImage.apiKey, '');
  assert.equal(visible.storage.accessKeyMasked, '••••alue');
  assert.equal(visible.aiImage.apiKeyMasked, '••••cret');
});

test('R2 更新会保留留空密钥，并能精确识别会破坏旧素材的存储身份切换', () => {
  const current = fixture().storage;
  const credentialsOnly = normalizeStorageUpdate({ region: 'auto', accessKeyId: '', secretAccessKey: '' }, current);
  assert.equal(credentialsOnly.accessKeyId, current.accessKeyId);
  assert.equal(credentialsOnly.secretAccessKey, current.secretAccessKey);
  assert.equal(storageIdentity(credentialsOnly), storageIdentity(current));
  const switched = normalizeStorageUpdate({ bucket: 'another-media' }, current);
  assert.notEqual(storageIdentity(switched), storageIdentity(current));
});

test('AI 生图配置限制 HTTPS 和请求主机白名单，并支持明确清除密钥', () => {
  const current = fixture().aiImage;
  assert.throws(() => normalizeAiImageUpdate({ baseUrl: 'http://example.com/v1' }, current), /HTTPS/);
  assert.throws(() => normalizeAiImageUpdate({ baseUrl: 'https://images.example.com/v1', allowedHosts: 'api.openai.com' }, current), /白名单/);
  const cleared = normalizeAiImageUpdate({ clearApiKey: true }, current);
  assert.equal(cleared.apiKey, '');
  assert.ok(cleared.configuredModels.split(',').includes(cleared.defaultModel));
});

test('图片处理与上传限制会标准化并持久化可执行参数', () => {
  const current = fixture().image;
  const next = normalizeImageUpdate({
    variantWidths: '1600,400,800,800',
    webpQuality: 88,
    keepOriginal: false,
    maxUploadMb: 64,
    allowedMimeTypes: 'image/jpeg,image/png,application/pdf'
  }, current);
  assert.equal(next.variantWidths, '400,800,1600');
  assert.equal(next.webpQuality, 88);
  assert.equal(next.keepOriginal, false);
  assert.equal(next.maxUploadBytes, 64 * 1024 * 1024);
  assert.equal(next.allowedMimeTypes, 'image/jpeg,image/png,application/pdf');
  assert.throws(() => normalizeImageUpdate({ allowedMimeTypes: 'image/svg+xml' }, current), /暂不支持/);
});

test('ADMIN_MEDIA_DRIVER=local 必须真正切换为本地存储，且不被旧运行时配置覆盖', () => {
  const local = mediaRuntimeConfigFromEnv({ ADMIN_MEDIA_DRIVER: 'local' });
  assert.equal(local.storage.driver, 'local');
  const hydrated = hydrateMediaRuntimeConfig({
    config: { storage: { driver: 's3', bucket: 'old-bucket' } },
    secrets: {}
  }, local);
  assert.equal(hydrated.storage.driver, 'local');
  assert.equal(publicMediaRuntimeConfig(hydrated).storage.driver, 'local');
});
