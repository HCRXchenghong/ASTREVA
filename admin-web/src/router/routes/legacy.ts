import type { RouteRecordRaw } from 'vue-router';

// 旧地址兼容：客服管理根模块已合并进「运营中心」，会话监管台改名为「询盘管理」。
// 这些记录只做重定向、且不参与菜单收集（菜单只读取 router/routes/modules/*）。
export const legacyRedirectRoutes: RouteRecordRaw[] = [
  {
    path: '/customers',
    redirect: '/operations/agents',
    meta: {
      title: '客服账号',
      locale: '客服账号',
      requiresAuth: true,
      hideInMenu: true,
    },
  },
  {
    path: '/customers/agents',
    redirect: '/operations/agents',
    meta: {
      title: '客服账号',
      locale: '客服账号',
      requiresAuth: true,
      hideInMenu: true,
    },
  },
  {
    path: '/customers/evaluations',
    redirect: '/operations/evaluations',
    meta: {
      title: '服务评价',
      locale: '服务评价',
      requiresAuth: true,
      hideInMenu: true,
    },
  },
  {
    path: '/workspace',
    redirect: '/operations/inquiries',
    meta: {
      title: '询盘管理',
      locale: '询盘管理',
      requiresAuth: true,
      hideInMenu: true,
    },
  },
];

export default legacyRedirectRoutes;
