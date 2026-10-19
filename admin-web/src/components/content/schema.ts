// 官网内容编辑器的字段 schema。
// 字段含义、标签与校验以 admin-server/server.mjs 的 renderAdminPage / saveSection /
// assertContentIntegrity 为准；这里只描述“怎么编辑”，服务端仍是唯一事实来源。

export type FieldType =
  | 'text'
  | 'textarea'
  | 'number'
  | 'checkbox'
  | 'select'
  | 'media'
  | 'listText'
  | 'pairList'
  | 'objectList'
  | 'group';

export interface FieldDef {
  /** 相对 model 的路径，如 home.hero.title 或 hero.title */
  key: string;
  label: string;
  type?: FieldType;
  rows?: number;
  placeholder?: string;
  hint?: string;
  /** 媒体字段：上传 accept 与素材库过滤类型 */
  accept?: string;
  mediaType?: 'image' | 'video' | 'file';
  /** 下拉选项 */
  options?: { label: string; value: any }[];
  /** objectList 的子字段 */
  schema?: FieldDef[];
  /** pairList 的键名 */
  keyA?: string;
  keyB?: string;
  labelA?: string;
  labelB?: string;
  /** listText 是否提供“上传后追加 URL”按钮（如产品图库） */
  appendUpload?: boolean;
  appendAccept?: string;
  /** 占满整行 */
  full?: boolean;
}

export interface ContentModuleGroup {
  id: string;
  label: string;
  icon: string;
  description: string;
  items: string[];
}

/** 与旧编辑器 CONTENT_MODULE_GROUPS 完全一致的分组（末尾补充开发者高级模式入口）。 */
export const CONTENT_MODULE_GROUPS: ContentModuleGroup[] = [
  {
    id: 'pages',
    label: '页面',
    icon: 'icon-file',
    description: '编辑首页、公司介绍、解决方案、支持与法务页面。',
    items: [
      'homeHero',
      'homeProducts',
      'homeAdvantages',
      'homeTestimonials',
      'homeCta',
      'solutions',
      'support',
      'about',
      'warranty',
      'install',
      'faqPage',
      'privacy',
      'agreement',
    ],
  },
  {
    id: 'products',
    label: '产品',
    icon: 'icon-apps',
    description: '管理产品中心、产品品类、产品资料和常见问题。',
    items: ['productCenter', 'categories', 'products', 'faq'],
  },
  {
    id: 'media',
    label: '素材',
    icon: 'icon-image',
    description: '统一上传和查看图片、视频与文件。',
    items: ['media'],
  },
  {
    id: 'global',
    label: '全局组件',
    icon: 'icon-settings',
    description: '管理品牌、导航、页脚、联系方式、Cookie 和在线客服。',
    items: ['site', 'navigation', 'footer', 'social', 'cookie', 'chat'],
  },
  {
    id: 'seo',
    label: 'SEO',
    icon: 'icon-search',
    description: '集中管理站点默认 SEO 和各主要页面的搜索摘要。',
    items: ['seo'],
  },
  {
    id: 'leads',
    label: '客户咨询',
    icon: 'icon-email',
    description: '查看咨询单并维护处理状态、优先级和内部备注。',
    items: ['leads'],
  },
  {
    id: 'publish',
    label: '发布中心',
    icon: 'icon-upload',
    description: '创建、启用和删除官网降级版本。',
    items: ['fallbackVersions'],
  },
  {
    id: 'advanced',
    label: '开发者高级',
    icon: 'icon-code',
    description: '完整内容字段的 JSON 编辑，误操作风险较高。',
    items: ['allContent'],
  },
];

/** 与旧 CONTENT_EDITOR_ITEMS 一致的模块标签。 */
export const MODULE_LABELS: Record<string, string> = {
  allContent: '全部内容字段',
  homeHero: '首页首屏',
  homeProducts: '首页产品区',
  homeAdvantages: '首页优势',
  homeTestimonials: '客户评价',
  homeCta: '首页行动区',
  productCenter: '产品中心',
  categories: '产品品类',
  products: '产品管理',
  solutions: '解决方案',
  support: '服务支持',
  about: '关于我们',
  warranty: '质保页面',
  install: '安装指南',
  faq: '常见问题',
  faqPage: 'FAQ 页面设置',
  privacy: '隐私政策',
  agreement: '用户协议',
  media: '媒体库',
  leads: '客户咨询',
  seo: 'SEO 设置',
  site: '全站设置',
  fallbackVersions: '降级版本',
  navigation: '导航文案',
  footer: '页脚链接',
  social: '社交方式',
  cookie: 'Cookie 提示',
  chat: '在线客服',
};

