/**
 * Shared video URL normalisation for the hero modal, product detail pages and
 * the solutions page.
 *
 * YouTube links are always rewritten to youtube-nocookie.com so that no
 * tracking cookie is written before the visitor actually starts playback.
 * Playable media files stay untouched and are rendered with a native <video>.
 */

export type VideoEmbed = {
  /** iframe src (youtube-nocookie) or the original URL for other providers. */
  url: string;
  /** YouTube Shorts need a 9:16 frame instead of the default 16:9 one. */
  isShorts: boolean;
  /** URL exactly as configured in the content model (external link / <video> src). */
  originalUrl: string;
  /** True when the URL points at a playable media file (mp4/webm/ogg/...). */
  isFile: boolean;
  /** True when the URL was recognised as a YouTube link. */
  isYouTube: boolean;
};

const MEDIA_FILE_PATTERN = /\.(mp4|webm|ogg|ogv|m4v|mov)(?:[?#].*)?$/i;
const YOUTUBE_HOSTS = new Set([
  'youtube.com',
  'www.youtube.com',
  'm.youtube.com',
  'music.youtube.com',
  'youtu.be',
  'www.youtu.be',
  'youtube-nocookie.com',
  'www.youtube-nocookie.com'
]);
const YOUTUBE_EMBED_QUERY = 'rel=0&modestbranding=1&playsinline=1';
const YOUTUBE_ID_PATTERN = /^[A-Za-z0-9_-]+$/;

const EMPTY_VIDEO: VideoEmbed = {
  url: '',
  isShorts: false,
  originalUrl: '',
  isFile: false,
  isYouTube: false
};

/** True when the URL ends with a media extension supported by <video>. */
export function isMediaFileUrl(value: string | undefined | null): boolean {
  const candidate = String(value ?? '').trim();
  if (!candidate) return false;
  try {
    const parsed = new URL(candidate, 'https://placeholder.invalid');
    return MEDIA_FILE_PATTERN.test(parsed.pathname);
  } catch {
    return MEDIA_FILE_PATTERN.test(candidate.split(/[?#]/)[0] || '');
  }
}

function youtubeVideoId(value: string): { id: string; isShorts: boolean } | null {
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return null;
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null;
  const host = parsed.hostname.toLowerCase();
  if (!YOUTUBE_HOSTS.has(host)) return null;

  const segments = parsed.pathname.split('/').filter(Boolean);
  const [first, second] = segments;
  let id = '';
  let isShorts = false;

  if (host === 'youtu.be' || host === 'www.youtu.be') {
    id = first || '';
  } else if (first === 'shorts') {
    id = second || '';
    isShorts = true;
  } else if (first === 'embed' || first === 'v' || first === 'live') {
    id = second || '';
  } else {
    id = parsed.searchParams.get('v') || '';
  }

  if (!id || !YOUTUBE_ID_PATTERN.test(id)) return null;
  return { id, isShorts };
}

/**
 * Normalises any configured video URL.
 *
 * - watch / shorts / youtu.be / already-embedded links become
 *   `https://www.youtube-nocookie.com/embed/<id>`.
 * - mp4/webm/ogg (and similar) stay as-is and report `isFile`.
 * - Anything else is returned unchanged so callers can still embed it.
 */
export function resolveVideoSource(input: string | undefined | null): VideoEmbed {
  const originalUrl = String(input ?? '').trim();
  if (!originalUrl) return { ...EMPTY_VIDEO };

  // Untrusted CMS links must never become arbitrary iframe origins or script URLs.
  if (!originalUrl.startsWith('/') || originalUrl.startsWith('//')) {
    try {
      if (!['https:', 'http:'].includes(new URL(originalUrl).protocol)) return { ...EMPTY_VIDEO };
    } catch {
      return { ...EMPTY_VIDEO };
    }
  }

  if (isMediaFileUrl(originalUrl)) {
    return { url: originalUrl, isShorts: false, originalUrl, isFile: true, isYouTube: false };
  }

  const youtube = youtubeVideoId(originalUrl);
  if (youtube) {
    return {
      url: `https://www.youtube-nocookie.com/embed/${youtube.id}?${YOUTUBE_EMBED_QUERY}`,
      isShorts: youtube.isShorts,
      originalUrl,
      isFile: false,
      isYouTube: true
    };
  }

  return { ...EMPTY_VIDEO };
}
