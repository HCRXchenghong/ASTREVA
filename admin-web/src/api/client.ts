import axios from 'axios';

// 统一后台与 admin-server 同源：Cookie 会话（2FA）由浏览器自动携带。
const client = axios.create({
  baseURL: '',
  timeout: 30000,
  withCredentials: true,
});

// 客服引擎接口一律通过 admin-server 的服务端代理访问，浏览器永远拿不到客服管理员口令。
export const serviceBridgePrefix = '/site-admin-api/servicebridge';

export default client;
