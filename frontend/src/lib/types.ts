export type SEO = {
  title: string;
  description: string;
  keywords?: string;
  image?: string;
};

export type SiteSettings = {
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
    projectsLabel: string;
    supportLabel: string;
    aboutLabel: string;
    categoryPrompt: string;
    productDetailLabel: string;
    mobileMenuLabel: string;
  };
  footer: {
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
      nameCompanyLabel?: string;
      nameCompanyPlaceholder?: string;
      companyLabel: string;
      companyPlaceholder: string;
      optionalLabel: string;
      phoneLabel: string;
      phonePlaceholder: string;
      contactLabel?: string;
      contactPlaceholder?: string;
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

export type StaticPage = {
  slug: 'about' | 'solutions' | 'projects' | 'faq' | 'warranty' | 'install' | 'privacy' | 'agreement';
  title: string;
  hero?: {
    eyebrow?: string;
    title: string;
    description?: string;
    backgroundImage?: string;
    backgroundVideo?: string;
  };
  body: string;
  sections?: Record<string, any>;
  seo: SEO;
};

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
