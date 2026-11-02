import test from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeHtml, sanitizeCmsContent, safeUrl } from './html-sanitize.mjs';

test('未闭合标签不会原样输出（历史 XSS 绕过）', () => {
  const input = '<p>ok</p><img src=x onerror=document.title="PWNED"//';
  const output = sanitizeHtml(input);
  assert.ok(!output.includes('<img'), output);
  assert.ok(output.includes('&lt;img'), output);
});

test('危险协议与协议相对链接被丢弃', () => {
  assert.equal(safeUrl('javascript:alert(1)'), '');
  assert.equal(safeUrl('//evil.example/x'), '');
  assert.equal(safeUrl('data:text/html,<script>'), '');
  assert.equal(safeUrl('/safe/path'), '/safe/path');
  assert.equal(safeUrl('https://example.com/a?b=1'), 'https://example.com/a?b=1');
  assert.ok(!sanitizeHtml('<a href="javascript:alert(1)">x</a>').includes('javascript:'));
  assert.ok(!sanitizeHtml('<a href="//evil.example">x</a>').includes('//evil.example'));
});

test('事件属性与 style 被移除，引用属性被转义', () => {
  const output = sanitizeHtml('<p title="x" onmouseover=alert(1)>hi</p>');
  assert.equal(output, '<p title="x">hi</p>');
  const link = sanitizeHtml('<a href="https://x.test/?a=1&b=2" target="_self">x</a>');
  assert.ok(link.includes('target="_blank"'));
  assert.ok(link.includes('rel="noopener noreferrer"'));
  assert.ok(link.includes('a=1&amp;b=2'));
});

test('CMS 链接字段递归校验', () => {
  const content = sanitizeCmsContent({
    home: { hero: { primaryHref: 'javascript:alert(1)', videoUrl: 'https://youtu.be/abc123' } },
    products: [{ downloads: [{ url: '//evil.example/x' }, { url: '/files/a.pdf' }] }]
  });
  assert.equal(content.home.hero.primaryHref, '');
  assert.equal(content.home.hero.videoUrl, 'https://youtu.be/abc123');
  assert.equal(content.products[0].downloads[0].url, '');
  assert.equal(content.products[0].downloads[1].url, '/files/a.pdf');
});
