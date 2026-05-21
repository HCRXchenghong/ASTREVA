<template>
  <view class="page safe-page">
    <view class="header safe-header">
      <text class="title">AI 模型配置</text>
    </view>
    <scroll-view class="content flex-scroll" scroll-y>
      <view class="group">
        <view class="item">
          <text>智能接待总开关</text>
          <switch :checked="form.enabled" color="#07c160" @change="form.enabled = $event.detail.value" />
        </view>
        <view class="item">
          <text>接待模式</text>
          <picker :range="modeLabels" :value="modeIndex" @change="changeMode">
            <text class="value">{{ modeLabels[modeIndex] }}</text>
          </picker>
        </view>
      </view>

      <text class="section">大模型接口设置 (OpenAI 兼容)</text>
      <view class="group">
        <view class="item">
          <text>Base URL</text>
          <input v-model="form.base_url" class="input" />
        </view>
        <view class="item">
          <text>API Key</text>
          <input v-model="form.api_key" password class="input" :placeholder="apiKeyPlaceholder" />
        </view>
        <view class="item">
          <text>Model 名称</text>
          <input v-model="form.model" class="input" />
        </view>
        <view class="item">
          <text>协议类型</text>
          <picker :range="apiTypes" :value="apiTypeIndex" @change="changeAPIType">
            <text class="value">{{ apiTypes[apiTypeIndex] }}</text>
          </picker>
        </view>
        <view class="item">
          <text>Temperature</text>
          <input v-model.number="form.temperature" type="number" class="input" />
        </view>
        <view class="item">
          <text>超时时间(秒)</text>
          <input v-model.number="form.timeout_seconds" type="number" class="input" />
        </view>
        <view class="item">
          <text>最大输出 Tokens</text>
          <input v-model.number="form.max_output_tokens" type="number" class="input" />
        </view>
        <view class="item">
          <text>无人回复转AI(秒)</text>
          <input v-model.number="form.agent_no_reply_timeout_seconds" type="number" class="input" />
        </view>
      </view>

      <text class="section">System Prompt</text>
      <view class="prompt-box">
        <textarea v-model="form.system_prompt" class="prompt" />
      </view>

      <view class="actions">
        <button class="secondary" :loading="testing" @tap="test">测试接口联调</button>
        <button class="primary" @tap="save">保存配置</button>
        <view v-if="lastTest.message" :class="['test-result', lastTest.ok ? 'ok' : 'bad']">
          <text>{{ lastTest.message }}</text>
        </view>
      </view>
    </scroll-view>
    <admin-tab-bar active="ai" />
  </view>
</template>

<script>
import { fetchAISettings, updateAISettings, testAISettings } from '../../common/api.js'
import AdminTabBar from '../../components/AdminTabBar.vue'

const modes = ['human_first', 'always_ai', 'manual_only']
const modeLabels = ['人工优先', '一直 AI', '只人工']
const apiTypes = ['chat_completions', 'responses']

