<template>
  <a-card :bordered="false">
    <template #title>关键词规则</template>
    <template #extra>
      <a-button type="primary" @click="openEditor()">新增规则</a-button>
    </template>
    <a-table :data="rules" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="关键词" data-index="keyword" />
        <a-table-column title="匹配方式">
          <template #cell="{ record }">{{ matchLabel(record.match_type) }}</template>
        </a-table-column>
        <a-table-column title="动作">
          <template #cell="{ record }">{{ actionLabel(record.action) }}</template>
        </a-table-column>
        <a-table-column title="回复内容" data-index="reply" />
        <a-table-column title="优先级" data-index="priority" />
        <a-table-column title="快捷回复">
          <template #cell="{ record }">{{ record.show_in_quick_replies ? '是' : '否' }}</template>
        </a-table-column>
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">{{ record.enabled ? '启用' : '停用' }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="150">
          <template #cell="{ record }">
            <a-space>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link status="danger" @click="remove(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="editorVisible" :title="form.id ? '编辑规则' : '新增规则'" @ok="save">
      <a-form :model="form" layout="vertical">
        <a-form-item label="关键词" required>
          <a-input v-model="form.keyword" />
        </a-form-item>
        <a-form-item label="匹配方式">
          <a-select v-model="form.match_type" :options="matchOptions" />
        </a-form-item>
        <a-form-item label="动作">
          <a-select v-model="form.action" :options="actionOptions" />
        </a-form-item>
        <a-form-item label="优先级">
          <a-input-number v-model="form.priority" :min="-1000" :max="1000" />
        </a-form-item>
        <a-form-item label="回复内容">
          <a-textarea v-model="form.reply" :auto-size="{ minRows: 3 }" />
        </a-form-item>
        <a-form-item label="快捷回复按钮文字">
          <a-textarea v-model="form.quick_reply_text" :auto-size="{ minRows: 2 }" />
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-checkbox v-model="form.enabled">启用规则</a-checkbox>
            <a-checkbox v-model="form.show_in_quick_replies">显示为快捷回复</a-checkbox>
          </a-space>
        </a-form-item>
      </a-form>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { createKeywordRule, deleteKeywordRule, fetchKeywordRules, updateKeywordRule } from '@/api/admin';

  const rules = ref<any[]>([]);
  const loading = ref(false);
  const editorVisible = ref(false);
  const form = reactive({
    id: '',
    keyword: '',
    match_type: 'contains',
    action: 'text',
    priority: 50,
    reply: '',
    quick_reply_text: '',
    enabled: true,
    show_in_quick_replies: false,
  });
  const matchOptions = [
    { label: '包含', value: 'contains' },
    { label: '完全匹配', value: 'exact' },
    { label: '前缀匹配', value: 'prefix' },
  ];
  const actionOptions = [
    { label: '发送文本', value: 'text' },
    { label: '发送电话', value: 'telephone' },
    { label: '发送 WhatsApp', value: 'whatsapp' },
    { label: '发送 Telegram', value: 'telegram' },
    { label: '转人工', value: 'handoff' },
  ];

  function matchLabel(value: string) {
    return matchOptions.find((item) => item.value === value)?.label || value;
  }
  function actionLabel(value: string) {
    return actionOptions.find((item) => item.value === value)?.label || value;
  }

  async function load() {
    loading.value = true;
    try {
      rules.value = (await fetchKeywordRules()).rules || [];
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    Object.assign(form, {
      id: record?.id || '',
      keyword: record?.keyword || '',
      match_type: record?.match_type || 'contains',
      action: record?.action || 'text',
      priority: record?.priority ?? 50,
      reply: record?.reply || '',
      quick_reply_text: record?.quick_reply_text || '',
      enabled: record?.enabled ?? true,
      show_in_quick_replies: record?.show_in_quick_replies ?? false,
    });
    editorVisible.value = true;
  }

  async function save() {
    if (!form.keyword.trim()) {
      Message.warning('请填写关键词');
      return;
    }
    const payload = { ...form, priority: Number(form.priority) };
    delete (payload as any).id;
    if (form.id) await updateKeywordRule(form.id, payload);
    else await createKeywordRule(payload);
    editorVisible.value = false;
    Message.success('已保存');
    await load();
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除规则',
      content: `确定删除关键词「${record.keyword}」？`,
      onOk: async () => {
        await deleteKeywordRule(record.id);
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>
