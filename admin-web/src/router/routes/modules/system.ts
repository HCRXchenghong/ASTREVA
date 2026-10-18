import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/system',
  name: 'SystemRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/system/logs',
  meta: {
    title: '系统',
    locale: '系统',
    requiresAuth: true,
    icon: 'IconSettings',
    order: 6,
    hideInMenu: false,
  },
  children: [
    {
      path: 'logs',
      name: 'SystemLogs',
      component: () => import('@/views/system/logs.vue'),
      meta: { title: '系统日志', locale: '系统日志', requiresAuth: true },
    },
    {
      path: 'audit',
      name: 'Audit',
      component: () => import('@/views/operations/audit.vue'),
      meta: { title: '审计日志', locale: '审计日志', requiresAuth: true },
    },
    {
      path: 'general',
      name: 'SystemGeneral',
      component: () => import('@/views/system/general.vue'),
      meta: {
        title: '营业时间与联系方式',
        locale: '营业时间与联系方式',
        requiresAuth: true,
      },
    },
    {
      // 旧地址兼容：AI 设置已迁到 AI 管理
      path: 'ai',
      redirect: '/ai/reception',
      meta: {
        title: 'AI 设置',
        locale: 'AI 设置',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      path: 'push',
      name: 'SystemPush',
      component: () => import('@/views/system/push.vue'),
      meta: { title: '推送设置', locale: '推送设置', requiresAuth: true },
    },
  ],
};
