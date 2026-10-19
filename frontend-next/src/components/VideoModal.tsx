import { ArrowUpRight, Play, X } from 'lucide-react';
import React, { useState } from 'react';
import { createPortal } from 'react-dom';
import { resolveVideoSource } from '../lib/video';

type Props = {
  primaryLabel: string;
  primaryHref: string;
  secondaryLabel: string;
  videoUrl?: string;
  videoTitle: string;
  previewLabel: string;
  openExternalLabel: string;
  closeLabel: string;
};

export default function VideoModal({ primaryLabel, primaryHref, secondaryLabel, videoUrl, videoTitle, previewLabel, openExternalLabel, closeLabel }: Props) {
  const [open, setOpen] = useState(false);
  const embed = resolveVideoSource(videoUrl);
  const modal = (
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/95 p-4 backdrop-blur-sm"
      aria-modal="true"
      role="dialog"
    >
      <button
        onClick={() => setOpen(false)}
        className="fixed right-5 top-5 z-[10000] flex h-12 w-12 items-center justify-center rounded-full border border-white/15 bg-white/10 text-white shadow-2xl backdrop-blur-md transition-colors hover:bg-white/20 md:right-8 md:top-8"
        aria-label={closeLabel}
      >
        <X size={30} />
      </button>
      <div className="flex w-full flex-col items-center">
        <div
          className={`w-full bg-black rounded-xl overflow-hidden border border-gray-800 flex items-center justify-center ${
            embed.isShorts ? 'max-w-[420px] max-h-[85vh] aspect-[9/16]' : 'max-w-5xl aspect-video'
          }`}
        >
          {embed.isFile ? (
            // Uploaded mp4/webm files (or a direct file URL in the CMS) must play through a
            // native <video>; putting them in an iframe only yields a shrunken default player.
            <video
              src={embed.url}
              title={videoTitle}
              controls
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
            <p className="text-gray-500 flex flex-col items-center gap-4">
              <Play size={48} />
              <span>{previewLabel}</span>
            </p>
          )}
        </div>
        {embed.originalUrl && !embed.isFile && (
          // Only external embeds (YouTube etc.) need an "open elsewhere" fallback; a
          // playable media file is already the source of truth in the native player.
          <a
            href={embed.originalUrl}
            target="_blank"
            rel="noreferrer"
            className="mt-5 inline-flex items-center gap-2 rounded-lg bg-white px-6 py-3 text-sm font-bold text-brand-800 shadow-lg shadow-black/20 transition-colors hover:bg-blue-50"
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
        <a href={primaryHref} className="bg-brand-500 hover:bg-brand-700 text-white px-8 py-3.5 rounded-lg font-medium transition-all flex items-center gap-2 shadow-lg shadow-blue-500/30">
          {primaryLabel} <ArrowUpRight size={20} />
        </a>
        <button onClick={() => setOpen(true)} className="text-white flex items-center gap-3 font-semibold hover:text-blue-300 transition-colors group">
          {secondaryLabel}
          <span className="w-12 h-12 bg-white/10 group-hover:bg-white/20 backdrop-blur-md rounded-full flex items-center justify-center transition-all border border-white/20">
            <Play size={20} className="fill-current ml-1" />
          </span>
        </button>
      </div>
      {open && typeof document !== 'undefined' ? createPortal(modal, document.body) : null}
    </>
  );
}
