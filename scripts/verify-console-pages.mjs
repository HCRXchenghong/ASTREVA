#!/usr/bin/env node
/**
 * verify-console-pages.mjs —— 控制台全页面巡检（大改造后的整体验收）
 *
 * 做三件事：
 *   1. 从 `admin-web/src/router/routes/modules/*.ts` 自动发现所有**可见**路由（跳过 hideInMenu），
 *      所以导航怎么重排都不需要改这个脚本；
 *   2. 用真实 Chrome 逐页打开（带真实管理员会话 Cookie），收集 console error、页面异常、
 *      以及任何 5xx 响应；
 *   3. 打印每页实际渲染出来的标题文字，便于确认"点开的是不是那个页面"，而不是被 SPA fallback
 *      或 not-found 页面糊弄过去。
 *
 * 用法：
 *   ASTREVA_ADMIN_COOKIE_FILE=/tmp/astreva-admin-session.txt \
 *     node scripts/verify-console-pages.mjs [--base http://127.0.0.1:1337] [--headed]
 *
 * 会话从哪来：本地可用一个真实会话（例如由运维写入的 600 权限文件）。脚本不会伪造签名、
 * 不会创建管理员账号，也不会改动任何数据——它只做只读访问（GET 页面与接口）。
 */

import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..');
const args = process.argv.slice(2);
const argValue = (name, fallback) => {
  const index = args.indexOf(name);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};
const BASE = argValue('--base', process.env.ASTREVA_ADMIN_BASE || 'http://127.0.0.1:1337').replace(/\/+$/, '');
const HEADED = args.includes('--headed');
const COOKIE_FILE = process.env.ASTREVA_ADMIN_COOKIE_FILE || '/tmp/astreva-admin-session.txt';
const CHROME = process.env.ASTREVA_CHROME_PATH || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';

/** 从路由模块里抽出「可见」路由：模块 path + 子 path，跳过 hideInMenu。 */
function discoverRoutes() {
  const dir = path.join(ROOT, 'admin-web', 'src', 'router', 'routes', 'modules');
  const routes = [];
  for (const file of fs.readdirSync(dir).filter((name) => name.endsWith('.ts'))) {
    const text = fs.readFileSync(path.join(dir, file), 'utf8');
    const baseMatch = text.match(/path:\s*'\/([^']*)'/);
    const base = baseMatch ? `/${baseMatch[1]}`.replace(/\/$/, '') : '';
    // 只解析 children: [ ... ] 区域，否则会把模块自己的对象字面量也当成一个子项
    // （表现为凭空多出 /ai/ai、/system/system 这种不存在的路径）。
    const childrenStart = text.indexOf('children: [');
    const body = childrenStart >= 0 ? text.slice(childrenStart + 'children: ['.length) : '';
    // 按 "path:" 的出现位置切块，而不是要求 { 紧跟 path——很多条目在 path 前有注释行。
    const markerRe = /path:\s*'([^']*)'/g;
    const marks = [];
    let match;
    while ((match = markerRe.exec(body)) !== null) marks.push({ path: match[1], index: match.index });
    for (let i = 0; i < marks.length; i += 1) {
      const blockStart = marks[i].index;
      const blockEnd = i + 1 < marks.length ? marks[i + 1].index : body.length;
      const block = body.slice(blockStart, blockEnd);
      if (/hideInMenu:\s*true/.test(block)) continue;
      const childPath = marks[i].path;
      const full = childPath === '' ? `${base}/` : `${base}/${childPath}`.replace(/\/{2,}/g, '/');
      routes.push({ file, path: full.endsWith('/') && full !== '/' ? full.slice(0, -1) : full });
    }
  }
  // 去重并稳定排序
  const seen = new Set();
  return routes.filter((route) => (seen.has(route.path) ? false : (seen.add(route.path), true)))
    .sort((a, b) => a.path.localeCompare(b.path));
}

