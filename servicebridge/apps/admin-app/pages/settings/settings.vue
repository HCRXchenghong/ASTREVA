<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <text class="title">系统配置</text>
    </view>
    <scroll-view class="content flex-scroll" scroll-y>
      <view class="group">
        <view class="item arrow-item" @tap="openKeywords">
          <text>关键词自动回复规则</text>
          <view class="right">
            <text class="badge">{{ keywordRules.length }}条</text>
            <text class="arrow">›</text>
          </view>
        </view>
        <view class="item arrow-item" @tap="openAudit">
          <text>管理操作审计</text>
          <view class="right">
            <text class="summary">{{ auditEvents.length }}条</text>
            <text class="arrow">›</text>
          </view>
        </view>
        <view class="item arrow-item" @tap="openPassword">
          <text>修改登录密码</text>
          <view class="right">
            <text class="arrow">›</text>
          </view>
        </view>
      </view>

      <text class="section">对外展示信息配置</text>
      <view class="group">
        <view class="item">
          <text>官方联系电话</text>
          <input v-model="contacts.phone" class="input" />
        </view>
        <view class="item arrow-item" @tap="openContact('wechat')">
          <text>官方微信配置</text>
          <view class="right">
            <text class="summary">{{ contactSummary('wechat') }}</text>
            <text class="arrow">›</text>
          </view>
        </view>
        <view class="item arrow-item" @tap="openContact('qq')">
          <text>官方QQ配置</text>
          <view class="right">
            <text class="summary">{{ contactSummary('qq') }}</text>
            <text class="arrow">›</text>
          </view>
        </view>
      </view>

      <text class="section">访客入口文案配置</text>
      <view class="group">
        <view class="textarea-block">
          <text class="textarea-label">进入会话后的首条回复</text>
          <textarea
            v-model="contacts.entry_reply"
            class="textarea"
            maxlength="1000"
            auto-height
            placeholder="留空则不自动发送首条回复"
          />
        </view>
      </view>

      <text class="section">人工营业时间</text>
      <view class="group">
        <view class="item">
          <text>启用营业时间</text>
          <switch :checked="business.enabled" color="#07c160" @change="business.enabled = $event.detail.value" />
        </view>
        <view class="item">
          <text>时区</text>
          <input v-model="business.timezone" class="input" />
        </view>
        <view class="item">
          <text>开始时间</text>
          <input v-model="business.start" class="input" />
        </view>
        <view class="item">
          <text>结束时间</text>
          <input v-model="business.end" class="input" />
        </view>
      </view>

      <view class="actions">
        <button class="primary" @tap="save">保存配置</button>
        <button class="logout" @tap="logout">退出管理账号</button>
      </view>
    </scroll-view>

      <view v-if="keywordVisible" class="keyword-page safe-page">
      <view class="keyword-header safe-header">
        <text class="back" @tap="keywordVisible = false">‹</text>
        <text class="keyword-title">关键词自动回复</text>
        <text class="add" @tap="addRule">＋</text>
      </view>
      <scroll-view class="keyword-content flex-scroll" scroll-y>
        <view class="rule-card" v-for="(rule, index) in keywordRules" :key="rule.id">
          <view class="rule-card-header">
            <view class="rule-card-main">
              <text class="rule-title">规则 {{ index + 1 }}</text>
              <text class="rule-id">{{ rule.id }}</text>
            </view>
            <switch :checked="rule.enabled" color="#07c160" @change="rule.enabled = $event.detail.value" />
          </view>

          <view class="rule-field">
            <text class="field-label">触发关键词</text>
            <input v-model="rule.keyword" class="input left-input" placeholder="例如：退款、发货、人工客服" />
          </view>

          <view class="rule-field arrow-item" @tap="selectMatchType(rule)">
            <text class="field-label">匹配方式</text>
            <view class="right">
              <text class="summary">{{ matchTypeText(rule.match_type) }}</text>
              <text class="arrow">›</text>
            </view>
          </view>

          <view class="rule-field arrow-item" @tap="selectRuleAction(rule)">
            <text class="field-label">回复动作</text>
            <view class="right">
              <text :class="['rule-action', rule.action]">{{ actionText(rule.action) }}</text>
              <text class="arrow">›</text>
            </view>
          </view>

          <view class="rule-field">
            <text class="field-label">优先级</text>
            <input v-model.number="rule.priority" class="input left-input short-input" type="number" />
          </view>

          <view class="rule-field">
            <text class="field-label">展示到猜你想问</text>
            <switch :checked="rule.show_in_quick_replies" color="#07c160" @change="rule.show_in_quick_replies = $event.detail.value" />
          </view>

          <view v-if="rule.show_in_quick_replies" class="rule-field">
            <text class="field-label">快捷问题文案</text>
            <input v-model="rule.quick_reply_text" class="input left-input" placeholder="留空则直接显示触发关键词" />
          </view>

          <view class="rule-textarea-block">
            <text class="textarea-label">自动回复内容</text>
            <textarea
              v-model="rule.reply"
              class="textarea"
              maxlength="1000"
              auto-height
              placeholder="命中关键词后发给用户的内容"
            />
          </view>

          <button class="rule-save" @tap="saveRule(rule)">保存规则</button>
        </view>
        <view class="scroll-bottom-spacer"></view>
      </scroll-view>
    </view>

    <view v-if="contactVisible" class="keyword-page safe-page">
      <view class="keyword-header safe-header">
        <text class="back" @tap="contactVisible = false">‹</text>
        <text class="keyword-title">{{ contactEdit.type === 'wechat' ? '官方微信配置' : '官方QQ配置' }}</text>
        <text class="add"></text>
      </view>
      <scroll-view class="keyword-content flex-scroll" scroll-y>
        <text class="section">账号信息</text>
        <view class="group">
          <view class="item">
            <text>{{ contactEdit.type === 'wechat' ? '微信号' : 'QQ号' }}</text>
            <input v-model="contactEdit.account" class="input blue-input" />
          </view>
        </view>

        <text class="section">当用户触发此联系方式时的回复格式</text>
        <view class="group">
          <view class="item arrow-item" @tap="selectReplyType">
            <text>下发格式</text>
            <view class="right">
              <text class="summary">{{ contactEdit.replyType }}</text>
              <text class="arrow">›</text>
            </view>
          </view>
          <view v-if="isImageReply(contactEdit.replyType)" class="qr-row">
            <text class="qr-title">上传二维码/照片</text>
            <view class="qr-main">
              <view class="qr-box" @tap="chooseQR">
                <image v-if="contactEdit.qrPreview" :src="contactEdit.qrPreview" class="qr-img" mode="aspectFill" />
                <text v-else class="qr-plus">＋</text>
              </view>
              <text class="qr-help">从设备相册选择二维码或图片。系统将按客服发送真实照片的形式下发。</text>
            </view>
          </view>
        </view>
        <text class="hint">选择图片回复时，系统将自动下发二维码图片；选择文字回复则直接发送纯文本账号。</text>
        <view class="actions">
          <button class="primary" @tap="saveContact">保存配置</button>
        </view>
        <view class="scroll-bottom-spacer"></view>
      </scroll-view>
    </view>

    <view v-if="auditVisible" class="keyword-page safe-page">
      <view class="keyword-header safe-header">
        <text class="back" @tap="auditVisible = false">‹</text>
        <text class="keyword-title">管理操作审计</text>
        <view class="audit-actions">
          <text class="audit-action" @tap="loadAudit">↻</text>
          <text class="audit-action text-action" @tap="exportAudit">导出</text>
        </view>
      </view>
      <scroll-view class="keyword-content flex-scroll" scroll-y>
        <view v-if="!auditEvents.length" class="empty">暂无审计记录</view>
        <view class="audit-row" v-for="event in auditEvents" :key="event.id">
          <view class="audit-main">
            <text class="rule-title">{{ auditActionText(event.action) }}</text>
            <text class="rule-reply">{{ event.description || event.resource }}</text>
          </view>
          <view class="audit-side">
            <text class="audit-time">{{ timeText(event.created_at) }}</text>
            <text class="audit-actor">{{ event.actor_id || '-' }}</text>
          </view>
        </view>
        <view class="scroll-bottom-spacer"></view>
      </scroll-view>
    </view>

    <view v-if="passwordVisible" class="keyword-page safe-page">
      <view class="keyword-header safe-header">
        <text class="back" @tap="passwordVisible = false">‹</text>
        <text class="keyword-title">修改登录密码</text>
        <text class="add"></text>
      </view>
      <view class="group">
        <view class="item">
          <text>当前密码</text>
          <input v-model="passwordForm.current" class="input" password />
        </view>
        <view class="item">
          <text>新密码</text>
          <input v-model="passwordForm.next" class="input" password />
        </view>
        <view class="item">
          <text>确认新密码</text>
          <input v-model="passwordForm.confirm" class="input" password />
        </view>
      </view>
      <view class="actions">
        <button class="primary" @tap="submitPassword">保存新密码</button>
      </view>
    </view>

    <admin-tab-bar active="settings" />
  </view>
