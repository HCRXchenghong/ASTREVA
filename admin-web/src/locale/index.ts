import { createI18n } from 'vue-i18n';

// 控制台是中文单语控制台：菜单标题直接使用路由 meta.locale/title（中文）。
// vue-i18n 找不到 key 时会原样返回 key，因此这里不需要维护语言包。
const i18n = createI18n({
  legacy: false,
  allowComposition: true,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  missingWarn: false,
  fallbackWarn: false,
  messages: { 'zh-CN': {} },
});

export default i18n;