/** 旧编辑器的“预览位置”，用于跳转已发布官网。 */
export const MODULE_PREVIEW_PATHS: Record<string, string> = {
  allContent: '/',
  homeHero: '/#home-hero',
  homeProducts: '/#home-products',
  homeAdvantages: '/#home-advantages',
  homeTestimonials: '/#home-testimonials',
  homeCta: '/#home-cta',
  productCenter: '/products/#products-hero',
  categories: '/products/#product-categories',
  products: '/products/#product-categories',
  solutions: '/solutions/#solutions-hero',
  support: '/support/#support-hero',
  about: '/about/#about-hero',
  warranty: '/support/warranty/#warranty-content',
  install: '/support/install/#install-content',
  faq: '/support/faq/#faq-list',
  faqPage: '/support/faq/#faq-list',
  privacy: '/privacy/#privacy-content',
  agreement: '/agreement/#agreement-content',
  media: '/#home-hero',
  leads: '/support/#support-contact',
  seo: '/#home-hero',
  site: '/#home-hero',
  fallbackVersions: '/#home-hero',
  navigation: '/#home-hero',
  footer: '/#site-footer',
  social: '/#site-footer',
  cookie: '/#cookie-consent',
  chat: '/#home-hero',
};

const imageField = (key: string, label: string): FieldDef => ({
  key,
  label,
  type: 'media',
  accept: 'image/*',
  mediaType: 'image',
});

const videoField = (key: string, label: string): FieldDef => ({
  key,
  label,
  type: 'media',
  accept: 'video/*',
  mediaType: 'video',
});

const seoFields = (prefix: string): FieldDef[] => [
  { key: `${prefix}.title`, label: 'SEO 标题' },
  { key: `${prefix}.description`, label: 'SEO 描述', type: 'textarea' },
  { key: `${prefix}.keywords`, label: 'SEO 关键词' },
  imageField(`${prefix}.image`, 'SEO 分享图'),
];

const HOME_HERO_FIELDS: FieldDef[] = [
  { key: 'home.hero.eyebrow', label: '眉标' },
  { key: 'home.hero.title', label: '主标题' },
  { key: 'home.hero.trademark', label: '商标角标' },
  { key: 'home.hero.subtitle', label: '副标题', type: 'textarea' },
  imageField('home.hero.backgroundImage', '背景图'),
  videoField('home.hero.backgroundVideo', '背景视频'),
  { key: 'home.hero.primaryCta', label: '主按钮文字' },
  { key: 'home.hero.primaryHref', label: '主按钮链接' },
  { key: 'home.hero.secondaryCta', label: '播放按钮文字' },
  { key: 'home.hero.videoUrl', label: '播放视频链接' },
  { key: 'home.hero.videoTitle', label: '视频弹窗标题' },
  { key: 'home.hero.videoPreviewLabel', label: '视频占位提示' },
  { key: 'home.hero.videoOpenExternalLabel', label: '外部打开按钮文字' },
  { key: 'home.hero.videoCloseLabel', label: '关闭按钮文字' },
  ...seoFields('home.seo'),
];

const HOME_PRODUCTS_FIELDS: FieldDef[] = [
  { key: 'home.featuredProducts.title', label: '产品区标题' },
  { key: 'home.featuredProducts.description', label: '产品区描述', type: 'textarea' },
  { key: 'home.featuredProducts.linkLabel', label: '产品卡片链接文字' },
  { key: 'home.featuredProducts.linkIcon', label: '产品卡片链接图标' },
  { key: 'home.productIntro.title', label: '产品介绍标题' },
  { key: 'home.productIntro.description', label: '产品介绍描述', type: 'textarea', rows: 5 },
  imageField('home.productIntro.image', '产品介绍图片'),
  {
    key: 'home.productIntro.bullets',
    label: '产品介绍要点',
    type: 'listText',
    hint: '每行一条',
    full: true,
  },
  { key: 'home.productIntro.bulletIcon', label: '产品介绍要点图标' },
  { key: 'home.productIntro.ctaLabel', label: '技术规格按钮文字' },
  { key: 'home.productIntro.ctaHref', label: '技术规格按钮链接' },
];

