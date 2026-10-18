import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import sharp from 'sharp';
import { chromium } from 'playwright-core';
import { applySessionCookie, createAdminSession } from './test-session-helper.mjs';

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

test('真实浏览器：五个参考仓库式视图、配置弹窗、智能上传、R2 直传、详情、稳定替换和删除', { skip: process.env.RUN_MEDIA_BROWSER_E2E !== 'true', timeout: 90_000 }, async () => {
  const env = await environment();
  const port = 19342;
  const base = `http://127.0.0.1:${port}`;
  const server = spawn('node', ['admin-server/server.mjs'], { cwd: process.cwd(), env: { ...process.env, ...env, PORT: String(port), HOST: '127.0.0.1', PUBLIC_ADMIN_ORIGIN: base }, stdio: 'ignore' });
  let browser;
  let context;
  const marker = `browser-e2e-${Date.now()}`;
  try {
    await waitFor(base + '/healthz');
    const session = await createAdminSession(base);
    browser = await chromium.launch({ executablePath: '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] });
    context = await browser.newContext({ baseURL: base, permissions: ['clipboard-read', 'clipboard-write'] });
    await applySessionCookie(context, base, session);
    const page = await context.newPage();
    page.on('dialog', (dialog) => dialog.accept());
    const root = await fs.mkdtemp(path.join(os.tmpdir(), 'astreva-media-browser-'));
    const first = path.join(root, `${marker}.png`);
    const second = path.join(root, `${marker}-replacement.png`);
    const directPdf = path.join(root, `${marker}.pdf`);
    await sharp({ create: { width: 720, height: 420, channels: 4, background: '#176fd1' } }).png().toFile(first);
    await sharp({ create: { width: 420, height: 720, channels: 4, background: '#e87931' } }).png().toFile(second);
    await fs.writeFile(directPdf, '%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n');

    await page.goto('/site-admin/media');
    await page.locator('#stats .stat').first().waitFor();
    assert.equal(await page.locator('#stats .stat').count(), 5);
    assert.equal(await page.locator('.module-nav [data-section]').count(), 5);

    await page.locator('[data-section="settings"]').click();
    await page.locator('#storageSummary dd').first().waitFor();
    assert.equal(await page.locator('.setting-card').count(), 6);
    await page.locator('#section-settings [data-open-setting="storage"]').click();
    await page.locator('#settingModal:not([hidden]) [name="bucket"]').waitFor();
    assert.ok((await page.locator('#settingModal [name="bucket"]').inputValue()).length > 0);
    assert.doesNotMatch(await page.locator('#settingModal').textContent(), new RegExp(env.S3_SECRET_ACCESS_KEY || 'unreachable-secret-marker'));
    await page.locator('#settingModal .modal-close').click();

    await page.locator('#section-settings [data-open-setting="image"]').click();
    await page.locator('#settingModal [data-setting-form="image"] input[name="variantWidths"]').waitFor();
    await page.locator('#settingModal [data-setting-form="image"] button.primary').click();
    await page.getByText('配置已保存并立即生效；重启后仍会保留。').waitFor({ timeout: 20_000 });
    await page.locator('#settingModal').waitFor({ state: 'hidden' });

    await page.locator('[data-section="guide"]').click();
    await page.getByRole('heading', { name: 'Cloudflare R2 接入指南' }).waitFor();
    assert.equal(await page.locator('.guide-steps article').count(), 4);

    await page.locator('[data-section="gallery"]').click();
    await page.locator('#openUpload').click();
    await page.locator('#uploadFiles').setInputFiles(first);
    await page.locator('#uploadButton').click();
    await page.locator('#uploadProgress').filter({ hasText: '完成 1 个，失败 0 个' }).waitFor({ timeout: 20_000 });
    await page.locator('#uploadModal').waitFor({ state: 'hidden' });

    await page.locator('#searchInput').fill(marker);
    await page.locator('.media-card').first().waitFor();
    assert.match(await page.locator('.media-card h3').first().textContent(), new RegExp(marker));
    await page.locator('[data-detail-media]').first().click();
    await page.locator('#detailModal:not([hidden])').waitFor();
    assert.match(await page.locator('.detail-table').textContent(), /image\/webp/);
    await page.locator('[data-detail-copy="url"]').click();
    assert.match(await page.evaluate(() => navigator.clipboard.readText()), /^http:\/\/127\.0\.0\.1:10006\/astreva-site-media\//);

    const chooserPromise = page.waitForEvent('filechooser');
    await page.locator('[data-detail-replace]').click();
    const chooser = await chooserPromise;
    await chooser.setFiles(second);
    await page.getByText('素材已替换，主 URL 未变。').waitFor({ timeout: 20_000 });
    await page.locator('#searchInput').fill(marker + '-replacement');
    await page.locator('.media-card').first().waitFor();
    await page.locator('[data-delete-media]').first().click();
    await page.getByText('素材已删除。').waitFor({ timeout: 20_000 });
    await page.locator('#mediaGrid .empty-state').waitFor();

    await page.locator('#openUpload').click();
    await page.locator('[data-mode="direct"]').click();
    await page.locator('#uploadFiles').setInputFiles(directPdf);
    await page.locator('#uploadButton').click();
    await page.locator('#uploadProgress').filter({ hasText: '完成 1 个，失败 0 个' }).waitFor({ timeout: 20_000 });
    await page.locator('#uploadModal').waitFor({ state: 'hidden' });
    await page.locator('#searchInput').fill(marker + '.pdf');
    await page.locator('.media-card').first().waitFor();
    assert.match(await page.locator('.media-card').first().textContent(), /file/);
    await page.locator('[data-delete-media]').first().click();
    await page.locator('#mediaGrid .empty-state').waitFor();
  } finally {
    if (context) {
      const response = await context.request.get('/site-admin-api/media?q=' + encodeURIComponent(marker) + '&pageSize=100').catch(() => null);
      if (response?.ok()) {
        const payload = await response.json();
        for (const item of payload.data || []) await context.request.delete('/site-admin-api/media/' + encodeURIComponent(item.id)).catch(() => {});
      }
    }
    await browser?.close();
    server.kill('SIGTERM');
    await new Promise((resolve) => server.once('exit', resolve));
  }
});
