<template>
  <view class="tabbar">
    <view :class="['tab', active === 'dashboard' ? 'active' : '']" @tap="go('dashboard')">
      <text class="icon">◔</text>
      <text>概览</text>
    </view>
    <view :class="['tab', active === 'monitor' ? 'active' : '']" @tap="go('monitor')">
      <text class="icon">▣</text>
      <text>监控</text>
    </view>
    <view :class="['tab', active === 'agents' ? 'active' : '']" @tap="go('agents')">
      <text class="icon">人</text>
      <text>客服</text>
    </view>
    <view :class="['tab', active === 'ai' ? 'active' : '']" @tap="go('ai')">
      <text class="icon small">AI</text>
      <text>AI设置</text>
    </view>
    <view :class="['tab', active === 'settings' ? 'active' : '']" @tap="go('settings')">
      <text class="icon">≡</text>
      <text>系统</text>
    </view>
  </view>
</template>

<script>
export default {
  props: {
    active: {
      type: String,
      default: 'dashboard'
    }
  },
  methods: {
    go(page) {
      if (page === this.active) return
      uni.redirectTo({ url: `/pages/${page}/${page}` })
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
.icon {
  font-size: 44rpx;
  line-height: 44rpx;
  height: 48rpx;
  margin-bottom: 4rpx;
  font-weight: 600;
}
.icon.small {
  font-size: 30rpx;
  line-height: 48rpx;
}
</style>
