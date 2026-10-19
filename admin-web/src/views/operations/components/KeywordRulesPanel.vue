<template>
  <div class="keyword-panel" data-testid="keyword-rules-panel">
    <div class="panel-head">
      <a-space>
        <a-typography-text type="secondary">
          共 {{ rules.length }} 条规则；命中的规则按优先级高的先回复。
        </a-typography-text>
      </a-space>
      <a-space>
        <a-button
          :loading="loading"
          size="small"
          data-testid="keyword-rules-refresh"
          @click="load"
        >
          刷新
        </a-button>
        <a-button
          type="primary"
          size="small"
          data-testid="keyword-rule-add"
          @click="openEditor()"
        >
          新增规则
        </a-button>
      </a-space>
    </div>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-table
      :data="rules"
      :loading="loading"
      :pagination="false"
      row-key="id"
      size="small"
      data-testid="keyword-rules-table"
    >
      <template #columns>
        <a-table-column title="关键词" data-index="keyword" :width="150" />
        <a-table-column title="匹配方式" :width="110">
          <template #cell="{ record }">{{ matchLabel(record.match_type) }}</template>
        </a-table-column>
        <a-table-column title="动作" :width="120">
          <template #cell="{ record }">{{ actionLabel(record.action) }}</template>
        </a-table-column>
        <a-table-column title="回复内容" data-index="reply" ellipsis />
        <a-table-column title="优先级" data-index="priority" :width="90" />
        <a-table-column title="快捷回复" :width="100">
          <template #cell="{ record }">
            {{ record.show_in_quick_replies ? '是' : '否' }}
          </template>
        </a-table-column>
        <a-table-column title="状态" :width="90">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">
              {{ record.enabled ? '启用' : '停用' }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="130">
          <template #cell="{ record }">
            <a-space>
              <a-link :data-testid="`keyword-rule-edit-${record.id}`" @click="openEditor(record)">
                编辑
              </a-link>
              <a-link
                status="danger"
                :data-testid="`keyword-rule-delete-${record.id}`"
                @click="remove(record)"
              >
                删除
              </a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="还没有关键词规则" />
      </template>
    </a-table>

    <a-modal
      v-model:visible="editorVisible"
      :title="form.id ? '编辑关键词规则' : '新增关键词规则'"
      :ok-loading="saving"
      :mask-closable="false"
      width="620px"
      unmount-on-close
      @ok="save"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="form" layout="vertical">
        <a-form-item label="关键词" required>
          <a-input
            v-model="form.keyword"
            placeholder="例如：价格"
            :input-attrs="{ 'data-testid': 'keyword-rule-keyword' }"
          />
        </a-form-item>
        <a-form-item label="匹配方式">
          <a-select
            v-model="form.match_type"
            :options="matchOptions"
            data-testid="keyword-rule-match"
          />
        </a-form-item>
        <a-form-item label="动作">
          <a-select
            v-model="form.action"
            :options="actionOptions"
            data-testid="keyword-rule-action"
          />
        </a-form-item>
        <a-form-item label="优先级（越大越先匹配）">
          <a-input-number
            v-model="form.priority"
            :min="-1000"
            :max="1000"
            :input-attrs="{ 'data-testid': 'keyword-rule-priority' }"
          />
        </a-form-item>
        <a-form-item label="回复内容">
          <a-textarea
            v-model="form.reply"
            :auto-size="{ minRows: 3 }"
            placeholder="命中后回复给访客的内容"
            :textarea-attrs="{ 'data-testid': 'keyword-rule-reply' }"
          />
        </a-form-item>
        <a-form-item label="快捷回复按钮文字">
          <a-textarea
            v-model="form.quick_reply_text"
            :auto-size="{ minRows: 2 }"
            placeholder="留空表示不生成快捷回复按钮"
          />
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-checkbox v-model="form.enabled">启用规则</a-checkbox>
            <a-checkbox v-model="form.show_in_quick_replies">显示为快捷回复</a-checkbox>
          </a-space>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createKeywordRule,
    deleteKeywordRule,
    fetchKeywordRules,
    updateKeywordRule,
  } from '@/api/admin';

  const emit = defineEmits<{ (e: 'changed', total: number): void }>();

  const rules = ref<any[]>([]);
  const loading = ref(false);
  const loadError = ref('');
  const saving = ref(false);
  const saveError = ref('');
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

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const payload = await fetchKeywordRules();
      rules.value = Array.isArray(payload?.rules) ? payload.rules : [];
      emit('changed', rules.value.length);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '关键词规则加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    saveError.value = '';
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
    if (!String(form.keyword ?? '').trim()) {
      Message.warning('请填写关键词');
      return;
    }
    const payload: Record<string, unknown> = {
      keyword: String(form.keyword).trim(),
      match_type: form.match_type,
      action: form.action,
      priority: Number(form.priority) || 0,
      reply: String(form.reply ?? ''),
      quick_reply_text: String(form.quick_reply_text ?? ''),
      enabled: Boolean(form.enabled),
      show_in_quick_replies: Boolean(form.show_in_quick_replies),
    };
    saving.value = true;
    saveError.value = '';
    try {
      if (form.id) await updateKeywordRule(form.id, payload);
      else await createKeywordRule(payload);
      editorVisible.value = false;
      Message.success('已保存');
      await load();
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '保存失败，请稍后重试。');
    } finally {
      saving.value = false;
    }
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

<style scoped lang="less">
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .banner {
    margin-bottom: 12px;
  }
</style>
