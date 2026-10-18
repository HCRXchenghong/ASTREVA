import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/workspace',
  name: 'WorkspaceRoot',
  component: DEFAULT_LAYOUT,
  meta: {
    title: '工作台',
        locale: '工作台',
    requiresAuth: true,
    icon: 'icon-dashboard',
    order: 1,
    hideInMenu: false,
  },
  children: [
    {
      path: '',
      name: 'Workspace',
      component: () => import('@/views/workspace/index.vue'),
      meta: {
        title: '实时工作台',
        locale: '实时工作台',
        requiresAuth: true,
      },
    },
  ],
};
