import assert from 'node:assert/strict';
import test from 'node:test';
import { collectSeoTargets, createWebsiteSeoService } from './website-seo-service.mjs';

function sampleContent() {
  return {
    site: {
      locale: 'zh-CN',
      brandName: '星渡ASTREVA',
      brandTagline: '排水系统',
      logoUrl: '/logo.png',
      defaultSeo: { title: '旧默认标题', description: '旧默认描述', image: '' },
      footer: { description: '工业与市政排水解决方案' }
    },
    home: {
      hero: { title: '排水系统', subtitle: '模块化排水产品', backgroundImage: '/home.jpg' },
      seo: { title: '旧首页标题', description: '旧首页描述', image: '' }
    },
    productCenter: {
      hero: { title: '产品中心', description: '产品列表', backgroundImage: '' },
      seo: { title: '旧产品标题', description: '旧产品描述', image: '' }
    },
    support: {
      hero: { title: '服务支持', description: '选型安装支持', backgroundImage: '' },
      seo: { title: '旧服务标题', description: '旧服务描述', image: '/support.jpg' }
    },
    categories: [{ id: 'cat_1', slug: 'drainage', name: '排水沟', description: '不锈钢排水沟', coverImage: '/category.jpg', seo: { title: '旧品类标题', description: '旧品类描述' } }],
    products: [{ id: 'prod_1', slug: 'p1', categorySlug: 'drainage', name: '模块化排水沟', summary: '适用于工业建筑', features: ['模块化'], specifications: [], body: '', coverImage: '/product.jpg', seo: { title: '旧产品详情', description: '旧产品详情描述' } }],
    staticPages: [{ slug: 'about', title: '关于我们', hero: { title: '关于 ASTREVA', description: '品牌介绍' }, body: '专注排水系统', seo: { title: '旧关于标题', description: '旧关于描述' } }]
  };
}

test('AI SEO 覆盖所有页面并保留或补齐分享图', async () => {
  const content = sampleContent();
  const targets = collectSeoTargets(content);
  assert.deepEqual(targets.map((item) => item.id), [
    'site.defaultSeo', 'home.seo', 'productCenter.seo', 'support.seo', 'category:cat_1', 'product:prod_1', 'staticPage:about'
  ]);
  const aiService = {
    async generateJson(prompt) {
      const input = JSON.parse(prompt.slice(prompt.lastIndexOf('\n') + 1));
      return input.map((item) => ({
        id: item.id,
        title: `${item.name}专业排水解决方案 | ASTREVA`,
        description: `${item.name}页面集中介绍 ASTREVA 现有的工业、建筑与市政排水产品信息、应用内容和专业服务，帮助用户快速完成选型与咨询。`,
        keywords: `${item.name},ASTREVA,排水系统,工业排水,建筑排水,市政排水`
      }));
    }
  };
  const result = await createWebsiteSeoService({ aiService }).generate(content);
  assert.equal(result.generatedCount, 7);
  assert.match(result.content.home.seo.title, /ASTREVA/);
  assert.equal(result.content.home.seo.image, '/home.jpg');
  assert.equal(result.content.support.seo.image, '/support.jpg');
  assert.equal(result.content.productCenter.seo.image, '/logo.png');
  assert.equal(result.content.staticPages[0].seo.image, '/logo.png');
  assert.equal(content.home.seo.title, '旧首页标题');
});

test('AI SEO 拒绝缺项和重复 ID', async () => {
  const aiService = {
    async generateJson() {
      return [{ id: 'site.defaultSeo', title: '有效的专业 SEO 标题', description: '这是一段长度足够但返回条目数量不足的 SEO 描述内容，用于验证严格校验。', keywords: 'ASTREVA,排水系统' }];
    }
  };
  await assert.rejects(() => createWebsiteSeoService({ aiService }).generate(sampleContent()), /预期 7 项/);
});

test('英文和阿拉伯语 SEO 使用本语言页面内容独立生成', async () => {
  const calls = [];
  const aiService = {
    async generateJson(prompt, options) {
      const locale = options.capability === 'seo'
        ? (prompt.match(/当前目标语言是 ([^；]+)/)?.[1] || '')
        : '';
      calls.push(locale);
      const input = JSON.parse(prompt.slice(prompt.lastIndexOf('\n') + 1));
      const words = locale === 'ar'
        ? ['حلول تصريف ASTREVA', 'تعرّف على منتجات وحلول ASTREVA لتصريف المياه الصناعية والمباني وخدماتها المهنية ومعلومات التطبيق.', 'تصريف المياه,ASTREVA,الصرف الصناعي']
        : ['ASTREVA Drainage Solutions', 'Explore ASTREVA drainage products, applications and professional services for industrial and building projects.', 'drainage systems,ASTREVA,industrial drainage'];
      return input.map((item) => ({ id: item.id, title: words[0], description: words[1], keywords: words[2] }));
    }
  };
  const source = sampleContent();
  const service = createWebsiteSeoService({ aiService });
  const englishContent = structuredClone(source);
  englishContent.site.brandName = 'ASTREVA';
  englishContent.home.hero.title = 'Industrial drainage systems';
  const english = await service.generateLocale(englishContent, 'en');
  const arabicContent = structuredClone(source);
  arabicContent.site.brandName = 'ASTREVA';
  arabicContent.home.hero.title = 'أنظمة تصريف صناعية';
  const arabic = await service.generateLocale(arabicContent, 'ar');

  assert.deepEqual(calls, ['en', 'ar']);
  assert.equal(english.locale, 'en');
  assert.equal(arabic.locale, 'ar');
  assert.equal(english.content.home.seo.title, 'ASTREVA Drainage Solutions');
  assert.equal(arabic.content.home.seo.title, 'حلول تصريف ASTREVA');
  assert.equal(source.home.seo.title, '旧首页标题');
});
