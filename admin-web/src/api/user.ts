import client from './client';

// admin-server 的 2FA 登录分两步：先校验邮箱密码拿 challengeId，再提交 6 位验证码。
export interface LoginChallenge {
  challengeId: string;
  secret?: string;
  qr?: string;
}

export interface AuthStatus {
  hasAdmin: boolean;
  authenticated: boolean;
}

export async function authStatus(): Promise<AuthStatus> {
  const { data } = await client.get('/site-admin-auth/status');
  return data;
}

export async function startLogin(email: string, password: string) {
  const { data } = await client.post('/site-admin-auth/login/start', {
    email,
    password,
  });
  return data as LoginChallenge;
}

export async function verifyLogin(challengeId: string, code: string) {
  const { data } = await client.post('/site-admin-auth/login/verify', {
    challengeId,
    code,
  });
  return data;
}

export async function startSetup(email: string, password: string) {
  const { data } = await client.post('/site-admin-auth/setup/start', {
    email,
    password,
  });
  return data as LoginChallenge;
}

export async function verifySetup(challengeId: string, code: string) {
  const { data } = await client.post('/site-admin-auth/setup/verify', {
    challengeId,
    code,
  });
  return data;
}

export async function logout() {
  await client.post('/site-admin-auth/logout');
}

// 模板的守卫与用户态依赖这三个接口，保持签名兼容。
export async function getUserInfo() {
  const status = await authStatus();
  return {
    data: {
      name: '管理员',
      email: '',
      role: 'admin',
      authenticated: status.authenticated,
    },
  };
}

export async function getMenuList() {
  return { data: [] };
}
