import { MessageCircle, X } from 'lucide-react';
import React, { useEffect, useMemo, useState } from 'react';

type ChatCopy = {
  title: string;
  ariaLabel: string;
  loadingText?: string;
  errorTitle?: string;
  errorDescription?: string;
  initialUnreadCount?: number;
};

export default function ChatWidget({ serviceBridgeUrl, chat }: { serviceBridgeUrl: string; chat: ChatCopy }) {
  const [isOpen, setIsOpen] = useState(false);
  const [unreadCount, setUnreadCount] = useState(Number(chat.initialUnreadCount ?? 1));
  const [loadState, setLoadState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
  const iframeUrl = useMemo(() => {
    const base = serviceBridgeUrl || import.meta.env.PUBLIC_SERVICEBRIDGE_URL || 'http://127.0.0.1:5173';
    try {
      const url = new URL(base);
      url.searchParams.set('embed', '1');
      return url.toString();
    } catch {
      return `${base.replace(/\/$/, '')}/?embed=1`;
    }
  }, [serviceBridgeUrl]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const data = event.data || {};
      if (data.type === 'servicebridge:ready') {
        setLoadState('ready');
      }
      if (data.type === 'servicebridge:unread' && !isOpen) {
        setUnreadCount((value) => value + Number(data.delta || 1));
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [isOpen]);

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
            src={iframeUrl}
            title={chat.title}
            className="w-full h-full border-0 bg-white"
            allow="microphone; camera; clipboard-write"
            onError={() => setLoadState('error')}
          />
          {loadState === 'loading' && (
            <div className="pointer-events-none absolute inset-0 grid place-items-center bg-white text-sm font-semibold text-gray-500">
              {chat.loadingText || '正在连接在线客服...'}
            </div>
          )}
          {loadState === 'error' && (
            <div className="absolute inset-0 grid place-items-center bg-white px-8 text-center">
              <div>
                <p className="mb-2 text-base font-bold text-gray-900">{chat.errorTitle || '客服窗口未加载'}</p>
                <p className="text-sm leading-relaxed text-gray-500">
                  {chat.errorDescription || '请确认 ServiceBridge 用户端服务已启动'}：{iframeUrl}
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
