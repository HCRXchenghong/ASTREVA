<template>
  <view class="page safe-page" @touchmove="onPageTouchMove" @touchend="onPageTouchEnd" @touchcancel="onPageTouchCancel">
    <view class="header safe-header">
      <text class="back" @tap="back">‹</text>
      <view class="header-main">
        <text class="title">{{ conversationTitle }}</text>
        <text class="sub">{{ conversationStatusText }}</text>
      </view>
      <text class="more" @tap="showProfile = true">···</text>
    </view>

    <scroll-view class="history flex-scroll" scroll-y :scroll-top="scrollTop">
      <view v-if="hasMore" class="load-more" @tap="loadOlder">点击加载更多历史记录</view>
      <view
        v-for="msg in messages"
        :key="msg.server_msg_id || msg.client_msg_id"
        :class="['msg', msg.sender_type === 'agent' ? 'self' : 'visitor', !msg.server_msg_id ? 'pending' : '']"
        @longpress="openMessageActions(msg)"
      >
        <view class="avatar">{{ senderText(msg.sender_type) }}</view>
        <view class="msg-body">
          <text v-if="msg.revoked_at" class="revoke-tag">已撤回</text>
          <image v-if="msg.message_type === 'image'" :src="assetURL(msg.content)" class="bubble-img" mode="widthFix" />
          <view v-else-if="msg.message_type === 'audio'" class="bubble bubble-audio" @tap="playAudio(msg.content)">
            <text class="audio-icon">▶</text>
            <text>语音消息</text>
          </view>
          <view v-else class="bubble" :class="{ 'bubble-revoked': !!msg.revoked_at }">{{ msg.content }}</view>
        </view>
      </view>
    </scroll-view>

    <view v-if="needsTakeover" class="takeover safe-footer">
      <button class="takeover-btn" @tap="takeover">接管会话 (结束 AI 托管)</button>
    </view>
    <view v-else class="footer safe-footer">
      <view class="input-row">
        <view
          class="tool-icon voice-tool"
          :class="{ 'tool-icon-active': recordOverlayVisible }"
          @touchstart.stop.prevent="startRecordHold"
        >
          <text class="voice-icon">🎙</text>
        </view>
        <view class="tool-icon" @tap="togglePanel('emoji')">😊</view>
        <textarea
          v-model="draft"
          class="input"
          auto-height
          maxlength="1000"
          placeholder="输入消息"
          confirm-type="send"
          :show-confirm-bar="false"
          :cursor-spacing="16"
          @focus="closePanels"
          @confirm="send"
        />
        <button v-if="draft.trim()" class="send-btn" @tap="send">发送</button>
        <view v-else class="tool-icon plus" @tap="togglePanel('more')">＋</view>
      </view>
      <view v-if="activePanel === 'emoji'" class="panel emoji-panel">
        <text v-for="item in emojis" :key="item" class="emoji" @tap="appendEmoji(item)">{{ item }}</text>
      </view>
      <view v-if="activePanel === 'more'" class="panel more-panel">
        <view class="action" @tap="sendAction('photo')">
          <view class="action-icon">▧</view>
          <text>相册</text>
        </view>
        <view class="action" @tap="sendAction('shortcut')">
          <view class="action-icon">⌘</view>
          <text>快捷回复</text>
        </view>
        <view class="action" @tap="sendAction('phone')">
          <view class="action-icon">☎</view>
          <text>发电话</text>
        </view>
        <view class="action" @tap="sendAction('wechat')">
          <view class="action-icon">微</view>
          <text>发微信</text>
        </view>
        <view class="action" @tap="sendAction('end')">
          <view class="action-icon danger">×</view>
          <text>结束会话</text>
        </view>
      </view>
    </view>

    <view v-if="recordOverlayVisible" class="record-overlay">
      <view :class="['record-card', recordCanceled ? 'record-card-cancel' : '']">
        <view class="record-icon-wrap">
          <text v-if="recordCanceled" class="record-cancel-icon">×</text>
          <view v-else class="record-dot"></view>
        </view>
        <text class="record-title">{{ recordCanceled ? '松开取消发送' : '松开发送语音' }}</text>
        <text class="record-tip">{{ recordCanceled ? '已上滑，松开后取消' : '上滑取消发送' }}</text>
      </view>
    </view>

    <view v-if="showProfile" class="mask" @tap="showProfile = false">
      <view class="profile" @tap.stop>
        <view class="profile-head">
          <text class="profile-title">访客资料</text>
          <text class="profile-close" @tap="showProfile = false">×</text>
        </view>
        <view class="profile-card">
        <view class="profile-row">
          <text>真实 IP</text>
          <text>{{ conversationIP }}</text>
        </view>
        <view class="profile-row">
          <text>来源</text>
          <text>{{ conversationSource }}</text>
        </view>
        <view class="profile-row">
          <text>当前状态</text>
          <text>{{ conversationStatusText }}</text>
        </view>
      </view>
        <text class="section-label">访客备注</text>
        <view class="remark-box">
          <input v-model="remark" class="remark-input" placeholder="输入备注名" />
          <button class="remark-btn" @tap="saveRemark">保存</button>
        </view>
        <button class="profile-action" @tap="closeCurrent">结束会话</button>
      </view>
    </view>
  </view>