const HOME_ADVANTAGES_FIELDS: FieldDef[] = [
  { key: 'home.advantagesIntro.title', label: '优势区标题' },
  { key: 'home.advantagesIntro.description', label: '优势区描述', type: 'textarea' },
  {
    key: 'home.advantages',
    label: '优势卡片',
    type: 'objectList',
    full: true,
    schema: [
      { key: 'title', label: '标题' },
      { key: 'description', label: '描述', type: 'textarea' },
      { key: 'icon', label: '图标代号' },
    ],
  },
];

const HOME_TESTIMONIALS_FIELDS: FieldDef[] = [
  { key: 'home.testimonialsTitle', label: '区域标题' },
  {
    key: 'home.testimonials',
    label: '评价列表',
    type: 'objectList',
    full: true,
    schema: [
      { key: 'name', label: '姓名' },
      { key: 'role', label: '身份' },
      { key: 'text', label: '评价内容', type: 'textarea' },
    ],
  },
];

const HOME_CTA_FIELDS: FieldDef[] = [
  { key: 'home.cta.title', label: '标题' },
  { key: 'home.cta.description', label: '描述', type: 'textarea' },
  { key: 'home.cta.primaryLabel', label: '主按钮' },
  { key: 'home.cta.primaryHref', label: '主按钮链接' },
  { key: 'home.cta.secondaryLabel', label: '次按钮' },
  { key: 'home.cta.secondaryHref', label: '次按钮链接' },
  imageField('home.cta.backgroundImage', '背景图'),
  {
    key: 'home.partners',
    label: '合作伙伴名单',
    type: 'listText',
    hint: '每行一家合作伙伴；显示在「关于我们」页面。',
  },
];

const PRODUCT_CENTER_FIELDS: FieldDef[] = [
  { key: 'productCenter.hero.title', label: '首屏标题' },
  imageField('productCenter.hero.backgroundImage', '首屏背景图'),
  videoField('productCenter.hero.backgroundVideo', '首屏背景视频'),
  { key: 'productCenter.catalog.eyebrow', label: '产品页眉标' },
  { key: 'productCenter.catalog.description', label: '产品页描述', type: 'textarea' },
  { key: 'productCenter.catalog.categoriesEyebrow', label: '分类区眉标' },
  { key: 'productCenter.catalog.categoriesTitle', label: '分类区标题' },
  { key: 'productCenter.catalog.categoriesDescription', label: '分类区描述', type: 'textarea' },
  { key: 'productCenter.catalog.categoriesAriaLabel', label: '分类区无障碍标签' },
  { key: 'productCenter.catalog.viewAllLabel', label: '查看全部文字' },
  { key: 'productCenter.catalog.emptyCategoryMessage', label: '分类为空时的提示' },
  { key: 'productCenter.categoryPage.categoryCtaIcon', label: '品类按钮图标' },
  { key: 'productCenter.categoryPage.titleTemplate', label: '品类页标题模板' },
  { key: 'productCenter.categoryPage.backLabel', label: '返回按钮文字' },
  { key: 'productCenter.categoryPage.backIcon', label: '返回按钮图标' },
  { key: 'productCenter.categoryPage.productHoverLabel', label: '产品悬浮按钮文字' },
  { key: 'productCenter.detailPage.breadcrumbSeparator', label: '面包屑分隔符' },
  { key: 'productCenter.detailPage.badgeLabel', label: '详情页徽标' },
  { key: 'productCenter.detailPage.badgeIcon', label: '详情页徽标图标' },
  { key: 'productCenter.detailPage.featureIcon', label: '卖点列表图标' },
  { key: 'productCenter.detailPage.quoteButtonLabel', label: '报价按钮文字' },
  { key: 'productCenter.detailPage.quoteButtonIcon', label: '报价按钮图标' },
  { key: 'productCenter.detailPage.downloadButtonIcon', label: '下载按钮图标' },
  { key: 'productCenter.detailPage.specsTitle', label: '规格区标题' },
  { key: 'productCenter.detailPage.specsDescription', label: '规格区描述', type: 'textarea' },
  { key: 'productCenter.detailPage.specsPrimaryIcon', label: '规格左侧图标' },
  { key: 'productCenter.detailPage.specsPrimaryTitle', label: '规格左侧标题' },
  { key: 'productCenter.detailPage.descriptionIcon', label: '说明右侧图标' },
  { key: 'productCenter.detailPage.descriptionTitle', label: '说明右侧标题' },
  { key: 'productCenter.detailPage.galleryTitle', label: '图库标题' },
  { key: 'productCenter.detailPage.videoTitle', label: '视频标题' },
  ...seoFields('productCenter.seo'),
];

