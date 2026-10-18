<template>
  <div class="lead-panel">
    <a-alert type="info" show-icon class="tips">
      咨询单来自官网联系表单。这里可以维护处理状态、优先级与内部备注；保存只更新这一条记录，不会重建官网。
    </a-alert>

    <a-row :gutter="16">
      <a-col :xs="24" :md="10">
        <a-card :bordered="false" title="咨询单列表" class="list-card">
          <a-empty v-if="!leads.length" description="暂无客户咨询" />
          <div v-else class="lead-list">
            <button
              v-for="(lead, index) in leads"
              :key="lead.id || index"
              type="button"
              class="lead-item"
              :class="{ active: index === selectedIndex }"
              @click="select(index)"
            >
              <div class="lead-item-head">
                <strong>{{ lead.name || '未命名' }}</strong>
                <a-tag :color="statusColor(lead.status)" size="small">
                  {{ lead.status || '新咨询' }}
                </a-tag>
              </div>
              <div class="lead-item-sub">
                {{ lead.contactInfo || lead.phone || lead.email || '无联系方式' }}
              </div>
              <div class="lead-item-sub">
                {{ localTime(lead.submittedAt) }} · {{ lead.sourcePage || '未知来源' }}
              </div>
            </button>
          </div>
        </a-card>
      </a-col>

      <a-col :xs="24" :md="14">
        <a-card :bordered="false" :title="form.id ? '处理咨询单' : '选择左侧咨询单'">
          <a-empty v-if="!form.id" description="请选择左侧咨询单进行编辑" />
          <template v-else>
            <a-descriptions :column="1" bordered size="small" class="facts">
              <a-descriptions-item label="姓名">{{ current?.name || '-' }}</a-descriptions-item>
              <a-descriptions-item label="公司">{{ current?.companyName || '-' }}</a-descriptions-item>
              <a-descriptions-item label="联系方式">
                {{ current?.contactInfo || current?.phone || current?.email || '-' }}
              </a-descriptions-item>
              <a-descriptions-item label="来源">{{ current?.sourcePage || '-' }}</a-descriptions-item>
              <a-descriptions-item label="留言">
                <span class="message">{{ current?.message || '-' }}</span>
              </a-descriptions-item>
            </a-descriptions>

            <a-form :model="form" layout="vertical">
              <a-form-item label="状态">
                <a-select v-model="form.status" :options="statusOptions" />
              </a-form-item>
              <a-form-item label="优先级">
                <a-select v-model="form.priority" :options="priorityOptions" />
              </a-form-item>
              <a-form-item label="内部备注">
                <a-textarea
                  v-model="form.internalNotes"
                  :auto-size="{ minRows: 4, maxRows: 10 }"
                  placeholder="仅后台可见"
                />
              </a-form-item>
            </a-form>
            <a-space>
              <a-button type="primary" :loading="saving" @click="submit">保存咨询单</a-button>
              <a-button :disabled="saving" @click="resetForm">重置</a-button>
            </a-space>
          </template>
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script lang="ts" setup>
  import { computed, reactive, ref, watch } from 'vue';

  const props = withDefaults(
    defineProps<{
      leads?: Record<string, any>[];
      saving?: boolean;
    }>(),
    { leads: () => [], saving: false }
  );

  const emit = defineEmits<{
    (e: 'save', payload: { id: string; status: string; priority: string; internalNotes: string }): void;
  }>();

  const statusOptions = ['新咨询', '处理中', '已联系', '已关闭'].map((value) => ({
    label: value,
    value,
  }));
  const priorityOptions = ['低', '普通', '高', '紧急'].map((value) => ({ label: value, value }));

  const selectedIndex = ref(0);
  const form = reactive({ id: '', status: '新咨询', priority: '普通', internalNotes: '' });

  const current = computed(() => props.leads[selectedIndex.value] || null);

  watch(
    () => props.leads,
    () => {
      if (selectedIndex.value >= props.leads.length) selectedIndex.value = 0;
      resetForm();
    },
    { deep: true }
  );

  function statusColor(value?: string) {
    return (
      { 新咨询: 'arcoblue', 处理中: 'orange', 已联系: 'green', 已关闭: 'gray' }[
        String(value || '')
      ] || 'gray'
    );
  }

  function localTime(value?: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  function select(index: number) {
    selectedIndex.value = index;
    resetForm();
  }

  function resetForm() {
    const lead = current.value || {};
    form.id = lead.id || '';
    form.status = lead.status || '新咨询';
    form.priority = lead.priority || '普通';
    form.internalNotes = lead.internalNotes || '';
  }

  function submit() {
    if (!form.id) return;
    emit('save', {
      id: form.id,
      status: form.status,
      priority: form.priority,
      internalNotes: form.internalNotes,
    });
  }

  resetForm();
</script>

<style scoped lang="less">
  .lead-panel {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .tips {
    margin-bottom: 0;
  }
  .list-card :deep(.arco-card-body) {
    padding: 0;
  }
  .lead-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 62vh;
    overflow: auto;
    padding: 4px;
  }
  .lead-item {
    display: flex;
    flex-direction: column;
    gap: 4px;
    text-align: left;
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: #fff;
    cursor: pointer;
  }
  .lead-item:hover {
    border-color: rgb(var(--primary-5));
  }
  .lead-item.active {
    border-color: rgb(var(--primary-6));
    background: rgb(var(--primary-1));
  }
  .lead-item-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .lead-item-sub {
    font-size: 12px;
    color: var(--color-text-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .facts {
    margin-bottom: 16px;
  }
  .message {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
