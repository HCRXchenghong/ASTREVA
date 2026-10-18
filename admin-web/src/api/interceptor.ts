import { Message } from '@arco-design/web-vue';
import client from './client';
import { clearToken } from '@/utils/auth';

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
    if (status !== 401) {
      Message.error(message);
    }
    return Promise.reject(error);
  }
);

export default client;
