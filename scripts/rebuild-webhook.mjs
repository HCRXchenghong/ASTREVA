import { createServer } from 'node:http';
import { existsSync, readFileSync } from 'node:fs';
import { buildFrontend } from './build-frontend.mjs';

if (existsSync('.env')) {
  const text = readFileSync('.env', 'utf8');
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || !trimmed.includes('=')) continue;
    const index = trimmed.indexOf('=');
    const key = trimmed.slice(0, index).trim();
    const value = trimmed.slice(index + 1).trim().replace(/^['"]|['"]$/g, '');
    if (!process.env[key]) process.env[key] = value;
  }
}

const port = Number(process.env.REBUILD_WEBHOOK_PORT || 8787);
const secret = process.env.REBUILD_WEBHOOK_SECRET || '';
let running = false;
let queued = false;
let queuedSource = 'current';
let queuedPreview = null;
let lastBuild = {
  status: 'idle',
  startedAt: null,
  finishedAt: null,
  exitCode: null,
  signal: null,
  source: null
};

function json(res, status, payload) {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(payload));
}

async function runBuild(source = 'current', previewContent = null) {
  if (running) {
    queued = true;
    queuedSource = source;
    queuedPreview = previewContent;
    return;
  }
  running = true;
  lastBuild = {
    status: 'running',
    startedAt: new Date().toISOString(),
    finishedAt: null,
    exitCode: null,
    signal: null,
    source
  };
  try {
    let result = {};
    if (previewContent) {
      result = await buildFrontend({ source: 'current', snapshotContent: previewContent, target: 'preview' });
    } else {
      result = await buildFrontend({ source });
    }
    lastBuild = {
      ...lastBuild,
      status: 'succeeded',
      finishedAt: new Date().toISOString(),
      exitCode: 0,
      signal: null,
      source,
      ...(result?.previewToken ? { previewToken: result.previewToken } : {})
    };
  } catch (error) {
    console.error(`[rebuild] ${error.message}`);
    lastBuild = {
      ...lastBuild,
      status: 'failed',
      finishedAt: new Date().toISOString(),
      exitCode: 1,
      signal: null,
      source
    };
  } finally {
    running = false;
    if (queued) {
      queued = false;
      const nextSource = queuedSource;
      const nextPreview = queuedPreview;
      queuedSource = 'current';
      queuedPreview = null;
      void runBuild(nextSource, nextPreview);
    }
  }
}

createServer(async (req, res) => {
  if (req.method === 'GET' && req.url === '/healthz') {
    json(res, 200, { ok: true, running, queued, lastBuild });
    return;
  }

  if (req.method === 'GET' && req.url === '/last-build') {
    json(res, 200, { running, queued, lastBuild });
    return;
  }

  if (req.method !== 'POST' || req.url !== '/rebuild') {
    res.writeHead(404);
    res.end('not found');
    return;
  }
  if (secret && req.headers.authorization !== `Bearer ${secret}`) {
    res.writeHead(401);
    res.end('unauthorized');
    return;
  }
  let raw = '';
  for await (const chunk of req) {
    raw += chunk;
    // 草稿预览会把整份内容 JSON 一并提交，上限放宽到 8 MiB。
    if (raw.length > 8 * 1024 * 1024) {
      json(res, 413, { error: 'request body too large' });
      return;
    }
  }
  let source = 'current';
  let previewContent = null;
  if (raw) {
    try {
      const payload = JSON.parse(raw);
      if (payload.source === 'fallback') source = 'fallback';
      if (payload.source === 'preview') {
        if (!payload.content || typeof payload.content !== 'object') {
          json(res, 400, { error: 'preview 需要提供 content 对象' });
          return;
        }
        previewContent = payload.content;
      }
    } catch {
      json(res, 400, { error: 'invalid json' });
      return;
    }
  }
  void runBuild(source, previewContent);
  json(res, 202, { ok: true, running, queued, lastBuild });
}).listen(port, () => {
  console.log(`Rebuild webhook listening on http://127.0.0.1:${port}/rebuild`);
  console.log(`Health check available at http://127.0.0.1:${port}/healthz`);
});
