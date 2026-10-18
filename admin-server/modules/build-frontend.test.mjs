import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { resolveBuildSnapshot } from '../../scripts/build-frontend.mjs';

test('官网构建可明确选择当前内容或已启用的降级版本', async () => {
  const seed = JSON.parse(await readFile(new URL('../seed/site-content.json', import.meta.url), 'utf8'));
  const current = structuredClone(seed);
  const fallback = structuredClone(seed);
  current.home.hero.title = '当前内容标题';
  fallback.home.hero.title = '降级版本标题';
  const server = createServer((req, res) => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url === '/current') res.end(JSON.stringify({ data: current }));
    else if (req.url === '/fallback') res.end(JSON.stringify({ data: { content: fallback, version: { id: 'fallback_test', name: '测试降级版本' } } }));
    else { res.statusCode = 503; res.end('{}'); }
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  const previousCurrent = process.env.CURRENT_CONTENT_URL;
  const previousFallback = process.env.FALLBACK_CONTENT_URL;
  try {
    process.env.CURRENT_CONTENT_URL = `http://127.0.0.1:${port}/current`;
    process.env.FALLBACK_CONTENT_URL = `http://127.0.0.1:${port}/fallback`;
    assert.equal((await resolveBuildSnapshot('current')).content.home.hero.title, '当前内容标题');
    assert.equal((await resolveBuildSnapshot('fallback')).content.home.hero.title, '降级版本标题');
    process.env.CURRENT_CONTENT_URL = `http://127.0.0.1:${port}/unavailable`;
    assert.equal((await resolveBuildSnapshot('auto')).content.home.hero.title, '降级版本标题');
    await assert.rejects(() => resolveBuildSnapshot('unknown'), /不支持的构建内容源/);
  } finally {
    if (previousCurrent == null) delete process.env.CURRENT_CONTENT_URL; else process.env.CURRENT_CONTENT_URL = previousCurrent;
    if (previousFallback == null) delete process.env.FALLBACK_CONTENT_URL; else process.env.FALLBACK_CONTENT_URL = previousFallback;
    await new Promise((resolve) => server.close(resolve));
  }
});
