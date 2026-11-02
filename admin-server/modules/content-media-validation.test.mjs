import test from 'node:test';
import assert from 'node:assert/strict';
import { validateContentMedia } from './content-media-validation.mjs';

test('媒体列表接受站内图片、MP4 和可信 YouTube 视频，并保留顺序', () => {
  const content = { home: { hero: { mediaItems: [
    { type: 'image', url: '/assets/placeholders/scenes/home-hero.webp' },
    { type: 'video', url: 'https://youtu.be/dQw4w9WgXcQ', playback: 'click', poster: '/assets/poster.webp' },
    { type: 'video', url: 'https://cdn.example.com/intro.mp4', playback: 'muted-autoplay' }
  ] } } };
  assert.doesNotThrow(() => validateContentMedia(content));
  assert.equal(content.home.hero.mediaItems[0].type, 'image');
});

test('不可信 iframe 来源与危险 URL 协议会被拒绝', () => {
  for (const url of ['javascript:alert(1)', 'https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ', '//cdn.example.com/a.mp4']) {
    assert.throws(() => validateContentMedia({ mediaItems: [{ type: 'video', url }] }));
  }
});

test('图片不能指向视频或 YouTube，视频必须是可播放源', () => {
  for (const item of [
    { type: 'image', url: '/uploads/clip.mp4' },
    { type: 'image', url: 'https://youtube.com/watch?v=dQw4w9WgXcQ' },
    { type: 'video', url: 'https://cdn.example.com/ordinary-page' }
  ]) assert.throws(() => validateContentMedia({ mediaItems: [item] }));
});

test('弹窗背景列表、封面和播放模式也受到校验', () => {
  assert.throws(() => validateContentMedia({ home: { hero: { videoModalBackgroundMediaItems: [{ type: 'image', url: 'data:image/png;base64,abc' }] } } }));
  assert.throws(() => validateContentMedia({ mediaItems: [{ type: 'video', url: '/clip.mp4', poster: 'javascript:alert(1)' }] }));
  assert.throws(() => validateContentMedia({ mediaItems: [{ type: 'video', url: '/clip.mp4', playback: 'unsafe' }] }));
});

test('空列表表示使用默认图，但超过 30 项会被拒绝', () => {
  assert.doesNotThrow(() => validateContentMedia({ mediaItems: [] }));
  assert.throws(() => validateContentMedia({ mediaItems: Array.from({ length: 31 }, () => ({ type: 'image', url: '/image.webp' })) }));
});
