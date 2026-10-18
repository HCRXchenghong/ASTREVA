import type { Router } from 'vue-router';
import NProgress from 'nprogress'; // progress bar

import usePermission from '@/hooks/permission';
import { NOT_FOUND } from '../constants';

export default function setupPermissionGuard(router: Router) {
  router.beforeEach(async (to, from, next) => {
    const Permission = usePermission();
    if (Permission.accessRouter(to)) next();
    else next(NOT_FOUND);
    NProgress.done();
  });
}
