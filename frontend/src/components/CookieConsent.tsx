import React, { useEffect, useState } from 'react';
import { readCookieConsent, writeCookieConsent } from '../lib/consent';

type CookieCopy = {
  title: string;
  text: string;
  privacyLabel: string;
  suffixText: string;
  acceptAllLabel: string;
  necessaryLabel: string;
};

/** Marker attribute used by the footer "cookie settings" entry. */
const COOKIE_SETTINGS_TRIGGER = '[data-cookie-settings]';

export default function CookieConsent({
  cookie,
  privacyHref = '/privacy/'
}: {
  cookie: CookieCopy;
  privacyHref?: string;
}) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    setVisible(readCookieConsent() === null);

    // The footer entry is server-rendered by the Astro layout, so the banner
    // listens for it instead of relying on shared component state.
    const reopen = () => setVisible(true);
    const onClick = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (target?.closest?.(COOKIE_SETTINGS_TRIGGER)) {
        event.preventDefault();
        reopen();
      }
    };

    document.addEventListener('click', onClick);
    window.addEventListener('astreva:cookie-settings', reopen);
    return () => {
      document.removeEventListener('click', onClick);
      window.removeEventListener('astreva:cookie-settings', reopen);
    };
  }, []);

  if (!visible) return null;

  const choose = (analytics: boolean) => {
    writeCookieConsent({ necessary: true, analytics });
    setVisible(false);
  };

  return (
    <div
      id="cookie-consent"
      role="dialog"
      aria-label={cookie.title}
      className="fixed bottom-0 left-0 w-full z-[120] bg-white border-t border-gray-200 shadow-[0_-10px_40px_rgba(0,0,0,0.08)]"
    >
      <div className="max-w-7xl mx-auto px-6 py-5 flex flex-col md:flex-row items-center justify-between gap-6">
        <div className="text-gray-600 text-sm leading-relaxed flex-1">
          <strong className="text-gray-900 font-bold mr-2 text-base">{cookie.title}</strong>
          {cookie.text}
          <a href={privacyHref} className="text-brand-500 hover:text-brand-700 hover:underline mx-1 font-semibold">
            {cookie.privacyLabel}
          </a>
          {cookie.suffixText}
        </div>
        <div className="flex shrink-0 gap-3 w-full md:w-auto">
          <button
            type="button"
            onClick={() => choose(true)}
            className="flex-1 md:flex-none bg-brand-500 text-white px-8 py-2.5 rounded-sm text-sm font-bold hover:bg-brand-700 transition-colors"
          >
            {cookie.acceptAllLabel}
          </button>
          <button
            type="button"
            onClick={() => choose(false)}
            className="flex-1 md:flex-none bg-gray-100 border border-gray-200 text-gray-700 px-6 py-2.5 rounded-sm text-sm font-bold hover:bg-gray-200 transition-colors"
          >
            {cookie.necessaryLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
