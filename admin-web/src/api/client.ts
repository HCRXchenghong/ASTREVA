import axios from 'axios';

// 允许单个请求声明「失败时不弹全局错误提示」，供需要静默降级的轮询接口使用
// （例如询盘页的 /api/admin/policy-alerts：失败时只保留上一次徽标，不打扰客服）。
declare module 'axios' {
  export interface AxiosRequestConfig {
    skipErrorMessage?: boolean;
  }
}

// 统一后台与 admin-server 同源：Cookie 会话（2FA）由浏览器自动携带。
const client = axios.create({
  baseURL: '',
  timeout: 30000,
  withCredentials: true,
});

// 客服引擎接口一律通过 admin-server 的服务端代理访问，浏览器永远拿不到客服管理员口令。
export const serviceBridgePrefix = '/site-admin-api/servicebridge';

export default client;
