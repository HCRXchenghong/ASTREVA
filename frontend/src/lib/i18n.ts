/**
 * Locale routing helpers shared by pages, views and the layout.
 *
 * Chinese (zh-CN) stays on the root path; all other locales get a real
 * crawlable prefix such as /en/ or /ar/. Nothing here fetches content, so it
 * is safe to import from client components as well.
 */
export const SITE_LOCALES = ['zh-CN', 'en', 'ar'] as const;

export type SiteLocale = (typeof SITE_LOCALES)[number];

export const DEFAULT_LOCALE: SiteLocale = 'zh-CN';

/** Locales that get a path prefix. */
export const PREFIXED_LOCALES: readonly SiteLocale[] = SITE_LOCALES.filter((locale) => locale !== DEFAULT_LOCALE);

export const OG_LOCALES: Record<SiteLocale, string> = {
  'zh-CN': 'zh_CN',
  en: 'en_US',
  ar: 'ar_SA'
};

export function isSiteLocale(value: unknown): value is SiteLocale {
  return typeof value === 'string' && (SITE_LOCALES as readonly string[]).includes(value);
}

/** Prefix a root-relative path with the locale segment (no-op for zh-CN). */
export function localePath(locale: SiteLocale, path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  if (locale === DEFAULT_LOCALE) return normalized;
  return normalized === '/' ? `/${locale}/` : `/${locale}${normalized}`;
}

const LOCALE_PREFIX = /^\/(en|ar)(?=\/|$)/;

/** Split a locale-prefixed path into its locale and root-relative path. */
export function stripLocalePrefix(path: string): { locale: SiteLocale; path: string } {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  const match = normalized.match(LOCALE_PREFIX);
  if (!match) return { locale: DEFAULT_LOCALE, path: normalized };
  const rest = normalized.slice(match[0].length);
  return { locale: match[1] as SiteLocale, path: rest ? (rest.startsWith('/') ? rest : `/${rest}`) : '/' };
}

/** Only allow navigation-safe URLs; javascript:/data:/vbscript: and //host are rejected. */
export function safeHref(href: string | undefined): string | undefined {
  if (!href) return undefined;
  const value = String(href).trim();
  if (!value || value.startsWith('//')) return undefined;
  if (/^(?:javascript|vbscript|data|file|blob):/i.test(value)) return undefined;
  if (/^(?:https?:|mailto:|tel:)/i.test(value)) return value;
  if (value.startsWith('/') || value.startsWith('#')) return value;
  return undefined;
}

/** Localize a root-relative href coming from the content model. */
export function localizeHref(locale: SiteLocale, href: string | undefined): string | undefined {
  const safe = safeHref(href);
  if (!safe || !safe.startsWith('/')) return safe;
  return localePath(locale, stripLocalePrefix(safe).path);
}

/**
 * Build the locale -> path map used by the language route controller.
 * The 404 page has no localized twins, so it falls back to each locale home.
 */
export function localeSwitchPaths(path: string, locales: readonly SiteLocale[]): Record<string, string> {
  const { path: basePath } = stripLocalePrefix(path);
  const fallback = /^\/404\/?$/.test(basePath);
  return Object.fromEntries(locales.map((locale) => [locale, localePath(locale, fallback ? '/' : basePath)]));
}
