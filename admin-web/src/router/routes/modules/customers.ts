import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/customers',
  name: 'CustomersRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/customers/agents',
  meta: {
    title: '客服管理',
    requiresAuth: true,
    icon: 'icon-user-group',
    order: 2,
  },
  children: [
    {
      path: 'agents',
      name: 'Agents',
      component: () => import('@/views/customers/agents.vue'),
      meta: { title: '客服账号', requiresAuth: true },
    },
    {
      path: 'evaluations',
      name: 'Evaluations',
      component: () => import('@/views/customers/evaluations.vue'),
      meta: { title: '服务评价', requiresAuth: true },
    },
  ],
};
