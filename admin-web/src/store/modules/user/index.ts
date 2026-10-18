import { defineStore } from 'pinia';
import {
  authStatus,
  getUserInfo,
  logout as userLogout,
  startLogin,
  verifyLogin,
} from '@/api/user';
import { setToken, clearToken } from '@/utils/auth';
import { removeRouteListener } from '@/utils/route-listener';
import { UserState } from './types';

// 后台使用 admin-server 的 2FA Cookie 会话：localStorage 只保存“已登录”标记，
// 真正的凭证是 HttpOnly Cookie，前端无法也不需要读取。
const useUserStore = defineStore('user', {
  state: (): UserState => ({
    name: undefined,
    email: undefined,
    role: '',
    avatar: undefined,
  }),

  getters: {
    userInfo(state: UserState): UserState {
      return { ...state };
    },
  },

  actions: {
    setInfo(partial: Partial<UserState>) {
      this.$patch(partial);
    },
    resetInfo() {
      this.$reset();
    },
    async info() {
      const res = await getUserInfo();
      this.setInfo({ ...res.data, role: 'admin' });
    },
    // 第一步：邮箱密码换 challengeId（登录或首次绑定 2FA）。
    async startLogin(email: string, password: string) {
      return startLogin(email, password);
    },
    // 第二步：提交 6 位验证码完成登录。
    async finishLogin(challengeId: string, code: string) {
      await verifyLogin(challengeId, code);
      setToken('cookie-session');
      await this.info();
    },
    async hasAdmin() {
      const status = await authStatus();
      return status.hasAdmin;
    },
    logoutCallBack() {
      this.resetInfo();
      clearToken();
      removeRouteListener();
    },
    async logout() {
      try {
        await userLogout();
      } finally {
        this.logoutCallBack();
      }
    },
  },
});

export default useUserStore;
