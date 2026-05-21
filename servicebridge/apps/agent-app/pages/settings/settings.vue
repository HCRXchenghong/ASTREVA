<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <text class="title">系统设置</text>
    </view>

    <scroll-view class="content flex-scroll" scroll-y>
      <view class="profile">
        <view class="avatar">服</view>
        <view class="profile-main">
          <text class="name">{{ agentName }}</text>
          <text class="sub">工号: KF-10086</text>
        </view>
      </view>

      <view class="group">
        <view class="item" @tap="openPassword">
          <text class="item-title">修改登录密码</text>
          <text class="arrow">›</text>
        </view>
        <view class="item">
          <text class="item-title">新消息提示音</text>
          <switch :checked="settings.sound" color="#07c160" @change="settings.sound = $event.detail.value" />
        </view>
        <view class="item">
          <text class="item-title">震动开关</text>
          <switch :checked="settings.vibrate" color="#07c160" @change="settings.vibrate = $event.detail.value" />
        </view>
        <view class="item">
          <view class="item-text">
            <text class="item-title">勿扰时段</text>
            <text class="item-desc">拦截新消息系统横幅与提示音</text>
          </view>
          <switch :checked="settings.dnd" color="#07c160" @change="settings.dnd = $event.detail.value" />
        </view>
      </view>

      <view class="logout-box" @tap="logout">
        <text>退出登录</text>
      </view>
    </scroll-view>

    <view v-if="passwordVisible" class="password-page safe-page">
      <view class="password-header safe-header">
        <text class="back" @tap="passwordVisible = false">‹</text>
        <text class="password-title">修改登录密码</text>
        <view class="header-space"></view>
      </view>
      <view class="group">
        <view class="item">
          <text class="item-title">当前密码</text>
          <input v-model="passwordForm.current" class="input" password />
        </view>
        <view class="item">
          <text class="item-title">新密码</text>
          <input v-model="passwordForm.next" class="input" password />
        </view>
        <view class="item">
          <text class="item-title">确认新密码</text>
          <input v-model="passwordForm.confirm" class="input" password />
        </view>
      </view>
      <view class="password-actions">
        <button class="save-btn" @tap="submitPassword">保存新密码</button>
      </view>
    </view>

    <agent-tab-bar active="settings" />
  </view>
</template>

<script>
import { changePassword, clearAuth, getAgent, setStatus } from '../../common/api.js'
import { disconnectRealtime } from '../../common/realtime.js'
import AgentTabBar from '../../components/AgentTabBar.vue'

export default {
  components: { AgentTabBar },
  data() {
    return {
      agent: null,
      settings: {
        sound: true,
        vibrate: true,
        dnd: false
      },
      passwordVisible: false,
      passwordForm: { current: '', next: '', confirm: '' }
    }
  },
  computed: {
    agentName() {
      return (this.agent && this.agent.name) || 'admin'
    }
  },
  onShow() {
    this.agent = getAgent()
    const saved = uni.getStorageSync('agent_notice_settings')
    if (saved) {
      this.settings.sound = typeof saved.sound === 'boolean' ? saved.sound : this.settings.sound
      this.settings.vibrate = typeof saved.vibrate === 'boolean' ? saved.vibrate : this.settings.vibrate
      this.settings.dnd = typeof saved.dnd === 'boolean' ? saved.dnd : this.settings.dnd
    }
  },
  watch: {
    settings: {
      deep: true,
      handler(value) {
        uni.setStorageSync('agent_notice_settings', value)
      }
    }
  },
  methods: {
    openPassword() {
      this.passwordForm = { current: '', next: '', confirm: '' }
      this.passwordVisible = true
    },
    async submitPassword() {
      if (!this.passwordForm.current || !this.passwordForm.next) {
        uni.showToast({ title: '请填写当前密码和新密码', icon: 'none' })
        return
      }
      if (this.passwordForm.next !== this.passwordForm.confirm) {
        uni.showToast({ title: '两次新密码不一致', icon: 'none' })
        return
      }
      try {
        await changePassword(this.passwordForm.current, this.passwordForm.next)
        uni.showToast({ title: '密码已修改，请重新登录', icon: 'none' })
        disconnectRealtime()
        clearAuth()
        uni.redirectTo({ url: '/pages/login/login' })
      } catch (err) {
        uni.showToast({ title: err.message || '修改失败', icon: 'none' })
      }
    },
    async logout() {
      uni.showModal({
        title: '系统提示',
        content: '确定要退出登录吗？',
        confirmColor: '#07c160',
        success: async (res) => {
          if (!res.confirm) return
          try {
            await setStatus('offline')
          } catch (err) {}
          disconnectRealtime()
          clearAuth()
          uni.redirectTo({ url: '/pages/login/login' })
        }
      })
    }
  }
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #ededed;
  position: relative;
}
.header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  justify-content: center;
}
.title {
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.content {
  height: calc(100vh - 220rpx);
  padding-bottom: 120rpx;
  box-sizing: border-box;
}
.profile {
  background: #fff;
  padding: 40rpx;
  display: flex;
  align-items: center;
  gap: 28rpx;
  margin-bottom: 16rpx;
}
.avatar {
  width: 128rpx;
  height: 128rpx;
  border-radius: 16rpx;
  background: #07c160;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 44rpx;
}
.profile-main {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.name {
  color: #1f2937;
  font-size: 36rpx;
  font-weight: 500;
}
.sub {
  color: #6b7280;
  font-size: 26rpx;
  margin-top: 10rpx;
}
.group {
  background: #fff;
  margin-bottom: 16rpx;
}
.item {
  min-height: 104rpx;
  padding: 0 32rpx;
  border-bottom: 1px solid #f3f4f6;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.item-title {
  color: #1f2937;
  font-size: 32rpx;
}
.arrow {
  color: #9ca3af;
  font-size: 40rpx;
}
.input {
  flex: 1;
  text-align: right;
  margin-left: 24rpx;
  font-size: 30rpx;
}
.item-text {
  display: flex;
  flex-direction: column;
}
.item-desc {
  color: #9ca3af;
  font-size: 22rpx;
  margin-top: 6rpx;
}
.logout-box {
  margin-top: 32rpx;
  height: 104rpx;
  background: #fff;
  color: #ef4444;
  font-size: 32rpx;
  font-weight: 500;
  display: flex;
  align-items: center;
  justify-content: center;
}
.password-page {
  position: fixed;
  inset: 0;
  background: #ededed;
  z-index: 40;
}
.password-header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  padding: 0 24rpx;
  box-sizing: border-box;
}
.back, .header-space {
  width: 96rpx;
  color: #374151;
  font-size: 48rpx;
}
.password-title {
  flex: 1;
  text-align: center;
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.password-actions {
  padding: 32rpx;
}
.save-btn {
  background: #07c160;
  color: #fff;
}
</style>
