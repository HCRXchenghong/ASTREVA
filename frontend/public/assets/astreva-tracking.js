/* First-party analytics runtime (external so CSP can stay script-src 'self'). */
(() => {
  const script = document.currentScript || document.querySelector('script[data-astreva-tracking]');
  const astrevaAnalyticsBase = (script && script.dataset && script.dataset.base) || '';
      (() => {
        const CONSENT_KEY = 'astreva_cookie_consent';
        const VISITOR_KEY = 'astreva_visitor_id';
        const SESSION_KEY = 'astreva_session_id';
        const ENDPOINT = '/site-admin-api/analytics/visit';
        // Page views are always measured, in one of two modes:
        // - analytics consent granted: the original behaviour, with visitor_id/session_id;
        // - no consent: an anonymous page view only. No local identifier is generated or
        //   read (astreva_visitor_id / astreva_session_id stay untouched). The server
        //   derives a same-day anonymous key from a daily rotating salt plus the
        //   truncated IP, used only for same-day UV deduplication.
        function analyticsAllowed() {
          try {
            const raw = localStorage.getItem(CONSENT_KEY);
            if (!raw) return false;
            if (raw === 'accepted') return true;
            const parsed = JSON.parse(raw);
            return !!(parsed && parsed.analytics === true);
          } catch {
            return false;
          }
        }
        function identifier(storage, key) {
          try {
            const existing = storage.getItem(key);
            if (existing) return existing;
            const value =
              window.crypto && typeof window.crypto.randomUUID === 'function'
                ? window.crypto.randomUUID()
                : `anon-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
            storage.setItem(key, value);
            return value;
          } catch {
            return '';
          }
        }
        function endpoint() {
          const base = String(astrevaAnalyticsBase || '').replace(/\/$/, '');
          return `${base}${ENDPOINT}`;
        }
        function send(payload) {
          const body = JSON.stringify(payload);
          try {
            if (typeof navigator.sendBeacon === 'function') {
              // text/plain keeps the beacon a CORS-simple request: the public
              // endpoint answers POST/OPTIONS but does not allow credentials,
              // and a JSON content type would trigger a preflight that fails.
              const blob = new Blob([body], { type: 'text/plain;charset=UTF-8' });
              if (navigator.sendBeacon(endpoint(), blob)) return;
            }
          } catch {
            /* ignore: storage unavailable */
          }
          try {
            fetch(endpoint(), {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body,
              keepalive: true,
              mode: 'cors',
              credentials: 'omit'
            }).catch(() => {});
          } catch {
            /* ignore: storage unavailable */
          }
        }
        let pageviewSent = false;
        function sendPageview() {
          if (pageviewSent) return;
          pageviewSent = true;
          if (!analyticsAllowed()) {
            // Anonymous mode: page context only, no visitor or session identifier.
            send({
              path: window.location.pathname,
              referrer: document.referrer || '',
              host: window.location.host,
              event_type: 'pageview',
              anonymous: true
            });
            return;
          }
          send({
            path: window.location.pathname,
            referrer: document.referrer || '',
            visitor_id: identifier(localStorage, VISITOR_KEY),
            session_id: identifier(sessionStorage, SESSION_KEY),
            event_type: 'pageview',
            host: window.location.host,
            timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || ''
          });
        }
        function track(eventType, detail) {
          if (eventType === 'pageview') {
            sendPageview();
            return;
          }
          // Other events (chat_open / lead_submit) are still consent-gated.
          if (!analyticsAllowed()) return;
          const extra = detail && typeof detail === 'object' ? detail : {};
          send({
            path: typeof extra.path === 'string' && extra.path ? extra.path : window.location.pathname,
            referrer: typeof extra.referrer === 'string' && extra.referrer ? extra.referrer : document.referrer || '',
            visitor_id: identifier(localStorage, VISITOR_KEY),
            session_id: identifier(sessionStorage, SESSION_KEY),
            event_type: eventType,
            host: window.location.host,
            timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || ''
          });
        }
        window.AstrevaTracking = { track };
        // The first page view is sent immediately in whichever mode applies. If consent
        // arrives later it is intentionally NOT re-sent with identifiers: that would count
        // the same page twice in PV. Every navigation on this static site is a fresh page
        // load, so the next view is identified; this view is already covered by the
        // anonymous same-day key.
        sendPageview();
      })();
})();
