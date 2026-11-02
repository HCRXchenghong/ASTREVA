import { Message } from '@arco-design/web-vue';
import client from './client';
import { clearToken } from '@/utils/auth';

function csrfToken() {
  const match = document.cookie.match(/(?:^|;\s*)astreva_admin_csrf=([^;]+)/);
  return match ? decodeURIComponent(match[1]) : '';
}

// 双提交 Cookie + 服务端会话内 csrf 双重校验；所有写请求都必须带这个头。
client.interceptors.request.use((config) => {
  const method = String(config.method || 'get').toUpperCase();
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    const token = csrfToken();
    if (token) {
      if (typeof (config.headers as any)?.set === 'function') {
        (config.headers as any).set('X-CSRF-Token', token);
      } else {
        config.headers = { ...(config.headers || {}), 'X-CSRF-Token': token } as any;
      }
    }
  }
  return config;
});

// 会话过期时统一回到 2FA 登录页；避免每个页面自己处理 401。
client.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error?.response?.status;
    if (status === 401) {
      clearToken();
      if (!window.location.pathname.startsWith('/site-admin/login')) {
        const returnTo = encodeURIComponent(
          window.location.pathname + window.location.search
        );
        window.location.href = `/site-admin/login?returnTo=${returnTo}`;
      }
    }
    const message =
      error?.response?.data?.error?.message ||
      error?.response?.data?.message ||
      error?.message ||
      '请求失败';
    // skipErrorMessage：页面自行静默降级（如告警轮询），不弹全局错误提示。
    if (status !== 401 && !error?.config?.skipErrorMessage) {
      Message.error(message);
    }
    return Promise.reject(error);
  }
);

export default client;
