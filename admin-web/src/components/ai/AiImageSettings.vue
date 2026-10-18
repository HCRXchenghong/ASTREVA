<template>
  <a-form :model="form" layout="vertical">
    <a-form-item label="Images API Base URL" required>
      <a-input v-model="form.baseUrl" placeholder="https://api.openai.com/v1" />
    </a-form-item>
    <a-form-item label="API Key">
      <a-input-password
        v-model="form.apiKey"
        :placeholder="configured ? `已配置 ${apiKeyMasked}；留空不修改` : '未配置'"
        autocomplete="new-password"
      />
    </a-form-item>
    <a-form-item label="默认模型" required>
      <a-select
        v-model="form.defaultModel"
        :options="modelOptions"
        allow-create
        allow-search
      />
    </a-form-item>
    <a-form-item label="允许的模型（逗号分隔）">
      <a-input v-model="form.configuredModels" />
    </a-form-item>
    <a-form-item label="请求超时（毫秒）">
      <a-input-number
        v-model="form.requestTimeoutMs"
        :min="10000"
        :max="600000"
        :step="1000"
      />
    </a-form-item>
    <a-grid :cols="{ xs: 1, md: 2 }" :col-gap="12">
      <a-grid-item>
        <a-form-item label="API 请求主机白名单">
          <a-input v-model="form.allowedHosts" placeholder="api.openai.com" />
        </a-form-item>
      </a-grid-item>
      <a-grid-item>
        <a-form-item label="图片下载主机白名单">
          <a-input v-model="form.downloadHosts" />
        </a-form-item>
      </a-grid-item>
    </a-grid>
    <a-form-item>
      <a-checkbox v-model="form.clearApiKey">
        清除已保存的 AI 生图 API Key
      </a-checkbox>
    </a-form-item>
    <a-alert type="info" class="tip">
      AI 生图与客服接待 AI 是两套独立配置；密钥使用主密钥加密入库，页面只回显末四位。
      自定义兼容 API 时必须把主机名写入请求白名单（用于防 SSRF）。
    </a-alert>
    <a-space>
      <a-button :loading="testing" @click="test">测试并同步模型</a-button>
      <a-button type="primary" :loading="saving" @click="save">
        保存 AI 生图配置
      </a-button>
    </a-space>
  </a-form>
</template>

<script lang="ts" setup>
  import { computed, reactive, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import type { MediaSettings } from '@/api/media';
  import { testMediaAiImage, updateMediaAiImage } from '@/api/media';

  const props = withDefaults(
    defineProps<{ settings?: MediaSettings | null }>(),
    { settings: null }
  );
  const emit = defineEmits<{ (event: 'saved'): void }>();

  const ai = computed(() => ({
    ...(props.settings?.config?.aiImage || {}),
    ...(props.settings?.aiImage || {}),
  }));
  const configured = computed(() => Boolean(ai.value.apiKeyConfigured));
  const apiKeyMasked = computed(() => ai.value.apiKeyMasked || '');
  const modelOptions = computed(() =>
    String(ai.value.configuredModels || '')
      .split(',')
      .filter(Boolean)
      .map((value) => ({ label: value.trim(), value: value.trim() }))
  );

  const saving = ref(false);
  const testing = ref(false);
  const form = reactive({
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    defaultModel: 'gpt-image-1',
    configuredModels: '',
    requestTimeoutMs: 120000,
    allowedHosts: 'api.openai.com',
    downloadHosts: '',
    clearApiKey: false,
  });

  watch(
    () => props.settings,
    () => {
      Object.assign(form, {
        baseUrl: ai.value.baseUrl || 'https://api.openai.com/v1',
        apiKey: '',
        defaultModel: ai.value.defaultModel || 'gpt-image-1',
        configuredModels: ai.value.configuredModels || '',
        requestTimeoutMs: Number(ai.value.requestTimeoutMs || 120000),
        allowedHosts: ai.value.allowedHosts || 'api.openai.com',
        downloadHosts: ai.value.downloadHosts || '',
        clearApiKey: false,
      });
    },
    { immediate: true, deep: true }
  );

  function payload() {
    const body: Record<string, unknown> = {
      baseUrl: form.baseUrl.trim(),
      defaultModel: form.defaultModel.trim(),
      configuredModels: form.configuredModels.trim(),
      requestTimeoutMs: Number(form.requestTimeoutMs),
      allowedHosts: form.allowedHosts.trim(),
      downloadHosts: form.downloadHosts.trim(),
      clearApiKey: form.clearApiKey,
    };
    if (form.apiKey.trim()) body.apiKey = form.apiKey.trim();
    return body;
  }

  async function test() {
    testing.value = true;
    try {
      const result = await testMediaAiImage(payload());
      const models = result.data?.models || [];
      if (models.length) form.configuredModels = models.join(',');
      Message.success(`验证通过，发现 ${models.length} 个可用模型`);
    } finally {
      testing.value = false;
    }
  }

  async function save() {
    saving.value = true;
    try {
      await updateMediaAiImage(payload());
      form.apiKey = '';
      form.clearApiKey = false;
      Message.success('AI 生图配置已加密保存并立即生效');
      emit('saved');
    } finally {
      saving.value = false;
    }
  }
</script>

<style scoped>
  .tip {
    margin-bottom: 12px;
  }
</style>
