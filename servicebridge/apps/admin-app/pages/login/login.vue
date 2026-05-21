<template>
  <view class="login-page">
    <view class="shield">盾</view>
    <text class="title">系统管理后台</text>
    <text class="subtitle">运营与高权限配置中心</text>
    <view class="form">
      <view class="field">
        <text class="field-icon">账号</text>
        <input v-model="account" class="input" placeholder="管理账号" />
      </view>
      <view class="field">
        <text class="field-icon">密码</text>
        <input v-model="password" class="input" password placeholder="管理员密码" />
      </view>
      <view class="field">
        <text class="field-icon">服务</text>
        <input v-model="apiBase" class="input" placeholder="https://api.example.com" />
      </view>
      <button class="login-btn" @tap="submit">安全登录</button>
    </view>
  </view>
</template>

<script>
import { getAPIBase, login, setAPIBase } from '../../common/api.js'
import { connectRealtime } from '../../common/realtime.js'

export default {
  data() {
    return {
      account: 'superadmin',
      password: '',
      apiBase: getAPIBase(),
      loading: false
    }
  },
  methods: {
    async submit() {
      if (this.loading) return
      this.loading = true
      try {
        this.apiBase = setAPIBase(this.apiBase)
        await login(this.account, this.password)
        connectRealtime()
        uni.showToast({ title: '登录成功', icon: 'none' })
        uni.redirectTo({ url: '/pages/dashboard/dashboard' })
      } catch (err) {
        uni.showToast({ title: err.message || '登录失败', icon: 'none' })
      } finally {
        this.loading = false
      }
    }
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  background: #ffffff;
  padding: calc(var(--status-bar-height, 0px) + 48rpx) 64rpx calc(env(safe-area-inset-bottom, 0px) + 48rpx);
  box-sizing: border-box;
}
.shield {
  width: 140rpx;
  height: 140rpx;
  border-radius: 28rpx;
  background: #f3f4f6;
  color: #576b95;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 42rpx;
  margin-bottom: 40rpx;
}
.title {
  font-size: 48rpx;
  font-weight: 600;
  color: #1f2937;
}
.subtitle {
  margin-top: 16rpx;
  color: #6b7280;
  font-size: 26rpx;
  margin-bottom: 56rpx;
}
.form {
  width: 100%;
}
.field {
  display: flex;
  align-items: center;
  border-bottom: 1px solid #d1d5db;
  padding: 20rpx 0;
  margin-bottom: 24rpx;
}
.field-icon {
  width: 80rpx;
  color: #9ca3af;
  font-size: 28rpx;
}
.input {
  flex: 1;
  font-size: 34rpx;
}
.login-btn {
  margin-top: 48rpx;
  background: #576b95;
  color: #fff;
  border-radius: 8rpx;
  font-size: 34rpx;
}
</style>
