<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <view class="space"></view>
      <text class="title">客服管理</text>
      <text class="add" @tap="createAgent">＋</text>
    </view>

    <scroll-view class="content flex-scroll" scroll-y>
      <view class="group">
        <view v-for="agent in agents" :key="agent.id" class="agent-row" @tap="openDetail(agent)">
          <view class="avatar-wrap">
            <view :class="['avatar', agent.status === 'offline' ? 'offline' : '']">{{ shortName(agent.name) }}</view>
            <view :class="['status-dot', agent.status]"></view>
          </view>
          <view class="main">
            <text class="name">{{ agent.name }} ({{ agent.account }})</text>
            <text class="desc">{{ agent.group }} | {{ statusLine(agent) }}</text>
          </view>
          <text class="arrow">›</text>
        </view>
      </view>
    </scroll-view>

    <view v-if="detailVisible" class="detail safe-page">
      <view class="detail-header safe-header">
        <text class="back" @tap="detailVisible = false">‹</text>
        <text class="detail-title">客服资料</text>
        <view class="space"></view>
      </view>
      <scroll-view class="detail-content flex-scroll" scroll-y>
        <view class="detail-card">
          <view class="big-avatar">{{ currentShortName }}</view>
          <text class="detail-name">{{ currentName }}</text>
          <text class="detail-account">账号: {{ currentAccount }}</text>
        </view>
        <view class="edit-group">
          <view class="edit-item">
            <text>所属分组</text>
            <input v-model="form.group" class="input" />
          </view>
          <view class="edit-item">
            <text>当前接待上限</text>
            <input v-model.number="form.max_conversations" type="number" class="input" />
          </view>
        </view>
        <view class="actions">
          <button class="secondary" @tap="saveCurrent">保存修改</button>
          <button class="outline" @tap="confirmReset">生成临时密码</button>
          <button class="danger" @tap="confirmDisable">禁用此账号</button>
          <button class="delete" @tap="confirmDelete">删除此账号</button>
        </view>
      </scroll-view>
    </view>

    <view v-if="createVisible" class="detail safe-page">
      <view class="detail-header safe-header">
        <text class="back" @tap="createVisible = false">‹</text>
        <text class="detail-title">新增客服</text>
        <view class="space"></view>
      </view>
      <view class="edit-group create-form">
        <view class="edit-item">
          <text>登录账号</text>
          <input v-model="createForm.account" class="input" />
        </view>
        <view class="edit-item">
          <text>初始密码</text>
          <input v-model="createForm.password" class="input" password />
        </view>
        <view class="edit-item">
          <text>客服姓名</text>
          <input v-model="createForm.name" class="input" />
        </view>
        <view class="edit-item">
          <text>所属分组</text>
          <input v-model="createForm.group" class="input" />
        </view>
        <view class="edit-item">
          <text>接待上限</text>
          <input v-model.number="createForm.max_conversations" type="number" class="input" />
        </view>
      </view>
      <view class="actions">
        <button class="secondary" @tap="submitCreate">保存新增客服</button>
      </view>
    </view>

    <admin-tab-bar active="agents" />
  </view>
</template>

<script>
import { createAgent, deleteAgent, disableAgent, fetchAgents, resetAgentPassword, updateAgent } from '../../common/api.js'
import AdminTabBar from '../../components/AdminTabBar.vue'

