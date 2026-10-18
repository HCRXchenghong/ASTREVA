import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/apps',
  name: 'AppsRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/apps/releases',
  meta: {
    title: '应用管理',
        locale: '应用管理',
    requiresAuth: true,
    icon: 'icon-mobile',
    order: 4,
    hideInMenu: false,
  },
  children: [
    {
      path: 'releases',
      name: 'Releases',
      component: () => import('@/views/apps/releases.vue'),
      meta: { title: 'App 版本',
        locale: 'App 版本', requiresAuth: true },
    },
    {
      path: 'api',
      name: 'ApiApps',
      component: () => import('@/views/apps/api.vue'),
      meta: { title: 'API 管理',
        locale: 'API 管理', requiresAuth: true },
    },
  ],
};
