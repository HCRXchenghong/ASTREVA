import React, { useEffect, useMemo, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, ExternalLink, Pause, Play } from 'lucide-react';
import type { MediaItem } from '../lib/types';
import { normalizeMediaItems } from '../lib/media';
import { resolveVideoSource } from '../lib/video';
import { MEDIA_LABELS } from '../lib/media-labels';
import type { SiteLocale } from '../lib/i18n';

type Props = {
  locale: SiteLocale;
  items: MediaItem[];
  alt: string;
  className?: string;
  mediaClassName?: string;
  background?: boolean;
  controls?: boolean;
  priority?: boolean;
  fallbackImage?: string;
  activeMediaEvent?: string;
};

export default function MediaCarousel({ locale, items, alt, className = '', mediaClassName = '', background = false, controls = true, priority = false, fallbackImage = '', activeMediaEvent }: Props) {
  const media = useMemo(() => normalizeMediaItems(items), [items]);
  const [active, setActive] = useState(0);
  const [started, setStarted] = useState(false);
  const [failed, setFailed] = useState(false);
  const [paused, setPaused] = useState(false);
  const [manualPaused, setManualPaused] = useState(false);
  const [visible, setVisible] = useState(false);
  const [documentVisible, setDocumentVisible] = useState(true);
  const [reducedMotion, setReducedMotion] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const current = media[active] || media[0];
  const labels = MEDIA_LABELS[locale];

  useEffect(() => {
    if (!activeMediaEvent) return;
    window.dispatchEvent(new CustomEvent(activeMediaEvent, { detail: { isVideo: current?.type === 'video' } }));
  }, [activeMediaEvent, current?.type]);

  useEffect(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    const update = () => setReducedMotion(query.matches);
    update();
    query.addEventListener?.('change', update);
    return () => query.removeEventListener?.('change', update);
  }, []);

  useEffect(() => {
    if (!root.current || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting), { threshold: 0.08 });
    observer.observe(root.current);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const update = () => setDocumentVisible(document.visibilityState === 'visible');
    update();
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);

  useEffect(() => {
    if (media.length < 2 || paused || manualPaused || !visible || !documentVisible || reducedMotion || started) return;
    const slide = media[active];
    if (slide?.type === 'video' && slide.playback !== 'muted-autoplay') return;
    const timer = window.setTimeout(() => setActive((index) => (index + 1) % media.length), slide?.type === 'video' ? 15000 : 6000);
    return () => window.clearTimeout(timer);
  }, [media, active, paused, manualPaused, visible, documentVisible, reducedMotion, started]);

  useEffect(() => {
    setStarted(false);
    setFailed(false);
  }, [active]);

  if (!current) return null;

  const go = (index: number) => {
    setActive((index + media.length) % media.length);
    setStarted(false);
  };
  const video = current.type === 'video' ? resolveVideoSource(current.url) : null;
  const autoplay = current.playback === 'muted-autoplay' && !started && visible && documentVisible && !reducedMotion;
  const showPlayer = Boolean(video?.url && visible && documentVisible && !failed && (autoplay || started));
  const poster = current.poster || fallbackImage;
  const embedUrl = video?.isYouTube
    ? `${video.url}&autoplay=1&mute=${autoplay ? '1' : '0'}${autoplay ? `&loop=1&playlist=${new URL(video.url).pathname.split('/').pop()}` : ''}`
    : video?.url || '';

  return (
    <div
      ref={root}
      className={`${background ? 'absolute inset-0' : 'relative'} overflow-hidden bg-brand-950 ${className}`}
      role={media.length > 1 ? 'region' : undefined}
      aria-roledescription={media.length > 1 ? 'carousel' : undefined}
      aria-label={alt}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setPaused(false); }}
    >
      {current.type === 'image' ? (
        <img
          key={`${active}-${current.url}`}
          src={current.url}
          alt={current.alt || alt}
          loading={priority ? 'eager' : 'lazy'}
          fetchPriority={priority ? 'high' : undefined}
          className={`absolute inset-0 h-full w-full object-cover ${mediaClassName}`}
          onError={(event) => {
            if (fallbackImage && event.currentTarget.src !== new URL(fallbackImage, window.location.href).href) event.currentTarget.src = fallbackImage;
          }}
        />
      ) : showPlayer ? (
        video?.isFile ? (
          <video
            key={`${active}-${current.url}`}
            src={video.url}
            poster={poster || undefined}
            autoPlay
            muted={autoplay}
            controls={!autoplay}
            loop={autoplay}
            playsInline
            preload="metadata"
            className={`absolute inset-0 h-full w-full object-cover ${mediaClassName}`}
            onError={() => setFailed(true)}
            onLoadedData={(event) => {
              if (autoplay) event.currentTarget.play().catch(() => setFailed(true));
            }}
            onPlay={() => { if (!autoplay) setStarted(true); }}
          />
        ) : (
          <iframe
            key={`${active}-${current.url}`}
            src={embedUrl}
            title={current.alt || alt}
            className={`absolute inset-0 h-full w-full border-0 ${mediaClassName}`}
            allow="autoplay; encrypted-media; picture-in-picture; web-share"
            allowFullScreen
            referrerPolicy="strict-origin-when-cross-origin"
          />
        )
      ) : (
        <>
          {poster && <img src={poster} alt="" loading={priority ? 'eager' : 'lazy'} className={`absolute inset-0 h-full w-full object-cover ${mediaClassName}`} />}
          <button
            type="button"
            className="absolute inset-0 z-10 flex items-center justify-center bg-brand-950/25 text-white transition-colors hover:bg-brand-950/35"
            aria-label={`${labels.play}${current.alt || alt}`}
            onClick={() => { setFailed(false); setStarted(true); }}
          >
            <span className="grid h-16 w-16 place-items-center rounded-full border border-white/70 bg-brand-950/65 shadow-xl"><Play className="ml-1" size={26} fill="currentColor" /></span>
          </button>
        </>
      )}
      {video?.isYouTube && (
        <a
          href={video.originalUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="absolute bottom-4 left-4 z-20 inline-flex items-center gap-1 rounded-md bg-brand-950/80 px-3 py-2 text-xs font-bold text-white hover:bg-brand-800"
          aria-label={labels.openYouTube}
        >
          YouTube <ExternalLink size={13} />
        </a>
      )}
      {controls && media.length > 1 && (
        <div className="absolute bottom-4 right-4 z-20 flex items-center gap-1 rounded-full bg-brand-950/70 p-1 text-white backdrop-blur-sm">
          <button type="button" className="grid h-9 w-9 place-items-center rounded-full hover:bg-white/20" aria-label={labels.previous} onClick={() => go(active - 1)}><ChevronLeft size={18} /></button>
          <span className="min-w-9 text-center text-xs font-bold tabular-nums">{active + 1}/{media.length}</span>
          <button type="button" className="grid h-9 w-9 place-items-center rounded-full hover:bg-white/20" aria-label={labels.next} onClick={() => go(active + 1)}><ChevronRight size={18} /></button>
          <button type="button" className="grid h-9 w-9 place-items-center rounded-full hover:bg-white/20" aria-label={manualPaused ? labels.resume : labels.pause} onClick={() => setManualPaused((value) => !value)}>{manualPaused ? <Play size={16} /> : <Pause size={16} />}</button>
        </div>
      )}
      {controls && media.length > 1 && media.length <= 8 && (
        <div className="absolute bottom-16 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1 rounded-full bg-brand-950/60 px-2 py-1 text-white">
          {media.map((item, index) => <button key={`${index}-${item.url}`} type="button" aria-label={`${labels.jump}${index + 1}${labels.item}`} aria-current={index === active ? 'true' : undefined} className={`h-5 w-5 rounded-full p-1 ${index === active ? 'bg-white/25' : ''}`} onClick={() => go(index)}><span className={`block h-full w-full rounded-full ${index === active ? 'bg-white' : 'bg-white/50'}`} /></button>)}
        </div>
      )}
    </div>
  );
}
