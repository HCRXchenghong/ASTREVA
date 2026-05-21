<template>
  <view class="login-page">
    <view class="login-card">
      <view class="logo">服</view>
      <text class="title">客服工作台</text>
      <view class="form">
        <view class="field">
          <text class="field-icon">账号</text>
          <input v-model="account" class="input" placeholder="账号" />
        </view>
        <view class="field">
          <text class="field-icon">密码</text>
          <input v-model="password" class="input" password placeholder="密码" />
        </view>
        <view class="field">
          <text class="field-icon">服务</text>
          <input v-model="apiBase" class="input" placeholder="https://api.example.com" />
        </view>
        <button class="login-btn" @tap="submit">登录</button>
      </view>
    </view>
  </view>
</template>

<script>
import { getAPIBase, login, registerPushDevice, setAPIBase, setStatus } from '../../common/api.js'
import { connectRealtime } from '../../common/realtime.js'

export default {
  data() {
    return {
      account: 'admin',
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
        await setStatus('online')
        this.registerPushIfAvailable()
        connectRealtime()
        uni.showToast({ title: '登录成功', icon: 'none' })
        uni.redirectTo({ url: '/pages/sessions/sessions' })
      } catch (err) {
        uni.showToast({ title: err.message || '登录失败', icon: 'none' })
      } finally {
        this.loading = false
      }
    },
    registerPushIfAvailable() {
      if (!uni.getPushClientId) return
      uni.getPushClientId({
        success: async (res) => {
          const token = res.cid || res.clientid || res.clientId
          if (!token) return
          const info = uni.getSystemInfoSync ? uni.getSystemInfoSync() : {}
          try {
            await registerPushDevice({
              platform: info.platform || 'app',
              provider: 'uni-push',
              token
            })
          } catch (err) {
            console.warn('register push device failed', err)
          }
        }
      })
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
  background: #ffffff;
  padding: 0 64rpx;
}
.login-card {
  width: 100%;
  display: flex;
  align-items: center;
  flex-direction: column;
}
.logo {
  width: 120rpx;
  height: 120rpx;
  line-height: 120rpx;
  border-radius: 24rpx;
  background: #07c160;
  color: #fff;
  font-size: 42rpx;
  text-align: center;
  margin-bottom: 48rpx;
}
.title {
  font-size: 48rpx;
  font-weight: 600;
  color: #1f2937;
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
  background: #07c160;
  color: #fff;
  border-radius: 8rpx;
  font-size: 34rpx;
}
</style>
