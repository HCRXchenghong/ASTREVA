<template>
  <div class="policy-page">
    <a-alert class="tips" type="info">
      <template #title>客服策略</template>
      这里的策略对全部客服与访客会话生效；保存后立即下发到客服引擎，无需重启。数字项 0 表示「不限制 / 不启用该判定」。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" data-testid="policy-retry" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-alert v-if="saveError" class="banner" type="error" show-icon>
      {{ saveError }}
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-row :gutter="16">
        <a-col :xs="24" :md="12">
          <a-card :bordered="false" title="排队与技能组" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="启用排队">
                <a-switch
                  v-model="form.queue_enabled"
                  data-testid="policy-queue_enabled"
                />
                <template #extra>
                  开启后访客会话先进入排队，按技能组与优先级分配客服；关闭则直接按现有逻辑分配。
                </template>
              </a-form-item>
              <a-form-item label="最长等待时间（秒）">
                <a-input-number
                  v-model="form.max_wait_seconds"
                  :min="0"
                  :precision="0"
                  :input-attrs="{ 'data-testid': 'policy-max_wait_seconds' }"
                  style="width: 200px"
                />
                <template #extra>
                  排队超过该秒数仍未接入客服时，超时转无人值守兜底回复。0 表示不限时。
                </template>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>

        <a-col :xs="24" :md="12">
          <a-card :bordered="false" title="响应时限（SLA）" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="首次回复时限（秒）">
                <a-input-number
                  v-model="form.sla_first_response_seconds"
                  :min="0"
                  :precision="0"
                  :input-attrs="{
                    'data-testid': 'policy-sla_first_response_seconds',
                  }"
                  style="width: 200px"
                />
                <template #extra>
                  客服接入会话后，超过该秒数仍未首次回复即记为超时，用于统计与提醒。0 表示不考核。
                </template>
              </a-form-item>
              <a-form-item label="客服空闲判定（分钟）">
                <a-input-number
                  v-model="form.sla_idle_minutes"
                  :min="0"
                  :precision="0"
                  :input-attrs="{ 'data-testid': 'policy-sla_idle_minutes' }"
                  style="width: 200px"
                />
                <template #extra>
                  客服连续多少分钟未回复访客即视为空闲，随后按下面的动作处理。0 表示不判定。
                </template>
              </a-form-item>
              <a-form-item label="空闲处理动作">
                <a-radio-group
                  v-model="form.sla_idle_action"
                  type="button"
                  data-testid="policy-sla_idle_action"
                >
                  <a-radio value="ai">交回 AI</a-radio>
                  <a-radio value="reassign">转其他客服</a-radio>
                </a-radio-group>
                <template #extra>
                  交回 AI：结束人工接待并由 AI 继续应答；转其他客服：重新排队分配给空闲客服。
                </template>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>

        <a-col :xs="24" :md="12">
          <a-card :bordered="false" title="会话" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="静默自动结束（分钟）">
                <a-input-number
                  v-model="form.auto_close_idle_minutes"
                  :min="0"
                  :precision="0"
                  :input-attrs="{
                    'data-testid': 'policy-auto_close_idle_minutes',
                  }"
                  style="width: 200px"
                />
                <template #extra>
                  访客与客服双方静默达到该分钟数后自动结束会话。0 表示不自动结束。
                </template>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>

        <a-col :xs="24" :md="12">
          <a-card :bordered="false" title="无人值守" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="启用无人值守兜底">
                <a-switch
                  v-model="form.unattended_enabled"
                  data-testid="policy-unattended_enabled"
                />
                <template #extra>
                  开启后，排队超时或非营业时间的会话由无人值守话术兜底；关闭则不做任何自动回复。
                </template>
              </a-form-item>
              <a-form-item label="无人值守话术">
                <a-textarea
                  v-model="form.unattended_reply"
                  :auto-size="{ minRows: 3, maxRows: 6 }"
                  :textarea-attrs="{
                    'data-testid': 'policy-unattended_reply',
                  }"
                  placeholder="例如：您好，客服当前繁忙，请留言，我们会尽快回复您。"
                />
                <template #extra>
                  按访客语言自动翻译后下发；每个会话只发一次，避免重复打扰。
                </template>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>

        <a-col :span="24">
          <a-card :bordered="false" title="优先级" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="关键词与 AI 的优先关系">
                <a-radio-group
                  v-model="form.ai_keyword_priority"
                  data-testid="policy-ai_keyword_priority"
                >
                  <a-radio value="keyword_first">
                    关键词优先（现状）
                    <a-typography-text type="secondary" class="option-hint">
                      先匹配关键词规则并回复，未命中才交给 AI。
                    </a-typography-text>
                  </a-radio>
                  <a-radio value="ai_first">
                    AI 优先
                    <a-typography-text type="secondary" class="option-hint">
                      先交给 AI 生成回复，只有 AI 不可用或拒答时才用关键词规则。
                    </a-typography-text>
                  </a-radio>
                  <a-radio value="keyword_only">
                    仅关键词
                    <a-typography-text type="secondary" class="option-hint">
                      只按关键词规则回复，完全不调用 AI。
                    </a-typography-text>
                  </a-radio>
                  <a-radio value="ai_only">
                    仅 AI
                    <a-typography-text type="secondary" class="option-hint">
                      全部交给 AI 回复，忽略关键词规则（快捷回复按钮仍可用）。
                    </a-typography-text>
                  </a-radio>
                </a-radio-group>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>

        <a-col :span="24">
          <a-card :bordered="false" title="接管" class="group-card">
            <a-form :model="form" layout="vertical">
              <a-form-item label="接管弹窗提示">
                <a-switch
                  v-model="form.takeover_popup_enabled"
                  data-testid="policy-takeover_popup_enabled"
                />
                <template #extra>
                  关闭后客服 App 不再弹接管提示，AI 继续接待该会话（客服仍可在会话列表手动接入）。
                </template>
              </a-form-item>
            </a-form>
          </a-card>
        </a-col>
      </a-row>
    </a-spin>

    <a-card :bordered="false" class="actions-card">
      <a-space>
        <a-button
          type="primary"
          :loading="saving"
          data-testid="policy-save"
          @click="save"
        >
          保存策略
        </a-button>
        <a-button :loading="loading" @click="load">重新加载</a-button>
        <a-typography-text type="secondary">
          保存后会按服务端返回的策略回填本页。
        </a-typography-text>
      </a-space>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchServicePolicy, updateServicePolicy } from '@/api/admin';

  // 与后端冻结契约一一对应：service-policy 的 policy 共 10 个字段。
  const DEFAULT_POLICY = {
    queue_enabled: true,
    max_wait_seconds: 0,
    sla_first_response_seconds: 0,
    sla_idle_minutes: 0,
    sla_idle_action: 'ai',
    auto_close_idle_minutes: 0,
    ai_keyword_priority: 'keyword_first',
    unattended_enabled: true,
    unattended_reply: '',
    takeover_popup_enabled: true,
  };

  const PRIORITY_VALUES = ['keyword_first', 'ai_first', 'keyword_only', 'ai_only'];
  const IDLE_ACTION_VALUES = ['ai', 'reassign'];

  const form = reactive({ ...DEFAULT_POLICY });
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref('');
  const saveError = ref('');

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  // 数字字段统一归一化：空值/null 视为 0，避免提交 null 让后端解析失败。
  function toPolicyNumber(value: unknown): number {
    if (value === '' || value === null || value === undefined) return 0;
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) return 0;
    return Math.max(0, Math.trunc(parsed));
  }

  function applyPolicy(policy: any) {
    const source = policy && typeof policy === 'object' ? policy : {};
    form.queue_enabled =
      typeof source.queue_enabled === 'boolean'
        ? source.queue_enabled
        : DEFAULT_POLICY.queue_enabled;
    form.max_wait_seconds = toPolicyNumber(source.max_wait_seconds);
    form.sla_first_response_seconds = toPolicyNumber(
      source.sla_first_response_seconds
    );
    form.sla_idle_minutes = toPolicyNumber(source.sla_idle_minutes);
    form.sla_idle_action = IDLE_ACTION_VALUES.includes(source.sla_idle_action)
      ? source.sla_idle_action
      : DEFAULT_POLICY.sla_idle_action;
    form.auto_close_idle_minutes = toPolicyNumber(source.auto_close_idle_minutes);
    form.ai_keyword_priority = PRIORITY_VALUES.includes(
      source.ai_keyword_priority
    )
      ? source.ai_keyword_priority
      : DEFAULT_POLICY.ai_keyword_priority;
    form.unattended_enabled =
      typeof source.unattended_enabled === 'boolean'
        ? source.unattended_enabled
        : DEFAULT_POLICY.unattended_enabled;
    form.unattended_reply =
      source.unattended_reply === null || source.unattended_reply === undefined
        ? ''
        : String(source.unattended_reply);
    form.takeover_popup_enabled =
      typeof source.takeover_popup_enabled === 'boolean'
        ? source.takeover_popup_enabled
        : DEFAULT_POLICY.takeover_popup_enabled;
  }

  function buildPayload() {
    return {
      queue_enabled: form.queue_enabled,
      max_wait_seconds: toPolicyNumber(form.max_wait_seconds),
      sla_first_response_seconds: toPolicyNumber(
        form.sla_first_response_seconds
      ),
      sla_idle_minutes: toPolicyNumber(form.sla_idle_minutes),
      sla_idle_action: form.sla_idle_action,
      auto_close_idle_minutes: toPolicyNumber(form.auto_close_idle_minutes),
      ai_keyword_priority: form.ai_keyword_priority,
      unattended_enabled: form.unattended_enabled,
      unattended_reply: String(form.unattended_reply ?? ''),
      takeover_popup_enabled: form.takeover_popup_enabled,
    };
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchServicePolicy();
      applyPolicy(result?.policy ?? result);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '客服策略加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  async function save() {
    saving.value = true;
    saveError.value = '';
    try {
      const result = await updateServicePolicy(buildPayload());
      const policy = result?.policy ?? result;
      if (policy && typeof policy === 'object') applyPolicy(policy);
      Message.success('客服策略已保存');
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '客服策略保存失败，请稍后重试。');
    } finally {
      saving.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 14px;
  }
  .group-card {
    margin-bottom: 16px;
  }
  .option-hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
  }
  .actions-card {
    margin-top: 0;
  }
</style>
