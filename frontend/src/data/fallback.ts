import type { Product, SiteContent } from '../lib/types';

const HERO_VIDEOS = {
  home: 'https://assets.mixkit.co/videos/4010/4010-1080.mp4',
  products: 'https://assets.mixkit.co/videos/23511/23511-720.mp4',
  municipal: 'https://assets.mixkit.co/videos/42368/42368-1080.mp4',
  commercial: 'https://assets.mixkit.co/videos/4339/4339-1080.mp4',
  industrial: 'https://assets.mixkit.co/videos/14631/14631-720.mp4',
  landscape: 'https://assets.mixkit.co/videos/4170/4170-1080.mp4',
  support: 'https://assets.mixkit.co/videos/22032/22032-720.mp4',
  about: 'https://assets.mixkit.co/videos/21226/21226-720.mp4',
  solutions: 'https://assets.mixkit.co/videos/1439/1439-1080.mp4',
  stainless: 'https://assets.mixkit.co/videos/4401/4401-1080.mp4',
  faq: 'https://assets.mixkit.co/videos/1457/1457-720.mp4',
  warranty: 'https://assets.mixkit.co/videos/25480/25480-720.mp4',
  install: 'https://assets.mixkit.co/videos/23511/23511-720.mp4',
  privacy: 'https://assets.mixkit.co/videos/1439/1439-1080.mp4',
  agreement: 'https://assets.mixkit.co/videos/21226/21226-720.mp4',
  industrialDetail: 'https://assets.mixkit.co/videos/4380/4380-1080.mp4'
} as const;

type CatalogProductSeed = Pick<Product, 'name' | 'slug' | 'categorySlug' | 'coverImage' | 'summary' | 'features' | 'specifications' | 'isFeatured' | 'sortOrder'> & {
  videoUrl?: string;
};

const catalogProduct = (seed: CatalogProductSeed): Product => ({
  id: `prod_${seed.slug.replaceAll('-', '_')}`,
  name: seed.name,
  slug: seed.slug,
  categorySlug: seed.categorySlug,
  coverImage: seed.coverImage,
  galleryImages: [],
  videoUrl: seed.videoUrl || HERO_VIDEOS.products,
  summary: seed.summary,
  features: seed.features,
  specifications: seed.specifications,
  downloads: [],
  body: `${seed.summary} 支持根据项目尺寸、材质、荷载与安装条件进行定制，具体参数请联系星渡 ASTREVA 工程团队。`,
  isFeatured: seed.isFeatured,
  sortOrder: seed.sortOrder,
  seo: {
    title: `${seed.name} | 星渡ASTREVA`,
    description: seed.summary
  }
});

