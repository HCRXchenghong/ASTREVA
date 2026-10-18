import assert from 'node:assert/strict';
import test from 'node:test';
import { createOpenApiDocument } from './openapi.mjs';

test('OpenAPI 文档覆盖内容、媒体、版本、审计和发布队列', () => {
  const document = createOpenApiDocument('https://admin.example.com');
  assert.equal(document.openapi, '3.1.0');
  assert.equal(document.servers[0].url, 'https://admin.example.com');
  for (const endpoint of [
    '/site-admin-api/public-content',
    '/site-admin-api/public-locale',
    '/site-admin-api/translations',
    '/site-admin-api/translations/settings',
    '/site-admin-api/translations/models',
    '/site-admin-api/translations/retranslate',
    '/site-admin-api/leads/{id}',
    '/site-admin-api/upload',
    '/site-admin-api/media',
    '/site-admin-api/media/{id}',
    '/site-admin-api/media/{id}/references',
    '/site-admin-api/media/{id}/replace',
    '/site-admin-api/media/categories',
    '/site-admin-api/media/stats',
    '/site-admin-api/media/runtime',
    '/site-admin-api/media/batch-delete',
    '/site-admin-api/media/presign',
    '/site-admin-api/media/confirm',
    '/site-admin-api/media/ai-models',
    '/site-admin-api/media/ai-generate',
    '/site-admin-api/media/ai-preview',
    '/site-admin-api/media/ai-preview/{id}/commit',
    '/site-admin-api/media/ai-preview/{id}',
    '/site-admin-api/system/media-settings',
    '/site-admin-api/system/media-settings/storage',
    '/site-admin-api/system/media-settings/image',
    '/site-admin-api/system/media-settings/ai-image',
    '/site-admin-api/system/media-settings/ai-image/test',
    '/site-admin-api/servicebridge/api/admin/agents',
    '/site-admin-api/servicebridge/api/admin/conversations',
    '/site-admin-api/servicebridge/api/conversations/{id}/messages',
    '/site-admin-api/servicebridge/api/admin/contact-settings',
    '/site-admin-api/servicebridge/api/admin/business-hours',
    '/site-admin-api/servicebridge/api/admin/ai-settings',
    '/site-admin-api/servicebridge/api/admin/ai-settings/models',
    '/site-admin-api/servicebridge/api/admin/keyword-rules',
    '/site-admin-api/servicebridge/api/admin/audit-events',
    '/site-admin-api/fallback-versions',
    '/site-admin-api/audit-events',
    '/site-admin-api/publish-jobs'
  ]) assert.ok(document.paths[endpoint]);
});
