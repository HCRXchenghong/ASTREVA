import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/operations',
  name: 'OperationsRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/operations/leads',
  meta: {
    title: '运营',
    requiresAuth: true,
    icon: 'icon-bar-chart',
    order: 3,
  },
  children: [
    {
      path: 'leads',
      name: 'Leads',
      component: () => import('@/views/operations/leads.vue'),
      meta: { title: '咨询单', requiresAuth: true },
    },
    {
      path: 'keywords',
      name: 'Keywords',
      component: () => import('@/views/operations/keywords.vue'),
      meta: { title: '关键词规则', requiresAuth: true },
    },
    {
      path: 'audit',
      name: 'Audit',
      component: () => import('@/views/operations/audit.vue'),
      meta: { title: '审计日志', requiresAuth: true },
    },
  ],
};
