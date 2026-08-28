export type SEO = {
  title: string;
  description: string;
  keywords?: string;
  image?: string;
};

export type NavigationLink = {
  label: string;
  href: string;
  icon?: string;
};

export type SiteSettings = {
  locale: string;
  localization: {
    languageLabel: string;
    switchAriaLabel: string;
    chineseLabel: string;
    englishLabel: string;
    arabicLabel: string;
    switchingText: string;
    unavailableText: string;
  };
  brandName: string;
  brandTagline: string;
  logoUrl: string;
  phone: string;
  email: string;
  copyright: string;
  serviceBridgeUrl: string;
  defaultSeo: SEO;
  socialLinks: {
    facebook?: string;
    instagram?: string;
    twitter?: string;
    youtube?: string;
    tiktok?: string;
    whatsapp?: string;
    telegram?: string;
  };
  socialIcons?: Record<string, string>;
  cookie: {
    title: string;
    text: string;
    privacyLabel: string;
    suffixText: string;
    acceptAllLabel: string;
    necessaryLabel: string;
  };
  navigation: {
    homeLabel: string;
    productsLabel: string;
    solutionsLabel: string;
    supportLabel: string;
    aboutLabel: string;
    mobileMenuLabel: string;
    productMenu: {
      categoryLabel: string;
      popularSeriesLabel: string;
      popularSeriesTitleTemplate: string;
      viewAllCategoryLabelTemplate: string;
      mobileToggleLabel: string;
      utilityLinks: NavigationLink[];
    };
  };
  footer: {
    logoAlt: string;
    description: string;
    quickLinksTitle: string;
    supportTitle: string;
    socialTitle: string;
    socialEmptyText: string;
    privacyLabel: string;
    agreementLabel: string;
    quickLinks: Array<{ label: string; href: string }>;
    supportLinks: Array<{ label: string; href: string }>;
  };
  chat: {
    title: string;
    ariaLabel: string;
    loadingText: string;
    errorTitle: string;
    errorDescription: string;
    initialUnreadCount: number;
  };
};

export type Category = {
  id: string;
  name: string;
  slug: string;
  description: string;
  coverImage: string;
  heroVideo?: string;
  sortOrder: number;
  seo: SEO;
};

export type Product = {
  id: string;
  name: string;
  slug: string;
  categorySlug: string;
  coverImage: string;
  galleryImages: string[];
  videoUrl?: string;
  summary: string;
  features: string[];
  specifications: Array<{ label: string; value: string }>;
  downloads: Array<{ label: string; url: string }>;
  body: string;
  isFeatured: boolean;
  sortOrder: number;
  seo: SEO;
};

export type ProductCenterPage = {
  seo: SEO;
  hero: {
    title: string;
    description: string;
    backgroundImage: string;
    backgroundVideo?: string;
  };
  intro: {
    title: string;
    description: string;
    categoryCtaLabel: string;
  };
  catalog: {
    eyebrow: string;
    description: string;
    categoriesEyebrow: string;
    categoriesTitle: string;
    categoriesDescription: string;
    categoriesAriaLabel: string;
    viewAllLabel: string;
    emptyCategoryMessage: string;
  };
  categoryPage: {
    titleTemplate: string;
    backLabel: string;
    backIcon: string;
    productHoverLabel: string;
    categoryCtaIcon: string;
  };
  detailPage: {
    badgeLabel: string;
    badgeIcon: string;
    featureIcon: string;
    quoteButtonLabel: string;
    quoteButtonIcon: string;
    downloadButtonIcon: string;
    breadcrumbSeparator: string;
    specsTitle: string;
    specsDescription: string;
    specsPrimaryIcon: string;
    specsPrimaryTitle: string;
    descriptionIcon: string;
    descriptionTitle: string;
    galleryTitle: string;
    videoTitle: string;
  };
};

