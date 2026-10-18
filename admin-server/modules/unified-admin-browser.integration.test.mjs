import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { chromium } from 'playwright-core';
import { createAdminSession } from './test-session-helper.mjs';

async function environment() {
  const values = {};
  const text = await fs.readFile(path.join(process.cwd(), '.env'), 'utf8');
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    values[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
  }
  return values;
}

async function waitFor(url, timeout = 10_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try { if ((await fetch(url)).ok) return; } catch {}
    await new Promise((resolve) => setTimeout(resolve, 60));
  }
  throw new Error(`等待后台超时：${url}`);
}

test('统一后台：R2 素材一级入口可用、系统设置有值且工作台可滚到底部', { skip: process.env.RUN_UNIFIED_BROWSER_E2E !== 'true', timeout: 60_000 }, async () => {
  const env = await environment();
  const port = 19343;
  const base = `http://127.0.0.1:${port}`;
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: process.cwd(), env: { ...process.env, ...env, PORT: String(port), HOST: '127.0.0.1', PUBLIC_ADMIN_ORIGIN: base }, stdio: 'ignore'
  });
  let browser;
  try {
    await waitFor(base + '/healthz');
    const session = await createAdminSession(base);
    const payload = session.cookieValue.split('.')[0];
    const signature = session.cookieValue.split('.')[1];
    browser = await chromium.launch({ executablePath: '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] });
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    await context.addCookies([{ name: 'astreva_admin_session', value: `${payload}.${signature}`, url: base, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    const pageErrors = [];
    const dissatisfiedComment = '一直没有解决我的问题，\n需要尽快回访，请不要删除这段完整评价。';
    let requestedRatingAgentID = '';
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await page.route('**/site-admin-api/servicebridge/api/admin/ai-settings/models', async (route) => {
      const request = route.request();
      assert.equal(request.method(), 'POST');
      const payload = request.postDataJSON();
      assert.ok(['chat_completions', 'responses', 'anthropic_messages'].includes(payload.api_type));
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ models: ['gpt-live-a', 'gpt-live-b'] }) });
    });
    await page.route('**/site-admin-api/system/media-settings/ai-image/test', async (route) => {
      assert.equal(route.request().method(), 'POST');
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: { configured: true, models: ['gpt-image-1', 'gpt-image-live'] } }) });
    });
    await page.route('**/site-admin-api/servicebridge/api/admin/agents/*/ratings', async (route) => {
      const requestUrl = new URL(route.request().url());
      requestedRatingAgentID = decodeURIComponent(requestUrl.pathname.split('/').at(-2));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          agent: {
            id: requestedRatingAgentID,
            account: 'rating-detail-agent',
            name: '评价测试客服',
            group: '售后组',
            status: 'offline',
            current_conversations: 2,
            max_conversations: 12
          },
          summary: { total: 1, satisfied: 0, neutral: 0, unsatisfied: 1, satisfaction_rate: 0, display_satisfaction_rate: 0 },
          ratings: [{
            id: 'rating-dissatisfied-e2e',
            conversation_id: 'conversation-rating-e2e',
            assigned_agent_id: requestedRatingAgentID,
            score: 1,
            tags: ['回复慢', '问题未解决'],
            comment: dissatisfiedComment,
            created_at: '2026-08-24T08:00:00.000Z'
          }]
        })
      });
    });
    await page.goto(base + '/site-admin/?view=workspace');
    await page.locator('#stats .stat').first().waitFor();
    assert.equal(await page.locator('[data-view="ratings"]').count(), 0, '独立服务评价入口应已删除');
    assert.equal(await page.locator('#view-ratings').count(), 0, '独立服务评价页面应已删除');
    assert.equal(await page.locator('.workspace-shell').evaluate((element) => getComputedStyle(element).display), 'grid');
    assert.equal(await page.locator('.workspace-sidebar').count(), 1);
    assert.equal(await page.locator('.workspace-chat').count(), 1);
    assert.equal(await page.locator('.workspace-detail').count(), 1);
    const workspaceConversation = page.locator('#workspaceConversationList [data-workspace-conversation]').first();
    if (await workspaceConversation.count()) {
      await workspaceConversation.click();
      await page.locator('#workspaceMessageList .workspace-message, #workspaceMessageList .empty').first().waitFor();
      assert.match(await page.locator('#workspaceVisitorFacts').textContent(), /会话 ID/);
    }

    const layout = await page.locator('#contentRoot').evaluate((root) => ({ client: root.clientHeight, scroll: root.scrollHeight, body: document.body.scrollHeight, viewport: innerHeight }));
    assert.ok(layout.scroll > layout.client, '工作台应有可滚动内容');
    assert.equal(layout.body, layout.viewport, '外层应固定在视口内');
    const bottom = await page.locator('#contentRoot').evaluate((root) => { root.scrollTop = root.scrollHeight; return { top: root.scrollTop, max: root.scrollHeight - root.clientHeight }; });
    assert.equal(bottom.top, bottom.max);

    await page.locator('[data-view="customers"]').click();
    await page.locator('#createAgent').click();
    assert.equal(await page.locator('#agentDialog #agentGroupSelect').evaluate((element) => element.tagName), 'SELECT');
    page.once('dialog', (dialog) => dialog.accept('夜间支持组'));
    await page.locator('#addAgentGroup').click();
    assert.equal(await page.locator('#agentGroupSelect').inputValue(), '夜间支持组');
    await page.locator('#agentDialog [data-close]').first().click();
    const agentDetailButton = page.locator('#agentRows [data-agent-ratings]').first();
    await agentDetailButton.waitFor();
    assert.equal((await agentDetailButton.textContent()).trim(), '详情');
    const clickedAgentID = await agentDetailButton.getAttribute('data-agent-ratings');
    await agentDetailButton.click();
    await page.locator('#agentRatingsDialog[open]').waitFor();
    assert.equal(requestedRatingAgentID, clickedAgentID);
    assert.match(await page.locator('#agentRatingDialogTitle').textContent(), /客服详情 · 评价测试客服/);
    assert.match(await page.locator('#agentDetailFacts').textContent(), /登录账号[\s\S]*rating-detail-agent[\s\S]*所属分组[\s\S]*售后组[\s\S]*当前会话数[\s\S]*2[\s\S]*最大并发数[\s\S]*12/);
    assert.equal((await page.locator('#agentRatingRecordCount').textContent()).trim(), '1 条');
    const dissatisfiedRow = page.locator('#agentRatingRowsDialog tr.rating-bad');
    await dissatisfiedRow.waitFor();
    assert.match(await dissatisfiedRow.textContent(), /回复慢、问题未解决/);
    const renderedComment = dissatisfiedRow.locator('.rating-comment.negative');
    assert.equal((await renderedComment.textContent()).trim(), dissatisfiedComment);
    assert.deepEqual(await renderedComment.evaluate((element) => {
      const style = getComputedStyle(element);
      return { whiteSpace: style.whiteSpace, overflowWrap: style.overflowWrap };
    }), { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' });
    assert.equal(await page.locator('#ratingRows').count(), 0, '不应再存在脱离客服上下文的全局评价明细');
    await page.locator('#agentRatingsDialog [data-close]').click();

    await page.locator('[data-view="settings"]').click();
    const settingsFrame = page.frameLocator('#view-settings iframe');
    await settingsFrame.locator('#contactForm [name="telephone"]').waitFor();
    const settingsLayout = await page.locator('#view-settings').evaluate((element) => ({
      insideContent: element.parentElement?.id === 'contentRoot',
      top: element.getBoundingClientRect().top,
      bottom: element.getBoundingClientRect().bottom,
      viewport: innerHeight
    }));
    assert.equal(settingsLayout.insideContent, true, '系统设置必须保留在主内容滚动容器内');
    assert.ok(settingsLayout.top >= 0 && settingsLayout.top < settingsLayout.viewport, '系统设置必须实际出现在当前视口');
    assert.ok(settingsLayout.bottom > settingsLayout.top, '系统设置必须拥有可见高度');
    assert.equal(await settingsFrame.locator('#businessForm [name="timezone"]').inputValue(), 'Asia/Shanghai');
    assert.equal(await settingsFrame.locator('#businessForm [name="timezone"]').evaluate((element) => element.tagName), 'SELECT');
    assert.equal(await settingsFrame.locator('#contactForm input').count(), 3);
    assert.ok((await settingsFrame.locator('#contactForm [name="telephone"]').inputValue()).length > 0);
    assert.equal(await settingsFrame.locator('#contactForm [name="phone"], #contactForm [name="wechat"], #contactForm [name="qq"]').count(), 0);
    assert.match(await settingsFrame.locator('#settingsState').textContent(), /服务器直接读取并显示/);
    assert.match(await settingsFrame.locator('.health-grid').textContent(), /PostgreSQL[\s\S]*正常/);
    assert.equal(await settingsFrame.locator('.health-card').count(), 5);
    assert.equal(await settingsFrame.locator('#storageForm [name="bucket"]').inputValue(), 'astreva-site-media');
    assert.equal(await settingsFrame.locator('#aiImageForm [name="defaultModel"]').inputValue(), 'gpt-image-1');
    assert.equal(await settingsFrame.locator('#aiForm [name="model"]').evaluate((element) => element.tagName), 'SELECT');
    assert.deepEqual(await settingsFrame.locator('#aiForm [name="api_type"] option').evaluateAll((options) => options.map((option) => option.value)), ['chat_completions', 'responses', 'anthropic_messages']);
    await settingsFrame.locator('#fetchAiModels').click();
    await settingsFrame.locator('#aiForm .form-state.success').waitFor();
    assert.deepEqual(await settingsFrame.locator('#aiModelSelect option').evaluateAll((options) => options.map((option) => option.value).filter((value) => value.startsWith('gpt-live'))), ['gpt-live-a', 'gpt-live-b']);
    await settingsFrame.locator('#fetchAiImageModels').click();
    await settingsFrame.locator('#aiImageForm .form-state.success').waitFor();
    assert.ok(await settingsFrame.locator('#aiImageModelSelect option[value="gpt-image-live"]').count());
    await settingsFrame.locator('#businessForm button[type="submit"]').click();
    await settingsFrame.locator('#businessForm .form-state.success').waitFor();
    assert.match(await settingsFrame.locator('#businessForm .form-state').textContent(), /已保存/);
    await settingsFrame.locator('#storageForm button[type="submit"]').click();
    await settingsFrame.locator('#storageForm .form-state.success').waitFor();
    assert.match(await settingsFrame.locator('#storageForm .form-state').textContent(), /真实连通/);

    const runtimeResponse = await context.request.get(base + '/site-admin-api/system/media-settings');
    assert.equal(runtimeResponse.status(), 200);
    const runtime = (await runtimeResponse.json()).data;
    assert.equal(runtime.storageHealth.ok, true);
    assert.equal(runtime.config.storage.bucket, 'astreva-site-media');
    assert.equal(runtime.config.storage.accessKeyConfigured, true);
    assert.equal(runtime.config.storage.accessKeyId, '');
    assert.equal(runtime.config.storage.secretAccessKey, '');
    assert.equal(runtime.config.aiImage.apiKey, '');

    const directResponse = await context.request.get(base + '/site-admin/settings');
    assert.equal(directResponse.status(), 200);
    const directHtml = await directResponse.text();
    assert.match(directHtml, /<select name="timezone" required>[\s\S]*value="Asia\/Shanghai" selected/);
    assert.match(directHtml, /name="telephone" value="400-123-4567"/);
    assert.doesNotMatch(directHtml, /name="wechat"|name="qq"|name="phone"/);

    const noJsContext = await browser.newContext({ viewport: { width: 1280, height: 720 }, javaScriptEnabled: false });
    await noJsContext.addCookies([{ name: 'astreva_admin_session', value: `${payload}.${signature}`, url: base, httpOnly: true, sameSite: 'Lax' }]);
    const noJsPage = await noJsContext.newPage();
    await noJsPage.goto(base + '/site-admin/settings');
    assert.equal(await noJsPage.locator('#businessForm [name="timezone"]').inputValue(), 'Asia/Shanghai');
    assert.ok((await noJsPage.locator('#contactForm [name="telephone"]').inputValue()).length > 0);
    assert.match(await noJsPage.locator('#settingsState').textContent(), /服务器直接读取并显示/);
    await noJsContext.close();

    const mobileContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    await mobileContext.addCookies([{ name: 'astreva_admin_session', value: `${payload}.${signature}`, url: base, httpOnly: true, sameSite: 'Lax' }]);
    const mobilePage = await mobileContext.newPage();
    await mobilePage.goto(base + '/site-admin/settings');
    assert.equal(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.ok(await mobilePage.evaluate(() => document.documentElement.scrollHeight > innerHeight));
    await mobileContext.close();

    const mediaNav = page.locator('[data-view="media"]');
    await mediaNav.click();
    assert.equal(await mediaNav.textContent().then((text) => text.trim()), '▦R2 素材库');
    const frame = page.frameLocator('#view-media iframe');
    await frame.locator('#stats .stat').first().waitFor();
    assert.equal(await frame.locator('body').evaluate((body) => body.classList.contains('embedded')), true);
    assert.equal(await frame.locator('.module-topbar').count(), 0);
    assert.equal(await frame.locator('.module-nav [data-section]').count(), 5);
    assert.equal(await frame.locator('#stats .stat').count(), 5);
    assert.ok(Number(await frame.locator('#stats .stat strong').first().textContent()) >= 1);
    assert.equal(await frame.locator('#resourceStatus [data-resource]').count(), 6);
    assert.match(await frame.locator('[data-resource="system-cpu"] [data-resource-text]').textContent(), /%/);
    const firstRuntime = await context.request.get(base + '/site-admin-api/media/runtime').then((response) => response.json());
    await page.waitForTimeout(2200);
    const secondRuntime = await context.request.get(base + '/site-admin-api/media/runtime').then((response) => response.json());
    assert.notEqual(firstRuntime.data.generatedAt, secondRuntime.data.generatedAt);
    assert.equal(typeof secondRuntime.data.cpu.usagePercent, 'number');

    await page.locator('[data-view="audit"]').click();
    await page.locator('#auditStats .stat').first().waitFor();
    assert.equal(await page.locator('#auditSource option').count(), 3);
    const auditRow = page.locator('#auditRows [data-audit-id]').first();
    if (await auditRow.count()) {
      await auditRow.click();
      await page.locator('#auditDialog[open]').waitFor();
      assert.match(await page.locator('#auditDetail').textContent(), /来源[\s\S]*风险等级[\s\S]*User-Agent[\s\S]*详情/);
      await page.locator('#auditDialog [data-close]').click();
    }
    assert.deepEqual(pageErrors, []);
  } finally {
    await browser?.close();
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});

test('客服引擎不可用时，系统设置仍显示安全默认值和明确错误', { skip: process.env.RUN_UNIFIED_BROWSER_E2E !== 'true', timeout: 30_000 }, async () => {
  const env = await environment();
  const port = 19344;
  const base = `http://127.0.0.1:${port}`;
  const server = spawn('node', ['admin-server/server.mjs'], {
    cwd: process.cwd(),
    env: { ...process.env, ...env, PORT: String(port), HOST: '127.0.0.1', PUBLIC_ADMIN_ORIGIN: base, SERVICEBRIDGE_API_URL: 'http://127.0.0.1:1' },
    stdio: 'ignore'
  });
  let browser;
  try {
    await waitFor(base + '/healthz');
    const session = await createAdminSession(base);
    const payload = session.cookieValue.split('.')[0];
    const signature = session.cookieValue.split('.')[1];
    browser = await chromium.launch({ executablePath: '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] });
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    await context.addCookies([{ name: 'astreva_admin_session', value: `${payload}.${signature}`, url: base, httpOnly: true, sameSite: 'Lax' }]);
    const page = await context.newPage();
    await page.goto(base + '/site-admin/?view=settings');
    const frame = page.frameLocator('#view-settings iframe');
    await frame.locator('#settingsState.error').waitFor();
    assert.equal(await frame.locator('#businessForm [name="timezone"]').inputValue(), 'Asia/Shanghai');
    assert.match(await frame.locator('#contactForm [name="entry_reply"]').inputValue(), /欢迎咨询/);
    assert.match(await frame.locator('#settingsState').textContent(), /安全默认值，不会再空白/);
    assert.match(await frame.locator('.health-grid').textContent(), /ServiceBridge[\s\S]*异常/);
  } finally {
    await browser?.close();
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
