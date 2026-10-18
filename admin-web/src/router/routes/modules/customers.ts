import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/customers',
  name: 'CustomersRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/customers/agents',
  meta: {
    title: '客服管理',
        locale: '客服管理',
    requiresAuth: true,
    icon: 'IconUserGroup',
    order: 2,
    hideInMenu: false,
  },
  children: [
    {
      path: 'agents',
      name: 'Agents',
      component: () => import('@/views/customers/agents.vue'),
      meta: { title: '客服账号',
        locale: '客服账号', requiresAuth: true },
    },
    {
      path: 'evaluations',
      name: 'Evaluations',
      component: () => import('@/views/customers/evaluations.vue'),
      meta: { title: '服务评价',
        locale: '服务评价', requiresAuth: true },
    },
  ],
};
