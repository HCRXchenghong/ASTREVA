import React, { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowRight, ArrowUpRight, BookOpen, ChevronDown, ChevronRight, FileText, Menu, Search, X } from 'lucide-react';
import type { Category, Product, SiteSettings } from '../lib/types';

type Props = {
  site: SiteSettings;
  categories: Category[];
  products: Product[];
  currentPath: string;
};

const LANGUAGE_ORDER = [
  { locale: 'en', flag: '/flags/us.svg' },
  { locale: 'ar', flag: '/flags/sa.svg' },
  { locale: 'zh-CN', flag: '/flags/cn.svg' }
];

function LanguageSwitcher({ site }: { site: SiteSettings }) {
  const [locale, setLocale] = useState('zh-CN');
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const labels: Record<string, string> = {
    'zh-CN': site.localization.chineseLabel,
    en: site.localization.englishLabel,
    ar: site.localization.arabicLabel
  };

  useEffect(() => {
    const onLocale = (event: Event) => setLocale(String((event as CustomEvent).detail || 'zh-CN'));
    window.addEventListener('astreva:locale', onLocale);
    const api = (window as unknown as { AstrevaLanguage?: { get?: () => string } }).AstrevaLanguage;
    if (api && api.get) setLocale(api.get() || 'zh-CN');
    return () => window.removeEventListener('astreva:locale', onLocale);
  }, []);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onPointer);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onPointer);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const current = LANGUAGE_ORDER.find((item) => item.locale === locale) || LANGUAGE_ORDER[2];
  const others = LANGUAGE_ORDER.filter((item) => item.locale !== current.locale);

  return (
    <div ref={rootRef} className="relative" data-lang-switch-root>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={labels[current.locale]}
        className="flex h-11 items-center gap-1.5 rounded-xl border-2 border-[#0a66c2] bg-white px-2.5 shadow-[0_4px_14px_rgba(10,102,194,0.35)] transition-transform hover:scale-105"
      >
        <img src={current.flag} alt="" className="h-6 w-8 rounded-[4px] shadow-[0_0_0_1px_rgba(15,23,42,0.2)]" />
        <ChevronDown size={15} className={'text-[#0a66c2] transition-transform ' + (open ? 'rotate-180' : '')} />
      </button>
      {open && (
        <div className="absolute right-0 top-[52px] z-50 flex flex-col gap-1.5 rounded-xl border-2 border-[#0a66c2] bg-white p-2 shadow-[0_12px_32px_rgba(10,77,140,0.28)]">
          {others.map((item) => (
            <button
              key={item.locale}
              type="button"
              title={labels[item.locale]}
              aria-label={labels[item.locale]}
              onClick={() => {
                setOpen(false);
                const api = (window as unknown as { AstrevaLanguage?: { switch?: (locale: string) => void } }).AstrevaLanguage;
                if (api && api.switch) api.switch(item.locale);
              }}
              className="grid h-10 w-12 place-items-center rounded-lg border-2 border-transparent transition-colors hover:border-[#0a66c2] hover:bg-[#eaf3fd]"
            >
              <img src={item.flag} alt="" className="h-5 w-7 rounded-[3px] shadow-[0_0_0_1px_rgba(15,23,42,0.2)]" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export default function Navbar({ site, categories, products, currentPath }: Props) {
  const [activeDropdown, setActiveDropdown] = useState<string | null>(null);
  const [isScrolled, setIsScrolled] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [mobileProductsOpen, setMobileProductsOpen] = useState(false);
  const sortedCategories = useMemo(() => [...categories].sort((a, b) => a.sortOrder - b.sortOrder), [categories]);
  const [hoveredCategory, setHoveredCategory] = useState(sortedCategories[0]?.slug || '');
  const navItems = useMemo(
    () => [
      { href: '/', label: site.navigation.homeLabel, key: 'home' },
      { href: '/products/', label: site.navigation.productsLabel, key: 'products', hasDropdown: true },
      { href: '/solutions/', label: site.navigation.solutionsLabel, key: 'solutions' },
      { href: '/support/', label: site.navigation.supportLabel, key: 'support' },
      { href: '/about/', label: site.navigation.aboutLabel, key: 'about' }
    ],
    [site.navigation]
  );

  useEffect(() => {
    const handleScroll = () => setIsScrolled(window.scrollY > 50);
    handleScroll();
    window.addEventListener('scroll', handleScroll);
    return () => window.removeEventListener('scroll', handleScroll);
  }, []);

  useEffect(() => {
    if (!sortedCategories.some((category) => category.slug === hoveredCategory) && sortedCategories[0]) {
      setHoveredCategory(sortedCategories[0].slug);
    }
  }, [hoveredCategory, sortedCategories]);

  const activeProducts = products
    .filter((product) => product.categorySlug === hoveredCategory)
    .sort((a, b) => a.sortOrder - b.sortOrder)
    .slice(0, 4);
  const hoveredCategoryInfo = sortedCategories.find((category) => category.slug === hoveredCategory) || sortedCategories[0];

  const isActive = (item: (typeof navItems)[number]) => {
    if (item.href === '/') return currentPath === '/';
    return currentPath.startsWith(item.href);
  };

  const utilityIconMap = { file: FileText, book: BookOpen, search: Search, arrow: ArrowRight };
  const productMenu = site.navigation.productMenu;
  const popularSeriesTitle = productMenu.popularSeriesTitleTemplate.replace('{category}', hoveredCategoryInfo?.name || '');
  const viewAllCategoryLabel = productMenu.viewAllCategoryLabelTemplate.replace('{category}', hoveredCategoryInfo?.name || '');

  return (
    <div className={`fixed left-0 top-0 z-50 w-full transition-all duration-300 ${isScrolled ? 'bg-white shadow-md' : 'bg-white/95 shadow-sm backdrop-blur-sm'}`}>
      <div className="mx-auto flex h-24 max-w-7xl items-center justify-between px-7">
        <a className="flex items-center gap-3" href="/">
          {site.logoUrl ? (
            <img src={site.logoUrl} alt={site.brandName} className="h-[58px] w-[58px] object-contain" />
          ) : (
            <span role="img" aria-label={site.brandName} className="grid h-[58px] w-[58px] place-items-center rounded-xl bg-[#0a4d8c] text-xl font-black text-white">{site.brandName.slice(0, 1)}</span>
          )}
          <div className="flex flex-col">
            <span className="text-2xl font-extrabold leading-tight tracking-wide text-[#0a4d8c]">{site.brandName}</span>
            <span className="text-xs font-semibold uppercase tracking-widest text-gray-500">{site.brandTagline}</span>
          </div>
        </a>

        <div className="hidden h-full items-center gap-10 md:flex">
          {navItems.map((item) => (
            <div
              key={item.href}
              className="relative flex h-full items-center"
              onMouseEnter={() => item.hasDropdown && setActiveDropdown(item.key)}
              onMouseLeave={() => item.hasDropdown && setActiveDropdown(null)}
            >
              <a
                href={item.href}
                onFocus={() => item.hasDropdown && setActiveDropdown(item.key)}
                className={`flex items-center gap-1.5 text-[17px] font-semibold transition-colors ${isActive(item) ? 'text-[#0a5ca8]' : 'text-gray-700 hover:text-[#0a5ca8]'}`}
              >
                {item.label}
                {item.hasDropdown && <ChevronRight size={17} className={`transition-transform ${activeDropdown === item.key ? 'rotate-90' : 'rotate-0'}`} />}
              </a>

              {item.hasDropdown && activeDropdown === 'products' && (
                <div className="fixed left-0 top-24 w-screen overflow-hidden rounded-b-lg border-t border-[#d8dee7] bg-white shadow-[0_24px_60px_rgba(18,32,54,0.16)]">
                  <div className="border-b border-[#e1e6ed] bg-[#f7f9fb]">
                    <div className="mx-auto flex max-w-7xl items-center gap-9 px-7">
                      <span className="shrink-0 py-5 text-xs font-black uppercase tracking-[0.18em] text-[#7a8493]">{productMenu.categoryLabel}</span>
                      <div className="flex min-w-0 flex-1 items-center gap-8 overflow-x-auto">
                        {sortedCategories.map((category) => {
                          const active = hoveredCategory === category.slug;
                          return (
                            <a
                              key={category.slug}
                              href={`/products/${category.slug}/`}
                              onMouseEnter={() => setHoveredCategory(category.slug)}
                              onFocus={() => setHoveredCategory(category.slug)}
                              className={`relative shrink-0 py-5 text-[15px] font-black transition-colors after:absolute after:inset-x-0 after:bottom-0 after:h-[3px] after:bg-[#0a66c2] after:transition-opacity ${
                                active ? 'text-[#0a5ca8] after:opacity-100' : 'text-[#3d4755] after:opacity-0 hover:text-[#0a5ca8]'
                              }`}
                            >
                              {category.name}
                            </a>
              );
            })}
          </div>
        </div>

                  </div>

                  <div className="mx-auto max-w-7xl px-7 py-7">
                    <div className="mb-5 flex items-end justify-between gap-6">
                      <div>
                        <p className="text-[11px] font-black uppercase tracking-[0.2em] text-[#b88923]">{productMenu.popularSeriesLabel}</p>
                        <h3 className="mt-1.5 text-xl font-black text-[#172033]">{popularSeriesTitle}</h3>
                      </div>
                      {hoveredCategoryInfo && (
                        <a href={`/products/${hoveredCategoryInfo.slug}/`} className="inline-flex items-center gap-2 text-sm font-black text-[#0a66c2] hover:text-[#084b8c]">
                          {viewAllCategoryLabel} <ArrowRight size={16} />
                        </a>
                      )}
                    </div>

                    <div className="grid grid-cols-4 divide-x divide-[#e1e6ed] border-y border-[#e1e6ed]">
                      {activeProducts.map((product) => (
                        <a
                          key={product.id}
                          href={`/products/${product.categorySlug}/${product.slug}/`}
                          className="group flex min-w-0 items-center gap-4 px-4 py-4 first:pl-0 last:pr-0"
                        >
                          {product.coverImage ? (
                            <img src={product.coverImage} alt="" className="h-16 w-20 shrink-0 rounded-lg object-cover" />
                          ) : (
                            <span aria-hidden="true" className="grid h-16 w-20 shrink-0 place-items-center rounded-lg bg-gradient-to-br from-slate-100 to-slate-300 text-2xl font-black text-slate-400">◇</span>
                          )}
                          <span className="min-w-0 flex-1 text-[15px] font-black leading-6 text-[#303a4b] transition-colors group-hover:text-[#0a5ca8]">{product.name}</span>
                          <ArrowRight size={16} className="shrink-0 text-[#9aa5b4] transition-all group-hover:translate-x-1 group-hover:text-[#0a66c2]" />
                        </a>
                      ))}
                    </div>
                  </div>

                  <div className="border-t border-[#e1e6ed] bg-[#f7f9fb]">
                    <div className="mx-auto grid max-w-7xl grid-cols-4 divide-x divide-[#e1e6ed] px-7">
                      {productMenu.utilityLinks.map(({ label, href, icon }) => {
                        const Icon = utilityIconMap[icon as keyof typeof utilityIconMap] || ArrowRight;
                        return <a key={`${label}-${href}`} href={href} className="group flex items-center justify-center gap-3 px-5 py-4 text-sm font-black text-[#303a4b] transition-colors hover:bg-[#edf5ff] hover:text-[#0a5ca8]">
                          <Icon size={17} className="text-[#0a66c2]" />
                          {label}
                          <ArrowRight size={14} className="text-[#0a66c2] opacity-70 transition-transform group-hover:translate-x-1 group-hover:opacity-100" />
                        </a>;
                      })}
                    </div>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>

        <div className="flex items-center gap-3 md:gap-5">
          <LanguageSwitcher site={site} />

        <a href={`tel:${site.phone}`} className="hidden items-center gap-2.5 rounded-lg bg-[#0a66c2] px-7 py-3 text-[17px] font-medium text-white shadow-lg shadow-blue-500/30 transition-colors hover:bg-[#084b8c] md:flex">
          {site.phone} <ArrowUpRight size={19} />
        </a>

        <button className="text-gray-800 md:hidden" onClick={() => setMobileOpen((value) => !value)} aria-label={site.navigation.mobileMenuLabel} aria-expanded={mobileOpen}>
          {mobileOpen ? <X size={34} /> : <Menu size={34} />}
        </button>
        </div>
      </div>

      {mobileOpen && (
        <div className="max-h-[calc(100vh-6rem)] overflow-y-auto border-t border-gray-100 bg-white px-7 py-5 shadow-xl md:hidden">
          <div className="flex flex-col gap-1">
            {navItems.map((item) =>
              item.hasDropdown ? (
                <div key={item.href} className="border-b border-[#e5e9ef] pb-2">
                  <div className="flex items-center justify-between">
                    <a href={item.href} className={`py-3 text-lg font-bold ${isActive(item) ? 'text-[#0a66c2]' : 'text-gray-700'}`}>
                      {item.label}
                    </a>
                    <button
                      type="button"
                      onClick={() => setMobileProductsOpen((value) => !value)}
                      className="rounded-lg p-2 text-[#536173]"
                      aria-label={productMenu.mobileToggleLabel}
                      aria-expanded={mobileProductsOpen}
                    >
                      <ChevronRight size={20} className={`transition-transform ${mobileProductsOpen ? 'rotate-90' : ''}`} />
                    </button>
                  </div>
                  {mobileProductsOpen && (
                    <div className="mb-2 grid grid-cols-1 gap-1 border-l-2 border-[#0a66c2] pl-4">
                      {sortedCategories.map((category) => (
                        <a key={category.slug} href={`/products/${category.slug}/`} className="py-2 text-[15px] font-bold text-[#536173] hover:text-[#0a5ca8]">
                          {category.name}
                        </a>
                      ))}
                    </div>
                  )}
                </div>
              ) : (
                <a key={item.href} href={item.href} className={`border-b border-[#e5e9ef] py-3 text-lg font-bold ${isActive(item) ? 'text-[#0a66c2]' : 'text-gray-700'}`}>
                  {item.label}
                </a>
              )
            )}
            <a href={`tel:${site.phone}`} className="mt-4 rounded-lg bg-[#0a66c2] px-6 py-3.5 text-center text-lg font-bold text-white">
              {site.phone}
            </a>
          </div>
        </div>
      )}
    </div>
  );
}
