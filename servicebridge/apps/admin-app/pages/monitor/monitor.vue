<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <text class="title">会话监控</text>
      <view class="header-actions">
        <text class="top-action" @tap="load">刷新</text>
        <text class="top-action" @tap="exportCSV">导出</text>
      </view>
    </view>
    <view class="subbar">
      <text>当前活动会话 ({{ activeCount }})</text>
      <text class="filter">筛选</text>
    </view>
    <scroll-view class="list flex-scroll" scroll-y>
      <view v-for="item in conversations" :key="item.id" class="session" @tap="openDetail(item)">
        <view class="avatar">客</view>
        <view class="main">
          <view class="row">
            <text class="remark">{{ item.visitor_remark }}</text>
            <text class="time">{{ timeText(item.updated_at) }}</text>
          </view>
          <view class="row">
            <text :class="['tag', item.status]">{{ statusText(item.status) }}</text>
            <text class="last">{{ item.last_message }}</text>
          </view>
        </view>
        <button v-if="item.status === 'closed'" class="delete-btn" @tap.stop="deleteItem(item)">删除</button>
        <button v-else class="close-btn" @tap.stop="closeItem(item)">强关</button>
      </view>
    </scroll-view>

    <view v-if="detailVisible" class="detail-page safe-page">
      <view class="detail-header safe-header">
        <text class="back" @tap="detailVisible = false">‹</text>
        <view class="detail-title">
          <text class="detail-name">{{ currentRemark }}</text>
          <text class="detail-sub">{{ currentStatusText }}</text>
        </view>
        <text class="info" @tap="showIP">i</text>
      </view>
      <scroll-view class="history flex-scroll" scroll-y>
        <view class="history-tip">历史记录 (只读模式)</view>
        <view v-if="hasMore" class="load-more" @tap="loadOlder">点击加载更多历史记录</view>
        <view v-for="msg in messages" :key="msg.server_msg_id" :class="['msg', msg.sender_type === 'agent' ? 'self' : '']">
          <view class="msg-avatar">{{ msg.sender_type === 'agent' ? '服' : msg.sender_type === 'ai' ? 'AI' : '客' }}</view>
          <view class="msg-body">
            <text v-if="msg.revoked_at" class="revoke-tag">已撤回（管理端仍可查看原内容）</text>
            <image v-if="msg.message_type === 'image'" :src="assetURL(msg.content)" class="bubble-img" mode="widthFix" />
            <view v-else-if="msg.message_type === 'audio'" class="bubble bubble-audio" @tap="playAudio(msg.content)">
              <text class="audio-icon">▶</text>
              <text>语音消息</text>
            </view>
            <view v-else class="bubble" :class="{ 'bubble-revoked': !!msg.revoked_at }">{{ msg.content }}</view>
          </view>
        </view>
      </scroll-view>
      <view class="detail-footer">
        <button v-if="current && current.status === 'closed'" class="danger full" @tap="deleteCurrent">删除会话</button>
        <template v-else>
          <button class="transfer" @tap="transfer">强制转接</button>
          <button class="danger" @tap="closeCurrent">强制结束</button>
        </template>
      </view>
    </view>

    <admin-tab-bar active="monitor" />
  </view>
</template>

<script>
import { closeConversation, deleteConversation, exportConversationsCSV, fetchAgents, fetchConversations, fetchMessages, getAPIBase, transferConversation } from '../../common/api.js'
import { connectRealtime, onRealtime } from '../../common/realtime.js'
import AdminTabBar from '../../components/AdminTabBar.vue'

