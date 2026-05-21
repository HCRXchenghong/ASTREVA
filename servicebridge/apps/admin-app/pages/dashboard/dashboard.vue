<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <text class="title">数据概览</text>
    </view>
    <scroll-view class="content flex-scroll" scroll-y>
      <view class="grid">
        <view class="card">
          <text class="label">当前会话</text>
          <text class="value">{{ stats.active_conversations }}</text>
        </view>
        <view class="card">
          <text class="label">AI 接待中</text>
          <text class="value green">{{ stats.ai_serving }}</text>
        </view>
        <view class="card">
          <text class="label">当前排队</text>
          <text class="value orange">{{ stats.waiting + stats.human_requested }}</text>
        </view>
        <view class="card">
          <text class="label">人工接待</text>
          <text class="value blue">{{ stats.assigned }}</text>
        </view>
        <view class="card">
          <text class="label">评价均分</text>
          <text class="value blue">{{ averageScore }}</text>
        </view>
        <view class="card">
          <text class="label">满意率</text>
          <text class="value green">{{ satisfactionRate }}</text>
        </view>
      </view>
      <view class="status-card">
        <view class="status-head">
          <text class="status-title">系统状态通知</text>
          <text class="status-time">{{ systemStatusTime }}</text>
        </view>
        <view v-for="line in systemStatusLines" :key="line.key" class="status-line">
          <text :class="['status-dot', line.level]"></text>
          <text>{{ line.text }}</text>
        </view>
      </view>
      <view class="ratings-card">
        <view class="ratings-head">
          <text class="status-title">最近服务评价</text>
          <text class="rating-count">{{ stats.rating.total }} 条</text>
        </view>
        <view v-if="!ratings.length" class="empty">暂无评价</view>
        <view v-for="item in ratings" :key="item.id" class="rating-row">
          <text :class="['rating-score', item.score < 5 ? 'warning' : '']">{{ item.score }} 分</text>
          <view class="rating-main">
            <text class="rating-comment">{{ ratingTagsText(item) }}</text>
            <text v-if="ratingAdviceText(item)" :class="['rating-advice', item.score < 5 ? 'warning' : '']">建议：{{ ratingAdviceText(item) }}</text>
          </view>
        </view>
      </view>
    </scroll-view>
    <admin-tab-bar active="dashboard" />
  </view>
</template>

<script>
import { fetchDashboard, fetchRatings, fetchSystemStatus } from '../../common/api.js'
import { connectRealtime, getRealtimeState, onRealtime, onRealtimeState } from '../../common/realtime.js'
import AdminTabBar from '../../components/AdminTabBar.vue'

export default {
  components: { AdminTabBar },
  data() {
    return {
      stats: {
        active_conversations: 0,
        ai_serving: 0,
        waiting: 0,
        human_requested: 0,
        assigned: 0,
        online_agents: 0,
        total_agents: 0,
        rating: { total: 0, average: 0, satisfaction_rate: 0 }
      },
      ratings: [],
      systemStatus: null,
      realtimeState: getRealtimeState(),
      offRealtime: null,
      offRealtimeState: null
    }
  },
  onShow() {
    connectRealtime()
    this.load()
    if (!this.offRealtime) {
      this.offRealtime = onRealtime((payload) => {
        const event = String(payload.event || '')
        if (event.indexOf('conversation.') === 0 || event.indexOf('agent.') === 0 || event.indexOf('rating.') === 0) this.load()
      })
    }
    if (!this.offRealtimeState) {
      this.offRealtimeState = onRealtimeState((state) => {
        this.realtimeState = state
      })
    }
  },
  onUnload() {
    if (this.offRealtime) this.offRealtime()
    if (this.offRealtimeState) this.offRealtimeState()
  },
  computed: {
    averageScore() {
      const rating = this.stats && this.stats.rating ? this.stats.rating : {}
      const average = Number(rating.average || 0)
      return average ? average.toFixed(1) : '-'
    },
    satisfactionRate() {
      const rating = this.stats && this.stats.rating ? this.stats.rating : {}
      const rate = Number(rating.satisfaction_rate || 0)
      return rating.total ? `${Math.round(rate * 100)}%` : '-'
    },
    systemStatusTime() {
      const value = this.systemStatus && this.systemStatus.time
      return value ? `更新 ${String(value).replace('T', ' ').slice(11, 16)}` : '实时检测'
    },
    systemStatusLines() {
      const status = this.systemStatus || {}
      const database = status.database || {}
      const ai = status.ai || {}
      const socket = this.realtimeState || {}
      const hub = status.websocket || {}
      const lines = []

      if (!this.systemStatus) {
        lines.push({ key: 'loading', level: 'warn', text: '正在读取后端真实状态...' })
      } else {
        lines.push({
          key: 'database',
          level: database.ok ? 'ok' : 'bad',
          text: database.ok ? '数据库连接正常' : `数据库连接异常：${database.message || '未知错误'}`
        })
        if (!ai.enabled) {
          lines.push({ key: 'ai', level: 'warn', text: 'AI 接待已关闭' })
        } else if (ai.configured) {
          lines.push({ key: 'ai', level: 'ok', text: `AI 配置已加载：${ai.model || '-'} / ${ai.api_type || '-'}` })
        } else {
          lines.push({ key: 'ai', level: 'bad', text: 'AI 配置不完整：请检查 Base URL、Model 和 API Key' })
        }
      }

      lines.push({
        key: 'websocket',
        level: socket.connected ? 'ok' : (socket.connecting ? 'warn' : 'bad'),
        text: socket.connected
          ? `WebSocket 已连接（管理端 ${Number(hub.Admins || hub.admins || 0)}）`
          : (socket.connecting ? 'WebSocket 正在连接...' : `WebSocket 未连接${socket.last_error ? '：' + socket.last_error : ''}`)
      })
      lines.push({ key: 'agents', level: 'ok', text: `在线客服 ${statsSafe(this.stats.online_agents)} / ${statsSafe(this.stats.total_agents)}` })
      return lines
    }
  },
  methods: {
    async load() {
      try {
        this.stats = await fetchDashboard()
      } catch (err) {
        uni.showToast({ title: err.message || '概览加载失败', icon: 'none' })
      }
      try {
        const data = await fetchRatings(5)
        this.ratings = data.ratings || []
      } catch (err) {
        this.ratings = []
      }
      try {
        this.systemStatus = await fetchSystemStatus()
      } catch (err) {
        this.systemStatus = {
          database: { ok: false, message: err.message || '状态接口不可用' },
          ai: { enabled: false, configured: false },
          websocket: {},
          time: new Date().toISOString()
        }
      }
    },
    ratingTagsText(item) {
      const tags = (item && item.tags ? item.tags : []).filter(Boolean)
      return tags.join('、') || this.scoreText(item && item.score)
    },
    ratingAdviceText(item) {
      return String((item && item.comment) || '').trim()
    },
    scoreText(score) {
      if (Number(score) >= 5) return '非常满意'
      if (Number(score) >= 3) return '一般'
      return '不满意'
    }
  }
}

