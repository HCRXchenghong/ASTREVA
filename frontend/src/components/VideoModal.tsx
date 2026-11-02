import { ArrowUpRight, Play, X } from 'lucide-react';
import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { resolveVideoSource } from '../lib/video';
import MediaCarousel from './MediaCarousel';
import { mediaItemsFor } from '../lib/media';
import type { MediaItem } from '../lib/types';
import { sceneImages } from '../lib/default-images';
import type { SiteLocale } from '../lib/i18n';

type Props = {
  locale: SiteLocale;
  primaryLabel: string;
  primaryHref: string;
  secondaryLabel: string;
  videoUrl?: string;
  mediaItems?: MediaItem[];
  videoTitle: string;
  videoModalEyebrow?: string;
  videoModalDescription?: string;
  videoModalBackgroundImage?: string;
  videoModalBackgroundMediaItems?: MediaItem[];
  videoPosterImage?: string;
  previewLabel: string;
  openExternalLabel: string;
  closeLabel: string;
  showSecondary?: boolean;
  activeMediaEvent?: string;
};

export default function VideoModal({
  locale,
  primaryLabel,
  primaryHref,
  secondaryLabel,
  videoUrl,
  mediaItems,
  videoTitle,
  videoModalEyebrow,
  videoModalDescription,
  videoModalBackgroundImage,
  videoModalBackgroundMediaItems,
  videoPosterImage,
  previewLabel,
  openExternalLabel,
  closeLabel,
  showSecondary = true,
  activeMediaEvent
}: Props) {
  const [open, setOpen] = useState(false);
  const [showPlayButton, setShowPlayButton] = useState(showSecondary);
  useEffect(() => {
    if (!activeMediaEvent) return;
    const update = (event: Event) => setShowPlayButton(Boolean((event as CustomEvent<{ isVideo: boolean }>).detail?.isVideo));
    window.addEventListener(activeMediaEvent, update);
    return () => window.removeEventListener(activeMediaEvent, update);
  }, [activeMediaEvent]);
  const embed = resolveVideoSource(videoUrl);
  const configuredMedia = mediaItemsFor(mediaItems, { video: videoUrl }, [...new Set([videoPosterImage, ...sceneImages('homeHero')].filter(Boolean) as string[])], videoTitle);
  const displayEmbed = Array.isArray(mediaItems) ? resolveVideoSource(configuredMedia[0]?.url) : embed;
  const modal = (
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center overflow-y-auto bg-brand-950/95 p-4 backdrop-blur-md md:p-8"
      aria-modal="true"
      role="dialog"
      aria-labelledby="home-video-modal-title"
    >
      <MediaCarousel locale={locale} items={mediaItemsFor(videoModalBackgroundMediaItems, { image: videoModalBackgroundImage }, sceneImages('homeHero'), videoTitle, 'muted-autoplay')} alt={videoTitle} background mediaClassName="opacity-25" controls={false} />
      <div className="pointer-events-none absolute inset-0 bg-linear-to-br from-brand-950/65 via-brand-950/90 to-navy-950/95" />
      <div className="blueprint-grid pointer-events-none absolute inset-0 opacity-30" aria-hidden="true" />
      <button
        onClick={() => setOpen(false)}
        className="fixed right-5 top-5 z-[10000] flex h-12 w-12 items-center justify-center rounded-full border border-white/20 bg-white/10 text-white shadow-2xl backdrop-blur-md transition-colors hover:bg-white/20 md:right-8 md:top-8"
        aria-label={closeLabel}
      >
        <X size={30} />
      </button>
      <div className="relative z-10 flex w-full flex-col items-center py-10">
        <div className="mb-7 w-full max-w-5xl px-2 pr-16 md:px-5">
          {videoModalEyebrow && <p className="mb-3 text-xs font-black uppercase tracking-[0.24em] text-accent-500">{videoModalEyebrow}</p>}
          <h2 id="home-video-modal-title" className="text-2xl font-black text-white md:text-4xl">{videoTitle}</h2>
          {videoModalDescription && <p className="mt-3 max-w-3xl text-sm leading-7 text-slate-200 md:text-base">{videoModalDescription}</p>}
        </div>
        <div
          className={`w-full bg-black rounded-2xl overflow-hidden border border-white/15 shadow-[0_28px_90px_rgba(0,0,0,0.5)] flex items-center justify-center ${
            displayEmbed.isShorts ? 'max-w-[420px] max-h-[85vh] aspect-[9/16]' : 'max-w-5xl aspect-video'
          }`}
        >
          {Array.isArray(mediaItems) ? (
            <MediaCarousel locale={locale} items={configuredMedia} alt={videoTitle} fallbackImage={videoPosterImage} className="h-full w-full" />
          ) : embed.isFile ? (
            // Uploaded mp4/webm files (or a direct file URL in the CMS) must play through a
            // native <video>; putting them in an iframe only yields a shrunken default player.
            <video
              src={embed.url}
              title={videoTitle}
              controls
              poster={videoPosterImage || undefined}
              playsInline
              preload="metadata"
              className="h-full w-full bg-black object-contain"
            />
          ) : embed.url ? (
            <iframe
              src={embed.url}
              title={videoTitle}
              className="w-full h-full border-0"
              allow="accelerometer; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
              allowFullScreen
              referrerPolicy="strict-origin-when-cross-origin"
            />
          ) : (
            <p className="flex flex-col items-center gap-4 px-6 text-center text-slate-300">
              <Play size={48} className="text-accent-500" />
              <span>{previewLabel}</span>
            </p>
          )}
        </div>
        {!Array.isArray(mediaItems) && embed.originalUrl && !embed.isFile && (
          // Only external embeds (YouTube etc.) need an "open elsewhere" fallback; a
          // playable media file is already the source of truth in the native player.
          <a
            href={embed.originalUrl}
            target="_blank"
            rel="noreferrer"
            className="mt-5 inline-flex items-center gap-2 rounded-lg bg-white px-6 py-3 text-sm font-bold text-brand-800 shadow-lg shadow-black/20 transition-colors hover:bg-brand-50"
          >
            {openExternalLabel}
            <ArrowUpRight size={16} />
          </a>
        )}
      </div>
    </div>
  );

  return (
    <>
      <div className="flex flex-col sm:flex-row gap-6 items-start sm:items-center">
        <a
          href={primaryHref}
          className="bg-brand-500 hover:bg-brand-700 text-white px-8 py-3.5 rounded-lg font-medium transition-all flex items-center gap-2 shadow-lg shadow-blue-500/30"
        >
          {primaryLabel} <ArrowUpRight size={20} />
        </a>
        {showPlayButton && (
          <button
            onClick={() => setOpen(true)}
            className="text-white flex items-center gap-3 font-semibold hover:text-blue-300 transition-colors group"
          >
            {secondaryLabel}
            <span className="w-12 h-12 bg-white/10 group-hover:bg-white/20 backdrop-blur-md rounded-full flex items-center justify-center transition-all border border-white/20">
              <Play size={20} className="fill-current ml-1" />
            </span>
          </button>
        )}
      </div>
      {open && typeof document !== 'undefined' ? createPortal(modal, document.body) : null}
    </>
  );
}