export const fallbackContent: SiteContent = {
  site: {
    brandName: '星渡ASTREVA',
    brandTagline: 'A Grate Innovation',
    logoUrl: '/assets/astreva-logo-transparent.png',
    phone: '+86 400 888 8888',
    email: 'info@astreva.com',
    copyright: '@星渡ASTREVA 2026 版权所有.',
    serviceBridgeUrl: process.env.PUBLIC_SERVICEBRIDGE_URL || '',
    defaultSeo: {
      title: '星渡ASTREVA | 建筑与市政排水系统',
      description: '星渡ASTREVA 提供模块化不锈钢排水沟、重型格栅、景观排水与工业排水系统。',
      image: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=1200'
    },
    socialLinks: {},
    socialIcons: {
      whatsapp: 'WA',
      telegram: 'TG',
      facebook: 'F',
      instagram: 'IG',
      twitter: 'X',
      youtube: 'YT',
      tiktok: 'TT'
    },
    cookie: {
      title: '隐私与 Cookie 使用提示 :',
      text: '本网站使用 Cookie 以确保您在我们的网站上获得最佳体验、分析站点流量并协助我们的营销工作。',
      privacyLabel: '隐私政策',
      suffixText: '以了解更多信息或更改您的设置。',
      acceptAllLabel: '接受全部 Cookie',
      necessaryLabel: '仅接受必要'
    },
    navigation: {
      homeLabel: '首页',
      productsLabel: '产品中心',
      solutionsLabel: '解决方案',
      supportLabel: '服务支持',
      aboutLabel: '关于我们',
      categoryPrompt: '选择业务分类 :',
      productDetailLabel: '进入产品详情',
      mobileMenuLabel: '打开导航'
    },
    footer: {
      description: '建筑与市政排水领域的创新方案引领者。提供最高效、最模块化的不锈钢排水系统。',
      quickLinksTitle: '快速链接',
      supportTitle: '服务支持',
      socialTitle: '关注我们',
      socialEmptyText: '后台可配置社交媒体链接',
      privacyLabel: '隐私政策',
      agreementLabel: '用户协议',
      quickLinks: [
        { label: '首页', href: '/' },
        { label: '产品中心', href: '/products/' },
        { label: '解决方案', href: '/solutions/' },
        { label: '关于我们', href: '/about/' }
      ],
      supportLinks: [
        { label: '安装指南', href: '/support/install/' },
        { label: '常见问题 (FAQs)', href: '/support/faq/' },
        { label: '售后与质保', href: '/support/warranty/' }
      ]
    },
    chat: {
      title: '在线客服',
      ariaLabel: '在线客服',
      loadingText: '正在连接在线客服...',
      errorTitle: '客服窗口未加载',
      errorDescription: '请确认 ServiceBridge 用户端服务已启动',
      initialUnreadCount: 1
    }
  },
  home: {
    seo: {
      title: '星渡ASTREVA | 模块化不锈钢排水系统',
      description: '建筑与市政排水领域的创新方案，专注高效、美观、耐用的模块化不锈钢排水系统。'
    },
    hero: {
      eyebrow: '最快的排水系统。绝对的。',
      title: '星渡ASTREVA',
      trademark: 'TM',
      subtitle: '建筑与市政排水领域的创新方案',
      backgroundImage: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=2000',
      backgroundVideo: HERO_VIDEOS.home,
      primaryCta: '联系我们',
      primaryHref: '/support/',
      secondaryCta: '播放视频',
      videoUrl: 'https://youtube.com/shorts/XhHzoY3kG54?feature=share',
      videoTitle: '星渡ASTREVA 宣传视频',
      videoPreviewLabel: '宣传视频播放器 (预览)',
      videoOpenExternalLabel: '无法播放？在 YouTube 打开',
      videoCloseLabel: '关闭视频'
    },
    partnerLabel: '我们的合作伙伴:',
    featuredProducts: {
      title: '产品展示',
      description: '探索我们全系列的高性能排水产品，专为各类建筑与市政需求设计。',
      linkLabel: '了解更多',
      linkIcon: '→'
    },
    partners: ['LANDLEASE', 'LA SIMON BUILDERS', 'MA CIVIL DESIGN', 'MULTIPLEX', 'BESIX Watpac'],
    productIntro: {
      title: '我们的产品是什么？',
      description: '星渡ASTREVA 致力于研发和生产全球最快、最可靠的模块化排水系统。我们摒弃了传统笨重的排水沟渠，转而采用高强度不锈钢打造出点击即用的拼接系统。',
      image: 'https://images.unsplash.com/photo-1503387762-592deb58ef4e?auto=format&fit=crop&q=80&w=800',
      bullets: ['100% 模块化设计，缩短80%工期', '轻量化材料，减少运输和人力成本', '无缝隙拼接，彻底杜绝渗漏', '美观的建筑级外观，适配任何地坪'],
      bulletIcon: '✓',
      ctaLabel: '查看技术规格',
      ctaHref: '/products/'
    },
    advantagesIntro: {
      title: '体验产品优势',
      description: '为什么全球顶尖的承包商和建筑师选择 星渡ASTREVA？'
    },
    advantages: [
      { title: '极速安装', description: '专利 Click 咔哒锁扣系统，一秒拼接。', icon: 'play' },
      { title: '极致耐用', description: '提供高达 50 年的使用寿命质保。', icon: 'check' },
      { title: '超强承重', description: '通过 Class G 承载力测试，适用于重载区。', icon: 'wrench' },
      { title: '环保材料', description: '100% 可回收不锈钢，助力 LEED 认证。', icon: 'droplet' }
    ],
    testimonialsTitle: '我们的客户怎么评价',
    testimonials: [
      { name: '张伟', role: '项目经理, 某大型建设集团', text: '星渡ASTREVA 的模块化系统彻底改变了我们的施工方式。原本需要两周的排水沟安装，现在仅仅三天就完成了。太棒了！' },
      { name: 'David Miller', role: '首席建筑师', text: '从外观设计到水力性能，这都是我见过最优秀的排水产品。它能完美融入我们设计的现代商业广场。' },
      { name: '王工', role: '市政工程师', text: '他们提供的不仅是产品，更是完整的解决方案。遇到技术难题时，服务团队的支持非常及时专业。' }
    ],
    cta: {
      title: '正在规划您的下一个项目？',
      description: '如果您正好需要高效、美观且耐用的排水系统，请立即联系我们的工程专家团队获取免费报价。',
      primaryLabel: '立即联系我们',
      primaryHref: '/support/',
      secondaryLabel: '下载产品目录',
      secondaryHref: '/products/',
      backgroundImage: 'https://images.unsplash.com/photo-1558227691-41ea78d1f631?auto=format&fit=crop&q=80&w=1000'
    }
  },
  productCenter: {
    seo: {
      title: '产品中心 | 星渡ASTREVA',
      description: '按产品类型浏览星渡ASTREVA 地漏、盖板、排水沟、隔油沉淀设备与不锈钢井盖。'
    },
    hero: {
      title: '产品中心',
      description: '按产品类型快速查找专业不锈钢排水产品与系统组件。',
      backgroundImage: 'https://images.unsplash.com/photo-1504307651254-35680f356fce?auto=format&fit=crop&q=80&w=2000',
      backgroundVideo: HERO_VIDEOS.products
    },
    intro: {
      title: '产品分类',
      description: '选择产品体系，查看对应系列产品与组件。',
      categoryCtaLabel: '查看全部产品'
    },
    categoryPage: {
      titleTemplate: '{category} - 系列产品',
      backLabel: '返回产品分类',
      backIcon: '←',
      productHoverLabel: '查看详情',
      categoryCtaIcon: '→'
    },
    detailPage: {
      badgeLabel: '顶级工业品质',
      badgeIcon: '✓',
      featureIcon: '✓',
      quoteButtonLabel: '获取项目报价',
      quoteButtonIcon: '✉',
      downloadButtonIcon: '↓',
      breadcrumbSeparator: '›',
      specsTitle: '技术规格与参数',
      specsDescription: '标准化模块化尺寸，亦支持根据项目需求进行深度定制。',
      specsPrimaryIcon: '层',
      specsPrimaryTitle: '材质与工艺',
      descriptionIcon: '工',
      descriptionTitle: '产品说明',
      galleryTitle: '产品图库',
      videoTitle: '产品视频'
    }
  },
  categories: [
    {
      id: 'cat_floor_drain_system',
      name: '地漏系统',
      slug: 'floor-drain-system',
      description: '面向商业、住宅及工业空间的防臭、排水和过滤组件。',
      coverImage: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=800',
      heroVideo: HERO_VIDEOS.municipal,
      sortOrder: 10,
      seo: { title: '地漏系统 | 星渡ASTREVA', description: '防臭水封地漏、简易地漏、地漏箱体与滤网系列。' }
    },
    {
      id: 'cat_cover_grating',
      name: '盖板与格栅',
      slug: 'cover-grating',
      description: '缝隙、激光开孔、格栅及冲压成型盖板系列。',
      coverImage: 'https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&q=80&w=800',
      heroVideo: HERO_VIDEOS.commercial,
      sortOrder: 20,
      seo: { title: '盖板与格栅 | 星渡ASTREVA', description: '缝隙式、激光开孔、格栅与冲压拉伸盖板系列。' }
    },
    {
      id: 'cat_drainage_channel',
      name: '排水沟系统',
      slug: 'drainage-channel',
      description: '模块化不锈钢沟体、沟盖板与成品线性排水系统。',
      coverImage: 'https://images.unsplash.com/photo-1581094288338-2314dddb7ece?auto=format&fit=crop&q=80&w=800',
      heroVideo: HERO_VIDEOS.industrial,
      sortOrder: 30,
      seo: { title: '排水沟系统 | 星渡ASTREVA', description: '不锈钢排水沟、水沟盖板与成品线性排水沟。' }
    },
    {
      id: 'cat_oil_sedimentation',
      name: '隔油与沉淀设备',
      slug: 'oil-sedimentation',
      description: '用于餐饮、食品及工业场景的排水预处理设备。',
      coverImage: 'https://images.unsplash.com/photo-1572120360610-d971b9d7767c?auto=format&fit=crop&q=80&w=800',
      heroVideo: HERO_VIDEOS.landscape,
      sortOrder: 40,
      seo: { title: '隔油与沉淀设备 | 星渡ASTREVA', description: '不锈钢隔油池与沉淀池预处理设备。' }
    },
    {
      id: 'cat_stainless_manhole',
      name: '不锈钢井盖',
      slug: 'stainless-manhole',
      description: '隐形、可填充及定制检修井盖系列。',
      coverImage: 'https://images.unsplash.com/photo-1503387762-592deb58ef4e?auto=format&fit=crop&q=80&w=800',
      heroVideo: HERO_VIDEOS.stainless,
      sortOrder: 50,
      seo: { title: '不锈钢井盖 | 星渡ASTREVA', description: '不锈钢隐形井盖与可填充式井盖系列。' }
    }
  ],
  products: [
    catalogProduct({
      name: '防臭水封地漏', slug: 'anti-odor-water-seal-floor-drain', categorySlug: 'floor-drain-system',
      coverImage: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=900',
      summary: '利用稳定水封结构阻隔异味与害虫，适用于卫生间、厨房及公共空间。',
      features: ['稳定水封防臭', '可拆卸清洁', '304/316L 不锈钢'], specifications: [{ label: '出水口径', value: 'DN50 / DN75 / DN100' }], isFeatured: true, sortOrder: 10
    }),
    catalogProduct({
      name: '简易地漏', slug: 'simple-floor-drain', categorySlug: 'floor-drain-system',
      coverImage: 'https://images.unsplash.com/photo-1517581177682-a085bb7ffb15?auto=format&fit=crop&q=80&w=900',
      summary: '无防臭结构并配备可拆卸滤网，适合设备间、阳台及一般排水点。',
      features: ['结构简洁', '内置滤网', '维护方便'], specifications: [{ label: '材质', value: '304 / 316L 不锈钢' }], isFeatured: true, sortOrder: 20
    }),
    catalogProduct({
      name: '地漏箱体', slug: 'floor-drain-box', categorySlug: 'floor-drain-system',
      coverImage: 'https://images.unsplash.com/photo-1504307651254-35680f356fce?auto=format&fit=crop&q=80&w=900',
      summary: '模块化不锈钢地漏箱体，支持多种盖板、出水方向及现场安装条件。',
      features: ['一体成型箱体', '多规格接口', '支持定制深度'], specifications: [{ label: '接口方向', value: '底出 / 侧出' }], isFeatured: false, sortOrder: 30
    }),
    catalogProduct({
      name: '地漏滤网', slug: 'floor-drain-filter', categorySlug: 'floor-drain-system',
      coverImage: 'https://images.unsplash.com/photo-1581094288338-2314dddb7ece?auto=format&fit=crop&q=80&w=900',
      summary: '拦截毛发、残渣和较大杂物的可拆卸不锈钢过滤组件。',
      features: ['精细过滤', '快速拆装', '耐腐蚀'], specifications: [{ label: '过滤精度', value: '按使用场景定制' }], isFeatured: false, sortOrder: 40
    }),
    catalogProduct({
      name: '缝隙式盖板', slug: 'slot-cover', categorySlug: 'cover-grating',
      coverImage: 'https://images.unsplash.com/photo-1503387762-592deb58ef4e?auto=format&fit=crop&q=80&w=900',
      summary: '以窄缝收水降低视觉干扰，适配商业、住宅与景观铺装。',
      features: ['低视觉干扰', '适配多种铺装', '模块化安装'], specifications: [{ label: '缝隙宽度', value: '8mm / 12mm / 20mm' }], isFeatured: true, sortOrder: 10
    }),
    catalogProduct({
      name: '激光开孔盖板', slug: 'laser-perforated-cover', categorySlug: 'cover-grating',
      coverImage: 'https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&q=80&w=900',
      summary: '激光精密开孔，可按排水能力、图案和防滑要求定制。',
      features: ['精密激光加工', '孔型可定制', '表面平整'], specifications: [{ label: '板厚', value: '1.5mm - 5mm' }], isFeatured: true, sortOrder: 20
    }),
    catalogProduct({
      name: '格栅盖板', slug: 'grating-cover', categorySlug: 'cover-grating',
      coverImage: 'https://images.unsplash.com/photo-1572120360610-d971b9d7767c?auto=format&fit=crop&q=80&w=900',
      summary: '兼顾快速收水、承载与检修效率的不锈钢格栅盖板。',
      features: ['高效过水', '多级承载', '便于检修'], specifications: [{ label: '承载等级', value: 'A15 - D400' }], isFeatured: false, sortOrder: 30
    }),
    catalogProduct({
      name: '冲压拉伸盖板', slug: 'stamped-stretched-cover', categorySlug: 'cover-grating',
      coverImage: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=900',
      summary: '冲压拉伸一体成型，强度稳定并适合标准化批量应用。',
      features: ['一体成型', '稳定耐用', '标准化生产'], specifications: [{ label: '表面处理', value: '拉丝 / 喷砂 / 防滑' }], isFeatured: false, sortOrder: 40
    }),
    catalogProduct({
      name: '不锈钢排水沟', slug: 'stainless-drainage-channel', categorySlug: 'drainage-channel',
      coverImage: 'https://images.unsplash.com/photo-1581094288338-2314dddb7ece?auto=format&fit=crop&q=80&w=900',
      summary: '适用于商业、食品、工业与公共工程的模块化不锈钢排水沟体。',
      features: ['模块化拼接', '耐腐蚀', '截面可定制'], specifications: [{ label: '主体材质', value: '304 / 316L 不锈钢' }], isFeatured: true, sortOrder: 10
    }),
    catalogProduct({
      name: '水沟盖板', slug: 'channel-cover', categorySlug: 'drainage-channel',
      coverImage: 'https://images.unsplash.com/photo-1517581177682-a085bb7ffb15?auto=format&fit=crop&q=80&w=900',
      summary: '与排水沟体配套的盖板系统，支持缝隙、格栅及开孔等形式。',
      features: ['多种面板形式', '快速开启', '承载可选'], specifications: [{ label: '适配沟宽', value: '100mm - 600mm' }], isFeatured: true, sortOrder: 20
    }),
    catalogProduct({
      name: '成品线性排水沟', slug: 'finished-linear-drain', categorySlug: 'drainage-channel',
      coverImage: 'https://images.unsplash.com/photo-1504307651254-35680f356fce?auto=format&fit=crop&q=80&w=900',
      summary: '沟体、盖板与连接件成套交付，缩短现场安装时间。',
      features: ['系统化交付', '安装高效', '节点完整'], specifications: [{ label: '标准长度', value: '500mm / 1000mm' }], isFeatured: false, sortOrder: 30
    }),
    catalogProduct({
      name: '隔油池', slug: 'grease-interceptor', categorySlug: 'oil-sedimentation',
      coverImage: 'https://images.unsplash.com/photo-1572120360610-d971b9d7767c?auto=format&fit=crop&q=80&w=900',
      summary: '用于餐饮与食品场景的油水分离设备，支持地上或地埋安装。',
      features: ['油水分离', '易清掏维护', '容量可定制'], specifications: [{ label: '安装方式', value: '地上 / 地埋' }], isFeatured: true, sortOrder: 10
    }),
    catalogProduct({
      name: '沉淀池', slug: 'sedimentation-tank', categorySlug: 'oil-sedimentation',
      coverImage: 'https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&q=80&w=900',
      summary: '对排水中的颗粒、泥沙与悬浮物进行预沉淀处理。',
      features: ['颗粒沉淀', '多级结构可选', '检修清理方便'], specifications: [{ label: '有效容积', value: '按流量设计' }], isFeatured: false, sortOrder: 20
    }),
    catalogProduct({
      name: '不锈钢隐形井盖', slug: 'stainless-concealed-manhole-cover', categorySlug: 'stainless-manhole',
      coverImage: 'https://images.unsplash.com/photo-1503387762-592deb58ef4e?auto=format&fit=crop&q=80&w=900',
      summary: '与石材、地砖及景观铺装融合的不锈钢隐形检修井盖。',
      features: ['隐藏式外观', '防臭防水', '开启便利'], specifications: [{ label: '常用尺寸', value: '600×600 / 800×800mm' }], isFeatured: true, sortOrder: 10
    }),
    catalogProduct({
      name: '可填充式井盖', slug: 'infill-manhole-cover', categorySlug: 'stainless-manhole',
      coverImage: 'https://images.unsplash.com/photo-1541888086425-d81bb19240f5?auto=format&fit=crop&q=80&w=900',
      summary: '托盘内可填充石材、混凝土或地砖，兼顾承载与环境一致性。',
      features: ['多种填充面层', '高承载框体', '防跳动设计'], specifications: [{ label: '填充深度', value: '30mm - 100mm' }], isFeatured: false, sortOrder: 20
    })
  ],
  support: {
    seo: { title: '服务与支持 | 星渡ASTREVA', description: '获取星渡ASTREVA 产品报价、技术支持、安装指南和售后质保服务。' },
    hero: {
      title: '服务与支持',
      description: '我们提供全方位的技术支持、安装指导与售后服务，确保您的项目万无一失。',
      backgroundImage: 'https://images.unsplash.com/photo-1581094288338-2314dddb7ece?auto=format&fit=crop&q=80&w=2000',
      backgroundVideo: HERO_VIDEOS.support
    },
    services: [
      { title: '售后与质保', description: '了解我们长达50年的超长使用寿命质保政策，以及专业的现场维护与售后检测服务。', icon: 'shield', href: '/support/warranty/', cta: '查看保修政策' },
      { title: '安装指南', description: '查看详细的视频教程与步骤指南，了解 Click咔哒锁扣的极致安装技巧与图纸规范。', icon: 'wrench', href: '/support/install/', cta: '查看安装手册' },
      { title: '常见问题 (FAQ)', description: '快速寻找关于产品选型、承重标准、发货周期以及定制化需求的常见问题解答。', icon: 'help', href: '/support/faq/', cta: '浏览常见问题' }
    ],
    contact: {
      title: '联系我们',
      description: '无论您是需要索取报价，还是技术咨询，我们的专家团队都在这里准备为您服务。',
      phoneLabel: '销售咨询',
      phoneIcon: '☎',
      emailLabel: '企业邮箱',
      emailIcon: '✉',
      form: {
        nameLabel: '您的姓名',
        namePlaceholder: '请输入姓名或公司名',
        nameCompanyLabel: '您的姓名 / 公司名',
        nameCompanyPlaceholder: '请输入您的姓名或公司名',
        companyLabel: '公司或项目名称',
        companyPlaceholder: '请输入公司或项目名称',
        optionalLabel: '选填',
        phoneLabel: '联系电话',
        phonePlaceholder: '请输入手机号或邮箱号',
        contactLabel: '手机号 / 邮箱号',
        contactPlaceholder: '请输入手机号或邮箱号',
        emailLabel: '电子邮箱',
        emailPlaceholder: '您的邮箱',
        messageLabel: '留言内容',
        messagePlaceholder: '请描述您的项目需求或技术疑问...',
        submitLabel: '提交信息',
        submittingLabel: '提交中...',
        successMessage: '提交成功，我们会尽快联系您。',
        errorMessage: '提交失败，请通过电话或邮箱联系我们。',
        missingEndpointMessage: '后台接口尚未配置，请稍后再试。'
      }
    }
  },
  faqs: [
    { id: 'faq_custom', question: '你们的排水系统支持定制吗？', answer: '完全支持。除了标准化模块，我们的工程团队还可以根据您的现场地形、水力流量和曲线要求，定制异形、弧形及特殊宽度的不锈钢排水系统。', sortOrder: 10, enabled: true },
    { id: 'faq_click', question: 'Click系统为什么比传统的排水沟安装快这么多？', answer: '传统树脂混凝土或砖砌排水沟非常笨重，且需要繁琐的抹灰或现场焊接。我们的 Click 系统重量仅为传统的五分之一，且自带机械互锁防水卡扣，彻底免除了焊接与固化等待时间，即拼即用。', sortOrder: 20, enabled: true },
    { id: 'faq_load', question: '产品能够承受重型卡车碾压吗？', answer: '可以。我们特定的重载型号符合欧盟 EN1433 的 F900 级别，能承受机场跑道和重型物流仓储的极端负荷。', sortOrder: 30, enabled: true },
    { id: 'faq_delivery', question: '订货周期一般是多久？', answer: '对于常规型号和长度，我们拥有充足的库存，可在 3-5 个工作日内发货。如遇高度定制化的工程系统，生产周期通常在 2-4 周之间。', sortOrder: 40, enabled: true }
  ],
  staticPages: [
    {
      slug: 'about',
      title: '关于星渡ASTREVA',
      hero: {
        eyebrow: 'About Us',
        title: '推动全球排水创新的力量',
        description: '自成立以来，星渡ASTREVA 始终致力于用前沿科技与顶级制造工艺，重塑建筑环境的生命线。',
        backgroundImage: 'https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&q=80&w=2000',
        backgroundVideo: HERO_VIDEOS.about
      },
      sections: {
        introIcon: '滴',
        partnersTitle: '全球信赖的合作伙伴'
      },
      body: '<p>星渡ASTREVA 是全球领先的建筑与市政排水系统研发制造商。我们摒弃了传统的铸铁与水泥沟渠，开创性地推出了 <strong>100% 模块化的不锈钢 Click 咔哒锁扣系统</strong>。</p><p>我们的研发中心与制造基地配备了全球最先进的自动化生产线，确保每一寸不锈钢材都达到航天级精度。</p><h2>我们不仅制造产品，更在重构工程的标准。</h2><p>从商业广场的隐形缝隙，到市政重载道路的高强度格栅，星渡ASTREVA 以极致的耐腐蚀性、超过 50 年的超长使用寿命以及极致的安装速度，赢得了全球顶级建筑师和总包商的信赖。</p>',
      seo: { title: '关于我们 | 星渡ASTREVA', description: '了解星渡ASTREVA 的品牌、技术理念和排水系统创新。' }
    },
    {
      slug: 'solutions',
      title: '解决方案',
      hero: {
        eyebrow: 'Solutions',
        title: '面向真实场景的\n系统化排水方案',
        description: '从水力模型计算、承载等级评估到材料化学性能匹配，星渡ASTREVA 不仅提供排水沟，更提供应对极端环境的工程确定性。',
        backgroundImage: 'https://images.unsplash.com/photo-1504307651254-35680f356fce?auto=format&fit=crop&q=80&w=2000',
        backgroundVideo: HERO_VIDEOS.solutions
      },
      sections: {
        introTitle: '按应用场景选择方案',
        introDescription: '每个方案都关联到真实排水场景、产品组合和工程交付要求，方便客户快速判断选型方向。',
        ctaLabel: '查看详细技术手册',
        ctaHref: '/support/',
        sideNavTitle: '方案导航',
        architectureTitle: '隐形缝隙式系统架构',
        detailNote: '后台可继续补充该节点的技术说明。',
        adviceTitle: '需要专业选型建议？',
        advicePrimaryLabel: '咨询方案专家',
        advicePrimaryHref: '/support/',
        adviceSecondaryLabel: '下载全线目录',
        adviceSecondaryHref: '/products/',
        solutionSections: [
          {
            id: 'sol-sec-1',
            number: '01',
            navLabel: '市政道路排水',
            title: '市政道路与城市公共广场',
            description: '针对城市主干道、交通枢纽及大型公共广场，排水系统面临的首要挑战是高频率载荷与突发性强降水。星渡 F900 级模块化系统专为此设计。',
            image: 'https://images.unsplash.com/photo-1577086664693-894d8405334a?auto=format&fit=crop&q=80&w=800',
            videoImage: 'https://images.unsplash.com/photo-1545143333-11bb2f7d9ad6?auto=format&fit=crop&q=80&w=1200',
            videoUrl: HERO_VIDEOS.products,
            videoCaption: '演示视频：F900 承载不锈钢系统在城市枢纽的安装演示',
            focusTitle: '核心解决：结构稳定性',
            focusDescription: '采用不锈钢一体化预制沟体，替代传统现浇沟渠。其物理特性保证了在重载碾压下不会产生混凝土剥落或缝隙扩大的风险。Click 锁扣系统确保长距离安装的一致性。',
            bullets: ['满足 EN1433 标准 F900 最高承载要求', '抗沉降设计，防止路面衔接处断裂', '极速排水模型计算，应对 50 年一遇暴雨']
          },
          {
            id: 'sol-sec-2',
            number: '02',
            navLabel: '商业综合体',
            title: '商业综合体与五星级酒店',
            description: '商业环境不仅要求功能卓越，更要求视觉隐形。排水系统必须与高端石材铺装完美融合，同时满足高人流量下的安全性与维护便捷性。',
            image: 'https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&q=80&w=900',
            focusTitle: '美学与性能的终极平衡',
            focusDescription: '星渡隐形排水系统已广泛应用于核心地标项目。我们提供完整的 BIM 适配图纸，确保在设计阶段即可完成铺装模数对齐。',
            bullets: ['10-20mm 极窄缝隙收水', '隐藏式检修口与同色铺装融合', '适配花岗岩、陶板、金属地坪等多种材料']
          },
          {
            id: 'sol-sec-3',
            number: '03',
            navLabel: '工业与洁净',
            title: '工业厂房与洁净室排水',
            description: '在食品、医药生产车间，排水系统是污染控制的核心。星渡工业级全不锈钢明沟系统采用镜面抛光处理，支持 CIP 在位清洗。',
            image: 'https://images.unsplash.com/photo-1581091226825-a6a2a5aee158?auto=format&fit=crop&q=80&w=1200',
            focusTitle: '抗腐蚀、易清洁、可追溯',
            focusDescription: '针对重工业仓储，我们提供可承载重型叉车频繁转向的高强格栅配置，并为洁净厂房提供无卫生死角的圆角沟体。',
            bullets: ['316L 不锈钢材质，耐强酸强碱清洗环境', '符合 HACCP 要求的圆角设计', '叉车与物流转运区域可选重载格栅']
          },
          {
            id: 'sol-sec-4',
            number: '04',
            navLabel: '高端景观定制',
            title: '高端景观与异形地坪定制',
            description: '景观空间的排水不应打断设计语言。星渡支持曲线、异形、窄缝、隐藏检修口等定制构造，让排水系统融入地坪线条。',
            image: 'https://images.unsplash.com/photo-1572120360610-d971b9d7767c?auto=format&fit=crop&q=80&w=1200',
            focusTitle: '让排水成为景观的一部分',
            focusDescription: '从弧形铺装到树池边界，从屋顶花园到商业中庭，我们把排水节点拆成可预制、可维护、可替换的模块。',
            bullets: ['支持弧形、折线与特殊节点预制', '盖板和缝隙宽度可按铺装模数定制', '减少现场切割和二次返工']
          },
          {
            id: 'sol-sec-5',
            number: '05',
            navLabel: '海绵城市/生态',
            title: '海绵城市与生态雨水利用',
            description: '面对城市内涝和雨洪管理压力，星渡将线性排水、沉砂、过滤、调蓄与雨水回用节点组合为完整系统。',
            image: 'https://images.unsplash.com/photo-1519681393784-d120267933ba?auto=format&fit=crop&q=80&w=1200',
            focusTitle: '从排走雨水到管理雨水',
            focusDescription: '通过模块化检查井、雨水收集器与透水铺装节点，我们帮助项目在满足排水安全的同时兼顾绿色建筑指标。',
            bullets: ['适配海绵城市设计导则', '支持沉砂、过滤、调蓄节点组合', '可与景观水体和雨水回用系统衔接']
          }
        ],
        systemCards: [
          { step: 'Step 01. 窄缝收水', title: '仅 10-20mm 的缝隙，完美隐藏于石材缝隙中。' },
          { step: 'Step 02. 高强沟体', title: 'U 型光滑底部提供超高自净流速，减少淤积。' },
          { step: 'Step 03. 隐藏式检修', title: '专利检修口盖板可填充同色石材，实现视觉统一。' }
        ]
      },
      body: '<p>星渡ASTREVA 将排水系统拆解为可组合的模块化方案，帮助工程团队更快完成选型、深化与现场安装。</p>',
      seo: { title: '解决方案 | 星渡ASTREVA', description: '浏览星渡ASTREVA 市政、商业、工业、景观、海绵城市等排水系统解决方案。' }
    },
    {
      slug: 'faq',
      title: '常见问题 (FAQs)',
      hero: {
        eyebrow: 'FAQ',
        title: '常见问题 (FAQs)',
        description: '查看星渡ASTREVA 排水系统的产品选型、定制、承载、交付和安装常见问题。',
        backgroundVideo: HERO_VIDEOS.faq
      },
      body: '',
      sections: {
        intro: ''
      },
      seo: { title: '常见问题 | 星渡ASTREVA', description: '查看星渡ASTREVA 排水系统的产品选型、定制、承载、交付和安装常见问题。' }
    },
    {
      slug: 'warranty',
      title: '售后与质保政策',
      hero: {
        eyebrow: 'Warranty',
        title: '售后与质保政策',
        description: '了解星渡ASTREVA 不锈钢排水系统质保政策和售后服务。',
        backgroundVideo: HERO_VIDEOS.warranty
      },
      sections: { icon: '盾' },
      body: '<p>在星渡ASTREVA，我们对产品的卓越品质充满信心。我们的不锈钢排水系统专为承受最严苛的环境而设计。</p><h3>1. 50年超长材料质保</h3><p>对于采用 316/316L 船用级不锈钢制造的核心排水模块，在正常使用和适当维护的情况下，我们提供长达 50 年的抗腐蚀和结构完整性质保。</p><h3>2. 性能与承载素质保证</h3><p>星渡ASTREVA 出厂的所有格栅与盖板均通过独立的第三方负载测试，符合全球主流承重等级。</p>',
      seo: { title: '售后与质保政策 | 星渡ASTREVA', description: '星渡ASTREVA 不锈钢排水系统质保政策和售后服务。' }
    },
    {
      slug: 'install',
      title: 'Click 咔哒锁扣系统 安装指南',
      hero: {
        eyebrow: 'Installation',
        title: 'Click 咔哒锁扣系统 安装指南',
        description: '查看星渡ASTREVA Click 咔哒锁扣排水系统安装步骤。',
        backgroundVideo: HERO_VIDEOS.install
      },
      sections: { icon: '工' },
      body: '<h3>Step 1 基础沟槽准备</h3><p>根据项目图纸要求，开挖具有足够宽度和深度的沟槽，并铺设混凝土垫层。</p><h3>Step 2 模块拼接与锁定</h3><p>将相邻两段星渡不锈钢排水沟模块对齐，通过专利 Click 锁扣机制实现无缝防水连接。</p><h3>Step 3 高度调节与固定</h3><p>利用沟体两侧自带的高度调节支架螺栓，将排水系统精确调整至标高水平面。</p>',
      seo: { title: '安装指南 | 星渡ASTREVA', description: '查看星渡ASTREVA Click 咔哒锁扣排水系统安装步骤。' }
    },
    {
      slug: 'privacy',
      title: '隐私政策',
      hero: {
        eyebrow: 'Privacy',
        title: '隐私政策',
        description: '星渡ASTREVA 官网隐私政策。',
        backgroundVideo: HERO_VIDEOS.privacy
      },
      body: '<p>生效日期：2026年1月1日</p><p>星渡ASTREVA 高度重视您的隐私。本隐私政策解释了当您访问我们的网站或使用我们的服务时，我们如何收集、使用、披露和保护您的个人信息。</p><h3>信息收集</h3><p>当您通过我们的在线表单提交咨询、申请报价或订阅资讯时，我们可能会收集您的姓名、联系电话、电子邮件地址以及项目需求。</p>',
      seo: { title: '隐私政策 | 星渡ASTREVA', description: '星渡ASTREVA 官网隐私政策。' }
    },
    {
      slug: 'agreement',
      title: '用户服务协议',
      hero: {
        eyebrow: 'Agreement',
        title: '用户服务协议',
        description: '星渡ASTREVA 官网用户服务协议。',
        backgroundVideo: HERO_VIDEOS.agreement
      },
      body: '<p>感谢您访问星渡ASTREVA官方网站。在使用本网站之前，请仔细阅读以下条款和条件。</p><h3>网站内容与知识产权</h3><p>本网站上的所有内容均受版权法保护，未经书面许可，任何人不得出于商业目的复制、分发或修改这些资料。</p><h3>产品信息免责声明</h3><p>具体工程参数应以我们的工程师为您出具的最终正式方案及技术协议为准。</p>',
      seo: { title: '用户服务协议 | 星渡ASTREVA', description: '星渡ASTREVA 官网用户服务协议。' }
    }
  ]
};
