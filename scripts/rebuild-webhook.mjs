import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';

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
let lastBuild = {
  status: 'idle',
  startedAt: null,
  finishedAt: null,
  exitCode: null,
  signal: null
};

function json(res, status, payload) {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(payload));
}

function runBuild() {
  if (running) {
    queued = true;
    return;
  }
  running = true;
  lastBuild = {
    status: 'running',
    startedAt: new Date().toISOString(),
    finishedAt: null,
    exitCode: null,
    signal: null
  };
  const child = spawn('npm', ['run', 'build:frontend'], {
    cwd: process.cwd(),
    stdio: 'inherit',
    env: process.env
  });
  child.on('exit', (code, signal) => {
    lastBuild = {
      ...lastBuild,
      status: code === 0 ? 'succeeded' : 'failed',
      finishedAt: new Date().toISOString(),
      exitCode: code,
      signal
    };
    running = false;
    if (queued) {
      queued = false;
      runBuild();
    }
  });
}

createServer((req, res) => {
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
  req.resume();
  runBuild();
  json(res, 202, { ok: true, running, queued, lastBuild });
}).listen(port, () => {
  console.log(`Rebuild webhook listening on http://127.0.0.1:${port}/rebuild`);
  console.log(`Health check available at http://127.0.0.1:${port}/healthz`);
});