export default {
  components: { AdminTabBar },
  data() {
    return {
      agents: [],
      detailVisible: false,
      createVisible: false,
      current: null,
      form: {
        group: '',
        max_conversations: 10
      },
      createForm: {
        account: '',
        password: '',
        name: '',
        group: '售前组',
        max_conversations: 10
      }
    }
  },
  onShow() {
    this.load()
  },
  computed: {
    currentName() {
      return (this.current && this.current.name) || ''
    },
    currentAccount() {
      return (this.current && this.current.account) || ''
    },
    currentShortName() {
      return this.shortName(this.currentName)
    }
  },
  methods: {
    async load() {
      try {
        const data = await fetchAgents()
        this.agents = data.agents || []
      } catch (err) {
        uni.showToast({ title: err.message || '加载失败', icon: 'none' })
      }
    },
    openDetail(agent) {
      this.current = agent
      this.form = {
        group: agent.group,
        max_conversations: agent.max_conversations
      }
      this.detailVisible = true
    },
    createAgent() {
      const suffix = Date.now().toString().slice(-4)
      this.createForm = {
        account: `kf_${suffix}`,
        password: this.generateTempPassword(),
        name: `客服${suffix}`,
        group: '售前组',
        max_conversations: 10
      }
      this.createVisible = true
    },
    async submitCreate() {
      const initialPassword = this.createForm.password
      const data = await createAgent(this.createForm)
      this.agents.unshift(data.agent)
      this.createVisible = false
      uni.showModal({
        title: '客服已新增',
        content: `初始临时密码：${initialPassword}`,
        showCancel: false,
        confirmColor: '#576b95'
      })
    },
    async saveCurrent() {
      if (!this.current) return
      const data = await updateAgent(this.current.id, {
        name: this.current.name,
        group: this.form.group,
        max_conversations: Number(this.form.max_conversations) || 10
      })
      const idx = this.agents.findIndex((item) => item.id === this.current.id)
      if (idx >= 0) this.agents.splice(idx, 1, data.agent)
      this.current = data.agent
      uni.showToast({ title: '资料已保存', icon: 'none' })
    },
    confirmReset() {
      uni.showModal({
        title: '系统提示',
        content: '确定为此客服生成新的临时密码吗？',
        confirmColor: '#576b95',
        success: async (res) => {
          if (!res.confirm || !this.current) return
          const data = await resetAgentPassword(this.current.id)
          const password = data.temporary_password || '请在后台日志确认'
          uni.showModal({
            title: '临时密码已生成',
            content: `请立即告知客服并要求登录后修改：${password}`,
            showCancel: false,
            confirmColor: '#576b95'
          })
        }
      })
    },
    confirmDisable() {
      uni.showModal({
        title: '系统提示',
        content: '确定禁用该客服账号吗？',
        confirmColor: '#576b95',
        success: async (res) => {
          if (!res.confirm || !this.current) return
          await disableAgent(this.current.id)
          this.detailVisible = false
          await this.load()
          this.toast('账号已禁用')
        }
      })
    },
    confirmDelete() {
      uni.showModal({
        title: '删除客服账号',
        content: '确定删除该客服账号吗？删除后账号无法登录，历史会话会保留但不再关联此账号。',
        confirmColor: '#ef4444',
        success: async (res) => {
          if (!res.confirm || !this.current) return
          await deleteAgent(this.current.id)
          this.detailVisible = false
          this.current = null
          await this.load()
          this.toast('账号已删除')
        }
      })
    },
    toast(title) {
      uni.showToast({ title, icon: 'none' })
    },
    shortName(name) {
      return String(name || '客').slice(-1)
    },
    statusLine(agent) {
      if (agent.disabled_at) return '已禁用'
      if (agent.status === 'online') return `正在接待 ${agent.current_conversations} 人`
      if (agent.status === 'busy') return '忙碌'
      return '离线'
    },
    generateTempPassword() {
      return `Tmp-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
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
  justify-content: space-between;
  padding: 0 32rpx;
  box-sizing: border-box;
}
.title, .detail-title {
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.space {
  width: 80rpx;
}
.add {
  width: 80rpx;
  text-align: right;
  color: #07c160;
  font-size: 42rpx;
}
.content {
  height: calc(100vh - 220rpx);
  padding-bottom: 120rpx;
  box-sizing: border-box;
}
.group {
  background: #fff;
  margin-top: 16rpx;
  border-top: 1px solid #e5e7eb;
  border-bottom: 1px solid #e5e7eb;
}
.agent-row {
  min-height: 128rpx;
  padding: 24rpx 32rpx;
  box-sizing: border-box;
  border-bottom: 1px solid #f3f4f6;
  display: flex;
  align-items: center;
}
.avatar-wrap {
  width: 96rpx;
  height: 96rpx;
  position: relative;
  flex-shrink: 0;
}
.avatar {
  width: 96rpx;
  height: 96rpx;
  border-radius: 8rpx;
  background: #10b981;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 34rpx;
}
.avatar.offline {
  background: #d1d5db;
  color: #fff;
}
.status-dot {
  position: absolute;
  right: -2rpx;
  bottom: -2rpx;
  width: 26rpx;
  height: 26rpx;
  border-radius: 50%;
  border: 4rpx solid #fff;
  background: #9ca3af;
}
.status-dot.online { background: #22c55e; }
.status-dot.busy { background: #ef4444; }
.main {
  flex: 1;
  margin-left: 24rpx;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.name {
  color: #111827;
  font-size: 32rpx;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.desc {
  color: #6b7280;
  font-size: 26rpx;
  margin-top: 8rpx;
}
.arrow {
  color: #9ca3af;
  font-size: 44rpx;
}
.detail {
  position: fixed;
  inset: 0;
  z-index: 40;
  background: #ededed;
  display: flex;
  flex-direction: column;
}
.detail-header {
  height: 100rpx;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  padding: 0 24rpx;
  box-sizing: border-box;
}
.back {
  width: 80rpx;
  color: #374151;
  font-size: 48rpx;
}
.detail-title {
  flex: 1;
  text-align: center;
}
.detail-content {
  flex: 1;
}
.detail-card {
  background: #fff;
  padding: 48rpx 32rpx;
  display: flex;
  flex-direction: column;
  align-items: center;
  border-bottom: 1px solid #e5e7eb;
  margin-bottom: 16rpx;
}
.big-avatar {
  width: 160rpx;
  height: 160rpx;
  border-radius: 16rpx;
  background: #10b981;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 52rpx;
  margin-bottom: 24rpx;
}
.detail-name {
  color: #1f2937;
  font-size: 40rpx;
  font-weight: 700;
}
.detail-account {
  color: #6b7280;
  font-size: 26rpx;
  margin-top: 8rpx;
}
.edit-group {
  background: #fff;
  border-top: 1px solid #e5e7eb;
  border-bottom: 1px solid #e5e7eb;
  margin-bottom: 32rpx;
}
.edit-item {
  min-height: 104rpx;
  padding: 0 32rpx;
  border-bottom: 1px solid #f3f4f6;
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: #1f2937;
  font-size: 32rpx;
}
.input {
  flex: 1;
  text-align: right;
  margin-left: 24rpx;
}
.actions {
  padding: 0 32rpx 48rpx;
}
.create-form {
  margin-top: 16rpx;
}
.secondary, .outline, .danger, .delete {
  margin-bottom: 24rpx;
  border-radius: 8rpx;
  font-size: 30rpx;
}
.secondary {
  background: #fff;
  color: #111827;
  border: 1px solid #d1d5db;
}
.outline {
  background: #fff;
  color: #576b95;
  border: 1px solid #576b95;
}
.danger {
  background: #ef4444;
  color: #fff;
}
.delete {
  background: #fff;
  color: #dc2626;
  border: 1px solid #fecaca;
}
</style>