const SUPPORT_FIELDS: FieldDef[] = [
  { key: 'support.hero.title', label: '首屏标题' },
  { key: 'support.hero.description', label: '首屏描述', type: 'textarea' },
  imageField('support.hero.backgroundImage', '首屏背景图'),
  videoField('support.hero.backgroundVideo', '首屏背景视频'),
  {
    key: 'support.services',
    label: '服务卡片',
    type: 'objectList',
    full: true,
    schema: [
      { key: 'title', label: '标题' },
      { key: 'description', label: '描述', type: 'textarea' },
      { key: 'icon', label: '图标' },
      { key: 'href', label: '链接' },
      { key: 'cta', label: '按钮文字' },
    ],
  },
  { key: 'support.contact.title', label: '联系标题' },
  { key: 'support.contact.description', label: '联系说明', type: 'textarea' },
  { key: 'support.contact.phoneLabel', label: '电话标签' },
  { key: 'support.contact.phoneIcon', label: '电话图标' },
  { key: 'support.contact.emailLabel', label: '邮箱标签' },
  { key: 'support.contact.emailIcon', label: '邮箱图标' },
  { key: 'support.contact.form.nameLabel', label: '姓名标签' },
  { key: 'support.contact.form.namePlaceholder', label: '姓名占位' },
  { key: 'support.contact.form.companyLabel', label: '公司标签' },
  { key: 'support.contact.form.companyPlaceholder', label: '公司占位' },
  { key: 'support.contact.form.phoneLabel', label: '电话标签' },
  { key: 'support.contact.form.phonePlaceholder', label: '电话占位' },
  { key: 'support.contact.form.emailLabel', label: '邮箱标签' },
  { key: 'support.contact.form.emailPlaceholder', label: '邮箱占位' },
  { key: 'support.contact.form.messageLabel', label: '留言标签' },
  { key: 'support.contact.form.messagePlaceholder', label: '留言占位' },
  { key: 'support.contact.form.optionalLabel', label: '选填提示' },
  { key: 'support.contact.form.submitLabel', label: '提交按钮' },
  { key: 'support.contact.form.submittingLabel', label: '提交中按钮' },
  { key: 'support.contact.form.successMessage', label: '提交成功提示' },
  { key: 'support.contact.form.errorMessage', label: '提交失败提示' },
  { key: 'support.contact.form.missingEndpointMessage', label: '接口未配置提示' },
  ...seoFields('support.seo'),
];

export const SITE_FIELDS: FieldDef[] = [
  { key: 'site.brandName', label: '品牌名' },
  { key: 'site.brandTagline', label: '品牌标语' },
  imageField('site.logoUrl', 'Logo'),
  { key: 'site.phone', label: '电话' },
  { key: 'site.email', label: '邮箱' },
  { key: 'site.copyright', label: '版权文案' },
  { key: 'site.serviceBridgeUrl', label: 'ServiceBridge 地址' },
  { key: 'site.localization.languageLabel', label: '语言选择标签' },
  { key: 'site.localization.chineseLabel', label: '中文名称' },
  { key: 'site.localization.englishLabel', label: '英语名称' },
  { key: 'site.localization.arabicLabel', label: '阿拉伯语名称' },
  { key: 'site.notFound.title', label: '404 页标题' },
  { key: 'site.notFound.description', label: '404 页说明', type: 'textarea' },
  { key: 'site.notFound.homeLabel', label: '404 页「返回首页」文字' },
  { key: 'site.notFound.productsLabel', label: '404 页「浏览产品」文字' },
  { key: 'site.notFound.supportLabel', label: '404 页「获取支持」文字' },
];

