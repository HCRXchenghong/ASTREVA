function validMediaUrl(raw) {
  const value = String(raw || '').trim();
  if (value.startsWith('/') && !value.startsWith('//')) return true;
  try { return ['http:', 'https:'].includes(new URL(value).protocol); }
  catch { return false; }
}

function validVideoUrl(raw) {
  if (!validMediaUrl(raw)) return false;
  const value = String(raw).trim();
  let parsed;
  try { parsed = new URL(value, 'https://astreva.invalid'); }
  catch { return false; }
  if (/\.(mp4|webm|ogg|ogv|m4v|mov)$/i.test(parsed.pathname)) return true;
  const host = parsed.hostname.toLowerCase();
  if (!['youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com', 'youtu.be', 'www.youtu.be', 'youtube-nocookie.com', 'www.youtube-nocookie.com'].includes(host)) return false;
  const parts = parsed.pathname.split('/').filter(Boolean);
  const id = host.endsWith('youtu.be') ? parts[0] : ['embed', 'shorts', 'live', 'v'].includes(parts[0]) ? parts[1] : parsed.searchParams.get('v');
  return /^[A-Za-z0-9_-]{6,}$/.test(String(id || ''));
}

export function validateContentMedia(content) {
  function visit(value, path = 'content') {
    if (Array.isArray(value)) { value.forEach((item, index) => visit(item, `${path}[${index}]`)); return; }
    if (!value || typeof value !== 'object') return;
    for (const [key, list] of Object.entries(value)) {
      if (key === 'mediaItems' || /MediaItems$/.test(key)) {
        if (list === undefined) continue;
        if (!Array.isArray(list) || list.length > 30) throw new Error(`${path}.${key} 媒体列表无效或超过 30 项。`);
        list.forEach((item, index) => {
          const itemPath = `${path}.${key}[${index}]`;
          if (!item || !['image', 'video'].includes(item.type) || !validMediaUrl(item.url)) throw new Error(`${itemPath} 类型或地址无效。`);
          if (item.type === 'video' && !validVideoUrl(item.url)) throw new Error(`${itemPath} 不是可播放的视频或 YouTube 链接。`);
          if (item.type === 'image' && (/\.(mp4|webm|ogg|ogv|m4v|mov)(?:[?#]|$)/i.test(item.url) || /(?:^|\/\/)(?:www\.|m\.|music\.)?(?:youtube(?:-nocookie)?\.com|youtu\.be)(?:\/|$)/i.test(item.url))) throw new Error(`${itemPath} 图片地址指向视频或 YouTube。`);
          if (item.poster && !validMediaUrl(item.poster)) throw new Error(`${itemPath} 视频封面地址无效。`);
          if (item.playback && !['click', 'muted-autoplay'].includes(item.playback)) throw new Error(`${itemPath} 播放方式无效。`);
        });
      } else visit(list, path ? `${path}.${key}` : key);
    }
  }
  visit(content);
}

