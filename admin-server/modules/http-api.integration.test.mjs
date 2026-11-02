import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createAdminSession } from './test-session-helper.mjs';
import crypto from 'node:crypto';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

async function waitFor(url, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`后台未能在 ${timeoutMs}ms 内启动：${url}`);
}

test('HTTP API 暴露 OpenAPI、公开内容并保护管理接口', async () => {
  const root = process.cwd();
  const dataDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-http-api-data-'));
  const uploadDir = await mkdtemp(path.join(os.tmpdir(), 'astreva-http-api-upload-'));
  const port = 19338;
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: root,
    env: {
      ...process.env,
      PORT: String(port),
      HOST: '127.0.0.1',
      CMS_STORE_DRIVER: 'json',
      ALLOW_LEGACY_STORAGE_FOR_TESTS: 'true',
      ADMIN_2FA_SESSION_SECRET: 'http-api-test-session-secret',
      ADMIN_DATA_DIR: dataDir,
      ADMIN_UPLOAD_DIR: uploadDir,
      SITE_CONTENT_SEED_FILE: path.join(root, 'admin-server', 'seed', 'site-content.json'),
      FRONTEND_REBUILD_WEBHOOK_URL: 'http://127.0.0.1:1/rebuild'
    },
    stdio: 'ignore'
  });

  try {
    const base = `http://127.0.0.1:${port}`;
    await waitFor(`${base}/healthz`);
    const { cookieHeader: adminCookie } = await createAdminSession(base);
    const openapi = await (await fetch(`${base}/openapi.json`, { headers: { Cookie: adminCookie } })).json();
    const content = await (await fetch(`${base}/site-admin-api/public-content`)).json();
    const fallback = await (await fetch(`${base}/site-admin-api/public-fallback-content`)).json();
    const corsPreflight = await fetch(`${base}/site-admin-api/leads`, {
      method: 'OPTIONS',
      headers: { Origin: 'http://127.0.0.1:4321', 'Access-Control-Request-Method': 'POST', 'Access-Control-Request-Headers': 'content-type' }
    });
    const rejectedCors = await fetch(`${base}/site-admin-api/leads`, { method: 'OPTIONS', headers: { Origin: 'https://attacker.example' } });
    const publicContentCors = await fetch(`${base}/site-admin-api/public-content`, { method: 'OPTIONS', headers: { Origin: 'http://127.0.0.1:4321' } });
    const rejectedPublicContentCors = await fetch(`${base}/site-admin-api/public-content`, { method: 'OPTIONS', headers: { Origin: 'https://attacker.example' } });
    const invalidLeadResponse = await fetch(`${base}/site-admin-api/leads`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: '缺少联系方式' })
    });
    const leadBody = { name: '接口测试', email: 'test@example.com', message: '测试咨询单', sourcePage: '/support/' };
    const leadResponse = await fetch(`${base}/site-admin-api/leads`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(leadBody)
    });
    const duplicateLeadResponse = await fetch(`${base}/site-admin-api/leads`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(leadBody) });
    const protectedResponse = await fetch(`${base}/site-admin-api/fallback-versions`);
    const protectedMediaResponse = await fetch(`${base}/site-admin-api/media`);
    const protectedMediaSettingsResponse = await fetch(`${base}/site-admin-api/system/media-settings`);
    const protectedMediaEndpoints = await Promise.all([
      fetch(`${base}/site-admin-api/media/stats`),
      fetch(`${base}/site-admin-api/media/categories`),
      fetch(`${base}/site-admin-api/media/batch-delete`, { method: 'POST' }),
      fetch(`${base}/site-admin-api/media/presign`, { method: 'POST' }),
      fetch(`${base}/site-admin-api/media/confirm`, { method: 'POST' }),
      fetch(`${base}/site-admin-api/media/ai-models`),
      fetch(`${base}/site-admin-api/media/ai-generate`, { method: 'POST' }),
      fetch(`${base}/site-admin-api/media/example/replace`, { method: 'PUT' })
    ]);
    const protectedPublishResponse = await fetch(`${base}/site-admin-api/publish-jobs`);
    const protectedTranslationsResponse = await fetch(`${base}/site-admin-api/translations`);
    const protectedSeoResponse = await fetch(`${base}/site-admin-api/seo/ai-generate`, { method: 'POST' });
    const localeRecommendation = await fetch(`${base}/site-admin-api/public-locale`, { headers: { 'CF-IPCountry': 'AE', 'Accept-Language': 'zh-CN' } });
    const unavailableEnglish = await fetch(`${base}/site-admin-api/public-content?locale=en`);

    // 上面的初始化会话可复用；服务端会话带 sid 并由服务端校验。
    const cookie = adminCookie;
    const previewGrant = await fetch(base + '/site-admin-api/preview-grant', { headers: { Cookie: cookie } });
    assert.equal(previewGrant.status, 200);
    assert.match(previewGrant.headers.get('set-cookie') || '', /astreva_preview_access=/);
    const previewGrantAnonymous = await fetch(base + '/site-admin-api/preview-grant');
    assert.equal(previewGrantAnonymous.status, 401);
    const initialBootstrap = await (await fetch(base + '/site-admin-api/bootstrap', { headers: { Cookie: cookie } })).json();
    const nextContent = structuredClone(content.data);
    nextContent.site.brandTagline = '全量内容编辑测试';
    const fullSaveResponse = await fetch(base + '/site-admin-api/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ section: 'content', data: nextContent, revision: initialBootstrap.data.contentRevision })
    });
    const staleSaveResponse = await fetch(base + '/site-admin-api/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Cookie: cookie },
      body: JSON.stringify({ section: 'content', data: nextContent, revision: initialBootstrap.data.contentRevision })
    });
    const protectedEditorPage = await fetch(base + '/site-admin/content/editor?section=products', { redirect: 'manual' });
    const protectedSettingsPage = await fetch(base + '/site-admin/settings', { redirect: 'manual' });
    const contentHubPage = await fetch(base + '/site-admin/content/?embed=1', { headers: { Cookie: cookie }, redirect: 'manual' });
    const allContentPage = await fetch(base + '/site-admin/content/editor?section=allContent', { headers: { Cookie: cookie }, redirect: 'manual' });
    const seoEditorPage = await fetch(base + '/site-admin/content/editor?section=seo', { headers: { Cookie: cookie }, redirect: 'manual' });
    const unifiedPage = await fetch(base + '/site-admin/?view=media', { headers: { Cookie: cookie } });
    const spaDeepLink = await fetch(base + '/site-admin/workspace');
    const legacyAdmin2fa = await fetch(base + '/admin-2fa?returnTo=%2Fsite-admin%2Fmedia', { redirect: 'manual' });
    const translationPage = await fetch(base + '/site-admin/translations?embed=1', { headers: { Cookie: cookie }, redirect: 'manual' });
    const translationStatus = await fetch(base + '/site-admin-api/translations', { headers: { Cookie: cookie } });
    const protectedMediaPage = await fetch(base + '/site-admin/media', { redirect: 'manual' });
    const mediaPage = await fetch(base + '/site-admin/media', { headers: { Cookie: cookie }, redirect: 'manual' });
    const legacyMediaEditor = await fetch(base + '/site-admin/content/editor?section=media', { headers: { Cookie: cookie }, redirect: 'manual' });

    assert.equal(openapi.openapi, '3.1.0');
    assert.equal(content.data.site.brandName, '星渡ASTREVA');
    assert.equal(content.data.site.navigation.productMenu.categoryLabel, '产品类别');
    assert.equal(content.data.productCenter.catalog.emptyCategoryMessage, '该分类的系列产品正在整理中。');
    assert.equal(content.data.staticPages.find((page) => page.slug === 'solutions').sections.videoPlayIcon, '▶');
    assert.ok(fallback.data.version.id);
    assert.equal(corsPreflight.status, 204);
    assert.equal(corsPreflight.headers.get('access-control-allow-origin'), 'http://127.0.0.1:4321');
    assert.equal(rejectedCors.status, 403);
    assert.equal(publicContentCors.status, 204);
    assert.equal(publicContentCors.headers.get('access-control-allow-origin'), 'http://127.0.0.1:4321');
    assert.equal(rejectedPublicContentCors.status, 403);
    assert.equal(invalidLeadResponse.status, 400);
    assert.equal(leadResponse.status, 201);
    assert.ok((await leadResponse.json()).data.id.startsWith('lead_'));
    assert.equal(duplicateLeadResponse.status, 200);
    assert.equal((await duplicateLeadResponse.json()).meta.duplicate, true);
    assert.equal(protectedResponse.status, 401);
    assert.equal(protectedMediaResponse.status, 401);
    assert.equal(protectedMediaSettingsResponse.status, 401);
    assert.deepEqual(protectedMediaEndpoints.map((response) => response.status), Array(8).fill(401));
    assert.equal(protectedPublishResponse.status, 401);
    assert.equal(protectedTranslationsResponse.status, 401);
    assert.equal(protectedSeoResponse.status, 401);
    assert.ok(openapi.paths['/site-admin-api/seo/ai-generate']);
    assert.equal(localeRecommendation.status, 200);
    assert.equal((await localeRecommendation.json()).data.locale, 'ar');
    assert.equal(unavailableEnglish.status, 409);
    // 旧版后台页面全部 302 到 Arco 控制台对应路由。
    assert.equal(protectedEditorPage.status, 302);
    assert.equal(protectedEditorPage.headers.get('location'), '/site-admin/content/site');
    assert.equal(protectedSettingsPage.status, 302);
    assert.equal(protectedSettingsPage.headers.get('location'), '/site-admin/system/general');
    assert.equal(fullSaveResponse.status, 200);
    assert.equal(staleSaveResponse.status, 409);
    assert.equal((await (await fetch(base + '/site-admin-api/public-content')).json()).data.site.brandTagline, '全量内容编辑测试');
    assert.equal(contentHubPage.status, 302);
    assert.equal(contentHubPage.headers.get('location'), '/site-admin/content/site');
    assert.equal(allContentPage.status, 302);
    assert.equal(allContentPage.headers.get('location'), '/site-admin/content/site');
    assert.equal(seoEditorPage.status, 302);
    assert.equal(seoEditorPage.headers.get('location'), '/site-admin/content/site');
    assert.equal(protectedMediaPage.status, 302);
    assert.equal(protectedMediaPage.headers.get('location'), '/site-admin/content/media');
    assert.equal(mediaPage.status, 302);
    assert.equal(mediaPage.headers.get('location'), '/site-admin/content/media');
    assert.equal(legacyMediaEditor.status, 302);
    assert.equal(legacyMediaEditor.headers.get('location'), '/site-admin/content/media');
    assert.equal(translationPage.status, 302);
    assert.equal(translationPage.headers.get('location'), '/site-admin/content/translations');
    assert.equal(translationStatus.status, 200);
    assert.deepEqual((await translationStatus.json()).data.translations.map((item) => item.locale), ['en', 'ar']);
  } finally {
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
