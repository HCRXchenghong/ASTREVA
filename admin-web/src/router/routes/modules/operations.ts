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
      path: 'profiles',
      name: 'VisitorProfiles',
      component: () => import('@/views/operations/profiles.vue'),
      meta: { title: '访客画像', locale: '访客画像', requiresAuth: true },
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
    {
      path: 'policy',
      name: 'ServicePolicy',
      component: () => import('@/views/operations/policy.vue'),
      meta: { title: '客服策略', locale: '客服策略', requiresAuth: true },
    },
    {
      path: 'queue',
      name: 'ServiceQueue',
      component: () => import('@/views/operations/queue.vue'),
      meta: { title: '排队队列', locale: '排队队列', requiresAuth: true },
    },
    {
      path: 'blacklist',
      name: 'Blacklist',
      component: () => import('@/views/operations/blacklist.vue'),
      meta: { title: '黑名单', locale: '黑名单', requiresAuth: true },
    },
  ],
};
