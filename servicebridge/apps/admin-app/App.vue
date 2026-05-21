<script>
export default {
  onLaunch() {
    // Admin app bootstrap will be wired here.
  }
}
</script>

<style>
page {
  background: #ededed;
  font-family: -apple-system, BlinkMacSystemFont, "Helvetica Neue", sans-serif;
}

.safe-page {
  height: 100vh;
  display: flex !important;
  flex-direction: column;
  overflow: hidden;
}

.safe-header {
  padding-top: calc(var(--status-bar-height, 0px) + 10rpx);
  height: calc(112rpx + var(--status-bar-height, 0px) + 10rpx) !important;
  box-sizing: border-box;
}

.flex-scroll {
  flex: 1 1 auto !important;
  height: auto !important;
  min-height: 0 !important;
}

.safe-footer {
  padding-bottom: calc(16rpx + env(safe-area-inset-bottom, 0px));
}
</style>
