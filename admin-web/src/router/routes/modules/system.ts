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
      // 系统日志 + 审计日志合并为一页两个视图（系统日志 / 审计日志 Tab）。
      path: 'logs',
      name: 'SystemLogs',
      component: () => import('@/views/system/logs.vue'),
      meta: { title: '系统日志', locale: '系统日志', requiresAuth: true },
    },
    {
      // 原「营业时间与联系方式 / 推送设置 / 提醒设置」合并为一页多个独立保存的分区。
      path: 'settings',
      name: 'SystemSettings',
      component: () => import('@/views/system/settings.vue'),
      meta: { title: '系统设置', locale: '系统设置', requiresAuth: true },
    },
    {
      // 旧地址兼容：审计日志已并入系统日志页的第二个视图。
      path: 'audit',
      redirect: '/system/logs?tab=audit',
      meta: {
        title: '审计日志',
        locale: '审计日志',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      // 旧地址兼容：以下设置项已并入「系统设置」页的对应分区。
      path: 'general',
      redirect: '/system/settings?tab=general',
      meta: {
        title: '营业时间与联系方式',
        locale: '营业时间与联系方式',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      path: 'push',
      redirect: '/system/settings?tab=push',
      meta: {
        title: '推送设置',
        locale: '推送设置',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      path: 'alerts',
      redirect: '/system/settings?tab=alerts',
      meta: {
        title: '提醒设置',
        locale: '提醒设置',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      // 旧地址兼容：系统里的访客画像设置与「运营中心 → 访客画像 → 画像设置」重复，已删除。
      path: 'profile',
      redirect: '/operations/profiles',
      meta: {
        title: '访客画像',
        locale: '访客画像',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
    {
      // 旧地址兼容：AI 设置已迁到 AI 管理。
      path: 'ai',
      redirect: '/ai/reception',
      meta: {
        title: 'AI 设置',
        locale: 'AI 设置',
        requiresAuth: true,
        hideInMenu: true,
      },
    },
  ],
};
