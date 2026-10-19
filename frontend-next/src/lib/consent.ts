/**
 * Cookie consent record shared by the banner (`CookieConsent.tsx`) and the
 * consumers of optional storage (e.g. `ChatWidget.tsx`).
 *
 * Stored shape: `{"necessary":true,"analytics":true|false}` under
 * `localStorage.astreva_cookie_consent`.
 *
 * Categories:
 * - `necessary`: strictly required to deliver the site and the chat service
 *   (visitor session token, language preference). Always granted.
 * - `analytics`: optional. The public site currently loads no third-party
 *   analytics or marketing script, so nothing reads this flag yet; any future
 *   tracking script must check it and stay off while it is false.
 *
 * Backwards compatibility: the legacy plain string `'accepted'` (written by the
 * previous banner) counts as "everything accepted".
 */

export const COOKIE_CONSENT_KEY = 'astreva_cookie_consent';

export type CookieConsentValue = {
  necessary: boolean;
  analytics: boolean;
};

const LEGACY_ACCEPTED = 'accepted';

/** Reads the stored consent, or `null` when the visitor has not chosen yet. */
export function readCookieConsent(): CookieConsentValue | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = window.localStorage.getItem(COOKIE_CONSENT_KEY);
    if (!raw) return null;
    if (raw === LEGACY_ACCEPTED) return { necessary: true, analytics: true };
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return null;
    const record = parsed as Record<string, unknown>;
    return { necessary: record.necessary !== false, analytics: record.analytics === true };
  } catch {
    return null;
  }
}

/** Persists the visitor choice and notifies any listener on the page. */
export function writeCookieConsent(value: CookieConsentValue) {
  try {
    window.localStorage.setItem(COOKIE_CONSENT_KEY, JSON.stringify(value));
  } catch {
    /* storage unavailable - the banner still closes for this page view */
  }
  window.dispatchEvent(new CustomEvent('astreva:cookie-consent', { detail: value }));
}

/**
 * Only non-essential storage is gated by consent. The site never blocks
 * strictly necessary storage (it is required to provide the requested service).
 */
export function consentAllowsCategory(category: 'necessary' | 'analytics'): boolean {
  if (category === 'necessary') return true;
  return readCookieConsent()?.analytics === true;
}
