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
    // 询盘管理同时承载两个来源：会话询盘（引擎 conversations）与表单线索（官网 site_leads）。
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
    // 客服策略是运营配置的统一入口：排队与技能组 / SLA / 超时自动关闭 / AI 与关键词优先级 /
    // 无人值守 / 接管 / 关键词规则 / 黑名单（仅 IP）/ 排队队列 全部收进本页弹窗。
    {
      path: 'policy',
      name: 'ServicePolicy',
      component: () => import('@/views/operations/policy.vue'),
      meta: { title: '客服策略', locale: '客服策略', requiresAuth: true },
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
    // 已并入「客服策略」的旧入口：保留 redirect，避免旧书签/深链直接 404。
    {
      path: 'keywords',
      name: 'KeywordsLegacy',
      redirect: '/operations/policy',
      meta: { title: '关键词规则', locale: '关键词规则', requiresAuth: true, hideInMenu: true },
    },
    {
      path: 'blacklist',
      name: 'BlacklistLegacy',
      redirect: '/operations/policy',
      meta: { title: '黑名单', locale: '黑名单', requiresAuth: true, hideInMenu: true },
    },
    {
      path: 'queue',
      name: 'ServiceQueueLegacy',
      redirect: '/operations/policy',
      meta: { title: '排队队列', locale: '排队队列', requiresAuth: true, hideInMenu: true },
    },
    // 已并入「询盘管理 → 表单线索」的旧入口。
    {
      path: 'leads',
      name: 'LeadsLegacy',
      redirect: '/operations/inquiries?tab=leads',
      meta: { title: '咨询单', locale: '咨询单', requiresAuth: true, hideInMenu: true },
    },
  ],
};