const NAVIGATION_FIELDS: FieldDef[] = [
  { key: 'site.navigation.homeLabel', label: '首页' },
  { key: 'site.navigation.productsLabel', label: '产品中心' },
  { key: 'site.navigation.solutionsLabel', label: '解决方案' },
  { key: 'site.navigation.supportLabel', label: '服务支持' },
  { key: 'site.navigation.aboutLabel', label: '关于我们' },
  { key: 'site.navigation.mobileMenuLabel', label: '移动端菜单无障碍标签' },
  { key: 'site.navigation.productMenu.categoryLabel', label: '产品下拉：分类区标题' },
  { key: 'site.navigation.productMenu.popularSeriesLabel', label: '产品下拉：热门系列眉标' },
  { key: 'site.navigation.productMenu.popularSeriesTitleTemplate', label: '产品下拉：热门系列标题模板', hint: '用 {category} 代表当前分类名。' },
  { key: 'site.navigation.productMenu.viewAllCategoryLabelTemplate', label: '产品下拉：查看全部模板', hint: '用 {category} 代表当前分类名。' },
  { key: 'site.navigation.productMenu.mobileToggleLabel', label: '产品下拉：移动端展开标签' },
  {
    key: 'site.navigation.productMenu.utilityLinks',
    label: '产品下拉：侧栏工具链接',
    type: 'objectList',
    full: true,
    hint: '显示在产品下拉菜单右侧的快捷入口。',
    schema: [
      { key: 'label', label: '文字' },
      { key: 'href', label: '链接' },
      { key: 'icon', label: '图标名（file/book/sea 等）' },
    ],
  },
];

const FOOTER_FIELDS: FieldDef[] = [
  { key: 'site.footer.logoAlt', label: '页脚 Logo 替代文字' },
  { key: 'site.footer.description', label: '页脚描述', type: 'textarea' },
  { key: 'site.footer.quickLinksTitle', label: '快速链接标题' },
  {
    key: 'site.footer.quickLinks',
    label: '快速链接',
    type: 'pairList',
    keyA: 'label',
    keyB: 'href',
    labelA: '文字',
    labelB: '链接',
    full: true,
  },
  { key: 'site.footer.supportTitle', label: '服务链接标题' },
  {
    key: 'site.footer.supportLinks',
    label: '服务链接',
    type: 'pairList',
    keyA: 'label',
    keyB: 'href',
    labelA: '文字',
    labelB: '链接',
    full: true,
  },
  { key: 'site.footer.socialTitle', label: '关注标题' },
  { key: 'site.footer.socialEmptyText', label: '无社交链接提示' },
  { key: 'site.footer.privacyLabel', label: '隐私政策文字' },
  { key: 'site.footer.agreementLabel', label: '用户协议文字' },
];

const SOCIAL_FIELDS: FieldDef[] = [
  { key: 'site.socialLinks.whatsapp', label: 'WhatsApp 链接' },
  { key: 'site.socialIcons.whatsapp', label: 'WhatsApp 图标文字' },
  { key: 'site.socialLinks.telegram', label: 'Telegram 链接' },
  { key: 'site.socialIcons.telegram', label: 'Telegram 图标文字' },
  { key: 'site.socialLinks.facebook', label: 'Facebook 链接' },
  { key: 'site.socialIcons.facebook', label: 'Facebook 图标文字' },
  { key: 'site.socialLinks.instagram', label: 'Instagram 链接' },
  { key: 'site.socialIcons.instagram', label: 'Instagram 图标文字' },
  { key: 'site.socialLinks.twitter', label: 'Twitter / X 链接' },
  { key: 'site.socialIcons.twitter', label: 'Twitter / X 图标文字' },
  { key: 'site.socialLinks.youtube', label: 'YouTube 链接' },
  { key: 'site.socialIcons.youtube', label: 'YouTube 图标文字' },
  { key: 'site.socialLinks.tiktok', label: 'TikTok 链接' },
  { key: 'site.socialIcons.tiktok', label: 'TikTok 图标文字' },
];

