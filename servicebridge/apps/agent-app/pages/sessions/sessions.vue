<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <view class="status" @tap="toggleStatus">
        <text class="title">消息</text>
        <view :class="['dot', status]"></view>
        <text class="angle">⌄</text>
      </view>
    </view>

    <view v-if="showStatusMenu" class="status-menu safe-menu">
      <view class="status-item" @tap="changeStatus('online')"><view class="menu-dot online"></view><text>在线接待</text></view>
      <view class="status-item" @tap="changeStatus('busy')"><view class="menu-dot busy"></view><text>忙碌 (勿扰)</text></view>
      <view class="status-item" @tap="changeStatus('offline')"><view class="menu-dot offline"></view><text>离线</text></view>
    </view>

    <scroll-view class="list flex-scroll" scroll-y>
      <view
        v-for="item in conversations"
        :key="item.id"
        class="session"
        @tap="openChat(item)"
        @longpress="openDeleteMenu(item)"
      >
        <view class="avatar">客</view>
        <view class="session-main">
          <view class="session-row session-top">
            <view class="title-wrap">
              <text class="remark">{{ item.visitor_remark }}</text>
              <text :class="['inline-tag', item.status]" v-if="item.status !== 'assigned'">{{ statusText(item.status) }}</text>
            </view>
          </view>
          <view class="session-row">
            <text class="last">{{ item.last_message }}</text>
          </view>
        </view>
        <view class="session-side">
          <text class="time">{{ timeText(item.updated_at) }}</text>
          <view v-if="item.unread_for_agent > 0" class="unread">{{ unreadText(item.unread_for_agent) }}</view>
          <view v-else class="unread-placeholder"></view>
        </view>
      </view>
    </scroll-view>

    <view v-if="deleteVisible" class="delete-mask" @tap="closeDeleteMenu">
      <view class="delete-sheet" @tap.stop>
        <view class="delete-action" @tap="confirmDelete">删除</view>
      </view>
    </view>

    <agent-tab-bar active="sessions" />
  </view>
</template>

<script>
import { deleteConversation, fetchConversations, markConversationRead, setStatus } from '../../common/api.js'
import { connectRealtime, onRealtime } from '../../common/realtime.js'
import AgentTabBar from '../../components/AgentTabBar.vue'

