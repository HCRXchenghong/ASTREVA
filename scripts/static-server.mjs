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

http.createServer(async (req, res) => {
  try {
    const file = await resolveFile(new URL(req.url || '/', `http://${host}:${port}`).pathname);
    if (!file) {
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
