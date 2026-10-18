#!/usr/bin/env node
// 仅供 scripts/verify-all.sh 使用的“桩内容 API”。
//
// 当 frontend/dist 不存在或为空时，官网构建需要一个可达的内容接口。
// 这里用一个只读桩替代 admin-server：直接读取仓库内的只读种子
// admin-server/seed/site-content.json，并返回构建进程需要的两个端点：
//
//   GET /site-admin-api/public-content   -> { data: <site-content> }
//   GET /site-admin-api/public-locale    -> { data: { locale } }
//
// 注意：不要用这个桩替代生产后台；它不读数据库、不放行写入、也不做鉴权。
// 启动后会向 stdout 打印一行 `STUB_PORT=<端口>`，供调用方解析。

import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const CONTENT_FILE = process.env.SITE_CONTENT_FILE
  ? path.resolve(process.env.SITE_CONTENT_FILE)
  : path.join(ROOT_DIR, 'admin-server', 'seed', 'site-content.json');
const HOST = process.env.STUB_HOST || '127.0.0.1';
const PORT = Number.parseInt(process.env.STUB_PORT || '0', 10);

function isSiteContent(value) {
  return Boolean(
    value &&
      typeof value === 'object' &&
      value.site &&
      value.home &&
      value.productCenter &&
      value.support &&
      Array.isArray(value.categories) &&
      Array.isArray(value.products) &&
      Array.isArray(value.faqs) &&
      Array.isArray(value.staticPages)
  );
}

function sendJSON(res, status, payload) {
  const body = Buffer.from(JSON.stringify(payload), 'utf8');
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': body.length,
    'Cache-Control': 'no-store'
  });
  res.end(body);
}

let contentCache = null;

async function loadContent() {
  if (contentCache) return contentCache;
  const raw = await readFile(CONTENT_FILE, 'utf8');
  const parsed = JSON.parse(raw);
  // 种子文件既可能是裸的 SiteContent，也可能包在 { data: ... } 里。
  const content = parsed?.data && isSiteContent(parsed.data) ? parsed.data : parsed;
  if (!isSiteContent(content)) {
    throw new Error(`种子内容不是合法的 SiteContent：${CONTENT_FILE}`);
  }
  contentCache = content;
  return content;
}

const server = createServer(async (req, res) => {
  try {
    const url = new URL(req.url || '/', `http://${req.headers.host || 'localhost'}`);
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      sendJSON(res, 405, { error: { message: '桩内容 API 只支持 GET' } });
      return;
    }
    if (url.pathname === '/site-admin-api/public-content') {
      const content = await loadContent();
      const requestedLocale = url.searchParams.get('locale') || content.site.locale || 'zh-CN';
      sendJSON(res, 200, {
        data: content,
        meta: { source: 'verify-stub', locale: requestedLocale }
      });
      return;
    }
    if (url.pathname === '/site-admin-api/public-locale') {
      const content = await loadContent();
      sendJSON(res, 200, {
        data: { locale: content.site.locale || 'zh-CN' },
        meta: { source: 'verify-stub' }
      });
      return;
    }
    if (url.pathname === '/healthz') {
      sendJSON(res, 200, { status: 'ok' });
      return;
    }
    sendJSON(res, 404, { error: { message: `桩内容 API 未实现：${url.pathname}` } });
  } catch (error) {
    sendJSON(res, 500, { error: { message: error?.message || String(error) } });
  }
});

server.on('error', (error) => {
  console.error(`[verify-stub] 启动失败：${error?.message || String(error)}`);
  process.exit(1);
});

server.listen(PORT, HOST, () => {
  const address = server.address();
  // 这一行是调用方解析端口的契约，不要改动格式。
  console.log(`STUB_PORT=${address.port}`);
});

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    server.close(() => process.exit(0));
    // 兜底：连接未关闭时最多再等 1 秒。
    setTimeout(() => process.exit(0), 1000).unref();
  });
}