export default {
  components: { AgentTabBar },
  data() {
    return {
      status: 'online',
      showStatusMenu: false,
      conversations: [],
      offRealtime: null,
      deleteVisible: false,
      deleteTarget: null,
      suppressTapId: ''
    }
  },
  onShow() {
    connectRealtime()
    this.load()
    if (!this.offRealtime) {
      this.offRealtime = onRealtime((payload) => {
        if (['conversation.assigned', 'conversation.status_changed', 'message.receive', 'agent.notification'].indexOf(payload.event) >= 0) {
          this.load()
        }
        if (payload.event === 'agent.notification') {
          uni.showToast({ title: (payload.data && payload.data.body) || '新消息', icon: 'none' })
        }
      })
    }
  },
  onUnload() {
    if (this.offRealtime) this.offRealtime()
  },
  methods: {
    async load() {
      try {
        const data = await fetchConversations()
        this.conversations = data.conversations || []
      } catch (err) {
        uni.showToast({ title: err.message || '加载失败', icon: 'none' })
      }
    },
    toggleStatus() {
      this.showStatusMenu = !this.showStatusMenu
    },
    async changeStatus(status) {
      this.status = status
      this.showStatusMenu = false
      await setStatus(status)
      uni.showToast({ title: '状态已切换', icon: 'none' })
    },
    openChat(item) {
      if (this.suppressTapId && this.suppressTapId === item.id) return
      this.showStatusMenu = false
      this.clearUnreadLocal(item.id)
      this.markReadSilently(item.id)
      uni.navigateTo({ url: `/pages/chat/chat?id=${item.id}` })
    },
    clearUnreadLocal(conversationId) {
      const current = this.conversations.find((item) => item.id === conversationId)
      if (current) current.unread_for_agent = 0
    },
    async markReadSilently(conversationId) {
      try {
        const data = await markConversationRead(conversationId)
        if (data && data.conversation) {
          this.replaceConversation(data.conversation)
        }
      } catch (err) {}
    },
    replaceConversation(next) {
      if (!next || !next.id) return
      const idx = this.conversations.findIndex((item) => item.id === next.id)
      if (idx >= 0) {
        this.conversations.splice(idx, 1, next)
      }
    },
    openDeleteMenu(item) {
      if (!item || item.status !== 'closed') return
      this.suppressTapId = item.id
      this.deleteTarget = item
      this.deleteVisible = true
      setTimeout(() => {
        if (this.suppressTapId === item.id) this.suppressTapId = ''
      }, 400)
    },
    closeDeleteMenu() {
      this.deleteVisible = false
      this.deleteTarget = null
      this.suppressTapId = ''
    },
    async confirmDelete() {
      if (!this.deleteTarget) return
      const targetId = this.deleteTarget.id
      try {
        await deleteConversation(targetId)
        this.conversations = this.conversations.filter((item) => item.id !== targetId)
        uni.showToast({ title: '会话已删除', icon: 'none' })
      } catch (err) {
        uni.showToast({ title: err.message || '删除失败', icon: 'none' })
      } finally {
        this.closeDeleteMenu()
      }
    },
    statusText(status) {
      const map = {
        assigned: '人工接待',
        ai_serving: 'AI接待',
        human_requested: '请求人工',
        waiting: '等待中',
        closed: '已结束'
      }
      return map[status] || status
    },
    timeText(value) {
      if (!value) return ''
      return String(value).slice(11, 16)
    },
    unreadText(value) {
      const count = Number(value || 0)
      return count > 99 ? '99+' : String(count)
    }
  }
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #ffffff;
  position: relative;
}
.header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  padding: 0 32rpx;
}
.status {
  display: flex;
  align-items: center;
  gap: 14rpx;
}
.title {
  font-size: 48rpx;
  font-weight: 700;
  color: #111;
}
.dot {
  width: 20rpx;
  height: 20rpx;
  border-radius: 50%;
}
.dot.online { background: #22c55e; }
.dot.busy { background: #ef4444; }
.dot.offline { background: #9ca3af; }
.angle {
  color: #6b7280;
  font-size: 24rpx;
  margin-top: 6rpx;
}
.status-menu {
  position: absolute;
  top: 100rpx;
  left: 16rpx;
  z-index: 10;
  background: #fff;
  border-radius: 8rpx;
  box-shadow: 0 8rpx 32rpx rgba(0,0,0,.12);
}
.status-item {
  padding: 24rpx 32rpx;
  border-bottom: 1px solid #f3f4f6;
  font-size: 28rpx;
  display: flex;
  align-items: center;
  gap: 16rpx;
  color: #111827;
}
.menu-dot {
  width: 18rpx;
  height: 18rpx;
  border-radius: 50%;
}
.menu-dot.online { background: #22c55e; }
.menu-dot.busy { background: #ef4444; }
.menu-dot.offline { background: #9ca3af; }
.list {
  height: calc(100vh - 220rpx);
  padding-bottom: 120rpx;
  box-sizing: border-box;
}
.session {
  display: flex;
  align-items: flex-start;
  padding: 24rpx;
  border-bottom: 1px solid #f3f4f6;
  gap: 24rpx;
}
.avatar {
  width: 96rpx;
  height: 96rpx;
  border-radius: 8rpx;
  background: #d1d5db;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
}
.session-main {
  flex: 1;
  overflow: hidden;
  min-width: 0;
}
.session-row {
  display: flex;
  align-items: center;
}
.session-top {
  margin-bottom: 10rpx;
}
.title-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  min-width: 0;
}
.remark {
  flex: 1;
  min-width: 0;
  font-size: 34rpx;
  font-weight: 500;
  color: #111827;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.session-side {
  width: 80rpx;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 12rpx;
}
.time {
  color: #9ca3af;
  font-size: 24rpx;
}
.inline-tag {
  font-size: 20rpx;
  padding: 4rpx 10rpx;
  border-radius: 999rpx;
  margin-left: 10rpx;
  background: #dcfce7;
  color: #15803d;
  flex-shrink: 0;
}
.inline-tag.ai_serving { background: #dbeafe; color: #2563eb; }
.inline-tag.human_requested, .inline-tag.waiting { background: #ffedd5; color: #ea580c; }
.inline-tag.closed { background: #f3f4f6; color: #6b7280; }
.last {
  flex: 1;
  font-size: 28rpx;
  color: #6b7280;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.unread {
  min-width: 36rpx;
  height: 36rpx;
  padding: 0 10rpx;
  box-sizing: border-box;
  border-radius: 18rpx;
  background: #ef4444;
  color: #fff;
  font-size: 20rpx;
  text-align: center;
  line-height: 36rpx;
}
.unread-placeholder {
  width: 1rpx;
  height: 36rpx;
}
.delete-mask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.18);
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 0 24rpx calc(24rpx + env(safe-area-inset-bottom, 0px));
  box-sizing: border-box;
  z-index: 30;
}
.delete-sheet {
  width: 100%;
}
.delete-action {
  height: 96rpx;
  background: #fff;
  border-radius: 18rpx;
  color: #111827;
  font-size: 32rpx;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
