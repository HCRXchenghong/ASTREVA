import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/dashboard',
  name: 'DashboardRoot',
  component: DEFAULT_LAYOUT,
  meta: {
    title: '工作台',
    locale: '工作台',
    requiresAuth: true,
    icon: 'IconDashboard',
    order: 1,
    hideInMenu: false,
  },
  children: [
    {
      path: '',
      name: 'Workspace',
      component: () => import('@/views/dashboard/index.vue'),
      meta: {
        title: '数据看板',
        locale: '数据看板',
        requiresAuth: true,
      },
    },
  ],
};
