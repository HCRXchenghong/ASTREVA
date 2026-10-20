/**
 * CMS icon vocabulary.
 *
 * The content model stores icons as short strings (mostly single glyphs such as
 * ✓ ▶ 工 滴 盾). Rendering those directly looks inconsistent: they fall back to
 * whatever font the visitor has, they scale badly and they don't share a stroke
 * weight. This module maps them to a curated vector set without touching the
 * CMS data, so editors keep the same field.
 *
 * Resolution order:
 *   1. context-specific map (the same glyph means different things per section)
 *   2. global glyph map
 *   3. semantic name (file / book / search / arrow …)
 *   4. a neutral circle fallback
 */

export type IconName =
  | 'check'
  | 'arrowRight'
  | 'arrowLeft'
  | 'download'
  | 'zap'
  | 'shieldCheck'
  | 'shield'
  | 'truck'
  | 'leaf'
  | 'droplet'
  | 'wrench'
  | 'help'
  | 'phone'
  | 'mail'
  | 'layers'
  | 'file'
  | 'box'
  | 'play'
  | 'star'
  | 'building'
  | 'circle';

/** Same glyph, different meaning depending on where it is rendered. */
const BY_CONTEXT: Record<string, Record<string, IconName>> = {
  advantage: {
    '▶': 'zap',
    '✓': 'shieldCheck',
    工: 'truck',
    滴: 'leaf',
    '◇': 'layers'
  },
  service: {
    盾: 'shieldCheck',
    工: 'wrench',
    '?': 'help',
    '☎': 'phone',
    '✉': 'mail'
  },
  // "产品说明" 里存的是「工」，但那一段是文字说明，用文档图标更准确。
  description: {
    工: 'file',
    层: 'layers'
  }
};

const BY_GLYPH: Record<string, IconName> = {
  '✓': 'check',
  '✔': 'check',
  '→': 'arrowRight',
  '←': 'arrowLeft',
  '↓': 'download',
  '▶': 'play',
  工: 'wrench',
  滴: 'droplet',
  盾: 'shieldCheck',
  '?': 'help',
  '☎': 'phone',
  '✉': 'mail',
  层: 'layers',
  文: 'file',
  '◇': 'box',
  '★': 'star'
};

const BY_NAME: Record<string, IconName> = {
  file: 'layers',
  book: 'layers',
  search: 'help',
  arrow: 'arrowRight',
  download: 'download',
  back: 'arrowLeft',
  check: 'check',
  play: 'play',
  phone: 'phone',
  mail: 'mail',
  shield: 'shieldCheck',
  truck: 'truck',
  leaf: 'leaf',
  water: 'droplet',
  building: 'building'
};

export function resolveIcon(value: string | undefined | null, context?: string): IconName {
  const raw = String(value ?? '').trim();
  if (!raw) return 'circle';
  if (context && BY_CONTEXT[context]?.[raw]) return BY_CONTEXT[context][raw];
  if (BY_GLYPH[raw]) return BY_GLYPH[raw];
  const byName = BY_NAME[raw.toLowerCase()];
  if (byName) return byName;
  return 'circle';
}
