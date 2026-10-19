import { createReadStream, existsSync } from 'node:fs';
import fs from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '..');
const distDir = path.resolve(process.env.STATIC_DIR || path.join(ROOT, 'frontend', 'dist'));
const host = process.env.FRONTEND_HOST || '127.0.0.1';
const port = Number(process.env.FRONTEND_PORT || 4321);
const adminOrigin = (process.env.STATIC_ADMIN_ORIGIN || process.env.PUBLIC_ADMIN_URL || 'http://127.0.0.1:1337').replace(/\/$/, '');

const mimeTypes = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.xml': 'application/xml; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.mp4': 'video/mp4',
  '.webm': 'video/webm',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2'
};

function safePath(urlPath) {
  const decoded = decodeURIComponent(urlPath.split('?')[0]);
  const normalized = path.normalize(decoded).replace(/^(\.\.[/\\])+/, '');
  const absolute = path.join(distDir, normalized);
  if (!absolute.startsWith(distDir)) return null;
  return absolute;
}

async function resolveFile(reqPath) {
  const base = safePath(reqPath);
  if (!base) return null;
  if (existsSync(base)) {
    const stats = await fs.stat(base);
    if (stats.isDirectory()) {
      const indexFile = path.join(base, 'index.html');
      return existsSync(indexFile) ? indexFile : null;
    }
    return base;
  }
  if (!path.extname(base)) {
    const indexFile = path.join(base, 'index.html');
    return existsSync(indexFile) ? indexFile : null;
  }
  return null;
}

async function proxyLeadRequest(req, res) {
  const chunks = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 64 * 1024) {
      res.writeHead(413, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ error: { message: '请求体太大' } }));
      return;
    }
    chunks.push(chunk);
  }
  const headers = {};
  for (const name of ['content-type', 'origin', 'access-control-request-method', 'access-control-request-headers', 'user-agent']) {
    if (req.headers[name]) headers[name] = req.headers[name];
  }
  const upstream = await fetch(`${adminOrigin}/site-admin-api/leads`, {
    method: req.method,
    headers,
    body: req.method === 'POST' ? Buffer.concat(chunks) : undefined
  });
  const responseHeaders = { 'Cache-Control': 'no-store' };
  for (const name of ['content-type', 'access-control-allow-origin', 'access-control-allow-methods', 'access-control-allow-headers', 'access-control-max-age', 'retry-after', 'vary']) {
    const value = upstream.headers.get(name);
    if (value) responseHeaders[name] = value;
  }
  res.writeHead(upstream.status, responseHeaders);
  res.end(Buffer.from(await upstream.arrayBuffer()));
}

http.createServer(async (req, res) => {
  try {
    const requestUrl = new URL(req.url || '/', `http://${host}:${port}`);
    if (requestUrl.pathname === '/site-admin-api/leads' && ['POST', 'OPTIONS'].includes(req.method || '')) {
      await proxyLeadRequest(req, res);
      return;
    }
    const file = await resolveFile(requestUrl.pathname);
    if (!file) {
      // 本地也按生产行为返回真正的 404 页（Astro 产物里的 404.html），
      // 这样后台配置的 404 文案在本地就能验证，而不是只回一句 "not found"。
      const notFoundPage = await resolveFile('/404.html').catch(() => null);
      if (notFoundPage) {
        res.writeHead(404, {
          'Content-Type': 'text/html; charset=utf-8',
          'Cache-Control': 'no-store'
        });
        createReadStream(notFoundPage).pipe(res);
        return;
      }
      res.writeHead(404, {
        'Content-Type': 'text/plain; charset=utf-8',
        'Cache-Control': 'no-store'
      });
      res.end('not found');
      return;
    }
    const ext = path.extname(file).toLowerCase();
    res.writeHead(200, {
      'Content-Type': mimeTypes[ext] || 'application/octet-stream',
      'Cache-Control': 'no-store, no-cache, must-revalidate, proxy-revalidate',
      Pragma: 'no-cache',
      Expires: '0'
    });
    createReadStream(file).pipe(res);
  } catch (error) {
    res.writeHead(500, {
      'Content-Type': 'text/plain; charset=utf-8',
      'Cache-Control': 'no-store'
    });
    res.end(error.message);
  }
}).listen(port, host, () => {
  console.log(`Static frontend server listening on http://${host}:${port}/`);
  console.log(`Serving ${distDir}`);
});