export type HomePage = {
  seo: SEO;
  hero: {
    eyebrow: string;
    title: string;
    trademark: string;
    subtitle: string;
    backgroundImage: string;
    backgroundVideo?: string;
    primaryCta: string;
    primaryHref: string;
    secondaryCta: string;
    videoUrl?: string;
    videoTitle: string;
    videoPreviewLabel: string;
    videoOpenExternalLabel: string;
    videoCloseLabel: string;
  };
  partnerLabel: string;
  featuredProducts: {
    title: string;
    description: string;
    linkLabel: string;
    linkIcon: string;
  };
  partners: string[];
  productIntro: {
    title: string;
    description: string;
    image: string;
    bullets: string[];
    bulletIcon: string;
    ctaLabel: string;
    ctaHref: string;
  };
  advantagesIntro: {
    title: string;
    description: string;
  };
  advantages: Array<{ title: string; description: string; icon: string }>;
  testimonialsTitle: string;
  testimonials: Array<{ name: string; role: string; text: string }>;
  cta: {
    title: string;
    description: string;
    primaryLabel: string;
    primaryHref: string;
    secondaryLabel: string;
    secondaryHref: string;
    backgroundImage: string;
  };
};

export type SupportPage = {
  seo: SEO;
  hero: {
    title: string;
    description: string;
    backgroundImage: string;
    backgroundVideo?: string;
  };
  services: Array<{ title: string; description: string; icon: string; href: string; cta: string }>;
  contact: {
    title: string;
    description: string;
    phoneLabel: string;
    phoneIcon: string;
    emailLabel: string;
    emailIcon: string;
    form: {
      nameLabel: string;
      namePlaceholder: string;
      companyLabel: string;
      companyPlaceholder: string;
      optionalLabel: string;
      phoneLabel: string;
      phonePlaceholder: string;
      emailLabel: string;
      emailPlaceholder: string;
      messageLabel: string;
      messagePlaceholder: string;
      submitLabel: string;
      submittingLabel: string;
      successMessage: string;
      errorMessage: string;
      missingEndpointMessage: string;
    };
  };
};

export type FAQ = {
  id: string;
  question: string;
  answer: string;
  sortOrder: number;
  enabled: boolean;
};

export type StaticPageSlug = 'about' | 'solutions' | 'faq' | 'warranty' | 'install' | 'privacy' | 'agreement';

type StaticPageBase<TSlug extends StaticPageSlug> = {
  slug: TSlug;
  title: string;
  hero: {
    eyebrow?: string;
    title: string;
    description?: string;
    backgroundImage?: string;
    backgroundVideo?: string;
  };
  body: string;
  seo: SEO;
};

export type AboutPage = StaticPageBase<'about'> & {
  sections: {
    introIcon: string;
    partnersTitle: string;
  };
};

export type SolutionSection = {
  id: string;
  number: string;
  navLabel: string;
  title: string;
  description: string;
  image?: string;
  videoImage?: string;
  videoUrl?: string;
  videoCaption?: string;
  focusTitle: string;
  focusDescription: string;
  bullets: string[];
  ctaLabel: string;
  ctaHref: string;
  detailNote: string;
};

export type SolutionsPage = StaticPageBase<'solutions'> & {
  sections: {
    introTitle: string;
    introDescription: string;
    architectureTitle: string;
    ctaLabel: string;
    ctaHref: string;
    ctaIcon: string;
    sideNavTitle: string;
    detailNote: string;
    adviceTitle: string;
    advicePrimaryLabel: string;
    advicePrimaryHref: string;
    adviceSecondaryLabel: string;
    adviceSecondaryHref: string;
    featureIcon: string;
    bulletIcon: string;
    videoPlayIcon: string;
    solutionSections: SolutionSection[];
    systemCards: Array<{ step: string; title: string }>;
  };
};

export type FaqPage = StaticPageBase<'faq'>;
export type WarrantyPage = StaticPageBase<'warranty'> & {
  sections: { icon: string };
};
export type InstallPage = StaticPageBase<'install'> & {
  sections: { icon: string };
};
export type PrivacyPage = StaticPageBase<'privacy'>;
export type AgreementPage = StaticPageBase<'agreement'>;

export type StaticPage = AboutPage | SolutionsPage | FaqPage | WarrantyPage | InstallPage | PrivacyPage | AgreementPage;
export type StaticPageForSlug<TSlug extends StaticPageSlug> = Extract<StaticPage, { slug: TSlug }>;

export type SiteContent = {
  site: SiteSettings;
  home: HomePage;
  productCenter: ProductCenterPage;
  categories: Category[];
  products: Product[];
  support: SupportPage;
  faqs: FAQ[];
  staticPages: StaticPage[];
};
