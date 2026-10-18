import { DEFAULT_LAYOUT } from '../base';

export default {
  path: '/system',
  name: 'SystemRoot',
  component: DEFAULT_LAYOUT,
  redirect: '/system/general',
  meta: {
    title: '系统设置',
        locale: '系统设置',
    requiresAuth: true,
    icon: 'icon-settings',
    order: 6,
    hideInMenu: false,
  },
  children: [
    {
      path: 'general',
      name: 'SystemGeneral',
      component: () => import('@/views/system/general.vue'),
      meta: { title: '营业时间与联系方式',
        locale: '营业时间与联系方式', requiresAuth: true },
    },
    {
      path: 'ai',
      name: 'SystemAi',
      component: () => import('@/views/system/ai.vue'),
      meta: { title: 'AI 设置',
        locale: 'AI 设置', requiresAuth: true },
    },
    {
      path: 'push',
      name: 'SystemPush',
      component: () => import('@/views/system/push.vue'),
      meta: { title: '推送设置',
        locale: '推送设置', requiresAuth: true },
    },
  ],
};
