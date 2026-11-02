<template>
  <div class="login-page">
    <a-card class="login-card" :bordered="false">
      <div class="brand">
        <img :src="logoUrl" alt="ASTREVA" />
        <div>
          <h1>ASTREVA 统一后台</h1>
          <p>{{ isSetup ? '首次使用：创建管理员并绑定 2FA' : '管理员登录' }}</p>
        </div>
      </div>

      <a-form v-if="step === 1" :model="form" layout="vertical" @submit-success="submitCredentials">
        <a-form-item
          v-if="isSetup"
          field="setupToken"
          label="初始化令牌"
          :rules="[{ required: true, message: '请输入服务器启动日志中的初始化令牌' }]"
        >
          <a-input-password v-model="form.setupToken" placeholder="查看 admin-server 启动日志中的一次性令牌" />
        </a-form-item>
        <a-form-item field="email" label="邮箱" :rules="[{ required: true, message: '请输入邮箱' }]">
          <a-input v-model="form.email" placeholder="请输入管理员邮箱" allow-clear />
        </a-form-item>
        <a-form-item field="password" label="密码" :rules="[{ required: true, message: '请输入密码' }]">
          <a-input-password v-model="form.password" placeholder="请输入密码" />
        </a-form-item>
        <a-form-item v-if="isSetup" field="confirm" label="确认密码" :rules="[{ required: true, message: '请再次输入密码' }]">
          <a-input-password v-model="form.confirm" placeholder="再次输入密码" />
        </a-form-item>
        <a-button type="primary" long html-type="submit" :loading="loading">
          {{ isSetup ? '生成 2FA 二维码' : '下一步：输入验证码' }}
        </a-button>
      </a-form>

      <div v-else>
        <a-alert v-if="isSetup" type="info" class="tips">
          用 Google Authenticator / 1Password 等应用扫描二维码，或手动输入密钥后填写 6 位验证码。
        </a-alert>
        <div v-if="challenge.qr" class="qr">
          <img :src="challenge.qr" alt="2FA 二维码" />
        </div>
        <p v-if="challenge.secret" class="secret">密钥：{{ challenge.secret }}</p>
        <a-form :model="form" layout="vertical" @submit-success="submitCode">
          <a-form-item field="code" label="2FA 验证码" :rules="[{ required: true, message: '请输入 6 位验证码' }]">
            <a-input v-model="form.code" placeholder="6 位数字" maxlength="6" allow-clear />
          </a-form-item>
          <a-space>
            <a-button type="primary" html-type="submit" :loading="loading">登录</a-button>
            <a-button @click="step = 1">返回</a-button>
          </a-space>
        </a-form>
      </div>
      <a-alert v-if="error" type="error" class="tips">{{ error }}</a-alert>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import {
    startLogin,
    startSetup,
    verifyLogin,
    verifySetup,
    authStatus,
  } from '@/api/user';
  import { setToken } from '@/utils/auth';
  import useUserStore from '@/store/modules/user';
  import { DEFAULT_ROUTE } from '@/router/constants';

  const logoUrl = `${import.meta.env.BASE_URL}assets/astreva-logo-transparent.png`;
  const router = useRouter();
  const route = useRoute();
  const userStore = useUserStore();

  const step = ref(1);
  const isSetup = ref(false);
  const loading = ref(false);
  const error = ref('');
  const challenge = reactive<{ challengeId: string; qr?: string; secret?: string }>({
    challengeId: '',
  });
  const form = reactive({ email: '', password: '', confirm: '', code: '', setupToken: '' });

  onMounted(async () => {
    try {
      const status = await authStatus();
      isSetup.value = !status.hasAdmin;
      if (status.authenticated) {
        router.replace(DEFAULT_ROUTE);
      }
    } catch (err) {
      // 状态读取失败时按普通登录处理。
    }
  });

  async function submitCredentials() {
    error.value = '';
    if (isSetup.value && form.password !== form.confirm) {
      error.value = '两次输入的密码不一致。';
      return;
    }
    if (isSetup.value && form.password.length < 12) {
      error.value = '密码至少 12 位。';
      return;
    }
    loading.value = true;
    try {
      const result = isSetup.value
        ? await startSetup(form.email, form.password, form.setupToken)
        : await startLogin(form.email, form.password);
      challenge.challengeId = result.challengeId;
      challenge.qr = result.qr;
      challenge.secret = result.secret;
      step.value = 2;
    } catch (err: any) {
      error.value = err?.response?.data?.error?.message || '请求失败，请重试。';
    } finally {
      loading.value = false;
    }
  }

  async function submitCode() {
    error.value = '';
    loading.value = true;
    try {
      if (isSetup.value) {
        await verifySetup(challenge.challengeId, form.code, form.setupToken);
      } else {
        await verifyLogin(challenge.challengeId, form.code);
      }
      setToken('cookie-session');
      await userStore.info();
      Message.success('登录成功');
      const returnTo = String(route.query.returnTo || '');
      if (returnTo.startsWith('/site-admin/')) {
        router.replace(returnTo);
      } else {
        router.replace(DEFAULT_ROUTE);
      }
    } catch (err: any) {
      error.value = err?.response?.data?.error?.message || '验证码不正确。';
    } finally {
      loading.value = false;
    }
  }
</script>

<style scoped lang="less">
  .login-page {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: radial-gradient(circle at 10% 0%, #e8f2ff, transparent 40rem), #f5f8fc;
  }
  .login-card {
    width: 460px;
    padding: 12px 8px;
    border-radius: 14px;
    box-shadow: 0 18px 60px rgba(20, 50, 90, 0.12);
  }
  .brand {
    display: flex;
    gap: 14px;
    align-items: center;
    margin-bottom: 22px;
    img {
      width: 48px;
      height: 48px;
      object-fit: contain;
    }
    h1 {
      margin: 0 0 4px;
      font-size: 19px;
    }
    p {
      margin: 0;
      color: var(--color-text-3);
      font-size: 13px;
    }
  }
  .qr {
    text-align: center;
    margin: 10px 0;
    img {
      width: 200px;
      height: 200px;
    }
  }
  .secret {
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
    text-align: center;
  }
  .tips {
    margin-top: 14px;
  }
</style>