</template>

<script>
import { fetchMessages, fetchConversations, updateRemark, closeConversation, getAPIBase, markConversationRead, revokeMessage, uploadFile } from '../../common/api.js'
import { closeRealtimeConversation, onRealtime, sendMessage } from '../../common/realtime.js'

let sharedRecorderManager = null
let recorderListenersBound = false
let activeChatPage = null

export default {
  data() {
    return {
      conversationId: '',
      conversation: null,
      messages: [],
      draft: '',
      remark: '',
      showProfile: false,
      activePanel: '',
      emojis: ['😀', '😂', '😊', '😍', '👍', '🙏', '🎉', '😅', '🤝', '📞', '💬', '✅', '❗', '❤️', '☕', '🌟'],
      scrollTop: 0,
      offRealtime: null,
      hasMore: false,
      nextBefore: '',
      audioPlayer: null,
      recorderManager: null,
      recordHoldTimer: null,
      recordPressing: false,
      recordOverlayVisible: false,
      recordCanceled: false,
      recordStartY: 0,
      recordStartAt: 0,
      recordStopping: false,
      recordPendingCancel: false,
      recordStartRequested: false,
      recordFinishAfterStart: false,
      recordingSupported: true
    }
  },
  computed: {
    needsTakeover() {
      const status = this.conversation && this.conversation.status
      return ['ai_serving', 'human_requested', 'waiting'].indexOf(status) >= 0
    },
    conversationTitle() {
      return (this.conversation && this.conversation.visitor_remark) || '访客'
    },
    conversationStatusText() {
      return this.statusText(this.conversation && this.conversation.status)
    },
    conversationIP() {
      return (this.conversation && this.conversation.visitor_ip) || ''
    },
    conversationSource() {
      return (this.conversation && this.conversation.source) || 'web'
    }
  },
  onLoad(query) {
    this.conversationId = query.id
    activeChatPage = this
    this.ensureRecorderManager()
    this.load()
    this.offRealtime = onRealtime((payload) => {
      if (payload.event === 'message.ack' && payload.data && payload.data.conversation_id === this.conversationId) {
        this.upsertMessage(payload.data)
        this.toBottom()
      }
      if (payload.event === 'message.receive' && payload.data && payload.data.conversation_id === this.conversationId) {
        this.upsertMessage(payload.data)
        this.syncReadState()
        this.toBottom()
      }
      if (payload.event === 'message.revoked' && payload.data && payload.data.conversation_id === this.conversationId) {
        this.upsertMessage(payload.data)
      }
      if (payload.event === 'conversation.status_changed' && payload.data && payload.data.id === this.conversationId) {
        this.conversation = payload.data
      }
    })
  },
  onUnload() {
    if (this.offRealtime) this.offRealtime()
    if (this.audioPlayer) {
      this.audioPlayer.destroy()
      this.audioPlayer = null
    }
    if (activeChatPage === this) activeChatPage = null
    this.resetRecordState(true)
  },
  methods: {
    ensureRecorderManager() {
      if (typeof uni.getRecorderManager !== 'function') {
        this.recordingSupported = false
        return
      }
      if (!sharedRecorderManager) {
        sharedRecorderManager = uni.getRecorderManager()
      }
      this.recorderManager = sharedRecorderManager
      this.recordingSupported = true
      if (recorderListenersBound) return
      recorderListenersBound = true
      sharedRecorderManager.onStart(() => {
        if (activeChatPage) activeChatPage.handleRecorderStart()
      })
      sharedRecorderManager.onStop((res) => {
        if (activeChatPage) activeChatPage.handleRecorderStop(res)
      })
      sharedRecorderManager.onError((err) => {
        if (activeChatPage) activeChatPage.handleRecorderError(err)
      })
    },
    async load() {
      const convs = await fetchConversations()
      this.conversation = (convs.conversations || []).find((item) => item.id === this.conversationId)
      this.remark = (this.conversation && this.conversation.visitor_remark) || ''
      const data = await fetchMessages(this.conversationId, { limit: 50 })
      this.messages = data.messages || []
      this.hasMore = !!data.has_more
      this.nextBefore = data.next_before || ((this.messages[0] && this.messages[0].server_msg_id) || '')
      this.syncReadState()
      this.toBottom()
    },
    async loadOlder() {
      if (!this.hasMore || !this.nextBefore) return
      const data = await fetchMessages(this.conversationId, { limit: 50, before: this.nextBefore })
      const older = data.messages || []
      const seen = new Set(this.messages.map((item) => item.server_msg_id).filter(Boolean))
      this.messages = older.filter((item) => !seen.has(item.server_msg_id)).concat(this.messages)
      this.hasMore = !!data.has_more
      this.nextBefore = data.next_before || ((older[0] && older[0].server_msg_id) || this.nextBefore)
    },
    upsertMessage(msg) {
      if (!msg) return
      const index = this.messages.findIndex((item) => {
        if (msg.server_msg_id && item.server_msg_id === msg.server_msg_id) return true
        return msg.client_msg_id && item.client_msg_id === msg.client_msg_id
      })
      if (index >= 0) {
        this.messages.splice(index, 1, Object.assign({}, this.messages[index], msg))
        return
      }
      this.messages.push(msg)
    },
    async syncReadState() {
      try {
        const data = await markConversationRead(this.conversationId)
        if (data && data.conversation) {
          this.conversation = data.conversation
        }
      } catch (err) {}
    },
    senderText(senderType) {
      if (senderType === 'agent') return '我'
      if (senderType === 'ai') return 'AI'
      if (senderType === 'system') return '系'
      return '客'
    },
    buildLocalAgentMessage(content, messageType, clientMsgId) {
      return {
        server_msg_id: '',
        client_msg_id: clientMsgId,
        conversation_id: this.conversationId,
        sender_type: 'agent',
        sender_id: '',
        message_type: messageType || 'text',
        content,
        created_at: new Date().toISOString()
      }
    },
    pushLocalAgentMessage(content, messageType = 'text') {
      const clientMsgId = sendMessage(this.conversationId, content, messageType)
      if (!clientMsgId) {
        uni.showToast({ title: '发送失败，请稍后重试', icon: 'none' })
        return false
      }
      this.upsertMessage(this.buildLocalAgentMessage(content, messageType, clientMsgId))
      this.toBottom()
      return true
    },
    assetURL(value) {
      value = String(value || '')
      if (!value || value.indexOf('http://') === 0 || value.indexOf('https://') === 0 || value.indexOf('data:') === 0) return value
      if (value.indexOf('/') === 0) return getAPIBase().replace(/\/$/, '') + value
      return value
    },
    extractTouchY(event) {
      const touch = event && event.touches && event.touches[0]
      if (touch && typeof touch.clientY === 'number') return touch.clientY
      const changed = event && event.changedTouches && event.changedTouches[0]
      if (changed && typeof changed.clientY === 'number') return changed.clientY
      return 0
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
    send() {
      const text = this.draft.trim()
      if (!text) return
      this.draft = ''
      this.closePanels()
      this.pushLocalAgentMessage(text, 'text')
    },
    takeover() {
      this.pushLocalAgentMessage('您好，我是人工客服，很高兴为您服务。', 'text')
      if (this.conversation) this.conversation.status = 'assigned'
    },
    closePanels() {
      this.activePanel = ''
    },
    startRecordHold(event) {
      if (this.needsTakeover) return
      this.ensureRecorderManager()
      if (!this.recordingSupported || !this.recorderManager) {
        uni.showToast({ title: '当前环境不支持录音', icon: 'none' })
        return
      }
      if (this.recordPressing || this.recordOverlayVisible || this.recordStopping) return
      this.closePanels()
      this.recordPressing = true
      this.recordCanceled = false
      this.recordPendingCancel = false
      this.recordStartRequested = false
      this.recordFinishAfterStart = false
      this.recordStartY = this.extractTouchY(event)
      clearTimeout(this.recordHoldTimer)
      this.recordHoldTimer = setTimeout(() => {
        if (!this.recordPressing || !this.recorderManager) return
        try {
          this.recordStartRequested = true
          this.recorderManager.start({
            duration: 60000,
            sampleRate: 16000,
            numberOfChannels: 1,
            encodeBitRate: 96000,
            format: 'mp3'
          })
        } catch (err) {
          try {
            this.recordStartRequested = true
            this.recorderManager.start({ duration: 60000 })
          } catch (innerErr) {
            this.resetRecordState(true)
            uni.showToast({ title: '无法启动录音', icon: 'none' })
          }
        }
      }, 260)
    },
    onPageTouchMove(event) {
      if (!this.recordPressing && !this.recordOverlayVisible) return
      const currentY = this.extractTouchY(event)
      if (!currentY || !this.recordStartY) return
      this.recordCanceled = this.recordStartY - currentY > 80
    },
    onPageTouchEnd() {
      this.finishRecordGesture(false)
    },
    onPageTouchCancel() {
      this.finishRecordGesture(true)
    },
    finishRecordGesture(forceCancel) {
      if (!this.recordPressing && !this.recordOverlayVisible) return
      this.recordPressing = false
      clearTimeout(this.recordHoldTimer)
      this.recordHoldTimer = null
      if ((!this.recordOverlayVisible && !this.recordStartRequested) || !this.recorderManager) {
        this.resetRecordState(true)
        return
      }
      if (this.recordStopping) return
      this.recordStopping = true
      this.recordPendingCancel = this.recordCanceled || forceCancel
      if (!this.recordOverlayVisible && this.recordStartRequested) {
        this.recordFinishAfterStart = true
        return
      }
      try {
        this.recorderManager.stop()
      } catch (err) {
        this.resetRecordState(true)
        uni.showToast({ title: '录音结束失败', icon: 'none' })
      }
    },
    handleRecorderStart() {
      const shouldStopImmediately = this.recordFinishAfterStart
      this.recordOverlayVisible = true
      this.recordStartAt = Date.now()
      this.recordStartRequested = true
      if (!shouldStopImmediately) {
        this.recordCanceled = false
        this.recordStopping = false
        this.recordPendingCancel = false
      }
      if (shouldStopImmediately && this.recorderManager) {
        this.recordStopping = true
        try {
          this.recorderManager.stop()
        } catch (err) {
          this.resetRecordState(true)
          uni.showToast({ title: '录音结束失败', icon: 'none' })
        }
      }
    },
    async handleRecorderStop(res) {
      const wasCanceled = this.recordPendingCancel
      const duration = (res && res.duration) || (this.recordStartAt ? Date.now() - this.recordStartAt : 0)
      const tempFilePath = res && res.tempFilePath
      this.resetRecordState(true)
      if (wasCanceled) {
        uni.showToast({ title: '已取消发送', icon: 'none' })
        return
      }
      if (!tempFilePath) {
        uni.showToast({ title: '录音文件生成失败', icon: 'none' })
        return
      }
      if (duration < 500) {
        uni.showToast({ title: '录音时间太短', icon: 'none' })
        return
      }
      try {
        uni.showLoading({ title: '发送中' })
        const uploaded = await uploadFile(tempFilePath)
        this.pushLocalAgentMessage(uploaded.url, 'audio')
      } catch (err) {
        uni.showToast({ title: err.message || '语音发送失败', icon: 'none' })
      } finally {
        uni.hideLoading()
      }
    },
    handleRecorderError() {
      this.resetRecordState(true)
      uni.showToast({ title: '录音失败，请检查权限', icon: 'none' })
    },
    cancelRecordHold() {
      this.finishRecordGesture(true)
    },
    resetRecordState(forceSilent) {
      clearTimeout(this.recordHoldTimer)
      this.recordHoldTimer = null
      this.recordPressing = false
      this.recordOverlayVisible = false
      this.recordCanceled = false
      this.recordStartY = 0
      this.recordStartAt = 0
      this.recordStopping = false
      this.recordPendingCancel = false
      this.recordStartRequested = false
      this.recordFinishAfterStart = false
      if (!forceSilent && this.draft.trim()) this.closePanels()
    },
    togglePanel(panel) {
      if (uni.hideKeyboard) uni.hideKeyboard()
      this.activePanel = this.activePanel === panel ? '' : panel
    },
    appendEmoji(item) {
      this.draft += item
    },
    sendAction(type) {
      this.closePanels()
      if (type === 'photo') {
        this.choosePhoto()
        return
      }
      if (type === 'shortcut') {
        this.pushLocalAgentMessage('您好，请问有什么可以帮您的？请详细描述您的问题。', 'text')
        return
      }
      if (type === 'phone') {
        this.pushLocalAgentMessage('您可以拨打我们的热线：400-123-4567', 'text')
        return
      }
      if (type === 'wechat') {
        this.pushLocalAgentMessage('您可以添加官方微信号：Service999 进行联系。', 'text')
        return
      }
      if (type === 'end') {
        this.closeCurrent()
      }
    },
    choosePhoto() {
      uni.chooseImage({
        count: 1,
        sizeType: ['compressed'],
        sourceType: ['album', 'camera'],
        success: async (res) => {
          const localPath = res.tempFilePaths[0]
          if (!localPath) return
          try {
            uni.showLoading({ title: '发送中' })
            const uploaded = await uploadFile(localPath)
            this.pushLocalAgentMessage(uploaded.url, 'image')
          } catch (err) {
            uni.showToast({ title: err.message || '图片发送失败', icon: 'none' })
          } finally {
            uni.hideLoading()
          }
        }
      })
    },
    async saveRemark() {
      const data = await updateRemark(this.conversationId, this.remark)
      this.conversation = data.conversation
      this.showProfile = false
      uni.showToast({ title: '备注保存成功', icon: 'none' })
    },
    async closeCurrent() {
      closeRealtimeConversation(this.conversationId)
      await closeConversation(this.conversationId)
      uni.showToast({ title: '会话已结束', icon: 'none' })
      this.back()
    },
    openMessageActions(msg) {
      if (!msg || msg.sender_type !== 'agent' || !msg.server_msg_id || msg.revoked_at) return
      uni.showActionSheet({
        itemList: ['撤回消息'],
        success: async (res) => {
          if (res.tapIndex !== 0) return
          try {
            const data = await revokeMessage(this.conversationId, msg.server_msg_id)
            if (data && data.message) this.upsertMessage(data.message)
            if (data && data.conversation) this.conversation = data.conversation
            uni.showToast({ title: '消息已撤回', icon: 'none' })
          } catch (err) {
            uni.showToast({ title: err.message || '撤回失败', icon: 'none' })
          }
        }
      })
    },
    back() {
      uni.navigateBack()
    },
    toBottom() {
      this.$nextTick(() => {
        this.scrollTop = Date.now()
      })
    },
    statusText(status) {
      const map = {
        assigned: '人工在线',
        ai_serving: 'AI 自动接待中',
        human_requested: '请求人工接管',
        waiting: '等待人工',
        closed: '已结束'
      }
      return map[status] || ''
    }
  }
}
</script>

<style scoped>
.page {
  min-height: 100vh;
  background: #ededed;
  display: flex;
  flex-direction: column;
}
.header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  padding: 0 20rpx;
}
.back, .more {
  width: 88rpx;
  font-size: 48rpx;
  color: #374151;
  flex-shrink: 0;
}
.more {
  text-align: right;
  letter-spacing: 4rpx;
}
.header-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 8rpx 24rpx 0;
  box-sizing: border-box;
}
.title {
  max-width: 100%;
  font-size: 38rpx;
  font-weight: 600;
  color: #111;
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sub {
  max-width: 100%;
  margin-top: 6rpx;
  font-size: 22rpx;
  color: #f97316;
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.history {
  flex: 1;
  padding: 28rpx 24rpx 16rpx;
  box-sizing: border-box;
}
.load-more {
  text-align: center;
  color: #576b95;
  font-size: 24rpx;
  margin-bottom: 28rpx;
}
.msg {
  display: flex;
  align-items: flex-start;
  gap: 16rpx;
  margin-bottom: 28rpx;
}
.msg.self {
  flex-direction: row-reverse;
}
.msg-body {
  max-width: 76%;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}
.self .msg-body {
  align-items: flex-end;
}
.avatar {
  width: 72rpx;
  height: 72rpx;
  border-radius: 12rpx;
  background: #d1d5db;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28rpx;
  flex-shrink: 0;
}
.self .avatar {
  background: #07c160;
}
.bubble {
  min-height: 52rpx;
  background: #fff;
  border-radius: 16rpx;
  padding: 22rpx 24rpx;
  box-sizing: border-box;
  font-size: 30rpx;
  color: #111;
  line-height: 1.55;
  word-break: break-word;
}
.self .bubble {
  background: #95ec69;
}
.bubble-revoked {
  background: #f3f4f6 !important;
  color: #6b7280 !important;
  border: 1px solid #e5e7eb;
}
.pending .bubble,
.pending .bubble-img {
  opacity: 0.72;
}
.revoke-tag {
  margin-bottom: 10rpx;
  font-size: 22rpx;
  color: #9ca3af;
}
.bubble-img {
  max-width: 320rpx;
  border-radius: 16rpx;
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
.footer, .takeover {
  background: #f7f7f7;
  border-top: 1px solid #d5d5d5;
  padding: 16rpx 20rpx;
}
.input-row {
  display: flex;
  align-items: center;
  gap: 12rpx;
}
.input {
  flex: 1;
  min-height: 84rpx;
  max-height: 196rpx;
  background: #fff;
  border: 1px solid #d1d5db;
  border-radius: 16rpx;
  padding: 20rpx 22rpx;
  font-size: 30rpx;
  line-height: 42rpx;
  box-sizing: border-box;
}
.send-btn, .takeover-btn {
  background: #07c160;
  color: #fff;
  border-radius: 16rpx;
  font-size: 30rpx;
}
.send-btn {
  width: 112rpx;
  height: 72rpx;
  line-height: 72rpx;
  padding: 0;
}
.takeover-btn {
  width: 100%;
}
.tool-icon {
  width: 72rpx;
  height: 72rpx;
  border-radius: 50%;
  background: #fff;
  border: 1px solid #d5d5d5;
  color: #374151;
  font-size: 38rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.voice-tool {
  background: #ffffff;
}
.tool-icon-active {
  background: #07c160;
  border-color: #07c160;
  color: #ffffff;
}
.tool-icon.plus {
  font-size: 44rpx;
}
.voice-icon {
  font-size: 34rpx;
  line-height: 1;
}
.panel {
  height: 360rpx;
  padding: 24rpx 16rpx 8rpx;
  box-sizing: border-box;
}
.emoji-panel {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 16rpx;
}
.emoji {
  font-size: 44rpx;
  text-align: center;
  line-height: 64rpx;
  border-radius: 12rpx;
}
.more-panel {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  column-gap: 16rpx;
  row-gap: 24rpx;
}
.action {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12rpx;
  color: #4b5563;
  font-size: 24rpx;
}
.action-icon {
  width: 104rpx;
  height: 104rpx;
  border-radius: 16rpx;
  background: #fff;
  color: #576b95;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 42rpx;
  border: 1px solid #e5e7eb;
}
.action-icon.danger {
  color: #ef4444;
}
.record-overlay {
  position: absolute;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  pointer-events: none;
  padding-bottom: 220rpx;
  box-sizing: border-box;
}
.record-card {
  min-width: 260rpx;
  background: rgba(17, 24, 39, 0.86);
  border-radius: 24rpx;
  padding: 28rpx 32rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
  color: #ffffff;
}
.record-card-cancel {
  background: rgba(220, 38, 38, 0.92);
}
.record-icon-wrap {
  width: 92rpx;
  height: 92rpx;
  border-radius: 24rpx;
  background: rgba(255, 255, 255, 0.12);
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 18rpx;
}
.record-dot {
  width: 28rpx;
  height: 28rpx;
  border-radius: 50%;
  background: #34d399;
  box-shadow: 0 0 0 14rpx rgba(52, 211, 153, 0.18);
}
.record-cancel-icon {
  font-size: 52rpx;
  line-height: 1;
  font-weight: 600;
}
.record-title {
  font-size: 28rpx;
  font-weight: 600;
}
.record-tip {
  margin-top: 12rpx;
  font-size: 22rpx;
  color: rgba(255, 255, 255, 0.82);
}
.mask {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,.5);
  display: flex;
  justify-content: flex-end;
}
.profile {
  width: 85%;
  height: 100%;
  background: #fff;
  padding: 32rpx 0;
  box-sizing: border-box;
}
.profile-head {
  height: 88rpx;
  padding: 0 32rpx;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.profile-title {
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.profile-close {
  font-size: 44rpx;
  color: #6b7280;
}
.profile-card {
  margin-top: 24rpx;
  border-top: 1px solid #f3f4f6;
  border-bottom: 1px solid #f3f4f6;
}
.profile-row {
  display: flex;
  justify-content: space-between;
  padding: 28rpx 32rpx;
  color: #4b5563;
  font-size: 28rpx;
  border-bottom: 1px solid #f3f4f6;
}
.profile-row:last-child {
  border-bottom: none;
}
.section-label {
  display: block;
  padding: 28rpx 32rpx 12rpx;
  color: #9ca3af;
  font-size: 24rpx;
}
.remark-box {
  display: flex;
  gap: 16rpx;
  padding: 0 32rpx;
}
.remark-input {
  flex: 1;
  border: 1px solid #d1d5db;
  border-radius: 8rpx;
  padding: 0 16rpx;
}
.remark-btn {
  background: #07c160;
  color: #fff;
  border-radius: 8rpx;
}
.profile-action {
  margin: 40rpx 32rpx 0;
  background: #fff;
  color: #ef4444;
  border: 1px solid #fecaca;
}
</style>
