import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/ai',
  name: 'AiRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/ai/manage',
  meta: {
    title: 'AI 管理',
    locale: 'AI 管理',
    requiresAuth: true,
    icon: 'IconRobot',
    order: 4,
    hideInMenu: false,
  },
  children: [
    {
      // AI 管理：接待 AI / 翻译 AI / AI SEO / AI 生图 的能力参数 + 能力绑定（故障转移链）+ 限额。
      // alias 保留全部旧入口（含 system.ts 里 /system/ai 的跳转、素材库里的「前往 AI 生图」），
      // 旧路径会落到对应的页签（manage.vue 按路径末段选页签）。
      path: 'manage',
      alias: [
        '/ai/reception',
        '/ai/image',
        '/ai/seo',
        '/ai/translation',
        '/ai/bindings',
        '/ai/limits',
      ],
      name: 'AiManage',
      component: () => import('@/views/ai/manage.vue'),
      meta: { title: 'AI 管理', locale: 'AI 管理', requiresAuth: true },
    },
    {
      path: 'providers',
      name: 'AiProviders',
      component: () => import('@/views/ai/providers.vue'),
      meta: { title: '模型供应商', locale: '模型供应商', requiresAuth: true },
    },
    {
      path: 'knowledge',
      name: 'AiKnowledge',
      component: () => import('@/views/ai/knowledge.vue'),
      meta: { title: '知识库', locale: '知识库', requiresAuth: true },
    },
  ],
};
