<template>
  <div class="leads-panel" data-testid="leads-panel">
    <div class="leads-head">
      <a-alert class="tips" type="info">
        表单线索来自官网表单提交（site_leads），与「会话询盘」（客服引擎 conversations）是两个独立来源，互不影响。
      </a-alert>
      <a-space class="leads-actions">
        <a-select
          v-model="statusFilter"
          :options="statusOptions"
          placeholder="按状态筛选"
          allow-clear
          style="width: 150px"
        />
        <a-button @click="exportCsv">导出 CSV</a-button>
        <a-button :loading="loading" data-testid="leads-refresh" @click="load">
          刷新
        </a-button>
      </a-space>
    </div>

    <a-table
      :data="filtered"
      :loading="loading"
      :pagination="{ pageSize: 20 }"
      row-key="id"
      data-testid="leads-table"
    >
      <template #columns>
        <a-table-column title="提交时间" :width="180">
          <template #cell="{ record }">{{ localTime(record.submittedAt) }}</template>
        </a-table-column>
        <a-table-column title="姓名 / 公司">
          <template #cell="{ record }">
            <div>{{ record.name || '-' }}</div>
            <a-typography-text type="secondary">
              {{ record.companyName || '-' }}
            </a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="联系方式">
          <template #cell="{ record }">
            {{ record.contactInfo || record.phone || record.email || '-' }}
          </template>
        </a-table-column>
        <a-table-column title="留言" data-index="message" :ellipsis="true" />
        <a-table-column title="来源" data-index="sourcePage" :width="120" />
        <a-table-column title="状态" :width="100">
          <template #cell="{ record }">
            <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="优先级" data-index="priority" :width="90" />
        <a-table-column title="操作" :width="90">
          <template #cell="{ record }">
            <a-link :data-testid="`lead-edit-${record.id}`" @click="openEditor(record)">
              处理
            </a-link>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="暂无表单线索" />
      </template>
    </a-table>

    <a-modal
      v-model:visible="editorVisible"
      title="处理表单线索"
      :mask-closable="false"
      @ok="save"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="状态">
          <a-select
            v-model="form.status"
            :options="statusOptions.filter((item) => item.value)"
          />
        </a-form-item>
        <a-form-item label="优先级">
          <a-select v-model="form.priority" :options="priorityOptions" />
        </a-form-item>
        <a-form-item label="内部备注">
          <a-textarea v-model="form.internalNotes" :auto-size="{ minRows: 4 }" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchBootstrap, updateLead } from '@/api/admin';

  const leads = ref<any[]>([]);
  const loading = ref(false);
  const statusFilter = ref('');
  const editorVisible = ref(false);
  const form = reactive({
    id: '',
    status: '新咨询',
    priority: '普通',
    internalNotes: '',
  });
  const statusOptions = [
    { label: '全部状态', value: '' },
    { label: '新咨询', value: '新咨询' },
    { label: '处理中', value: '处理中' },
    { label: '已联系', value: '已联系' },
    { label: '已关闭', value: '已关闭' },
  ];
  const priorityOptions = ['低', '普通', '高', '紧急'].map((value) => ({
    label: value,
    value,
  }));

  const filtered = computed(() =>
    statusFilter.value
      ? leads.value.filter((item) => item.status === statusFilter.value)
      : leads.value
  );

  function statusColor(value: string) {
    return (
      { 新咨询: 'arcoblue', 处理中: 'orange', 已联系: 'green', 已关闭: 'gray' }[
        value
      ] || 'gray'
    );
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  async function load() {
    loading.value = true;
    try {
      const payload = await fetchBootstrap();
      leads.value = payload.data?.leads || [];
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record: any) {
    Object.assign(form, {
      id: record.id,
      status: record.status || '新咨询',
      priority: record.priority || '普通',
      internalNotes: record.internalNotes || '',
    });
    editorVisible.value = true;
  }

  async function save() {
    await updateLead(form.id, {
      status: form.status,
      priority: form.priority,
      internalNotes: form.internalNotes,
    });
    editorVisible.value = false;
    Message.success('已保存');
    await load();
  }

  function exportCsv() {
    const header = [
      '提交时间',
      '姓名',
      '公司',
      '联系方式',
      '留言',
      '来源',
      '状态',
      '优先级',
      '内部备注',
    ];
    const cell = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`;
    const rows = filtered.value.map((item) => [
      item.submittedAt,
      item.name,
      item.companyName,
      item.contactInfo,
      item.message,
      item.sourcePage,
      item.status,
      item.priority,
      item.internalNotes,
    ]);
    const blob = new Blob(
      [
        '\uFEFF',
        [header, ...rows].map((row) => row.map(cell).join(',')).join('\r\n'),
      ],
      { type: 'text/csv;charset=utf-8' }
    );
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = `leads-${new Date().toISOString().slice(0, 10)}.csv`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .leads-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
  }
  .tips {
    flex: 1 1 420px;
  }
  .leads-actions {
    flex: none;
  }
</style>
