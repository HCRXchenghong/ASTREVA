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
                <div className="absolute top-full left-1/2 -translate-x-1/2 w-[1008px] max-w-[calc(100vw-48px)] bg-white shadow-2xl rounded-b-2xl border-t border-gray-100 overflow-hidden flex flex-col">
                  <div className="bg-gray-50 px-10 py-6 border-b border-gray-200 flex items-center gap-5">
                    <span className="text-[17px] font-bold text-gray-500 whitespace-nowrap">{site.navigation.categoryPrompt}</span>
                    <div className="flex flex-wrap gap-2.5">
                      {sortedCategories.map((category) => (
                        <a
                          key={category.slug}
                          href={`/products/${category.slug}/`}
                          onMouseEnter={() => setHoveredCategory(category.slug)}
                          className={`px-5 py-2.5 rounded-full text-[17px] font-bold transition-all ${
                            hoveredCategory === category.slug
                              ? 'bg-[#0a66c2] text-white shadow-md scale-105'
                              : 'bg-white text-gray-600 hover:bg-gray-200 hover:text-gray-900 border border-gray-200'
                          }`}
                        >
                          {category.name}
                        </a>
                      ))}
                    </div>
                  </div>
                  <div className="p-10 bg-white min-h-[336px]">
                    <div className="grid grid-cols-2 gap-10">
                      {activeProducts.map((product) => (
                        <a
                          key={product.id}
                          href={`/products/${product.categorySlug}/${product.slug}/`}
                          className="group flex gap-5 items-center bg-gray-50/50 hover:bg-blue-50/50 p-3.5 rounded-2xl transition-colors border border-transparent hover:border-blue-100"
                        >
                          <div className="h-[115px] w-[154px] shrink-0 rounded-xl overflow-hidden shadow-sm relative">
                            <img src={product.coverImage} alt={product.name} className="w-full h-full object-cover group-hover:scale-110 transition-transform duration-500" />
                          </div>
                          <div>
                            <h4 className="text-[19px] font-bold text-gray-800 group-hover:text-[#0a5ca8] transition-colors leading-snug mb-2.5">{product.name}</h4>
                            <span className="inline-flex text-[13px] text-[#0a66c2] font-semibold items-center gap-1.5 opacity-0 group-hover:opacity-100 transform translate-x-[-10px] group-hover:translate-x-0 transition-all">
                              {site.navigation.productDetailLabel} <ArrowRight size={14} />
                            </span>
                          </div>
                        </a>
                      ))}
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
