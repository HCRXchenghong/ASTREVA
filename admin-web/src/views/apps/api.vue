<template>
  <a-card :bordered="false">
    <template #title>API 管理</template>
    <template #extra>
      <a-space>
        <a-button @click="openDocs">接入说明</a-button>
        <a-button type="primary" @click="openEditor()">新增授权</a-button>
      </a-space>
    </template>
    <a-alert class="tips" type="info">
      第三方平台通过 Open API 接入客服引擎：创建会话、代用户收发消息，并用 WebSocket 实时接收 AI 与人工客服回复。
      Key 只在创建/重置时显示一次，系统仅保存 SHA-256 哈希。
    </a-alert>
    <a-table :data="apps" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="平台名称" data-index="name" />
        <a-table-column title="Key 前缀">
          <template #cell="{ record }"><code>{{ record.key_prefix }}…</code></template>
        </a-table-column>
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">{{ record.enabled ? '已启用' : '已禁用' }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="限速/分钟">
          <template #cell="{ record }">{{ Number(record.rate_limit) > 0 ? record.rate_limit : '默认 300' }}</template>
        </a-table-column>
        <a-table-column title="累计调用" data-index="request_count" />
        <a-table-column title="最近使用">
          <template #cell="{ record }">{{ localTime(record.last_used_at) }}</template>
        </a-table-column>
        <a-table-column title="备注" data-index="note" />
        <a-table-column title="操作" :width="240">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link @click="resetKey(record)">重置 Key</a-link>
              <a-link @click="toggle(record)">{{ record.enabled ? '禁用' : '启用' }}</a-link>
              <a-link status="danger" @click="remove(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="editorVisible" :title="form.id ? '编辑授权' : '新增授权'" @ok="save">
      <a-form :model="form" layout="vertical">
        <a-form-item label="平台名称" required>
          <a-input v-model="form.name" placeholder="例如：Shopify 独立站" />
        </a-form-item>
        <a-form-item label="限速（次/分钟，0=默认 300）">
          <a-input-number v-model="form.rate_limit" :min="0" />
        </a-form-item>
        <a-form-item label="备注">
          <a-textarea v-model="form.note" :auto-size="{ minRows: 2 }" />
        </a-form-item>
        <a-form-item>
          <a-checkbox v-model="form.enabled">启用授权</a-checkbox>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="keyVisible" title="API Key 已生成" :footer="false">
      <a-alert type="warning" class="tips">完整 Key 只显示这一次，关闭后系统只保存哈希，请立即复制保存。</a-alert>
      <a-typography-paragraph copyable code class="key">{{ apiKey }}</a-typography-paragraph>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    apiDocsUrl,
    createApiApp,
    deleteApiApp,
    fetchApiApps,
    resetApiAppKey,
    updateApiApp,
  } from '@/api/admin';

  const apps = ref<any[]>([]);
  const loading = ref(false);
  const editorVisible = ref(false);
  const keyVisible = ref(false);
  const apiKey = ref('');
  const form = reactive({ id: '', name: '', rate_limit: 0, note: '', enabled: true });

  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }
  function openDocs() {
    window.open(apiDocsUrl(), '_blank');
  }

  async function load() {
    loading.value = true;
    try {
      apps.value = (await fetchApiApps()).apps || [];
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    Object.assign(form, {
      id: record?.id || '',
      name: record?.name || '',
      rate_limit: record?.rate_limit || 0,
      note: record?.note || '',
      enabled: record?.enabled ?? true,
    });
    editorVisible.value = true;
  }

  async function save() {
    if (!form.name.trim()) {
      Message.warning('请填写平台名称');
      return;
    }
    const payload = { name: form.name, rate_limit: Number(form.rate_limit), note: form.note, enabled: form.enabled };
    if (form.id) {
      await updateApiApp(form.id, payload);
    } else {
      const created = await createApiApp(payload);
      if (created.api_key) {
        apiKey.value = created.api_key;
        keyVisible.value = true;
      }
    }
    editorVisible.value = false;
    await load();
    Message.success('已保存');
  }

  function resetKey(record: any) {
    Modal.confirm({
      title: '重置 Key',
      content: `重置后 ${record.name} 的旧 Key 立即失效。`,
      onOk: async () => {
        const payload = await resetApiAppKey(record.id);
        apiKey.value = payload.api_key || '';
        keyVisible.value = true;
        await load();
      },
    });
  }

  async function toggle(record: any) {
    await updateApiApp(record.id, { enabled: !record.enabled });
    await load();
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除授权',
      content: `删除后 ${record.name} 将无法再调用开放接口。`,
      onOk: async () => {
        await deleteApiApp(record.id);
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
  .key {
    margin-top: 8px;
    word-break: break-all;
  }
</style>
