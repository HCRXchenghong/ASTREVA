<template>
  <a-row :gutter="16">
    <a-col :span="14">
      <a-card :bordered="false" title="AI 接待设置">
        <a-form :model="form" layout="vertical">
          <a-form-item>
            <a-checkbox v-model="form.enabled">启用 AI 自动接待</a-checkbox>
          </a-form-item>
          <a-form-item label="接待模式">
            <a-select v-model="form.mode" :options="modeOptions" />
          </a-form-item>
          <a-form-item label="上游地址">
            <a-input v-model="form.base_url" placeholder="https://api.openai.com/v1" />
          </a-form-item>
          <a-form-item label="API Key">
            <a-input-password v-model="form.api_key" :placeholder="form.api_key_masked ? `已配置 ${form.api_key_masked}；留空不修改` : '请输入 API Key'" />
          </a-form-item>
          <a-form-item label="模型">
            <a-select v-model="form.model" :options="modelOptions" allow-create allow-search />
          </a-form-item>
          <a-form-item label="协议">
            <a-select v-model="form.api_type" :options="apiTypeOptions" />
          </a-form-item>
          <a-row :gutter="12">
            <a-col :span="8">
              <a-form-item label="温度">
                <a-input-number v-model="form.temperature" :min="0" :max="2" :step="0.1" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="最大输出 tokens">
                <a-input-number v-model="form.max_output_tokens" :min="1" :max="8192" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="超时（秒）">
                <a-input-number v-model="form.timeout_seconds" :min="5" :max="120" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item label="系统提示词">
            <a-textarea v-model="form.system_prompt" :auto-size="{ minRows: 4 }" />
          </a-form-item>
          <a-space>
            <a-button type="primary" :loading="saving" @click="save">保存 AI 设置</a-button>
            <a-button :loading="fetchingModels" @click="fetchModels">从上游获取模型</a-button>
            <a-button @click="testReply">测试回复</a-button>
          </a-space>
        </a-form>
      </a-card>
    </a-col>
    <a-col :span="10">
      <a-card :bordered="false" title="提示词缓存探测">
        <a-typography-paragraph>
          {{ cacheText }}
        </a-typography-paragraph>
        <a-button :loading="probing" @click="probe">立即探测</a-button>
      </a-card>
      <a-card :bordered="false" title="测试回复" class="test-card">
        <a-textarea v-model="testInput" :auto-size="{ minRows: 2 }" placeholder="输入一句访客消息" />
        <a-typography-paragraph v-if="testOutput" class="output">{{ testOutput }}</a-typography-paragraph>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchAiCacheProbe,
    fetchAiModels,
    fetchAiSettings,
    probeAiCache,
    testAiReply,
    updateAiSettings,
  } from '@/api/admin';

  const form = reactive<any>({
    enabled: false,
    mode: 'human_first',
    base_url: 'https://api.openai.com/v1',
    api_key: '',
    api_key_masked: '',
    model: 'gpt-4o-mini',
    api_type: 'chat_completions',
    temperature: 0.7,
    max_output_tokens: 512,
    timeout_seconds: 20,
    system_prompt: '',
  });
  const models = ref<string[]>([]);
  const saving = ref(false);
  const fetchingModels = ref(false);
  const probing = ref(false);
  const probeResult = ref<any>(null);
  const testInput = ref('你们的重型排水沟能承受多少吨？');
  const testOutput = ref('');

  const modeOptions = [
    { label: '人工优先（AI 先接待，人工接管后静默退出）', value: 'human_first' },
    { label: '始终 AI', value: 'always_ai' },
    { label: '仅人工（不自动回复）', value: 'manual_only' },
  ];
  const apiTypeOptions = [
    { label: 'Chat Completions', value: 'chat_completions' },
    { label: 'Responses', value: 'responses' },
    { label: 'Anthropic Messages', value: 'anthropic_messages' },
  ];
  const modelOptions = computed(() => {
    const values = [...new Set([form.model, ...models.value].filter(Boolean))];
    return values.map((value) => ({ label: value, value }));
  });
  const cacheText = computed(() => {
    const result = probeResult.value;
    if (!result) return '尚未探测。保存 AI 设置时会自动逐个实测模型缓存支持。';
    const entries = Object.entries(result.model_results || {});
    if (result.status === 'running') return '探测进行中，请稍候…';
    if (!entries.length) return '暂无探测结果。';
    const labels: Record<string, string> = { auto: '支持自动缓存', explicit: '支持显式缓存', none: '不支持' };
    return entries.map(([model, mode]) => `${model} → ${labels[mode as string] || mode}`).join('；');
  });

  async function load() {
    Object.assign(form, (await fetchAiSettings()) || {});
  }

  async function save() {
    saving.value = true;
    try {
      const payload: Record<string, unknown> = {
        enabled: form.enabled,
        mode: form.mode,
        base_url: form.base_url,
        model: form.model,
        api_type: form.api_type,
        temperature: Number(form.temperature),
        max_output_tokens: Number(form.max_output_tokens),
        timeout_seconds: Number(form.timeout_seconds),
        system_prompt: form.system_prompt,
      };
      if (form.api_key) payload.api_key = form.api_key;
      await updateAiSettings(payload);
      form.api_key = '';
      Message.success('AI 设置已保存');
      await load();
      setTimeout(() => fetchCacheProbe(), 2500);
    } finally {
      saving.value = false;
    }
  }

  async function fetchModels() {
    fetchingModels.value = true;
    try {
      const payload: Record<string, unknown> = {
        base_url: form.base_url,
        api_type: form.api_type,
        timeout_seconds: Number(form.timeout_seconds),
      };
      if (form.api_key) payload.api_key = form.api_key;
      const result = await fetchAiModels(payload);
      models.value = result.models || [];
      Message.success(`已获取 ${models.value.length} 个上游模型`);
    } finally {
      fetchingModels.value = false;
    }
  }

  async function fetchCacheProbe() {
    try {
      probeResult.value = await fetchAiCacheProbe();
    } catch {
      /* 探测状态读取失败不影响主流程 */
    }
  }

  async function probe() {
    probing.value = true;
    try {
      await probeAiCache(models.value.length ? models.value : [form.model]);
      Message.success('缓存探测已启动');
      setTimeout(() => fetchCacheProbe(), 2000);
    } finally {
      probing.value = false;
    }
  }

  async function testReply() {
    try {
      const result = await testAiReply(testInput.value);
      testOutput.value = result.reply || '';
    } catch (err) {
      testOutput.value = '';
    }
  }

  onMounted(async () => {
    await load();
    await fetchCacheProbe();
  });
</script>

<style scoped>
  .test-card {
    margin-top: 16px;
  }
  .output {
    margin-top: 10px;
    padding: 10px;
    border-radius: 8px;
    background: var(--color-fill-2);
    white-space: pre-wrap;
  }
</style>
