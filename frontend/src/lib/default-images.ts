import type { Category, Product } from './types';
import { firstMediaImage, normalizeMediaItems } from './media';

const ROOT = '/assets/placeholders';

export const DEFAULT_SCENE_IMAGES = {
  homeHero: `${ROOT}/scenes/home-hero.webp`,
  homeProductIntro: `${ROOT}/scenes/home-product-intro.webp`,
  installationGuide: `${ROOT}/scenes/installation-guide.webp`,
  municipal: `${ROOT}/scenes/municipal.webp`,
  commercial: `${ROOT}/scenes/commercial.webp`,
  industrial: `${ROOT}/scenes/industrial.webp`,
  landscape: `${ROOT}/scenes/landscape.webp`,
  ecological: `${ROOT}/scenes/ecological.webp`
} as const;

export function sceneImages(name: keyof typeof DEFAULT_SCENE_IMAGES): string[] {
  const primary = DEFAULT_SCENE_IMAGES[name];
  const filename = primary.split('/').pop() || '';
  const stem = filename.replace(/\.[^.]+$/, '');
  return [primary, `${ROOT}/scenes/${stem}-alternate.webp`];
}

export const DEFAULT_PRODUCT_IMAGES: Record<string, string> = {
  'sealed-floor-drain-housing': `${ROOT}/products/sealed-floor-drain-housing.jpg`,
  'sealed-floor-drain-strainer': `${ROOT}/products/sealed-floor-drain-strainer.jpg`,
  'slot-drain-cover': `${ROOT}/products/slot-drain-cover.jpg`,
  'laser-cut-perforated-drain-cover': `${ROOT}/products/laser-cut-perforated-drain-cover.jpg`,
  'bar-grating-drain-cover': `${ROOT}/products/bar-grating-drain-cover.jpg`,
  'stamped-drawn-drain-cover': `${ROOT}/products/stamped-drawn-drain-cover.jpg`,
  'trench-drain-cover': `${ROOT}/products/trench-drain-cover.jpg`,
  'heavy-modular-stainless-drain': `${ROOT}/products/heavy-modular-stainless-drain.webp`,
  'high-load-custom-municipal-grate': `${ROOT}/products/high-load-custom-municipal-grate.webp`,
  'invisible-slot-resin-drainage-system': `${ROOT}/products/invisible-slot-resin-drainage-system.webp`,
  'landscape-rainwater-collector': `${ROOT}/products/landscape-rainwater-collector.webp`,
  'anti-corrosion-industrial-stainless-channel': `${ROOT}/products/anti-corrosion-industrial-stainless-channel.webp`,
  'industrial-heavy-duty-manhole-cover': `${ROOT}/products/industrial-heavy-duty-manhole-cover.webp`,
  'custom-curved-floor-drainage-system': `${ROOT}/products/custom-curved-floor-drainage-system.webp`,
  'ecological-permeable-materials-and-inspection-well': `${ROOT}/products/ecological-permeable-materials-and-inspection-well.webp`
};

export function productImages(product: Pick<Product, 'slug' | 'categorySlug'>): string[] {
  const primary = DEFAULT_PRODUCT_IMAGES[product.slug] || DEFAULT_CATEGORY_IMAGES[product.categorySlug] || DEFAULT_SCENE_IMAGES.industrial;
  const alternate = DEFAULT_PRODUCT_IMAGES[product.slug]
    ? `${ROOT}/products/${product.slug}-alternate.webp`
    : `${ROOT}/scenes/industrial-alternate.webp`;
  return [primary, alternate];
}

export function categoryImages(category: Pick<Category, 'slug'>): string[] {
  const primary = DEFAULT_CATEGORY_IMAGES[category.slug] || DEFAULT_SCENE_IMAGES.homeHero;
  const mapped = Object.entries(DEFAULT_PRODUCT_IMAGES).find(([, image]) => image === primary);
  const scene = Object.entries(DEFAULT_SCENE_IMAGES).find(([, image]) => image === primary);
  return [primary, mapped ? `${ROOT}/products/${mapped[0]}-alternate.webp` : `${ROOT}/scenes/${scene ? primary.split('/').pop()?.replace(/\.[^.]+$/, '') : 'industrial'}-alternate.webp`];
}