const COOKIE_FIELDS: FieldDef[] = [
  { key: 'site.cookie.title', label: '标题' },
  { key: 'site.cookie.text', label: '正文', type: 'textarea' },
  { key: 'site.cookie.privacyLabel', label: '隐私政策文字' },
  { key: 'site.cookie.suffixText', label: '后缀文字' },
  { key: 'site.cookie.acceptAllLabel', label: '接受全部按钮' },
  { key: 'site.cookie.necessaryLabel', label: '仅必要按钮' },
  { key: 'site.cookie.settingsLabel', label: '页脚「Cookie 设置」按钮文字' },
];

const CHAT_FIELDS: FieldDef[] = [
  { key: 'site.chat.title', label: '客服标题' },
  { key: 'site.chat.ariaLabel', label: '无障碍标签' },
  { key: 'site.chat.loadingText', label: '连接中提示' },
  { key: 'site.chat.errorTitle', label: '加载失败标题' },
  { key: 'site.chat.errorDescription', label: '加载失败说明', type: 'textarea' },
  { key: 'site.chat.initialUnreadCount', label: '初始未读数字', type: 'number' },
  { key: 'site.serviceBridgeUrl', label: 'ServiceBridge 用户端地址' },
];

const SEO_FIELDS: FieldDef[] = [
  { key: 'site.searchVerification.google', label: 'Google 验证码' },
  { key: 'site.searchVerification.bing', label: 'Bing 验证码' },
  { key: 'site.searchVerification.baidu', label: '百度验证码' },
  { key: 'site.searchVerification.so360', label: '360 搜索验证码' },
  { key: 'site.searchVerification.sogou', label: '搜狗验证码' },
  { key: 'site.searchVerification.yandex', label: 'Yandex 验证码' },
  { key: 'site.defaultSeo.title', label: '默认 SEO 标题' },
  { key: 'site.defaultSeo.description', label: '默认 SEO 描述', type: 'textarea' },
  imageField('site.defaultSeo.image', '默认分享图'),
  { key: 'site.defaultSeo.keywords', label: '默认关键词' },
  { key: 'site.aiDiscovery.introduction', label: 'AI 发现：站点简介', type: 'textarea', hint: '供 llms.txt 等机器可读入口使用。' },
  { key: 'site.aiDiscovery.corePagesTitle', label: 'AI 发现：核心页面标题' },
  { key: 'site.aiDiscovery.categoriesTitle', label: 'AI 发现：产品分类标题' },
  { key: 'site.aiDiscovery.productsTitle', label: 'AI 发现：产品标题' },
  { key: 'site.aiDiscovery.contactTitle', label: 'AI 发现：联系方式标题' },
  { key: 'site.aiDiscovery.homeLabel', label: 'AI 发现：首页标签' },
  { key: 'site.aiDiscovery.productsLabel', label: 'AI 发现：产品标签' },
  { key: 'site.aiDiscovery.solutionsLabel', label: 'AI 发现：解决方案标签' },
  { key: 'site.aiDiscovery.supportLabel', label: 'AI 发现：支持标签' },
  { key: 'site.aiDiscovery.aboutLabel', label: 'AI 发现：关于我们标签' },
  { key: 'site.aiDiscovery.faqLabel', label: 'AI 发现：FAQ 标签' },
  { key: 'site.aiDiscovery.websiteLabel', label: 'AI 发现：官网标签' },
  { key: 'site.aiDiscovery.emailLabel', label: 'AI 发现：邮箱标签' },
  { key: 'site.aiDiscovery.citationNotice', label: 'AI 发现：引用声明', type: 'textarea' },
  ...seoFields('home.seo'),
  ...seoFields('productCenter.seo'),
  ...seoFields('support.seo'),
];

const STATIC_PAGE_COMMON: FieldDef[] = [
  { key: 'title', label: '页面标题' },
  { key: 'hero.eyebrow', label: '首屏眉标' },
  { key: 'hero.title', label: '首屏标题' },
  { key: 'hero.subtitle', label: '首屏副标题', type: 'textarea' },
  { key: 'hero.description', label: '首屏描述', type: 'textarea' },
  imageField('hero.backgroundImage', '首屏背景图'),
  videoField('hero.backgroundVideo', '首屏背景视频'),
];