export default {
  components: { AdminTabBar },
  data() {
    return {
      conversations: [],
      agents: [],
      detailVisible: false,
      current: null,
      messages: [],
      hasMore: false,
      nextBefore: '',
      offRealtime: null,
      audioPlayer: null
    }
  },
  computed: {
    activeCount() {
      return this.conversations.filter((item) => item.status !== 'closed').length
    },
    currentRemark() {
      return (this.current && this.current.visitor_remark) || ''
    },
    currentStatusText() {
      return this.statusText(this.current && this.current.status)
    }
  },
  onShow() {
    connectRealtime()
    this.load()
    if (!this.offRealtime) {
      this.offRealtime = onRealtime((payload) => {
        if (String(payload.event).indexOf('conversation.') === 0) this.load()
        if (!this.current || !this.detailVisible) return
        if (payload.event === 'message.receive' && payload.data && payload.data.conversation_id === this.current.id) {
          this.upsertMessage(payload.data)
        }
        if (payload.event === 'message.revoked' && payload.data && payload.data.conversation_id === this.current.id) {
          this.upsertMessage(payload.data)
        }
        if (payload.event === 'conversation.status_changed' && payload.data && payload.data.id === this.current.id) {
          this.current = payload.data
        }
      })
    }
  },
  onUnload() {
    if (this.offRealtime) this.offRealtime()
    if (this.audioPlayer) {
      this.audioPlayer.destroy()
      this.audioPlayer = null
    }
  },
  methods: {
    async load() {
      const data = await fetchConversations()
      this.conversations = data.conversations || []
      const agents = await fetchAgents()
      this.agents = agents.agents || []
    },
    async exportCSV() {
      try {
        uni.showLoading({ title: '导出中' })
        await exportConversationsCSV()
        uni.showToast({ title: '会话CSV已导出', icon: 'none' })
      } catch (err) {
        uni.showToast({ title: err.message || '导出失败', icon: 'none' })
      } finally {
        uni.hideLoading()
      }
    },
    async closeItem(item) {
      await closeConversation(item.id)
      uni.showToast({ title: '会话已强关', icon: 'none' })
      this.load()
    },
    deleteItem(item) {
      uni.showModal({
        title: '删除会话',
        content: '确定删除这条已结束会话吗？删除后会话记录和评价会一起移除。',
        confirmColor: '#ef4444',
        success: async (res) => {
          if (!res.confirm) return
          await deleteConversation(item.id)
          if (this.current && this.current.id === item.id) {
            this.detailVisible = false
            this.current = null
          }
          uni.showToast({ title: '会话已删除', icon: 'none' })
          this.load()
        }
      })
    },
    async openDetail(item) {
      this.current = item
      const data = await fetchMessages(item.id, { limit: 50 })
      this.messages = data.messages || []
      this.hasMore = !!data.has_more
      this.nextBefore = data.next_before || ((this.messages[0] && this.messages[0].server_msg_id) || '')
      this.detailVisible = true
    },
    async loadOlder() {
      if (!this.current || !this.hasMore || !this.nextBefore) return
      const data = await fetchMessages(this.current.id, { limit: 50, before: this.nextBefore })
      const older = data.messages || []
      const seen = new Set(this.messages.map((item) => item.server_msg_id).filter(Boolean))
      this.messages = older.filter((item) => !seen.has(item.server_msg_id)).concat(this.messages)
      this.hasMore = !!data.has_more
      this.nextBefore = data.next_before || ((older[0] && older[0].server_msg_id) || this.nextBefore)
    },
    assetURL(value) {
      value = String(value || '')
      if (!value || value.indexOf('http://') === 0 || value.indexOf('https://') === 0 || value.indexOf('data:') === 0) return value
      if (value.indexOf('/') === 0) return getAPIBase().replace(/\/$/, '') + value
      return value
    },
    playAudio(value) {
      const src = this.assetURL(value)
      if (!src) return
      if (!this.audioPlayer) {
        this.audioPlayer = uni.createInnerAudioContext()
      } else {
        this.audioPlayer.stop()
      }
      this.audioPlayer.src = src
      this.audioPlayer.play()
    },
    upsertMessage(msg) {
      if (!msg) return
      const index = this.messages.findIndex((item) => item.server_msg_id === msg.server_msg_id)
      if (index >= 0) {
        this.messages.splice(index, 1, Object.assign({}, this.messages[index], msg))
        return
      }
      this.messages.push(msg)
    },
    showIP() {
      const ip = (this.current && this.current.visitor_ip) || '-'
      uni.showToast({ title: `原始IP: ${ip}`, icon: 'none' })
    },
    transfer() {
      const candidates = this.agents.filter((item) => !item.disabled_at)
      if (!candidates.length) {
        uni.showToast({ title: '暂无可转接客服', icon: 'none' })
        return
      }
      uni.showActionSheet({
        itemList: candidates.map((item) => `${item.name} / ${item.group || '默认组'} / ${this.agentStatusText(item.status)}`),
        success: async (res) => {
          if (!this.current) return
          const target = candidates[res.tapIndex]
          const data = await transferConversation(this.current.id, { agent_id: target.id })
          this.current = data.conversation
          uni.showToast({ title: `已转接至: ${target.name}`, icon: 'none' })
          this.load()
        }
      })
    },
    async closeCurrent() {
      if (!this.current) return
      await this.closeItem(this.current)
      this.detailVisible = false
    },
    deleteCurrent() {
      if (!this.current) return
      this.deleteItem(this.current)
    },
    statusText(status) {
      const map = {
        assigned: '人工接待',
        ai_serving: 'AI接管',
        human_requested: '请求人工',
        waiting: '排队中',
        closed: '已结束'
      }
      return map[status] || status
    },
    timeText(value) {
      return value ? String(value).slice(11, 16) : ''
    },
    agentStatusText(status) {
      const map = { online: '在线', busy: '忙碌', offline: '离线' }
      return map[status] || '未知'
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
  display: flex;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid #d5d5d5;
  position: relative;
}
.title {
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.header-actions {
  position: absolute;
  right: 32rpx;
  display: flex;
  align-items: center;
  gap: 24rpx;
}
.top-action {
  color: #576b95;
  font-size: 28rpx;
}
.subbar {
  height: 64rpx;
  background: #f3f4f6;
  border-bottom: 1px solid #e5e7eb;
  color: #6b7280;
  font-size: 24rpx;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 32rpx;
  box-sizing: border-box;
}
.filter {
  color: #374151;
}
.list {
  height: calc(100vh - 284rpx);
  padding-bottom: 120rpx;
  box-sizing: border-box;
}
.session {
  display: flex;
  align-items: center;
  padding: 24rpx;
  border-bottom: 1px solid #f3f4f6;
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
.main {
  flex: 1;
  margin-left: 24rpx;
  overflow: hidden;
}
.row {
  display: flex;
  align-items: center;
  margin-bottom: 8rpx;
}
.remark {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 32rpx;
}
.time {
  color: #9ca3af;
  font-size: 24rpx;
}
.tag {
  font-size: 20rpx;
  padding: 2rpx 8rpx;
  border-radius: 4rpx;
  margin-right: 12rpx;
  background: #dcfce7;
  color: #15803d;
}
.tag.ai_serving { background: #dbeafe; color: #2563eb; }
.tag.waiting, .tag.human_requested { background: #ffedd5; color: #ea580c; }
.tag.closed { background: #f3f4f6; color: #6b7280; }
.last {
  flex: 1;
  color: #6b7280;
  font-size: 26rpx;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.close-btn,
.delete-btn {
  margin-left: 16rpx;
  background: #fff;
  font-size: 24rpx;
}
.close-btn {
  color: #ef4444;
  border: 1px solid #fecaca;
}
.delete-btn {
  color: #576b95;
  border: 1px solid #c7d2fe;
}
.detail-page {
  position: fixed;
  inset: 0;
  background: #ededed;
  z-index: 40;
  display: flex;
  flex-direction: column;
}
.detail-header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  padding: 0 24rpx;
  box-sizing: border-box;
}
.back, .info {
  width: 96rpx;
  font-size: 48rpx;
  color: #374151;
}
.info {
  text-align: right;
  color: #576b95;
  font-size: 36rpx;
  font-weight: 600;
}
.detail-title {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.detail-name {
  font-size: 34rpx;
  color: #111827;
  font-weight: 600;
}
.detail-sub {
  font-size: 20rpx;
  color: #16a34a;
}
.history {
  flex: 1;
  padding: 32rpx;
  box-sizing: border-box;
}
.history-tip {
  color: #fff;
  background: #d1d5db;
  font-size: 22rpx;
  padding: 4rpx 12rpx;
  border-radius: 4rpx;
  margin: 8rpx auto 28rpx;
  text-align: center;
  width: 260rpx;
}
.load-more {
  color: #576b95;
  font-size: 24rpx;
  text-align: center;
  margin-bottom: 28rpx;
}
.msg {
  display: flex;
  gap: 24rpx;
  margin-bottom: 28rpx;
}
.msg.self {
  flex-direction: row-reverse;
}
.msg-body {
  max-width: 70%;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.self .msg-body {
  align-items: flex-end;
}
.msg-avatar {
  width: 80rpx;
  height: 80rpx;
  border-radius: 8rpx;
  background: #d1d5db;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
}
.self .msg-avatar {
  background: #10b981;
}
.bubble {
  background: #fff;
  border-radius: 12rpx;
  padding: 20rpx;
  color: #111827;
  font-size: 32rpx;
  line-height: 1.45;
}
.self .bubble {
  background: #95ec69;
}
.bubble-revoked {
  background: #f3f4f6 !important;
  color: #6b7280 !important;
  border: 1px solid #e5e7eb;
}
.revoke-tag {
  margin-bottom: 10rpx;
  font-size: 22rpx;
  color: #9ca3af;
}
.bubble-img {
  max-width: 280rpx;
  border-radius: 12rpx;
  border: 1px solid #e5e7eb;
}
.bubble-audio {
  min-width: 220rpx;
  display: flex;
  align-items: center;
  gap: 14rpx;
}
.audio-icon {
  font-size: 30rpx;
  font-weight: 600;
}
.detail-footer {
  background: #f7f7f7;
  border-top: 1px solid #d5d5d5;
  padding: 20rpx 24rpx env(safe-area-inset-bottom);
  display: flex;
  gap: 24rpx;
}
.transfer, .danger {
  flex: 1;
  font-size: 30rpx;
  border-radius: 8rpx;
}
.full {
  flex: 1;
}
.transfer {
  background: #fff;
  color: #111827;
  border: 1px solid #d1d5db;
}
.danger {
  background: #fef2f2;
  color: #dc2626;
  border: 1px solid #fecaca;
}
</style>
