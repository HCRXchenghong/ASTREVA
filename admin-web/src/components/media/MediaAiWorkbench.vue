<template>
  <div class="ai-workbench">
    <a-card :bordered="false" class="ai-parameters">
      <template #title>生图参数</template>
      <template #extra>
        <a-tag :color="configured ? 'green' : 'red'">
          {{ configured ? 'AI 已配置' : 'AI 未配置' }}
        </a-tag>
      </template>

      <a-alert v-if="capabilityError" type="error" class="ai-alert">
        无法读取 AI 能力：{{ capabilityError }}
      </a-alert>

      <div v-if="!configured" class="not-configured">
        <a-empty description="尚未配置 AI 生图 API Key" />
        <p class="not-configured-tip">
          请在「系统设置」标签里填写接口地址、API Key 与模型，保存并验证通过后即可使用。
        </p>
        <a-space>
          <a-button :loading="modelsLoading" @click="loadModels(true)">重新检查</a-button>
          <a-link href="/site-admin/content/media" target="_self">前往设置</a-link>
        </a-space>
      </div>

      <template v-else>
        <div class="field">
          <label>提示词</label>
          <a-textarea
            v-model="prompt"
            :max-length="4000"
            :auto-size="{ minRows: 6, maxRows: 12 }"
            placeholder="描述产品、场景、构图、光线、材质和风格…"
          />
          <span class="field-help">{{ prompt.length }} / 4000</span>
        </div>

        <div class="field-row">
          <div class="field">
            <label>尺寸</label>
            <a-select v-model="size">
              <a-option v-for="option in SIZES" :key="option.value" :value="option.value">
                {{ option.label }}
              </a-option>
            </a-select>
          </div>
          <div class="field">
            <label>质量</label>
            <a-select v-model="quality">
              <a-option v-for="option in QUALITIES" :key="option.value" :value="option.value">
                {{ option.label }}
              </a-option>
            </a-select>
          </div>
        </div>

        <div class="field">
          <label>模型</label>
          <a-space class="model-row">
            <a-select v-model="model" style="flex: 1" :loading="modelsLoading">
              <a-option v-for="name in models" :key="name" :value="name">{{ name }}</a-option>
            </a-select>
            <a-button :loading="modelsLoading" @click="loadModels(true)">刷新模型</a-button>
          </a-space>
        </div>

        <div class="field">
          <label>上传分类</label>
          <a-select v-model="category">
            <a-option v-for="name in categoryOptions" :key="name" :value="name">{{ name }}</a-option>
          </a-select>
        </div>

        <a-button
          type="primary"
          long
          class="generate-button"
          :loading="generating"
          @click="generatePreview"
        >
          生成图片
        </a-button>
        <p class="form-state" :class="stateClass">{{ stateText }}</p>
      </template>
    </a-card>

    <a-card :bordered="false" class="ai-results">
      <template #title>生成结果</template>
      <template #extra>
        <a-tag>{{ results.length }} 张</a-tag>
      </template>

      <a-empty v-if="!results.length" description="还没有生成结果，请在左侧输入提示词并点击“生成图片”。" />

      <div v-else class="ai-result-grid">
        <article v-for="item in results" :key="item.id" class="ai-result-card">
          <div class="ai-result-image">
            <img :src="item.dataUrl" :alt="`AI 生成结果 ${item.id}`" />
          </div>
          <div class="ai-result-info">
            <div class="ai-result-meta">
              <span>{{ item.size || '—' }} · {{ formatBytes(item.bytes) }}</span>
              <a-tag size="small" :color="item.status === 'error' ? 'red' : 'gray'">
                {{ statusLabel(item.status) }}
              </a-tag>
            </div>
            <div class="ai-result-actions">
              <a-button
                type="primary"
                size="small"
                :loading="item.status === 'uploading'"
                :disabled="!['pending', 'error'].includes(item.status)"
                @click="commitPreview(item)"
              >
                {{ item.status === 'uploaded' ? '已上传' : '直接上传使用' }}
              </a-button>
              <a-button
                size="small"
                :disabled="item.status === 'uploading'"
                @click="discardPreview(item)"
              >
                {{ item.status === 'uploaded' ? '移除结果' : '放弃' }}
              </a-button>
            </div>
          </div>
        </article>
      </div>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    aiCommitPreview,
    aiDiscardPreview,
    aiPreview,
    fetchAiModels,
  } from '@/api/media';
  import type { AiCapability, AiPreviewItem, MediaCategory, MediaSettings } from '@/api/media';
  import { formatBytes } from '@/lib/media';

  type PreviewStatus = 'pending' | 'uploading' | 'uploaded' | 'error';
  interface PreviewResult extends AiPreviewItem {
    status: PreviewStatus;
  }

  const SIZES = [
    { value: '1024x1024', label: '1:1 正方形' },
    { value: '1536x1024', label: '3:2 横版' },
    { value: '1024x1536', label: '2:3 竖版' },
    { value: '1792x1024', label: '16:9 横版' },
    { value: '1024x1792', label: '9:16 竖版' },
  ];
  const QUALITIES = [
    { value: 'auto', label: '自动' },
    { value: 'medium', label: '中等' },
    { value: 'high', label: '高' },
    { value: 'standard', label: '标准' },
    { value: 'hd', label: 'HD' },
  ];

  const props = withDefaults(
    defineProps<{ categories?: MediaCategory[]; settings?: MediaSettings | null }>(),
    { categories: () => [], settings: null }
  );
  const emit = defineEmits<{ (e: 'changed'): void }>();

  const capability = ref<AiCapability | null>(null);
  const capabilityError = ref('');
  const modelsLoading = ref(false);
  const generating = ref(false);
  const prompt = ref('');
  const size = ref('1024x1024');
  const quality = ref('auto');
  const model = ref('');
  const category = ref('未分类');
  const results = ref<PreviewResult[]>([]);
  const stateText = ref('临时结果保留 15 分钟，不会自动进入素材库。');
  const stateClass = ref('');

  const models = computed(() => capability.value?.models || []);
  const configured = computed(
    () =>
      capability.value?.configured ??
      props.settings?.aiImage?.configured ??
      props.settings?.config?.aiImage?.apiKeyConfigured ??
      false
  );
  const categoryOptions = computed(() => {
    const names = props.categories.map((item) => item.name);
    return names.includes('未分类') ? names : ['未分类', ...names];
  });

  function statusLabel(status: PreviewStatus) {
    return { pending: '待处理', uploading: '上传中', uploaded: '已上传', error: '失败' }[status];
  }

  async function loadModels(refresh = false) {
    modelsLoading.value = true;
    capabilityError.value = '';
    try {
      const payload = await fetchAiModels(refresh);
      capability.value = payload.data || {};
      const list = capability.value.models || [];
      if (!model.value || !list.includes(model.value)) {
        model.value = capability.value.defaultModel || list[0] || '';
      }
    } catch (error: any) {
      capabilityError.value = error?.message || '请求失败';
    } finally {
      modelsLoading.value = false;
    }
  }

  async function generatePreview() {
    const text = prompt.value.trim();
    if (!text) {
      Message.warning('请先输入生图提示词。');
      return;
    }
    generating.value = true;
    stateClass.value = '';
    stateText.value = '正在请求 AI 生图，结果不会自动入库…';
    try {
      const payload = await aiPreview({
        prompt: text,
        model: model.value || undefined,
        size: size.value,
        quality: quality.value,
      });
      results.value.unshift({ ...payload.data, status: 'pending' });
      stateClass.value = 'success';
      stateText.value = `生成成功，临时结果将在 ${new Date(payload.data.expiresAt).toLocaleString(
        'zh-CN',
        { hour12: false }
      )} 前有效。`;
      Message.success('生成成功，请在结果区选择上传或放弃。');
    } catch (error: any) {
      stateClass.value = 'error';
      stateText.value = error?.message || '生成失败。';
    } finally {
      generating.value = false;
    }
  }

  async function commitPreview(item: PreviewResult) {
    item.status = 'uploading';
    try {
      const payload = await aiCommitPreview(item.id, {
        category: category.value,
        preserveOriginal: props.settings?.config?.image?.keepOriginal !== false,
      });
      item.status = 'uploaded';
      Message.success(`已上传到素材管理（${payload.data.category}）。`);
      emit('changed');
    } catch {
      item.status = 'error';
    }
  }

  async function discardPreview(item: PreviewResult) {
    try {
      if (item.status !== 'uploaded') await aiDiscardPreview(item.id);
      results.value = results.value.filter((entry) => entry.id !== item.id);
      Message.success(item.status === 'uploaded' ? '已从结果区移除。' : '临时结果已放弃。');
    } catch {
      // 错误提示已由响应拦截器统一处理。
    }
  }

  onMounted(() => {
    loadModels(false);
  });
