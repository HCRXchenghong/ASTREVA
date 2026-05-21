<template>
  <view class="tabbar">
    <view :class="['tab', active === 'sessions' ? 'active' : '']" @tap="go('sessions')">
      <view class="chat-icon"></view>
      <text>会话</text>
    </view>
    <view :class="['tab', active === 'settings' ? 'active' : '']" @tap="go('settings')">
      <text class="gear">⚙</text>
      <text>设置</text>
    </view>
  </view>
</template>

<script>
export default {
  props: {
    active: {
      type: String,
      default: 'sessions'
    }
  },
  methods: {
    go(page) {
      if (page === this.active) return
      const url = page === 'sessions' ? '/pages/sessions/sessions' : '/pages/settings/settings'
      uni.redirectTo({ url })
    }
  }
}
</script>

<style scoped>
.tabbar {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  height: 120rpx;
  padding-bottom: env(safe-area-inset-bottom);
  background: #f7f7f7;
  border-top: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  justify-content: space-around;
  z-index: 20;
}
.tab {
  flex: 1;
  height: 100%;
  color: #6b7280;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  font-size: 20rpx;
}
.tab.active {
  color: #07c160;
}
.chat-icon {
  width: 44rpx;
  height: 34rpx;
  border: 4rpx solid currentColor;
  border-radius: 16rpx;
  position: relative;
  margin-bottom: 8rpx;
}
.chat-icon::after {
  content: '';
  position: absolute;
  right: 4rpx;
  bottom: -10rpx;
  width: 14rpx;
  height: 14rpx;
  border-right: 4rpx solid currentColor;
  border-bottom: 4rpx solid currentColor;
  transform: rotate(35deg);
  background: #f7f7f7;
}
.gear {
  font-size: 44rpx;
  line-height: 44rpx;
  margin-bottom: 4rpx;
}
</style>