function statsSafe(value) {
  return Number(value || 0)
}
</script>

<style scoped>
.page {
  background: #ededed;
}
.header {
  height: 100rpx;
  display: flex;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid #d5d5d5;
}
.title {
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24rpx;
  padding: 32rpx;
}
.card {
  background: #fff;
  border-radius: 16rpx;
  padding: 32rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.label {
  color: #6b7280;
  font-size: 26rpx;
}
.value {
  margin-top: 12rpx;
  font-size: 48rpx;
  font-weight: 700;
  color: #111827;
}
.green { color: #07c160; }
.orange { color: #f97316; }
.blue { color: #576b95; }
.content {
  padding-bottom: 120rpx;
  box-sizing: border-box;
}
.status-card {
  background: #fff;
  padding: 28rpx 32rpx;
  border-top: 1px solid #e5e7eb;
  border-bottom: 1px solid #e5e7eb;
  display: flex;
  flex-direction: column;
  gap: 16rpx;
}
.status-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.status-title {
  color: #6b7280;
  font-size: 26rpx;
}
.status-time {
  color: #9ca3af;
  font-size: 22rpx;
}
.status-line {
  color: #111827;
  font-size: 28rpx;
  display: flex;
  align-items: center;
  gap: 12rpx;
}
.status-dot {
  width: 14rpx;
  height: 14rpx;
  border-radius: 50%;
  background: #9ca3af;
  flex-shrink: 0;
}
.status-dot.ok { background: #07c160; }
.status-dot.warn { background: #f59e0b; }
.status-dot.bad { background: #ef4444; }
.ratings-card {
  margin-top: 16rpx;
  background: #fff;
  border-top: 1px solid #e5e7eb;
  border-bottom: 1px solid #e5e7eb;
}
.ratings-head,
.rating-row {
  display: flex;
  align-items: center;
  padding: 24rpx 32rpx;
  border-bottom: 1px solid #f3f4f6;
}
.ratings-head {
  justify-content: space-between;
}
.rating-count {
  color: #9ca3af;
  font-size: 24rpx;
}
.rating-score {
  width: 92rpx;
  color: #f59e0b;
  font-size: 28rpx;
  font-weight: 600;
  flex-shrink: 0;
}
.rating-score.warning {
  color: #ef4444;
}
.rating-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8rpx;
}
.rating-comment {
  color: #4b5563;
  font-size: 26rpx;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rating-advice {
  color: #6b7280;
  font-size: 24rpx;
  line-height: 1.45;
}
.rating-advice.warning {
  color: #b45309;
}
.empty {
  color: #9ca3af;
  font-size: 26rpx;
  padding: 24rpx 32rpx;
}
</style>
