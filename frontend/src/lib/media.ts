import type { MediaItem } from './types';
import { resolveVideoSource } from './video';

export function safeMediaUrl(value: unknown): string {
  const url = String(value || '').trim();
  if (url.startsWith('/') && !url.startsWith('//')) return url;
  try {
    const parsed = new URL(url);
    return ['https:', 'http:'].includes(parsed.protocol) ? url : '';
  } catch {
    return '';
  }
}

function isImageSource(url: string): boolean {
  try {
    const parsed = new URL(url, 'https://astreva.invalid');
    if (/(^|\.)youtube(?:-nocookie)?\.com$|(^|\.)youtu\.be$/i.test(parsed.hostname)) return false;
    return !/\.(mp4|webm|ogg|ogv|m4v|mov)$/i.test(parsed.pathname);
  } catch { return false; }
}

export function normalizeMediaItems(value: unknown): MediaItem[] {
  if (!Array.isArray(value)) return [];
  return value.slice(0, 30).flatMap((raw): MediaItem[] => {
    const url = safeMediaUrl(raw?.url);
    const type = raw?.type === 'video' ? 'video' : 'image';
    if (!url || (type === 'video' && !resolveVideoSource(url).url) || (type === 'image' && !isImageSource(url))) return [];
    return [{
      type,
      url,
      alt: String(raw?.alt || '').slice(0, 200),
      poster: safeMediaUrl(raw?.poster),
      playback: raw?.playback === 'muted-autoplay' ? 'muted-autoplay' : 'click'
    }];
  });
}

export function mediaItemsFor(
  configured: MediaItem[] | undefined,
  legacy: { image?: string | string[]; video?: string | string[] } = {},
  defaults: string[] = [],
  alt = '',
  legacyPlayback: MediaItem['playback'] = 'click'
): MediaItem[] {
  if (Array.isArray(configured)) {
    const items = normalizeMediaItems(configured);
    if (items.length) return items;
    return defaults.map((url) => ({ type: 'image', url, alt }));
  }
  const images = (Array.isArray(legacy.image) ? legacy.image : [legacy.image]).map(safeMediaUrl).filter(Boolean);
  const videos = (Array.isArray(legacy.video) ? legacy.video : [legacy.video]).map(safeMediaUrl).filter(Boolean);
  const image = images[0] || '';
  if (videos.length) {
    const playable = videos.filter((url) => resolveVideoSource(url).url);
    if (playable.length) return playable.map((url) => ({ type: 'video', url, poster: image || defaults[0] || '', alt, playback: legacyPlayback }));
  }
  if (images.length) return images.map((url) => ({ type: 'image', url, alt }));
  return defaults.map((url) => ({ type: 'image', url, alt }));
}

export function firstMediaImage(items: MediaItem[], fallback = ''): string {
  return items.find((item) => item.type === 'image')?.url || items.find((item) => item.poster)?.poster || fallback;
}