export function pageHeroImages(page: string): string[] {
  const primary = DEFAULT_PAGE_HERO_IMAGES[page] || DEFAULT_SCENE_IMAGES.homeHero;
  const mapped = Object.entries(DEFAULT_SCENE_IMAGES).find(([, image]) => image === primary);
  return [primary, `${ROOT}/scenes/${mapped ? primary.split('/').pop()?.replace(/\.[^.]+$/, '') : 'home-hero'}-alternate.webp`];
}

export const DEFAULT_CATEGORY_IMAGES: Record<string, string> = {
  'water-seal-floor-drains': DEFAULT_PRODUCT_IMAGES['sealed-floor-drain-housing'],
  'drain-covers': DEFAULT_PRODUCT_IMAGES['laser-cut-perforated-drain-cover'],
  'channel-drains': DEFAULT_PRODUCT_IMAGES['trench-drain-cover'],
  'grease-traps': DEFAULT_SCENE_IMAGES.industrial,
  'settling-tanks': DEFAULT_SCENE_IMAGES.ecological,
  'simple-floor-drains': DEFAULT_PRODUCT_IMAGES['sealed-floor-drain-strainer'],
  'concealed-stainless-manhole-covers': `${ROOT}/products/industrial-heavy-duty-manhole-cover.webp`,
  'municipal-public-works': DEFAULT_SCENE_IMAGES.municipal,
  'commercial-premium-residential': DEFAULT_SCENE_IMAGES.commercial,
  'industrial-warehouse': DEFAULT_SCENE_IMAGES.industrial,
  'landscape-special-drainage': DEFAULT_SCENE_IMAGES.landscape
};

export const DEFAULT_SOLUTION_IMAGES: Record<string, string> = {
  'sol-sec-1': DEFAULT_SCENE_IMAGES.municipal,
  'sol-sec-2': DEFAULT_SCENE_IMAGES.commercial,
  'sol-sec-3': DEFAULT_SCENE_IMAGES.industrial,
  'sol-sec-4': DEFAULT_SCENE_IMAGES.landscape,
  'sol-sec-5': DEFAULT_SCENE_IMAGES.ecological
};

export const DEFAULT_PAGE_HERO_IMAGES: Record<string, string> = {
  'about-manufacturing': DEFAULT_SCENE_IMAGES.industrial,
  'about-quality': DEFAULT_SCENE_IMAGES.commercial,
  about: DEFAULT_SCENE_IMAGES.commercial,
  faq: DEFAULT_SCENE_IMAGES.homeHero,
  warranty: DEFAULT_SCENE_IMAGES.industrial,
  install: DEFAULT_SCENE_IMAGES.installationGuide,
  privacy: DEFAULT_SCENE_IMAGES.homeHero,
  agreement: DEFAULT_SCENE_IMAGES.homeHero,
  solutions: DEFAULT_SCENE_IMAGES.municipal,
  support: DEFAULT_SCENE_IMAGES.industrial
};

export function resolveImage(configured: string | null | undefined, fallback: string): string {
  return configured?.trim() || fallback;
}

export function productImage(product: Pick<Product, 'slug' | 'categorySlug' | 'coverImage' | 'mediaItems'>): string {
  if (Array.isArray(product.mediaItems)) return firstMediaImage(normalizeMediaItems(product.mediaItems), productImages(product)[0]);
  return resolveImage(
    product.coverImage,
    DEFAULT_PRODUCT_IMAGES[product.slug] ||
      DEFAULT_CATEGORY_IMAGES[product.categorySlug] ||
      DEFAULT_SCENE_IMAGES.industrial
  );
}

export function categoryImage(category: Pick<Category, 'slug' | 'coverImage' | 'mediaItems'>): string {
  if (Array.isArray(category.mediaItems)) return firstMediaImage(normalizeMediaItems(category.mediaItems), categoryImages(category)[0]);
  return resolveImage(category.coverImage, DEFAULT_CATEGORY_IMAGES[category.slug] || DEFAULT_SCENE_IMAGES.homeHero);
}