const ABOUT_EXTRA: FieldDef[] = [
  { key: 'sections.introIcon', label: '正文图标' },
  { key: 'sections.partnersTitle', label: '合作伙伴标题' },
];

const SOLUTION_EXTRA: FieldDef[] = [
  { key: 'sections.introTitle', label: '顶部列表标题' },
  { key: 'sections.introDescription', label: '顶部列表描述', type: 'textarea' },
  { key: 'sections.sideNavTitle', label: '二级导航标题' },
  { key: 'sections.architectureTitle', label: '商业架构标题' },
  { key: 'sections.ctaLabel', label: '详情按钮文字' },
  { key: 'sections.ctaHref', label: '详情按钮链接' },
  { key: 'sections.ctaIcon', label: '详情按钮图标' },
  { key: 'sections.videoPlayIcon', label: '视频播放图标' },
  { key: 'sections.adviceTitle', label: '侧栏咨询标题' },
  { key: 'sections.advicePrimaryLabel', label: '侧栏主按钮' },
  { key: 'sections.advicePrimaryHref', label: '侧栏主按钮链接' },
  { key: 'sections.adviceSecondaryLabel', label: '侧栏次按钮' },
  { key: 'sections.adviceSecondaryHref', label: '侧栏次按钮链接' },
  {
    key: 'sections.solutionSections',
    label: '方案长页面分区',
    type: 'objectList',
    full: true,
    schema: [
      { key: 'id', label: '锚点 ID，例如 sol-sec-1' },
      { key: 'number', label: '编号' },
      { key: 'navLabel', label: '导航标签' },
      { key: 'title', label: '标题' },
      { key: 'description', label: '场景说明', type: 'textarea' },
      imageField('image', '图片 URL'),
      imageField('videoImage', '视频封面 URL'),
      videoField('videoUrl', '视频 URL'),
      { key: 'videoCaption', label: '视频说明' },
      { key: 'focusTitle', label: '重点标题' },
      { key: 'focusDescription', label: '重点说明', type: 'textarea' },
      { key: 'detailNote', label: '技术提示文字' },
      { key: 'ctaLabel', label: '当前分区按钮文字' },
      { key: 'ctaHref', label: '当前分区按钮链接' },
      { key: 'bullets', label: '要点', type: 'listText', hint: '每行一条' },
    ],
  },
  {
    key: 'sections.systemCards',
    label: '商业系统架构卡片',
    type: 'objectList',
    full: true,
    schema: [
      { key: 'step', label: '步骤' },
      { key: 'title', label: '说明', type: 'textarea' },
    ],
  },
];

const ICON_EXTRA: FieldDef[] = [{ key: 'sections.icon', label: '页面图标' }];

const STATIC_PAGE_SEO: FieldDef[] = [
  { key: 'body', label: '正文内容', type: 'textarea', rows: 10, full: true, hint: '可输入正文文本，换行会保留；需要加粗或小标题时也可以使用少量 HTML。' },
  ...seoFields('seo'),
];

const STATIC_PAGE_EXTRAS: Record<string, FieldDef[]> = {
  about: ABOUT_EXTRA,
  solutions: SOLUTION_EXTRA,
  warranty: ICON_EXTRA,
  install: ICON_EXTRA,
  faq: [{ key: 'sections.intro', label: 'FAQ 区简介', type: 'textarea' }],
};

/** 静态页字段（model = 单个 staticPage 对象）。 */
export function staticPageFields(slug: string): FieldDef[] {
  return [
    ...STATIC_PAGE_COMMON,
    ...(STATIC_PAGE_EXTRAS[slug] || []),
    ...STATIC_PAGE_SEO,
  ];
}

/** 品类字段（model = 单个 category 对象）。 */
export const CATEGORY_FIELDS: FieldDef[] = [
  { key: 'name', label: '名称' },
  { key: 'slug', label: 'Slug' },
  { key: 'description', label: '简介', type: 'textarea' },
  imageField('coverImage', '封面图'),
  videoField('heroVideo', '首屏背景视频'),
  { key: 'sortOrder', label: '排序', type: 'number' },
  ...seoFields('seo'),
];

