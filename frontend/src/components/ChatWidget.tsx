import { MessageCircle, X } from 'lucide-react';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { readCookieConsent } from '../lib/consent';

type ChatCopy = {
  title: string;
  ariaLabel: string;
  loadingText: string;
  errorTitle: string;
  errorDescription: string;
  initialUnreadCount: number;
};

// The visitor session token is held by the host page (first-party) localStorage:
// the chat widget is a cross-site iframe (api.astreva.ltd) whose own storage is
// blocked by Safari ITP / privacy modes, losing the session on refresh. Scripts
// running in the host page can read/write first-party storage without restriction.
const VISITOR_SESSION_KEY = 'customer_service_visitor_session_v1';

type VisitorSessionPayload = {
  visitorToken: string;
  conversationId: string;
  sessionType: string;
  savedAt: number;
};

// Necessary storage is always allowed: persisting the visitor session token is
// required to provide the requested chat service (and to survive refreshes).
// The consent record is still parsed here so that optional categories remain
// opt-in; the public site currently ships no third-party analytics script, but
// any future one must gate on `consentAllowsCategory('analytics')`.
function consentAllowsStorage(): boolean {
  const consent = readCookieConsent();
  return consent ? consent.necessary : true;
}

function sanitizeVisitorSession(data: unknown): VisitorSessionPayload | null {
  if (!data || typeof data !== 'object') return null;
  const record = data as Record<string, unknown>;
  const visitorToken = String(record.visitorToken || '').trim();
  const conversationId = String(record.conversationId || '').trim();
  if (!visitorToken || !conversationId) return null;
  return {
    visitorToken,
    conversationId,
    sessionType: String(record.sessionType || ''),
    savedAt: Number(record.savedAt || 0),
  };
}