function readCookie() {
  if (!fs.existsSync(COOKIE_FILE)) return '';
  const raw = fs.readFileSync(COOKIE_FILE, 'utf8').trim();
  const pair = raw.split(';')[0];
  const index = pair.indexOf('=');
  return index > 0 ? { name: pair.slice(0, index), value: pair.slice(index + 1) } : '';
}

const routes = discoverRoutes();
if (!routes.length) {
  console.error('没有发现任何可见路由，检查 admin-web/src/router/routes/modules/*.ts');
  process.exit(2);
}
const cookie = readCookie();
if (!cookie) {
  console.error(`缺少管理员会话：${COOKIE_FILE} 不存在或为空。`);
  console.error('提示：本地可由运维写入一个真实会话文件（600 权限），然后重跑。');
  process.exit(2);
}
if (!fs.existsSync(CHROME)) {
  console.error(`找不到 Chrome：${CHROME}（可用 ASTREVA_CHROME_PATH 指定）`);
  process.exit(2);
}

const { chromium } = require('playwright-core');
const browser = await chromium.launch({ executablePath: CHROME, headless: !HEADED });
const context = await browser.newContext({ viewport: { width: 1440, height: 960 } });
await context.addCookies([{ ...cookie, domain: new URL(BASE).hostname, path: '/' }]);
// 控制台的登录态是前端 localStorage 里的 token 标记（会话本身由 HttpOnly Cookie 承载），
// 只带 Cookie 会被路由守卫弹回登录页，所以这里补上前端标记，才能真正进到页面里。
await context.addInitScript(() => {
  try {
    window.localStorage.setItem('token', 'cookie-session');
  } catch {}
});

let failed = 0;
const rows = [];
for (const route of routes) {
  const page = await context.newPage();
  const consoleErrors = [];
  const pageErrors = [];
  const serverErrors = [];
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text().slice(0, 160));
  });
  page.on('pageerror', (error) => pageErrors.push(String(error.message || error).slice(0, 160)));
  page.on('response', (response) => {
    if (response.status() >= 500) serverErrors.push(`${response.status()} ${response.url().replace(BASE, '')}`);
  });

  let heading = '';
  try {
    await page.goto(`${BASE}/site-admin${route.path}`, { waitUntil: 'networkidle', timeout: 30_000 });
    await page.waitForTimeout(400);
    heading = (await page.evaluate(() => {
      const text = document.body.innerText.replace(/\s+/g, ' ');
      if (location.pathname.includes('/login')) return '⚠ 被弹回登录页';
      const node = document.querySelector('.arco-layout-content h1, .arco-layout-content h2, .arco-page-header-title, .arco-card-header-title, h2, h3');
      const title = node ? node.textContent.replace(/\s+/g, ' ').trim().slice(0, 40) : '';
      if (title) return title;
      // 没有标题时退回内容区前 40 个字，至少能看出渲染了什么
      const content = document.querySelector('.arco-layout-content');
      return (content ? content.innerText.replace(/\s+/g, ' ').trim().slice(0, 40) : '') || '(内容为空)';
    })) || '(内容为空)';
    if (String(heading).startsWith('⚠')) pageErrors.push(String(heading));
  } catch (error) {
    pageErrors.push(`导航失败：${String(error.message || error).slice(0, 120)}`);
  }
  await page.close();

  const ok = !consoleErrors.length && !pageErrors.length && !serverErrors.length;
  if (!ok) failed += 1;
  rows.push({ route: route.path, heading, consoleErrors, pageErrors, serverErrors, ok });
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${route.path.padEnd(28)} 标题=${heading}`);
  for (const item of consoleErrors) console.log(`      console error: ${item}`);
  for (const item of pageErrors) console.log(`      页面异常: ${item}`);
  for (const item of serverErrors) console.log(`      服务端错误: ${item}`);
}

await browser.close();
console.log(`\n共 ${rows.length} 个页面：PASS ${rows.length - failed} / FAIL ${failed}`);
if (failed) {
  console.log('RESULT: FAIL —— 有页面存在 console error 或 5xx，见上方明细。');
  process.exit(1);
}
console.log('RESULT: PASS —— 所有可见页面均无 console error、无 5xx。');
