import React, { useEffect, useState } from 'react';

export default function CookieConsent({ cookie }: { cookie: { title: string; text: string; privacyLabel: string; suffixText: string; acceptAllLabel: string; necessaryLabel: string } }) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    setVisible(window.localStorage.getItem('astreva_cookie_consent') !== 'accepted');
  }, []);

  if (!visible) return null;

  const accept = () => {
    window.localStorage.setItem('astreva_cookie_consent', 'accepted');
    setVisible(false);
  };

  return (
    <div id="cookie-consent" className="fixed bottom-0 left-0 w-full z-[120] bg-white border-t border-gray-200 shadow-[0_-10px_40px_rgba(0,0,0,0.08)]">
      <div className="max-w-7xl mx-auto px-6 py-5 flex flex-col md:flex-row items-center justify-between gap-6">
        <div className="text-gray-600 text-sm leading-relaxed flex-1">
          <strong className="text-gray-900 font-bold mr-2 text-base">{cookie.title}</strong>
          {cookie.text}
          <a href="/privacy/" className="text-[#0a66c2] hover:text-[#084b8c] hover:underline mx-1 font-semibold">
            {cookie.privacyLabel}
          </a>
          {cookie.suffixText}
        </div>
        <div className="flex shrink-0 gap-3 w-full md:w-auto">
          <button onClick={accept} className="flex-1 md:flex-none bg-[#0a66c2] text-white px-8 py-2.5 rounded-sm text-sm font-bold hover:bg-[#084b8c] transition-colors">
            {cookie.acceptAllLabel}
          </button>
          <button onClick={accept} className="flex-1 md:flex-none bg-gray-100 border border-gray-200 text-gray-700 px-6 py-2.5 rounded-sm text-sm font-bold hover:bg-gray-200 transition-colors">
            {cookie.necessaryLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
