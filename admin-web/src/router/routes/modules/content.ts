import { DEFAULT_LAYOUT } from '../base';

// 内容/翻译/素材三个模块目前复用既有后台页面（iframe），
// 后续会像其它页面一样迁入原生 Arco 视图。
export default {
  path: '/content',
  name: 'ContentRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/content/site',
  meta: {
    title: '内容管理',
    requiresAuth: true,
    icon: 'icon-file',
    order: 5,
  },
  children: [
    {
      path: 'site',
      name: 'SiteContent',
      component: () => import('@/views/content/legacy-frame.vue'),
      props: { src: '/site-admin/content/?embed=1', title: '官网内容管理' },
      meta: { title: '官网内容', requiresAuth: true },
    },
    {
      path: 'translations',
      name: 'Translations',
      component: () => import('@/views/content/legacy-frame.vue'),
      props: { src: '/site-admin/translations?embed=1', title: '网站翻译' },
      meta: { title: '网站翻译', requiresAuth: true },
    },
    {
      path: 'media',
      name: 'Media',
      component: () => import('@/views/content/legacy-frame.vue'),
      props: { src: '/site-admin/media?embed=1', title: 'R2 素材库' },
      meta: { title: 'R2 素材库', requiresAuth: true },
    },
  ],
};
