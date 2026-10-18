<template>
  <a-card :bordered="false">
    <template #title>模型供应商</template>
    <template #extra>
      <a-button type="primary" @click="openEditor()">新增供应商</a-button>
    </template>
    <a-alert class="tips" type="info">
      统一维护文本 / 生图模型的上游供应商，密钥加密入库、只回显末四位。
      每项能力用哪个供应商由「AI 管理 → 能力绑定」决定。
    </a-alert>
    <a-table :data="providers" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="名称" :width="180">
          <template #cell="{ record }">
            <div><strong>{{ record.name }}</strong></div>
            <a-typography-text type="secondary">排序 {{ record.sort_order ?? 0 }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="类型" :width="90">
          <template #cell="{ record }">
            <a-tag :color="record.kind === 'image' ? 'purple' : 'arcoblue'">
              {{ kindLabel(record.kind) }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="协议" :width="150">
          <template #cell="{ record }">{{ apiTypeLabel(record.api_type) }}</template>
        </a-table-column>
        <a-table-column title="上游地址">
          <template #cell="{ record }">
            <a-typography-text code class="base-url">{{ record.base_url }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="模型数" :width="200">
          <template #cell="{ record }">
            <div>{{ (record.models || []).length }} 个</div>
            <a-typography-text type="secondary">
              默认：{{ record.default_model || '未设置' }}
            </a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="状态" :width="90">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">
              {{ record.enabled ? '启用' : '禁用' }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="260">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link
                :loading="modelsLoadingId === record.id"
                @click="fetchModelsFor(record.id)"
              >
                一键获取模型
              </a-link>
              <a-link
                :loading="testLoadingId === record.id"
                @click="testConnection(record.id)"
              >
                测试连接
              </a-link>
              <a-link status="danger" @click="remove(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-drawer
      v-model:visible="editorVisible"
      :width="560"
      :title="form.id ? '编辑供应商' : '新增供应商'"
      :ok-loading="saving"
      ok-text="保存"
      cancel-text="取消"
      unmount-on-close
      @ok="saveAndClose"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model="form.name" placeholder="例如：OpenAI 主账号" />
        </a-form-item>
        <a-form-item label="类型" required>
          <a-radio-group v-model="form.kind" type="button" @change="onKindChange">
            <a-radio value="text">文本</a-radio>
            <a-radio value="image">生图</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-form-item label="协议" required>
          <a-select v-model="form.api_type" :options="apiTypeOptions" />
        </a-form-item>
        <a-form-item label="上游地址" required>
          <a-input v-model="form.base_url" placeholder="https://api.openai.com/v1" />
        </a-form-item>
        <a-form-item label="API Key">
          <a-input-password
            v-model="form.api_key"
            :placeholder="apiKeyPlaceholder"
            autocomplete="new-password"
          />
          <a-checkbox v-if="form.id" v-model="form.clear_api_key" class="clear-key">
            清除已保存的 API Key
          </a-checkbox>
        </a-form-item>
        <a-form-item label="默认模型">
          <a-select
            v-model="form.default_model"
            :options="modelOptions"
            allow-create
            allow-search
            placeholder="先点「一键获取模型」，或手动填写"
          />
        </a-form-item>
        <a-form-item label="请求超时（毫秒）">
          <a-input-number v-model="form.timeout_ms" :min="1000" :max="600000" :step="1000" />
        </a-form-item>
        <a-form-item label="出站主机白名单（逗号分隔）">
          <a-input v-model="form.allowed_hosts" placeholder="api.openai.com,*.example.com" />
          <a-typography-text type="secondary" class="field-hint">
            用于防 SSRF，留空表示不限制。只允许主机名、主机:端口或 IP。
          </a-typography-text>
        </a-form-item>
        <a-grid :cols="2" :col-gap="12">
          <a-grid-item>
            <a-form-item label="排序">
              <a-input-number v-model="form.sort_order" :min="0" />
            </a-form-item>
          </a-grid-item>
          <a-grid-item>
            <a-form-item label="状态">
              <a-switch v-model="form.enabled" />
            </a-form-item>
          </a-grid-item>
        </a-grid>
        <a-space wrap>
          <a-button
            :loading="saving || (!!form.id && modelsLoadingId === form.id)"
            :disabled="!form.id && !canSave"
            @click="fetchModelsInEditor"
          >
            一键获取模型
          </a-button>
          <a-button
            :loading="saving || (!!form.id && testLoadingId === form.id)"
            :disabled="!form.id && !canSave"
            @click="testInEditor"
          >
            测试连接
          </a-button>
        </a-space>
        <a-typography-text type="secondary" class="field-hint">
          新增供应商时点这两个按钮会先保存当前表单，再对已保存的配置执行操作。
        </a-typography-text>
      </a-form>
    </a-drawer>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createAiProvider,
    deleteAiProvider,
    fetchAiProviderModels,
    fetchAiProviders,
    testAiProvider,
    updateAiProvider,
  } from '@/api/admin';

  const KIND_OPTIONS = [
    { label: '文本', value: 'text' },
    { label: '生图', value: 'image' },
  ] as const;
  const TEXT_API_TYPES = [
    { label: 'Chat Completions', value: 'chat_completions' },
    { label: 'Responses', value: 'responses' },
    { label: 'Anthropic Messages', value: 'anthropic_messages' },
  ];
  const IMAGE_API_TYPES = [{ label: 'Images', value: 'images' }];
  const API_TYPE_LABELS: Record<string, string> = {
    chat_completions: 'Chat Completions',
    responses: 'Responses',
    anthropic_messages: 'Anthropic Messages',
    images: 'Images',
  };

  const providers = ref<any[]>([]);
  const loading = ref(false);
  const saving = ref(false);
  const editorVisible = ref(false);
  const modelsLoadingId = ref('');
  const testLoadingId = ref('');
  const form = reactive<any>({
    id: '',
    name: '',
    kind: 'text',
    api_type: 'chat_completions',
    base_url: '',
    api_key: '',
    api_key_masked: '',
    clear_api_key: false,
    models: [] as string[],
    default_model: '',
    timeout_ms: 30000,
    allowed_hosts: '',
    enabled: true,
    sort_order: 0,
  });

  const apiTypeOptions = computed(() =>
    form.kind === 'image' ? IMAGE_API_TYPES : TEXT_API_TYPES
  );
  const modelOptions = computed(() => {
    const values = [
      ...new Set([form.default_model, ...(form.models || [])].filter(Boolean)),
    ];
    return values.map((value) => ({ label: value, value }));
  });
  const apiKeyPlaceholder = computed(() => {
    if (!form.id) return '请输入 API Key';
    const masked = String(form.api_key_masked || '');
    const display = masked.includes('****')
      ? masked
      : masked
        ? `****${masked.slice(-4)}`
        : '****';
    return `已配置 ${display}，留空不修改`;
  });
  const canSave = computed(() =>
    Boolean(form.name.trim() && form.base_url.trim() && form.api_type)
  );

  function kindLabel(kind: string) {
    return KIND_OPTIONS.find((item) => item.value === kind)?.label || kind || '-';
  }
  function apiTypeLabel(apiType: string) {
    return API_TYPE_LABELS[apiType] || apiType || '-';
  }

  async function load() {
    loading.value = true;
    try {
      const payload = await fetchAiProviders();
      providers.value = [...(payload.providers || [])].sort(
        (a, b) => Number(a.sort_order || 0) - Number(b.sort_order || 0)
      );
    } finally {
      loading.value = false;
    }
  }

  function applyProvider(provider: any, models?: string[]) {
    if (!provider?.id) return;
    const index = providers.value.findIndex((item) => item.id === provider.id);
    if (index === -1) {
      providers.value = [
        ...providers.value,
        models ? { ...provider, models } : provider,
      ];
      return;
    }
    providers.value[index] = {
      ...providers.value[index],
      ...provider,
      models: models || provider.models || providers.value[index].models || [],
    };
  }

  function openEditor(record?: any) {
    Object.assign(form, {
      id: record?.id || '',
      name: record?.name || '',
      kind: record?.kind || 'text',
      api_type: record?.api_type || 'chat_completions',
      base_url: record?.base_url || '',
      api_key: '',
      api_key_masked: record?.api_key_masked || '',
      clear_api_key: false,
      models: [...(record?.models || [])],
      default_model: record?.default_model || '',
      timeout_ms: Number(record?.timeout_ms || 30000),
      allowed_hosts: record?.allowed_hosts || '',
      enabled: record?.enabled ?? true,
      sort_order: Number(record?.sort_order || 0),
    });
    editorVisible.value = true;
  }

  function onKindChange() {
    const options = apiTypeOptions.value.map((item) => item.value);
    if (!options.includes(form.api_type)) {
      form.api_type = options[0];
    }
    form.default_model = '';
    form.models = [];
  }

  function validate(): boolean {
    if (!form.name.trim()) {
      Message.warning('请填写供应商名称');
      return false;
    }
    if (!form.api_type) {
      Message.warning('请选择协议');
      return false;
    }
    if (!form.base_url.trim()) {
      Message.warning('请填写上游地址');
      return false;
    }
    return true;
  }

  function buildPayload() {
    const models = [
      ...new Set([...(form.models || []), form.default_model].filter(Boolean)),
    ];
    const payload: Record<string, unknown> = {
      name: form.name.trim(),
      kind: form.kind,
      api_type: form.api_type,
      base_url: form.base_url.trim(),
      models,
      default_model: String(form.default_model || '').trim(),
      timeout_ms: Number(form.timeout_ms || 30000),
      allowed_hosts: String(form.allowed_hosts || '').trim(),
      enabled: Boolean(form.enabled),
      sort_order: Number(form.sort_order || 0),
    };
    if (form.clear_api_key) {
      payload.api_key = '';
    } else if (String(form.api_key || '').trim()) {
      payload.api_key = String(form.api_key).trim();
    }
    return payload;
  }

  // 新增时先创建拿到 id，编辑时直接 PATCH；返回最新供应商（失败返回 null）。
  async function persistProvider(): Promise<any | null> {
    if (!validate()) return null;
    saving.value = true;
    try {
      const payload = buildPayload();
      const result = form.id
        ? await updateAiProvider(form.id, payload)
        : await createAiProvider(payload);
      const provider = result?.provider || null;
      if (provider) {
        form.id = provider.id || form.id;
        form.api_key = '';
        form.clear_api_key = false;
        form.api_key_masked = provider.api_key_masked || form.api_key_masked;
        if (Array.isArray(provider.models)) form.models = [...provider.models];
        applyProvider(provider);
      }
      return provider || { id: form.id };
    } finally {
      saving.value = false;
    }
  }

  async function saveAndClose() {
    const provider = await persistProvider();
    if (!provider) return;
    editorVisible.value = false;
    Message.success('供应商已保存');
    await load();
  }

  async function fetchModelsFor(id: string) {
    modelsLoadingId.value = id;
    try {
      const result = await fetchAiProviderModels(id);
      const models = Array.isArray(result?.models)
        ? result.models
        : result?.provider?.models || [];
      if (result?.provider) applyProvider(result.provider, models);
      if (form.id === id) {
        form.models = [...models];
        if (!form.default_model && models.length) form.default_model = models[0];
      }
      Message.success(`已获取 ${models.length} 个模型`);
    } catch (error: any) {
      Message.error(`获取模型失败：${errorMessage(error)}`);
    } finally {
      modelsLoadingId.value = '';
    }
  }

  async function testConnection(id: string) {
    testLoadingId.value = id;
    try {
      const result = await testAiProvider(id);
      if (result?.ok) {
        Message.success(
          `连接成功：${Number(result.latency_ms || 0)} ms，${
            Number(result.models_count || 0)
          } 个模型`
        );
      } else {
        Message.error(`连接失败：${result?.message || '未知原因'}`);
      }
    } catch (error: any) {
      Message.error(`连接失败：${errorMessage(error)}`);
    } finally {
      testLoadingId.value = '';
    }
  }

  // 抽屉里的「一键获取模型」「测试连接」：先保存当前表单，再针对已保存的 id 执行。
  async function fetchModelsInEditor() {
    const provider = await persistProvider();
    if (!provider?.id) return;
    await fetchModelsFor(provider.id);
  }
  async function testInEditor() {
    const provider = await persistProvider();
    if (!provider?.id) return;
    await testConnection(provider.id);
  }

  function errorMessage(error: any) {
    return (
      error?.response?.data?.error?.message ||
      error?.response?.data?.message ||
      error?.message ||
      '请求失败'
    );
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除供应商',
      content: `删除 ${record.name} 后，绑定到它的能力需要重新选择供应商。`,
      onOk: async () => {
        await deleteAiProvider(record.id);
        if (form.id === record.id) editorVisible.value = false;
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>

<style scoped>
  .tips {
    margin-bottom: 14px;
  }
  .base-url {
    word-break: break-all;
  }
  .field-hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
  }
  .clear-key {
    margin-top: 6px;
  }
</style>
