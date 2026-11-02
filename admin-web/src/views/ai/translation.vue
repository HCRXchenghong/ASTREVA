<template>
  <a-row :gutter="16">
    <a-col :span="14">
      <a-card :bordered="false" title="翻译上游模型">
        <a-alert type="info" class="tip">
          官网以中文为唯一源内容；英语、阿拉伯语由这里的模型自动翻译。
          与「AI SEO」「内容翻译」共用同一套上游配置，密钥加密入库、只回显末四位。
          每批文案数与超时属于翻译执行参数，在「内容管理 → 网站翻译」里设置。
        </a-alert>
        <a-form :model="form" layout="vertical">
          <a-form-item>
            <a-checkbox v-model="form.enabled">中文内容保存后自动翻译</a-checkbox>
          </a-form-item>
          <a-form-item label="API 类型">
            <a-select v-model="form.apiType" :options="apiTypeOptions" />
          </a-form-item>
          <a-form-item label="上游地址" required>
            <a-input v-model="form.baseUrl" placeholder="https://api.openai.com/v1" />
          </a-form-item>
          <a-form-item label="API Key">
            <a-input-password
              v-model="form.apiKey"
              :placeholder="settings?.apiKeyConfigured ? `已配置 ${settings?.apiKeyMasked || ''}；留空不修改` : '未配置'"
              autocomplete="new-password"
            />
          </a-form-item>
          <a-form-item label="翻译模型" required>
            <a-select
              v-model="form.model"
              :options="modelOptions"
              allow-create
              allow-search
              placeholder="从上游获取或手动填写"
            />
            <template #help>请选择文本生成模型；语音识别和语音合成模型不能用于网站翻译。</template>
          </a-form-item>
          <a-form-item>
            <a-checkbox v-model="form.clearApiKey">清除已保存的 API Key</a-checkbox>
          </a-form-item>
          <a-space wrap>
            <a-button :loading="fetchingModels" @click="loadModels">从上游获取模型</a-button>
            <a-button type="primary" :loading="saving" @click="save">保存翻译上游</a-button>
          </a-space>
        </a-form>
      </a-card>
    </a-col>
    <a-col :span="10">
      <a-card :bordered="false" title="当前状态">
        <a-descriptions :column="1" bordered>
          <a-descriptions-item label="源语言修订号">
            {{ overview?.source?.revision ?? '—' }}
          </a-descriptions-item>
          <a-descriptions-item
            v-for="item in overview?.translations || []"
            :key="item.locale"
            :label="item.locale === 'en' ? '英语' : '阿拉伯语'"
          >
            {{ statusLabel(item) }}
          </a-descriptions-item>
        </a-descriptions>
        <a-typography-paragraph class="hint" type="secondary">
          完整的三语进度、重译任务与卡住原因在「内容管理 → 网站翻译」查看。
        </a-typography-paragraph>
        <a-button @click="goTranslations">前往网站翻译</a-button>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { useRouter } from 'vue-router';
  import type { TranslationOverview, TranslationSettingsPayload } from '@/api/translation';
  import {
    fetchTranslationModels,
    fetchTranslations,
    saveTranslationSettings,
  } from '@/api/translation';

  const router = useRouter();
  const overview = ref<TranslationOverview | null>(null);
  const settings = computed(() => overview.value?.settings || null);
  const saving = ref(false);
  const fetchingModels = ref(false);
  const form = reactive({
    enabled: false,
    apiType: 'chat_completions',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    model: '',
    batchSize: 20,
    timeoutMs: 300,
    configuredModels: [] as string[],
    clearApiKey: false,
  });
  const apiTypeOptions = [
    { label: 'Chat Completions', value: 'chat_completions' },
    { label: 'Responses', value: 'responses' },
    { label: 'Anthropic Messages', value: 'anthropic_messages' },
  ];
  const modelOptions = computed(() => {
    const values = [...new Set([form.model, ...form.configuredModels].filter(Boolean))];
    return values.map((value) => ({ label: value, value }));
  });

  function statusLabel(item: any) {
    const map: Record<string, string> = {
      pending: '排队中',
      running: '翻译中',
      ready: '已同步',
      failed: item?.lastError ? `失败：${item.lastError}` : '失败',
      unconfigured: '未配置',
    };
    const label = map[item?.status] || item?.status || '-';
    return item?.stale ? `${label}（源内容已更新，待重译）` : label;
  }

  async function load() {
    overview.value = await fetchTranslations();
    const current = overview.value?.settings;
    if (!current) return;
    Object.assign(form, {
      enabled: Boolean(current.enabled),
      apiType: current.apiType || 'chat_completions',
      baseUrl: current.baseUrl || 'https://api.openai.com/v1',
      apiKey: '',
      model: current.model || '',
      batchSize: Number(current.batchSize || 20),
      timeoutMs: Math.max(10, Math.round(Number(current.timeoutMs || 300000) / 1000)),
      configuredModels: current.configuredModels || [],
      clearApiKey: false,
    });
  }

  function payload(): TranslationSettingsPayload {
    const body: TranslationSettingsPayload = {
      enabled: form.enabled,
      apiType: form.apiType,
      baseUrl: form.baseUrl.trim(),
      model: form.model.trim(),
      batchSize: Number(form.batchSize),
      timeoutMs: Number(form.timeoutMs) * 1000,
      configuredModels: form.configuredModels,
      clearApiKey: form.clearApiKey,
    };
    if (form.apiKey.trim()) body.apiKey = form.apiKey.trim();
    return body;
  }

  async function loadModels() {
    fetchingModels.value = true;
    try {
      const result = await fetchTranslationModels(payload());
      form.configuredModels = result.models || [];
      Message.success(`已获取 ${form.configuredModels.length} 个上游模型`);
    } finally {
      fetchingModels.value = false;
    }
  }

  async function save() {
    if (!form.baseUrl.trim() || !form.model.trim()) {
      Message.warning('请先填写上游地址与翻译模型');
      return;
    }
    saving.value = true;
    try {
      // 每批文案数与超时由「网站翻译」页维护：保存前先取回最新设置，
      // 再还原本页编辑中的上游字段，避免用本页旧值覆盖翻译页刚改过的执行参数。
      const edited = {
        enabled: form.enabled,
        apiType: form.apiType,
        baseUrl: form.baseUrl,
        model: form.model,
        apiKey: form.apiKey,
        clearApiKey: form.clearApiKey,
      };
      await load();
      Object.assign(form, edited);
      await saveTranslationSettings(payload());
      form.apiKey = '';
      form.clearApiKey = false;
      Message.success('翻译上游已保存');
      await load();
    } finally {
      saving.value = false;
    }
  }

  function goTranslations() {
    router.push('/content/translations');
  }

  onMounted(load);
</script>

<style scoped>
  .tip {
    margin-bottom: 12px;
  }
  .hint {
    margin-top: 12px;
    font-size: 12px;
  }
</style>
