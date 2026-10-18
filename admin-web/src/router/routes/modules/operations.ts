import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/operations',
  name: 'OperationsRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/operations/inquiries',
  meta: {
    title: '运营中心',
    locale: '运营中心',
    requiresAuth: true,
    icon: 'IconBarChart',
    order: 2,
    hideInMenu: false,
  },
  children: [
    {
      path: 'inquiries',
      name: 'Inquiries',
      component: () => import('@/views/operations/inquiries.vue'),
      meta: { title: '询盘管理', locale: '询盘管理', requiresAuth: true },
    },
    {
      path: 'leads',
      name: 'Leads',
      component: () => import('@/views/operations/leads.vue'),
      meta: { title: '咨询单', locale: '咨询单', requiresAuth: true },
    },
    {
      path: 'agents',
      name: 'Agents',
      component: () => import('@/views/customers/agents.vue'),
      meta: { title: '客服账号', locale: '客服账号', requiresAuth: true },
    },
    {
      path: 'evaluations',
      name: 'Evaluations',
      component: () => import('@/views/customers/evaluations.vue'),
      meta: { title: '服务评价', locale: '服务评价', requiresAuth: true },
    },
    {
      path: 'keywords',
      name: 'Keywords',
      component: () => import('@/views/operations/keywords.vue'),
      meta: { title: '关键词规则', locale: '关键词规则', requiresAuth: true },
    },
  ],
};