</script>

<style scoped>
  .ai-workbench {
    display: grid;
    grid-template-columns: minmax(300px, 380px) 1fr;
    gap: 12px;
    align-items: start;
  }

  @media (max-width: 1100px) {
    .ai-workbench {
      grid-template-columns: 1fr;
    }
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
  }

  .field label {
    color: var(--color-text-2);
    font-size: 13px;
  }

  .field-row {
    display: flex;
    gap: 12px;
  }

  .field-row .field {
    flex: 1;
  }

  .field-help {
    align-self: flex-end;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .model-row {
    width: 100%;
  }

  .generate-button {
    margin-top: 4px;
  }

  .form-state {
    margin: 10px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .form-state.success {
    color: rgb(var(--success-6));
  }

  .form-state.error {
    color: rgb(var(--danger-6));
  }

  .not-configured {
    padding: 8px 0;
  }

  .not-configured-tip {
    color: var(--color-text-3);
    font-size: 12px;
    text-align: center;
  }

  .ai-alert {
    margin-bottom: 12px;
  }

  .ai-result-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
  }

  .ai-result-card {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    overflow: hidden;
  }

  .ai-result-image {
    height: 180px;
    background: var(--color-fill-1);
  }

  .ai-result-image img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .ai-result-info {
    padding: 10px 12px 12px;
  }

  .ai-result-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .ai-result-actions {
    display: flex;
    gap: 8px;
  }
</style>
