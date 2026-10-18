import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/ai',
  name: 'AiRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/ai/reception',
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
      path: 'reception',
      name: 'AiReception',
      component: () => import('@/views/ai/reception.vue'),
      meta: { title: '接待 AI', locale: '接待 AI', requiresAuth: true },
    },
    {
      path: 'image',
      name: 'AiImage',
      component: () => import('@/views/ai/image.vue'),
      meta: { title: 'AI 生图', locale: 'AI 生图', requiresAuth: true },
    },
    {
      path: 'seo',
      name: 'AiSeo',
      component: () => import('@/views/ai/seo.vue'),
      meta: { title: 'AI SEO', locale: 'AI SEO', requiresAuth: true },
    },
    {
      path: 'translation',
      name: 'AiTranslation',
      component: () => import('@/views/ai/translation.vue'),
      meta: { title: '翻译 AI', locale: '翻译 AI', requiresAuth: true },
    },
  ],
};