export default function ChatWidget({ serviceBridgeUrl, chat, lang }: { serviceBridgeUrl: string; chat: ChatCopy; lang?: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [unreadCount, setUnreadCount] = useState(Number(chat.initialUnreadCount));
  const [loadState, setLoadState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
  const [siteLocale, setSiteLocale] = useState<string>(() => {
    if (lang) return lang;
    return typeof document !== 'undefined' ? document.documentElement.lang || 'zh-CN' : 'zh-CN';
  });
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const envBridgeUrl = (import.meta.env.PUBLIC_SERVICEBRIDGE_URL || '').trim().replace(/\/$/, '');

  const normalizeBridgeUrl = (value: string) => {
    const candidate = `${value || ''}`.trim().replace(/\/$/, '');
    if (!candidate) return '';
    try {
      const url = new URL(candidate);
      return url.protocol === 'http:' || url.protocol === 'https:' ? url.toString().replace(/\/$/, '') : '';
    } catch {
      return '';
    }
  };

  const iframeUrl = useMemo(() => {
    const base =
      normalizeBridgeUrl(serviceBridgeUrl) || normalizeBridgeUrl(envBridgeUrl) || window.location.origin;
    try {
      const url = new URL(base);
      if (url.hostname === "localhost" || url.hostname === "127.0.0.1" || url.hostname === "::1") {
        url.hostname = window.location.hostname;
      }
      url.searchParams.set('embed', '1');
      const lang = String(siteLocale || '').toLowerCase().startsWith('ar') ? 'ar' : String(siteLocale || '').toLowerCase().startsWith('en') ? 'en' : 'zh-CN';
      url.searchParams.set('lang', lang);
      return url.toString();
    } catch {
      return `${base.replace(/\/$/, '')}/?embed=1`;
    }
  }, [serviceBridgeUrl, envBridgeUrl, siteLocale]);

  useEffect(() => {
    const onLocale = (event: Event) => {
      const detail = (event as CustomEvent).detail;
      setSiteLocale(typeof detail === 'string' && detail ? detail : document.documentElement.lang || 'zh-CN');
    };
    window.addEventListener('astreva:locale', onLocale);
    return () => window.removeEventListener('astreva:locale', onLocale);
  }, []);

  useEffect(() => {
    const expectedOrigin = new URL(iframeUrl).origin;
    const onMessage = (event: MessageEvent) => {
      if (event.origin !== expectedOrigin || event.source !== iframeRef.current?.contentWindow) return;
      const data = event.data || {};
      if (data.type === 'servicebridge:ready') {
        setLoadState('ready');
      }
      if (data.type === 'servicebridge:conversation-closed') {
        setIsOpen(false);
        setUnreadCount(0);
        setLoadState('idle');
        return;
      }
      if (data.type === 'servicebridge:unread' && !isOpen) {
        setUnreadCount((value) => value + Number(data.delta || 1));
      }
      // === first-party session storage bridge ===
      if (data.type === 'servicebridge:session-get') {
        let session: VisitorSessionPayload | null = null;
        if (consentAllowsStorage()) {
          try {
            const raw = window.localStorage.getItem(VISITOR_SESSION_KEY);
            session = raw ? sanitizeVisitorSession(JSON.parse(raw)) : null;
          } catch {
            session = null;
          }
        }
        event.source?.postMessage({ type: 'servicebridge:session-restore', session }, event.origin);
        return;
      }
      if (data.type === 'servicebridge:session-put') {
        if (!consentAllowsStorage()) return;
        const session = sanitizeVisitorSession(data.session);
        if (!session) return;
        try {
          window.localStorage.setItem(VISITOR_SESSION_KEY, JSON.stringify(session));
        } catch {
          /* storage unavailable - ignore */
        }
        return;
      }
      if (data.type === 'servicebridge:session-clear') {
        try {
          window.localStorage.removeItem(VISITOR_SESSION_KEY);
        } catch {
          /* ignore */
        }
        return;
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [iframeUrl, isOpen]);

  useEffect(() => {
    if (!isOpen) {
      setLoadState('idle');
      return undefined;
    }
    setLoadState('loading');
    const timer = window.setTimeout(() => {
      setLoadState((value) => (value === 'ready' ? value : 'error'));
    }, 6000);
    return () => window.clearTimeout(timer);
  }, [isOpen, iframeUrl]);

  const toggleChat = () => {
    setIsOpen((value) => {
      const next = !value;
      if (next) setUnreadCount(0);
      return next;
    });
  };

  return (
    <div className="fixed bottom-6 right-6 z-[100] flex flex-col items-end">
      {isOpen && (
        <div className="relative w-[360px] max-w-[calc(100vw-32px)] h-[620px] max-h-[calc(100vh-112px)] bg-white rounded-2xl shadow-2xl mb-4 overflow-hidden border border-gray-100">
          <iframe
            ref={iframeRef}
            src={iframeUrl}
            title={chat.title}
            className="w-full h-full border-0 bg-white"
            allow="microphone; camera; clipboard-write"
            onError={() => setLoadState('error')}
          />
          {loadState === 'loading' && (
            <div className="pointer-events-none absolute inset-0 grid place-items-center bg-white text-sm font-semibold text-gray-500">
              {chat.loadingText}
            </div>
          )}
          {loadState === 'error' && (
            <div className="absolute inset-0 grid place-items-center bg-white px-8 text-center">
              <div>
                <p className="mb-2 text-base font-bold text-gray-900">{chat.errorTitle}</p>
                <p className="text-sm leading-relaxed text-gray-500">
                  {chat.errorDescription}：{iframeUrl}
                </p>
              </div>
            </div>
          )}
        </div>
      )}
      <button
        onClick={toggleChat}
        className="relative w-14 h-14 bg-[#0a66c2] text-white rounded-full flex items-center justify-center shadow-[0_8px_30px_rgb(0,0,0,0.3)] hover:bg-[#084b8c] hover:scale-105 transition-all"
        aria-label={chat.ariaLabel}
      >
        {isOpen ? <X size={26} /> : <MessageCircle size={26} className="fill-current text-white" />}
        {!isOpen && unreadCount > 0 && (
          <span className="absolute -top-1 -right-1 flex h-5 w-5 items-center justify-center rounded-full bg-red-500 border-2 border-white text-[10px] font-bold text-white z-10 shadow-sm">
            {unreadCount}
          </span>
        )}
      </button>
    </div>
  );
}
