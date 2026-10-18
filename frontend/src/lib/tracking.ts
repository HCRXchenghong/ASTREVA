/**
 * First-party analytics bridge used by the React islands (chat widget and
 * contact form). The actual tracker is installed by the inline script in
 * `BaseLayout.astro`, which owns the consent gate and the visitor identifiers;
 * this module only forwards events to it.
 */

export type TrackingEventType = 'pageview' | 'chat_open' | 'lead_submit';

export type TrackingDetail = {
  path?: string;
  referrer?: string;
};

type TrackerHost = {
  track?: (eventType: TrackingEventType, detail?: TrackingDetail) => void;
};

export function trackEvent(eventType: TrackingEventType, detail?: TrackingDetail): void {
  if (typeof window === 'undefined') return;
  const host = (window as unknown as { AstrevaTracking?: TrackerHost }).AstrevaTracking;
  if (host && typeof host.track === 'function') {
    try {
      host.track(eventType, detail);
    } catch {
      /* analytics must never break the page */
    }
  }
}