</template>

<script>
import { clearAuth, request, fetchBusinessHours, updateBusinessHours, updateContactSettings, fetchKeywordRules, updateKeywordRule, createKeywordRule, uploadFile, fetchAuditEvents, exportAuditEventsCSV, changePassword } from '../../common/api.js'
import { disconnectRealtime } from '../../common/realtime.js'
import AdminTabBar from '../../components/AdminTabBar.vue'

export default {
  components: { AdminTabBar },
  data() {
    return {
      contacts: { phone: '', wechat: '', qq: '', entry_reply: '' },
      contactMeta: {
        wechat: { replyType: '图片回复 (二维码)', qrPreview: '' },
        qq: { replyType: '文字回复 (仅发账号)', qrPreview: '' }
      },
      contactVisible: false,
      contactEdit: { type: 'wechat', account: '', replyType: '图片回复 (二维码)', qrPreview: '' },
      business: { enabled: true, timezone: 'Asia/Shanghai', start: '09:00', end: '18:00' },
      keywordRules: [],
      keywordVisible: false,
      auditVisible: false,
      auditEvents: [],
      passwordVisible: false,
      passwordForm: { current: '', next: '', confirm: '' }
    }
  },
  onShow() {
    this.load()
  },
  methods: {
    normalizeRule(rule = {}) {
      return {
        id: rule.id || '',
        keyword: rule.keyword || '',
        match_type: rule.match_type || 'contains',
        reply: rule.reply || '',
        enabled: rule.enabled !== false,
        priority: Number.isFinite(Number(rule.priority)) ? Number(rule.priority) : 0,
        action: rule.action || 'text',
        show_in_quick_replies: !!rule.show_in_quick_replies,
        quick_reply_text: rule.quick_reply_text || ''
      }
    },
    async load() {
      this.contacts = await request('/api/contact-settings')
      this.contacts.entry_reply = this.contacts.entry_reply || ''
      this.contactMeta.wechat = {
        replyType: this.contacts.wechat_reply_type === 'image' ? '图片回复 (二维码)' : '文字回复 (仅发账号)',
        qrPreview: this.contacts.wechat_image_url || ''
      }
      this.contactMeta.qq = {
        replyType: this.contacts.qq_reply_type === 'image' ? '图片回复 (二维码)' : '文字回复 (仅发账号)',
        qrPreview: this.contacts.qq_image_url || ''
      }
      this.business = await fetchBusinessHours()
      const rules = await fetchKeywordRules()
      this.keywordRules = (rules.rules || []).map((rule) => this.normalizeRule(rule))
      await this.loadAudit()
    },
    async save() {
      await updateContactSettings(this.contacts)
      await updateBusinessHours(this.business)
      uni.showToast({ title: '配置已保存', icon: 'none' })
    },
    logout() {
      uni.showModal({
        title: '系统提示',
        content: '确定要退出管理账号吗？',
        confirmColor: '#576b95',
        success: (res) => {
          if (!res.confirm) return
          disconnectRealtime()
          clearAuth()
          uni.redirectTo({ url: '/pages/login/login' })
        }
      })
    },
    openKeywords() {
      this.keywordVisible = true
    },
    async openAudit() {
      await this.loadAudit()
      this.auditVisible = true
    },
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
    async loadAudit() {
      const data = await fetchAuditEvents(50)
      this.auditEvents = data.events || []
    },
    async exportAudit() {
      try {
        uni.showLoading({ title: '导出中' })
        await exportAuditEventsCSV(500)
        uni.showToast({ title: '审计CSV已导出', icon: 'none' })
      } catch (err) {
        uni.showToast({ title: err.message || '导出失败', icon: 'none' })
      } finally {
        uni.hideLoading()
      }
    },
    contactSummary(type) {
      const account = type === 'wechat' ? this.contacts.wechat : this.contacts.qq
      const meta = this.contactMeta[type]
      return `${account || '-'} ${this.isImageReply(meta.replyType) ? '(图文)' : '(纯文本)'}`
    },
    openContact(type) {
      this.contactEdit = {
        type,
        account: type === 'wechat' ? this.contacts.wechat : this.contacts.qq,
        replyType: this.contactMeta[type].replyType,
        qrPreview: this.contactMeta[type].qrPreview || ''
      }
      this.contactVisible = true
    },
    selectReplyType() {
      uni.showActionSheet({
        itemList: ['文字回复 (仅发账号)', '图片回复 (二维码)'],
        success: (res) => {
          this.contactEdit.replyType = res.tapIndex === 0 ? '文字回复 (仅发账号)' : '图片回复 (二维码)'
        }
      })
    },
    chooseQR() {
      uni.chooseImage({
        count: 1,
        success: async (res) => {
          const localPath = res.tempFilePaths[0]
          this.contactEdit.qrPreview = localPath
          try {
            uni.showLoading({ title: '上传中' })
            const uploaded = await uploadFile(localPath)
            this.contactEdit.qrPreview = uploaded.url
            uni.showToast({ title: '图片上传成功', icon: 'none' })
          } catch (err) {
            uni.showToast({ title: err.message || '上传失败', icon: 'none' })
          } finally {
            uni.hideLoading()
          }
        }
      })
    },
    async saveContact() {
      if (this.isImageReply(this.contactEdit.replyType) && !this.contactEdit.qrPreview) {
        uni.showToast({ title: '请先上传二维码图片', icon: 'none' })
        return
      }
      if (this.contactEdit.type === 'wechat') {
        this.contacts.wechat = this.contactEdit.account
      } else {
        this.contacts.qq = this.contactEdit.account
      }
      this.contactMeta[this.contactEdit.type] = {
        replyType: this.contactEdit.replyType,
        qrPreview: this.contactEdit.qrPreview
      }
      this.contacts.wechat_reply_type = this.isImageReply(this.contactMeta.wechat.replyType) ? 'image' : 'text'
      this.contacts.wechat_image_url = this.contactMeta.wechat.qrPreview || ''
      this.contacts.qq_reply_type = this.isImageReply(this.contactMeta.qq.replyType) ? 'image' : 'text'
      this.contacts.qq_image_url = this.contactMeta.qq.qrPreview || ''
      await updateContactSettings(this.contacts)
      this.contactVisible = false
      uni.showToast({ title: '配置已保存', icon: 'none' })
    },
    isImageReply(replyType) {
      return String(replyType || '').indexOf('图片') >= 0
    },
    async addRule() {
      const data = await createKeywordRule({
        keyword: '新规则关键词',
        reply: '请在这里填写自动回复内容。',
        enabled: true,
        priority: 50,
        action: 'text',
        match_type: 'contains',
        show_in_quick_replies: false,
        quick_reply_text: ''
      })
      this.keywordRules.unshift(this.normalizeRule(data.rule))
      uni.showToast({ title: '规则已新增', icon: 'none' })
    },
    async saveRule(rule) {
      try {
        const payload = this.normalizeRule(rule)
        const data = await updateKeywordRule(rule.id, payload)
        const idx = this.keywordRules.findIndex((item) => item.id === rule.id)
        if (idx >= 0) this.keywordRules.splice(idx, 1, this.normalizeRule(data.rule))
        uni.showToast({ title: '规则已保存', icon: 'none' })
      } catch (err) {
        uni.showToast({ title: err.message || '保存失败', icon: 'none' })
      }
    },
    selectMatchType(rule) {
      uni.showActionSheet({
        itemList: ['包含匹配', '完全匹配'],
        success: (res) => {
          rule.match_type = res.tapIndex === 1 ? 'exact' : 'contains'
        }
      })
    },
    selectRuleAction(rule) {
      const values = ['text', 'phone', 'wechat', 'handoff']
      uni.showActionSheet({
        itemList: ['文本回复', '电话联系方式', '微信联系方式', '转人工'],
        success: (res) => {
          rule.action = values[res.tapIndex] || 'text'
        }
      })
    },
    actionText(action) {
      const map = { phone: '电话', wechat: '微信', handoff: '转人工', text: '文本' }
      return map[action] || '文本'
    },
    matchTypeText(matchType) {
      return matchType === 'exact' ? '完全匹配' : '包含匹配'
    },
    auditActionText(action) {
      const map = {
        'agent.create': '创建客服',
        'agent.update': '更新客服',
        'agent.reset_password': '重置密码',
        'agent.disable': '禁用客服',
        'agent.delete': '删除客服',
        'contact_settings.update': '更新联系方式',
        'keyword_rule.create': '新增关键词',
        'keyword_rule.update': '更新关键词',
        'conversation.remark_update': '修改备注',
        'conversation.close': '关闭会话',
        'conversation.delete': '删除会话',
        'conversation.transfer': '转接会话',
        'ai_settings.update': '更新AI配置',
        'business_hours.update': '更新营业时间'
      }
      return map[action] || action
    },
    timeText(value) {
      return value ? String(value).replace('T', ' ').slice(5, 16) : ''
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
  display: flex;
  align-items: center;
  justify-content: center;
  border-bottom: 1px solid #d5d5d5;
}
.title {
  font-size: 34rpx;
  font-weight: 600;
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
.item {
  min-height: 96rpx;
  padding: 0 32rpx;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #f3f4f6;
}
.right {
  display: flex;
  align-items: center;
  gap: 12rpx;
  min-width: 0;
}
.badge {
  background: #ef4444;
  color: #fff;
  font-size: 20rpx;
  border-radius: 999rpx;
  padding: 4rpx 12rpx;
}
.arrow {
  color: #9ca3af;
  font-size: 40rpx;
}
.input {
  flex: 1;
  text-align: right;
  margin-left: 24rpx;
}
.left-input {
  text-align: left;
}
.short-input {
  max-width: 220rpx;
}
.blue-input {
  color: #576b95;
}
.summary {
  max-width: 360rpx;
  color: #6b7280;
  font-size: 26rpx;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.section {
  display: block;
  padding: 20rpx 32rpx 8rpx;
  color: #6b7280;
  font-size: 24rpx;
}
.actions {
  padding: 32rpx;
}
.primary {
  background: #07c160;
  color: #fff;
}
.logout {
  margin-top: 24rpx;
  background: #fff;
  color: #ef4444;
  border: 1px solid #e5e7eb;
}
.keyword-page {
  position: fixed;
  inset: 0;
  background: #ededed;
  z-index: 40;
  display: flex;
  flex-direction: column;
}
.keyword-header {
  height: 100rpx;
  background: #ededed;
  border-bottom: 1px solid #d5d5d5;
  display: flex;
  align-items: center;
  padding: 0 24rpx;
  box-sizing: border-box;
}
.back, .add {
  width: 96rpx;
  color: #374151;
  font-size: 48rpx;
}
.add {
  text-align: right;
  color: #07c160;
}
.audit-actions {
  width: 136rpx;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 16rpx;
}
.audit-action {
  color: #07c160;
  font-size: 40rpx;
}
.text-action {
  color: #576b95;
  font-size: 26rpx;
}
.keyword-title {
  flex: 1;
  text-align: center;
  font-size: 34rpx;
  font-weight: 600;
  color: #111827;
}
.keyword-content {
  flex: 1;
  min-height: 0;
  height: auto;
  box-sizing: border-box;
}
.scroll-bottom-spacer {
  height: calc(160rpx + env(safe-area-inset-bottom));
}
.textarea-block,
.rule-textarea-block {
  padding: 28rpx 32rpx;
  background: #fff;
}
.textarea-label {
  display: block;
  color: #111827;
  font-size: 28rpx;
  margin-bottom: 16rpx;
}
.textarea {
  width: 100%;
  min-height: 160rpx;
  background: #f9fafb;
  border: 1px solid #e5e7eb;
  border-radius: 16rpx;
  padding: 20rpx 24rpx;
  box-sizing: border-box;
  color: #374151;
  font-size: 28rpx;
  line-height: 1.6;
}
.rule-card {
  background: #fff;
  margin: 16rpx 24rpx 0;
  border-radius: 16rpx;
  overflow: hidden;
  box-shadow: 0 6rpx 24rpx rgba(15, 23, 42, 0.05);
}
.rule-card-header {
  min-height: 104rpx;
  padding: 0 32rpx;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #f3f4f6;
}
.rule-card-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.rule-id {
  color: #9ca3af;
  font-size: 22rpx;
  margin-top: 6rpx;
}
.rule-field {
  background: #fff;
  min-height: 96rpx;
  padding: 0 32rpx;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-sizing: border-box;
  border-bottom: 1px solid #f3f4f6;
}
.audit-row {
  min-height: 112rpx;
  background: #fff;
  border-bottom: 1px solid #f3f4f6;
  padding: 22rpx 32rpx;
  display: flex;
  align-items: center;
  box-sizing: border-box;
}
.audit-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.audit-side {
  margin-left: 20rpx;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8rpx;
}
.audit-time {
  color: #9ca3af;
  font-size: 22rpx;
}
.audit-actor {
  color: #576b95;
  font-size: 22rpx;
}
.empty {
  color: #9ca3af;
  font-size: 28rpx;
  text-align: center;
  padding: 48rpx 0;
}
.rule-main {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.rule-title {
  font-size: 32rpx;
  color: #111827;
  font-weight: 500;
}
.field-label {
  color: #111827;
  font-size: 28rpx;
  flex-shrink: 0;
}
.rule-reply {
  font-size: 26rpx;
  color: #6b7280;
  margin-top: 8rpx;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rule-side {
  margin-left: 20rpx;
  display: flex;
  align-items: center;
  gap: 16rpx;
}
.rule-action {
  font-size: 22rpx;
  color: #576b95;
  background: #eef2ff;
  border: 1px solid #dbeafe;
  border-radius: 4rpx;
  padding: 2rpx 8rpx;
}
.rule-action.phone { color: #15803d; background: #dcfce7; border-color: #bbf7d0; }
.rule-action.wechat { color: #2563eb; background: #dbeafe; border-color: #bfdbfe; }
.rule-action.handoff { color: #c2410c; background: #ffedd5; border-color: #fdba74; }
.rule-save {
  margin: 24rpx 32rpx 32rpx;
  background: #07c160;
  color: #fff;
  font-size: 28rpx;
}
.qr-row {
  background: #fff;
  padding: 28rpx 32rpx;
}
.qr-title {
  color: #111827;
  font-size: 32rpx;
}
.qr-main {
  margin-top: 20rpx;
  display: flex;
  align-items: flex-end;
  gap: 24rpx;
}
.qr-box {
  width: 168rpx;
  height: 168rpx;
  border: 2rpx dashed #d1d5db;
  border-radius: 16rpx;
  background: #f9fafb;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.qr-img {
  width: 100%;
  height: 100%;
}
.qr-plus {
  color: #9ca3af;
  font-size: 56rpx;
}
.qr-help, .hint {
  flex: 1;
  color: #9ca3af;
  font-size: 22rpx;
  line-height: 1.5;
}
.hint {
  display: block;
  padding: 20rpx 32rpx 0;
}
</style>