export default {
  components: { AdminTabBar },
  data() {
    return {
      modes,
      modeLabels,
      apiTypes,
      form: {
        enabled: true,
        mode: 'human_first',
        base_url: 'https://api.openai.com/v1',
        api_key: '',
        model: 'gpt-4o-mini',
        api_type: 'chat_completions',
        temperature: 0.7,
        timeout_seconds: 20,
        max_output_tokens: 512,
        system_prompt: '',
        agent_no_reply_timeout_seconds: 60,
        api_key_masked: ''
      },
      testing: false,
      lastTest: { ok: false, message: '' }
    }
  },
  computed: {
    modeIndex() {
      return Math.max(0, this.modes.indexOf(this.form.mode))
    },
    apiTypeIndex() {
      return Math.max(0, this.apiTypes.indexOf(this.form.api_type))
    },
    apiKeyPlaceholder() {
      return this.form.api_key_masked ? `已保存 ${this.form.api_key_masked}，不填则保持不变` : '请输入 API Key'
    }
  },
  onShow() {
    this.load()
  },
  methods: {
    async load() {
      const data = await fetchAISettings()
      this.form.enabled = typeof data.enabled === 'boolean' ? data.enabled : this.form.enabled
      this.form.mode = data.mode || this.form.mode
      this.form.base_url = data.base_url || this.form.base_url
      this.form.api_key = ''
      this.form.api_key_masked = data.api_key_masked || ''
      this.form.model = data.model || this.form.model
      this.form.api_type = data.api_type || this.form.api_type
      this.form.temperature = typeof data.temperature === 'number' ? data.temperature : this.form.temperature
      this.form.timeout_seconds = typeof data.timeout_seconds === 'number' ? data.timeout_seconds : this.form.timeout_seconds
      this.form.max_output_tokens = typeof data.max_output_tokens === 'number' ? data.max_output_tokens : this.form.max_output_tokens
      this.form.system_prompt = data.system_prompt || ''
      this.form.agent_no_reply_timeout_seconds = typeof data.agent_no_reply_timeout_seconds === 'number'
        ? data.agent_no_reply_timeout_seconds
        : (this.form.agent_no_reply_timeout_seconds || 60)
    },
    changeMode(e) {
      this.form.mode = this.modes[Number(e.detail.value)]
    },
    changeAPIType(e) {
      this.form.api_type = this.apiTypes[Number(e.detail.value)]
    },
    payload() {
      return {
        enabled: this.form.enabled,
        mode: this.form.mode,
        base_url: this.form.base_url,
        api_key: this.form.api_key,
        model: this.form.model,
        api_type: this.form.api_type,
        temperature: Number(this.form.temperature),
        timeout_seconds: Number(this.form.timeout_seconds),
        max_output_tokens: Number(this.form.max_output_tokens) || 512,
        system_prompt: this.form.system_prompt,
        agent_no_reply_timeout_seconds: Number(this.form.agent_no_reply_timeout_seconds) || 60
      }
    },
    validatePayload(requireAPIKey = false) {
      if (!String(this.form.base_url || '').trim()) return '请填写 Base URL'
      if (!String(this.form.model || '').trim()) return '请填写 Model 名称'
      if ((requireAPIKey || this.form.enabled) && !this.form.api_key && !this.form.api_key_masked) return '请填写 API Key'
      if (Number(this.form.timeout_seconds) <= 0) return '超时时间必须大于 0'
      return ''
    },
    async save() {
      const error = this.validatePayload(false)
      if (error) {
        uni.showToast({ title: error, icon: 'none' })
        return
      }
      try {
        await updateAISettings(this.payload())
        uni.showToast({ title: 'AI 配置已保存', icon: 'none' })
        await this.load()
      } catch (err) {
        uni.showToast({ title: err.message || '保存失败', icon: 'none' })
      }
    },
    async test() {
      const error = this.validatePayload(true)
      if (error) {
        uni.showToast({ title: error, icon: 'none' })
        return
      }
      this.testing = true
      this.lastTest = { ok: false, message: '' }
      try {
        uni.showLoading({ title: '联调中' })
        await updateAISettings(this.payload())
        const data = await testAISettings('请用一句话回复：AI 接口联调是否正常。')
        this.lastTest = { ok: true, message: `联调成功：${data.reply || 'AI 返回 200 OK'}` }
        uni.showModal({
          title: '联调成功',
          content: data.reply || 'AI 返回 200 OK',
          showCancel: false,
          confirmColor: '#576b95'
        })
        await this.load()
      } catch (err) {
        const message = err.message || 'AI 接口联调失败'
        this.lastTest = { ok: false, message: `联调失败：${message}` }
        uni.showModal({
          title: '联调失败',
          content: message,
          showCancel: false,
          confirmColor: '#ef4444'
        })
      } finally {
        this.testing = false
        uni.hideLoading()
      }
    }
  }
}
</script>

<style scoped>
.page {
  min-height: 100vh;
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
.value {
  color: #6b7280;
}
.input {
  flex: 1;
  text-align: right;
  margin-left: 24rpx;
}
.section {
  display: block;
  padding: 20rpx 32rpx 8rpx;
  color: #6b7280;
  font-size: 24rpx;
}
.prompt-box {
  padding: 24rpx;
  background: #fff;
}
.prompt {
  width: 100%;
  min-height: 220rpx;
  background: #f9fafb;
  border: 1px solid #e5e7eb;
  border-radius: 8rpx;
  padding: 16rpx;
  box-sizing: border-box;
}
.actions {
  padding: 32rpx;
}
.primary {
  background: #07c160;
  color: #fff;
}
.secondary {
  background: #fff;
  color: #111827;
  border: 1px solid #d1d5db;
  margin-bottom: 20rpx;
}
.test-result {
  margin-top: 20rpx;
  border-radius: 12rpx;
  padding: 20rpx 24rpx;
  font-size: 26rpx;
  line-height: 1.5;
}
.test-result.ok {
  background: #ecfdf5;
  color: #047857;
  border: 1px solid #a7f3d0;
}
.test-result.bad {
  background: #fef2f2;
  color: #dc2626;
  border: 1px solid #fecaca;
}
</style>
