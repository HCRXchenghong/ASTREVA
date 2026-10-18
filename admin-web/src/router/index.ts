import { createRouter, createWebHistory } from 'vue-router';
import NProgress from 'nprogress'; // progress bar
import 'nprogress/nprogress.css';

import { appRoutes } from './routes';
import { REDIRECT_MAIN, NOT_FOUND_ROUTE } from './routes/base';
import { legacyRedirectRoutes } from './routes/legacy';
import createRouteGuard from './guard';
import { DEFAULT_ROUTE } from './constants';

NProgress.configure({ showSpinner: false }); // NProgress Configuration

// 后台 SPA 挂在 /site-admin/ 下；旧的独立页面（content/translations/media/settings/api-docs）
// 仍由 admin-server 直接提供，不受这里影响。
const router = createRouter({
  history: createWebHistory('/site-admin/'),
  routes: [
    {
      path: '/',
      redirect: DEFAULT_ROUTE,
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/login/index.vue'),
      meta: {
        requiresAuth: false,
      },
    },
    ...appRoutes,
    ...legacyRedirectRoutes,
    REDIRECT_MAIN,
    NOT_FOUND_ROUTE,
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

createRouteGuard(router);

export default router;
