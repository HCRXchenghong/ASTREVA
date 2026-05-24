import React, { useEffect, useMemo, useState } from 'react';
import { ArrowRight, ArrowUpRight, ChevronRight, Menu, X } from 'lucide-react';
import type { Category, Product, SiteSettings } from '../lib/types';

type Props = {
  site: SiteSettings;
  categories: Category[];
  products: Product[];
  currentPath: string;
};

export default function Navbar({ site, categories, products, currentPath }: Props) {
  const [activeDropdown, setActiveDropdown] = useState<string | null>(null);
  const [isScrolled, setIsScrolled] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const sortedCategories = useMemo(() => [...categories].sort((a, b) => a.sortOrder - b.sortOrder), [categories]);
  const [hoveredCategory, setHoveredCategory] = useState(sortedCategories[0]?.slug || '');
  const navItems = useMemo(
    () => [
      { href: '/', label: site.navigation.homeLabel, key: 'home' },
      { href: '/products/', label: site.navigation.productsLabel, key: 'products', hasDropdown: true },
      { href: '/solutions/', label: site.navigation.solutionsLabel, key: 'solutions' },
      { href: '/projects/', label: site.navigation.projectsLabel, key: 'projects' },
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
    if (!hoveredCategory && sortedCategories[0]) setHoveredCategory(sortedCategories[0].slug);
  }, [hoveredCategory, sortedCategories]);

  const activeProducts = products
    .filter((product) => product.categorySlug === hoveredCategory)
    .sort((a, b) => a.sortOrder - b.sortOrder)
    .slice(0, 6);
  const hoveredCategoryInfo = sortedCategories.find((category) => category.slug === hoveredCategory) || sortedCategories[0];
  const menuProductTags = ['工程铺装', '模块化', '可定制'];

  const isActive = (item: (typeof navItems)[number]) => {
    if (item.href === '/') return currentPath === '/';
    return currentPath.startsWith(item.href);
  };

  return (
    <div className={`fixed top-0 left-0 w-full z-50 transition-all duration-300 ${isScrolled ? 'bg-white shadow-md' : 'bg-white/95 backdrop-blur-sm shadow-sm'}`}>
      <div className="max-w-7xl mx-auto px-7 h-24 flex items-center justify-between">
        <a className="flex items-center gap-3" href="/">
          <img src={site.logoUrl} alt={`${site.brandName} Logo`} className="w-[58px] h-[58px] object-contain" />
          <div className="flex flex-col">
            <span className="text-2xl font-extrabold text-[#0a4d8c] leading-tight tracking-wide">{site.brandName}</span>
            <span className="text-xs font-semibold text-gray-500 tracking-widest uppercase">{site.brandTagline}</span>
          </div>
        </a>

        <div className="hidden md:flex items-center gap-10 h-full">
          {navItems.map((item) => (
            <div
              key={item.href}
              className="relative h-full flex items-center"
              onMouseEnter={() => item.hasDropdown && setActiveDropdown(item.key)}
              onMouseLeave={() => setActiveDropdown(null)}
            >
              <a
                href={item.href}
                className={`text-[17px] font-semibold transition-colors flex items-center gap-1.5 ${isActive(item) ? 'text-[#0a5ca8]' : 'text-gray-700 hover:text-[#0a5ca8]'}`}
              >
                {item.label}
                {item.hasDropdown && <ChevronRight size={17} className={`transform transition-transform ${activeDropdown === item.key ? 'rotate-90' : 'rotate-0'}`} />}
              </a>

              {item.hasDropdown && activeDropdown === 'products' && (
                <div className="fixed left-0 top-24 w-screen border-y border-[#d9dee5] bg-[#f7f8f6] shadow-[0_24px_60px_rgba(18,32,54,0.12)]">
                  <div className="max-w-7xl mx-auto px-6 py-7">
                    <div className="flex flex-col gap-4 xl:flex-row xl:items-center">
                      <span className="text-[15px] font-extrabold text-[#596272] whitespace-nowrap">{site.navigation.categoryPrompt.replace(' :', '').replace(':', '')}</span>
                      <div className="flex flex-wrap gap-3">
                        {sortedCategories.map((category, index) => {
                          const active = hoveredCategory === category.slug;
                          return (
                            <a
                              key={category.slug}
                              href={`/products/${category.slug}/`}
                              onMouseEnter={() => setHoveredCategory(category.slug)}
                              className={`inline-flex items-center gap-3 rounded-xl border px-5 py-3 text-[15px] font-extrabold transition-all ${
                                active
                                  ? 'border-[#0a66c2] bg-[#0a66c2] text-white shadow-[0_12px_24px_rgba(10,102,194,0.22)]'
                                  : 'border-[#d8dee7] bg-white text-[#303a4b] hover:border-[#9fb7d1] hover:text-[#0a5ca8]'
                              }`}
                            >
                              <span className={`text-[11px] font-black ${active ? 'text-white/70' : 'text-[#a3acba]'}`}>{String(index + 1).padStart(2, '0')}</span>
                              {category.name}
                            </a>
                          );
                        })}
                      </div>
                    </div>
                  </div>

                  <div className="border-t border-[#d9dee5] bg-white">
                    <div className="max-w-7xl mx-auto px-6 py-9">
                      <div className="mb-6 flex items-end justify-between gap-8">
                        <div>
                          <p className="text-xs font-bold uppercase tracking-[0.24em] text-[#b88923]">Product Line</p>
                          <h3 className="mt-2 text-2xl font-black text-[#172033]">{hoveredCategoryInfo?.name}</h3>
                        </div>
                        <p className="hidden max-w-md text-sm leading-7 text-[#667085] lg:block">{hoveredCategoryInfo?.description}</p>
                      </div>

                      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
                        {activeProducts.map((product, index) => (
                          <a
                            key={product.id}
                            href={`/products/${product.categorySlug}/${product.slug}/`}
                            className="group overflow-hidden rounded-xl border border-[#dfe4ea] bg-white shadow-[0_12px_34px_rgba(18,32,54,0.06)] transition-all hover:-translate-y-0.5 hover:border-[#9fb7d1] hover:shadow-[0_18px_44px_rgba(18,32,54,0.12)]"
                          >
                            <div className="grid min-h-[178px] grid-cols-[190px_1fr]">
                              <div
                                className="relative overflow-hidden bg-[linear-gradient(135deg,#d7dde3,#f6f7f6)]"
                                style={{
                                  backgroundImage: `linear-gradient(135deg, rgba(10,77,140,0.10), rgba(184,137,35,0.10)), url('${product.coverImage}')`,
                                  backgroundSize: 'cover',
                                  backgroundPosition: 'center'
                                }}
                              >
                                <div className="absolute inset-0 bg-[linear-gradient(180deg,rgba(255,255,255,0)_45%,rgba(10,31,57,0.35))]" />
                                <span className="absolute left-4 top-4 rounded-full bg-white/90 px-3 py-1 text-[11px] font-black text-[#0a4d8c] shadow-sm">ASTREVA {String(index + 1).padStart(2, '0')}</span>
                              </div>
                              <div className="flex flex-col justify-between p-6">
                                <div>
                                  <div className="mb-4 h-[3px] w-10 bg-[#b88923]" />
                                  <h4 className="text-xl font-black text-[#172033] group-hover:text-[#0a5ca8] transition-colors leading-tight">{product.name}</h4>
                                  <p className="mt-3 text-sm leading-6 text-[#667085] line-clamp-2">{product.summary}</p>
                                  <div className="mt-4 flex flex-wrap gap-2">
                                    {menuProductTags.map((tag) => (
                                      <span key={tag} className="rounded-full border border-[#dfe4ea] bg-[#f7f8f6] px-3 py-1 text-[11px] font-bold text-[#536173]">{tag}</span>
                                    ))}
                                  </div>
                                </div>
                                <span className="mt-5 inline-flex items-center gap-2 text-sm font-extrabold text-[#0a66c2]">
                                  {site.navigation.productDetailLabel} <ArrowRight size={15} className="transition-transform group-hover:translate-x-1" />
                                </span>
                              </div>
                            </div>
                          </a>
                        ))}
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>

        <a href={`tel:${site.phone}`} className="hidden md:flex items-center gap-2.5 bg-[#0a66c2] hover:bg-[#084b8c] text-white px-7 py-3 rounded-full font-medium transition-colors text-[17px] shadow-lg shadow-blue-500/30">
          {site.phone} <ArrowUpRight size={19} />
        </a>

        <button className="md:hidden text-gray-800" onClick={() => setMobileOpen((value) => !value)} aria-label={site.navigation.mobileMenuLabel}>
          {mobileOpen ? <X size={34} /> : <Menu size={34} />}
        </button>
      </div>
      {mobileOpen && (
        <div className="md:hidden bg-white border-t border-gray-100 shadow-xl px-7 py-5">
          <div className="flex flex-col gap-5">
            {navItems.map((item) => (
              <a key={item.href} href={item.href} className={`text-lg font-bold ${isActive(item) ? 'text-[#0a66c2]' : 'text-gray-700'}`}>
                {item.label}
              </a>
            ))}
            <a href={`tel:${site.phone}`} className="bg-[#0a66c2] text-white px-6 py-3.5 rounded-full text-center text-lg font-bold">
              {site.phone}
            </a>
          </div>
        </div>
      )}
    </div>
  );
}
