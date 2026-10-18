<template>
  <a-card :bordered="false">
    <template #title>能力绑定</template>
    <template #extra>
      <a-button type="primary" :loading="savingAll" @click="saveAll">保存全部</a-button>
    </template>
    <a-alert class="tips" type="info">
      接待 AI 与翻译 / 语言检测由客服引擎使用；AI SEO 与 AI 生图当前仍由官网后台使用独立配置，下一轮接入。
      未绑定的能力会回落到「系统设置」里的默认上游。
    </a-alert>
    <a-table :data="rows" :loading="loading" :pagination="false" row-key="capability">
      <template #columns>
        <a-table-column title="能力" :width="200">
          <template #cell="{ record }">
            <div><strong>{{ record.label }}</strong></div>
            <a-typography-text type="secondary">
              {{ record.provider_id ? requirementHint(record) : UNBOUND_HINT }}
            </a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="供应商">
          <template #cell="{ record }">
            <a-select
              v-model="record.provider_id"
              :options="providerOptions(record)"
              allow-search
              placeholder="未绑定"
              @change="onProviderChange(record)"
            />
          </template>
        </a-table-column>
        <a-table-column title="模型">
          <template #cell="{ record }">
            <a-select
              v-model="record.model"
              :options="modelOptions(record)"
              :disabled="!record.provider_id"
              allow-create
              allow-search
              placeholder="跟随供应商默认模型"
            />
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="90">
          <template #cell="{ record }">
            <a-link :loading="savingCapability === record.capability" @click="saveRow(record)">
              保存
            </a-link>
          </template>
        </a-table-column>
      </template>
    </a-table>
    <a-typography-text type="secondary" class="footer-hint">
      模型下拉允许手填：上游未返回全部模型时可直接输入模型名。
    </a-typography-text>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchAiBindings, fetchAiProviders, updateAiBindings } from '@/api/admin';

  const UNBOUND_HINT = '未绑定（使用系统设置里的默认上游）';
  // 能力顺序固定，与服务端 domain.AICapabilities() 一致。
  const CAPABILITIES = [
    { capability: 'reception', label: '接待 AI', kind: 'text' },
    { capability: 'translation', label: '翻译', kind: 'text' },
    { capability: 'detection', label: '语言检测', kind: 'text' },
    { capability: 'seo', label: 'AI SEO', kind: 'text' },
    { capability: 'image', label: 'AI 生图', kind: 'image' },
  ];

  const providers = ref<any[]>([]);
  const rows = ref<any[]>([]);
  const loading = ref(false);
  const savingAll = ref(false);
  const savingCapability = ref('');

  // 兼容两种响应形状：契约里的 {capability:{...}} 映射，以及客服引擎实际返回的 [{capability,...}] 数组。
  function normalizeBindings(value: any): Record<string, any> {
    if (Array.isArray(value)) {
      return Object.fromEntries(
        value
          .filter((item) => item && item.capability)
          .map((item) => [item.capability, item])
      );
    }
    return value && typeof value === 'object' ? value : {};
  }

  function requirementHint(record: any) {
    return record.kind === 'image' ? '需要生图类型供应商' : '需要文本类型供应商';
  }

  // 只列启用且类型匹配的供应商：生图能力只认 image，其余能力只认 text。
  function providerOptions(record: any) {
    return providers.value
      .filter((provider) => provider.enabled && provider.kind === record.kind)
      .map((provider) => ({ label: provider.name, value: provider.id }));
  }

  function providerOf(id: string) {
    return providers.value.find((provider) => provider.id === id) || null;
  }

  function modelOptions(record: any) {
    const provider = providerOf(record.provider_id);
    const values = [
      ...new Set([record.model, ...(provider?.models || [])].filter(Boolean)),
    ];
    return values.map((value) => ({ label: value, value }));
  }

  function onProviderChange(record: any) {
    // 换供应商后原模型大概率不属于新上游，清空让管理员重新选择。
    record.model = '';
  }

  function applyBindings(bindings: any) {
    const map = normalizeBindings(bindings);
    rows.value = rows.value.map((row) => {
      const binding = map[row.capability];
      if (!binding) return row;
      return {
        ...row,
        provider_id: binding.provider_id || '',
        model: binding.model || '',
        params: binding.params || {},
      };
    });
  }

  async function load() {
    loading.value = true;
    try {
      const [providerPayload, bindingPayload] = await Promise.all([
        fetchAiProviders(),
        fetchAiBindings(),
      ]);
      providers.value = providerPayload.providers || [];
      const map = normalizeBindings(bindingPayload.bindings);
      rows.value = CAPABILITIES.map((meta) => {
        const binding = map[meta.capability] || {};
        return {
          ...meta,
          provider_id: binding.provider_id || '',
          model: binding.model || '',
          params: binding.params || {},
        };
      });
    } finally {
      loading.value = false;
    }
  }

  function toPayload(record: any) {
    return {
      capability: record.capability,
      provider_id: record.provider_id || '',
      model: record.model || '',
      params: record.params || {},
    };
  }

  async function saveRow(record: any) {
    savingCapability.value = record.capability;
    try {
      const result = await updateAiBindings([toPayload(record)]);
      if (result?.bindings) applyBindings(result.bindings);
      Message.success(`${record.label} 绑定已保存`);
    } finally {
      savingCapability.value = '';
    }
  }

  async function saveAll() {
    savingAll.value = true;
    try {
      const result = await updateAiBindings(rows.value.map(toPayload));
      if (result?.bindings) applyBindings(result.bindings);
      Message.success('全部能力绑定已保存');
    } finally {
      savingAll.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped>
  .tips {
    margin-bottom: 14px;
  }
  .footer-hint {
    display: block;
    margin-top: 12px;
    font-size: 12px;
  }
</style>