/** 产品字段（model = 单个 product 对象）；categorySlug 为动态下拉。 */
export function productFields(categories: Record<string, any>[] = []): FieldDef[] {
  return [
    { key: 'name', label: '产品名称' },
    { key: 'slug', label: 'Slug' },
    {
      key: 'categorySlug',
      label: '所属品类',
      type: 'select',
      options: categories.map((category) => ({
        label: category.name || category.slug,
        value: category.slug,
      })),
    },
    imageField('coverImage', '封面图'),
    {
      key: 'galleryImages',
      label: '图库图片',
      type: 'listText',
      hint: '每行一个图片 URL，也可以上传多张图片自动追加。',
      appendUpload: true,
      appendAccept: 'image/*',
      full: true,
    },
    { key: 'videoUrl', label: '产品视频', type: 'media', accept: 'video/*', mediaType: 'video', hint: '可填 YouTube/Bilibili iframe 地址，也可上传 mp4/webm。' },
    { key: 'summary', label: '简介', type: 'textarea' },
    { key: 'features', label: '卖点', type: 'listText', hint: '每行一个卖点', full: true },
    {
      key: 'specifications',
      label: '规格参数',
      type: 'objectList',
      full: true,
      hint: '每行一条参数名与参数值。',
      schema: [
        { key: 'label', label: '参数名' },
        { key: 'value', label: '参数值' },
      ],
    },
    {
      key: 'downloads',
      label: '下载资料',
      type: 'objectList',
      full: true,
      hint: '名称 + 文件地址；上传文件后会自动填入地址。',
      schema: [
        { key: 'label', label: '名称' },
        { key: 'url', label: 'URL', type: 'media', accept: '.pdf,.zip,.dwg,.dxf', mediaType: 'file' },
      ],
    },
    { key: 'body', label: '正文', type: 'textarea', rows: 5, full: true },
    { key: 'isFeatured', label: '首页推荐', type: 'checkbox' },
    { key: 'sortOrder', label: '排序', type: 'number' },
    ...seoFields('seo'),
  ];
}

/** FAQ 字段（model = 单个 faq 对象）。 */
export const FAQ_FIELDS: FieldDef[] = [
  { key: 'question', label: '问题' },
  { key: 'answer', label: '答案', type: 'textarea', rows: 5, full: true },
  { key: 'sortOrder', label: '排序', type: 'number' },
  { key: 'enabled', label: '启用', type: 'checkbox' },
];

/** 页面 / 全局 / SEO 模块使用的字段（model = 整份 content）。 */
export function pageFieldsForModule(moduleId: string): FieldDef[] {
  switch (moduleId) {
    case 'homeHero':
      return HOME_HERO_FIELDS;
    case 'homeProducts':
      return HOME_PRODUCTS_FIELDS;
    case 'homeAdvantages':
      return HOME_ADVANTAGES_FIELDS;
    case 'homeTestimonials':
      return HOME_TESTIMONIALS_FIELDS;
    case 'homeCta':
      return HOME_CTA_FIELDS;
    case 'productCenter':
      return PRODUCT_CENTER_FIELDS;
    case 'support':
      return SUPPORT_FIELDS;
    case 'site':
      return SITE_FIELDS;
    case 'navigation':
      return NAVIGATION_FIELDS;
    case 'footer':
      return FOOTER_FIELDS;
    case 'social':
      return SOCIAL_FIELDS;
    case 'cookie':
      return COOKIE_FIELDS;
    case 'chat':
      return CHAT_FIELDS;
    case 'seo':
      return SEO_FIELDS;
    default:
      return [];
  }
}

/** 静态页模块 id → staticPage slug。 */
export const STATIC_MODULE_SLUGS: Record<string, string> = {
  about: 'about',
  solutions: 'solutions',
  warranty: 'warranty',
  install: 'install',
  faqPage: 'faq',
  privacy: 'privacy',
  agreement: 'agreement',
};

export const STATIC_PAGE_OPTIONS = [
  { label: '关于我们', value: 'about' },
  { label: '解决方案', value: 'solutions' },
  { label: '质保页面', value: 'warranty' },
  { label: '安装指南', value: 'install' },
  { label: 'FAQ 页面', value: 'faq' },
  { label: '隐私政策', value: 'privacy' },
  { label: '用户协议', value: 'agreement' },
];
