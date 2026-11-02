const EMPTY_GEO = Object.freeze({ country: '', region: '', city: '', timezone: '' });

function headerValue(headers, names) {
  for (const name of names) {
    const value = headers?.[name.toLowerCase()] ?? headers?.[name];
    if (Array.isArray(value) && value.length) return String(value[0]);
    if (typeof value === 'string' && value.trim()) return value;
  }
  return '';
}

function cleanText(value, maxLength) {
  let text = String(value || '').replace(/[\u0000-\u001f\u007f]/g, '').trim();
  try {
    text = decodeURIComponent(text);
  } catch {
    // Keep the original header when it is not valid percent-encoding.
  }
  return text.replace(/[\u0000-\u001f\u007f]/g, '').trim().slice(0, maxLength);
}

/** Read geolocation headers only when the app is configured behind a trusted proxy. */
export function geolocationFromRequest(req, { trustedProxy = false } = {}) {
  if (!trustedProxy) return { ...EMPTY_GEO };
  const headers = req?.headers || {};
  const rawCountry = cleanText(
    headerValue(headers, ['cf-ipcountry', 'x-vercel-ip-country', 'x-country-code', 'x-geo-country']),
    8
  ).toUpperCase();
  const country = /^[A-Z]{2}$/.test(rawCountry) ? rawCountry : '';
  const region = cleanText(
    headerValue(headers, ['cf-region', 'x-vercel-ip-country-region', 'x-geo-region', 'x-appengine-region']),
    120
  );
  const city = cleanText(
    headerValue(headers, ['cf-ipcity', 'x-vercel-ip-city', 'x-geo-city', 'x-appengine-city']),
    120
  );
  const candidateTimezone = cleanText(
    headerValue(headers, ['cf-timezone', 'x-vercel-ip-timezone', 'x-geo-timezone']),
    80
  );
  let timezone = '';
  if (candidateTimezone) {
    try {
      new Intl.DateTimeFormat('en-US', { timeZone: candidateTimezone });
      timezone = candidateTimezone;
    } catch {
      timezone = '';
    }
  }
  return { country, region, city, timezone };
}
