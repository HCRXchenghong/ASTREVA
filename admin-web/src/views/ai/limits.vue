<template>
  <a-card :bordered="false" class="limits-page">
    <template #extra>
      <a-space>
        <a-button :loading="loading" @click="load">刷新</a-button>
        <a-button type="primary" :loading="saving" @click="save">保存</a-button>
      </a-space>
    </template>

    <a-alert class="tips" type="info">
      <template #title>限额规则说明</template>
      <ul class="tips-list">
        <li><strong>0 表示不限制</strong>；总开关关闭时，所有限额均不生效。</li>
        <li>触发访客每分钟 / 每天消息上限时，<strong>访客这条消息不会被处理</strong>，并收到下面的超限提示文案。</li>
        <li>触发每会话每天 AI 调用上限时，<strong>不再请求模型</strong>，直接回复超限提示文案。</li>
        <li>AI 回复超过单次最大字数时，会<strong>按句末标点截断</strong>到上限内。</li>
        <li>数字项留空表示不修改该项的当前值；填 0 才会把该项改为「不限制」。</li>
      </ul>
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-alert v-if="saveError" class="banner" type="error" show-icon>
      {{ saveError }}
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-form :model="form" layout="vertical" class="limits-form">
        <a-form-item label="总开关">
          <a-switch v-model="form.enabled" />
          <template #extra>关闭时不限制：访客消息与 AI 调用都不再受下面的阈值约束。</template>
        </a-form-item>

        <a-form-item
          v-for="field in NUMBER_FIELDS"
          :key="field.key"
          :label="field.label"
        >
          <a-input-number
            v-model="form[field.key]"
            :min="0"
            :precision="0"
            :placeholder="field.placeholder"
            :input-attrs="{ id: `ai-limit-${field.key}` }"
            style="width: 220px"
          />
          <template #extra>{{ field.hint }}</template>
        </a-form-item>

        <a-form-item label="超限提示文案">
          <a-textarea
            v-model="form.exceed_message"
            :auto-size="{ minRows: 3, maxRows: 6 }"
            :textarea-attrs="{ id: 'ai-limit-exceed-message' }"
            placeholder="例如：您发送消息太快了，请稍后再试。"
          />
          <template #extra>
            触发任一限额时下发给访客；会按访客语言自动翻译后下发。留空则使用系统内置默认文案，建议填写简短、礼貌的一句话。
          </template>
        </a-form-item>

        <a-form-item>
          <a-button type="primary" :loading="saving" @click="save">保存</a-button>
        </a-form-item>
      </a-form>
    </a-spin>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchAiLimits, updateAiLimits } from '@/api/admin';

  // 与后端契约字段一一对应；min 必须为 0，否则「0 表示不限制」会被组件夹取破坏。
  const NUMBER_FIELDS = [
    {
      key: 'visitor_messages_per_minute',
      label: '访客每分钟消息数',
      placeholder: '例如 20',
      hint: '同一访客每分钟最多可发送的消息条数；超出后该条消息不会被处理并收到提示文案。0 表示不限制。',
    },
    {
      key: 'visitor_messages_per_day',
      label: '访客每天消息数',
      placeholder: '例如 300',
      hint: '同一访客每天最多可发送的消息条数；超出后消息不会被处理并收到提示文案。0 表示不限制。',
    },
    {
      key: 'ai_calls_per_conversation_day',
      label: '每会话每天 AI 调用数',
      placeholder: '例如 60',
      hint: '单个会话每天最多请求模型的次数；超出后不再请求模型，直接回复提示文案。0 表示不限制。',
    },
    {
      key: 'ai_reply_max_chars',
      label: 'AI 单次回复最大字数',
      placeholder: '例如 1200',
      hint: 'AI 单次回复允许的最大字数；超出会按句末标点截断。0 表示不限制。',
    },
  ] as const;

  type NumberKey = (typeof NUMBER_FIELDS)[number]['key'];

  const form = reactive({
    enabled: true,
    visitor_messages_per_minute: undefined as number | undefined,
    visitor_messages_per_day: undefined as number | undefined,
    ai_calls_per_conversation_day: undefined as number | undefined,
    ai_reply_max_chars: undefined as number | undefined,
    exceed_message: '',
  });

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

  // 空值（''/null/undefined）返回 undefined，表示不提交该项；0 是合法值必须保留。
  function toLimitNumber(value: unknown): number | undefined {
    if (value === '' || value === null || value === undefined) return undefined;
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) return undefined;
    return Math.trunc(parsed);
  }

  function applyLimits(limits: any) {
    const source = limits && typeof limits === 'object' ? limits : {};
    if (typeof source.enabled === 'boolean') form.enabled = source.enabled;
    for (const field of NUMBER_FIELDS) {
      form[field.key as NumberKey] = toLimitNumber(source[field.key]);
    }
    form.exceed_message =
      source.exceed_message === null || source.exceed_message === undefined
        ? ''
        : String(source.exceed_message);
  }

  // 指针语义：只提交本次需要改动的字段，数字为空则省略，0 会原样提交。
  function buildPayload() {
    const payload: Record<string, unknown> = { enabled: form.enabled };
    for (const field of NUMBER_FIELDS) {
      const value = toLimitNumber(form[field.key as NumberKey]);
      if (value !== undefined) payload[field.key] = value;
    }
    payload.exceed_message = String(form.exceed_message ?? '');
    return payload;
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchAiLimits();
      applyLimits(result?.limits ?? result);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '限额设置加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  async function save() {
    saving.value = true;
    saveError.value = '';
    try {
      const result = await updateAiLimits(buildPayload());
      const limits = result?.limits ?? result;
      if (limits && typeof limits === 'object') applyLimits(limits);
      Message.success('限额设置已保存');
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '限额保存失败，请稍后重试。');
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
  .tips-list {
    margin: 0;
    padding-left: 18px;
    line-height: 22px;
  }
  .banner {
    margin-bottom: 14px;
  }
  .limits-form {
    max-width: 720px;
  }
</style>
