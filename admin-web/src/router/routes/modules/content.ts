import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/content',
  name: 'ContentRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/content/site',
  meta: {
    title: '内容管理',
        locale: '内容管理',
    requiresAuth: true,
    icon: 'icon-file',
    order: 5,
    hideInMenu: false,
  },
  children: [
    {
      path: 'site',
      name: 'SiteContent',
      component: () => import('@/views/content/content-editor.vue'),
      meta: { title: '官网内容',
        locale: '官网内容', requiresAuth: true },
    },
    {
      path: 'translations',
      name: 'Translations',
      component: () => import('@/views/content/translations.vue'),
      meta: { title: '网站翻译',
        locale: '网站翻译', requiresAuth: true },
    },
    {
      path: 'media',
      name: 'Media',
      component: () => import('@/views/content/media.vue'),
      meta: { title: 'R2 素材库',
        locale: 'R2 素材库', requiresAuth: true },
    },
  ],
};
