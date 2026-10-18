<template>
  <a-card :bordered="false">
    <template #title>客服账号</template>
    <template #extra>
      <a-button type="primary" @click="openEditor()">新增客服账号</a-button>
    </template>
    <a-table :data="agents" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="账号 / 姓名">
          <template #cell="{ record }">
            <div><strong>{{ record.name }}</strong></div>
            <a-typography-text type="secondary">{{ record.account }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="分组" data-index="group" />
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.disabled_at ? 'red' : record.status === 'online' ? 'green' : 'gray'">
              {{ record.disabled_at ? '已禁用' : statusLabel(record.status) }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="当前会话">
          <template #cell="{ record }">{{ record.current_conversations }} / {{ record.max_conversations }}</template>
        </a-table-column>
        <a-table-column title="真实好评率">
          <template #cell="{ record }">{{ summaryOf(record.id)?.satisfaction_rate != null ? percent(summaryOf(record.id).satisfaction_rate) : '-' }}</template>
        </a-table-column>
        <a-table-column title="展示好评率">
          <template #cell="{ record }">{{ record.manual_satisfaction_rate != null ? Math.round(record.manual_satisfaction_rate * 100) + '%' : '跟随真实' }}</template>
        </a-table-column>
        <a-table-column title="操作" :width="330">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link @click="openRating(record)">好评率</a-link>
              <a-link @click="openDetail(record)">详情</a-link>
              <a-link @click="onResetPassword(record)">重置密码</a-link>
              <a-link v-if="!record.disabled_at" status="warning" @click="onDisable(record)">禁用</a-link>
              <a-link status="danger" @click="onDelete(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="editorVisible" :title="form.id ? '编辑客服' : '新增客服账号'" @ok="saveAgent">
      <a-form :model="form" layout="vertical">
        <a-form-item label="登录账号" required>
          <a-input v-model="form.account" :disabled="Boolean(form.id)" />
        </a-form-item>
        <a-form-item label="客服姓名" required>
          <a-input v-model="form.name" />
        </a-form-item>
        <a-form-item label="分组">
          <a-input v-model="form.group" />
        </a-form-item>
        <a-form-item label="最大并发会话" required>
          <a-input-number v-model="form.max_conversations" :min="1" />
        </a-form-item>
        <a-form-item v-if="!form.id" label="初始密码（至少 10 位）" required>
          <a-input-password v-model="form.password" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="ratingVisible" title="设置展示好评率" @ok="saveRating">
      <a-form layout="vertical">
        <a-form-item label="展示好评率（0-100）">
          <a-input-number v-model="ratingValue" :min="0" :max="100" />
        </a-form-item>
        <a-space>
          <a-button size="small" @click="clearManualRating">清除手动值</a-button>
          <a-button size="small" status="danger" @click="resetRatings">重置该客服全部评价</a-button>
        </a-space>
      </a-form>
    </a-modal>

    <a-drawer v-model:visible="detailVisible" :width="520" title="客服详情">
      <a-descriptions :column="1" bordered>
        <a-descriptions-item label="账号">{{ detail?.agent?.account }}</a-descriptions-item>
        <a-descriptions-item label="姓名">{{ detail?.agent?.name }}</a-descriptions-item>
        <a-descriptions-item label="真实好评率">{{ detail?.summary ? percent(detail.summary.satisfaction_rate) : '-' }}</a-descriptions-item>
        <a-descriptions-item label="评价总数">{{ detail?.summary?.total || 0 }}</a-descriptions-item>
      </a-descriptions>
      <a-table :data="detail?.ratings || []" :pagination="false" row-key="id" class="rating-table">
        <template #columns>
          <a-table-column title="评分" data-index="score" />
          <a-table-column title="标签">
            <template #cell="{ record }">{{ (record.tags || []).join('、') || '-' }}</template>
          </a-table-column>
          <a-table-column title="评价" data-index="comment" />
          <a-table-column title="时间">
            <template #cell="{ record }">{{ localTime(record.created_at) }}</template>
          </a-table-column>
        </template>
      </a-table>
    </a-drawer>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createAgent,
    clearAgentManualRating,
    deleteAgent,
    disableAgent,
    fetchAgentRatings,
    fetchAgents,
    fetchRatingSummaries,
    resetAgentPassword,
    resetAgentRatings,
    setAgentManualRating,
    updateAgent,
  } from '@/api/admin';

  const agents = ref<any[]>([]);
  const loading = ref(false);
  const summaries = ref<Record<string, any>>({});
  const editorVisible = ref(false);
  const ratingVisible = ref(false);
  const detailVisible = ref(false);
  const ratingValue = ref(100);
  const ratingAgent = ref<any>(null);
  const detail = ref<any>(null);
  const form = reactive({
    id: '',
    account: '',
    name: '',
    group: '',
    max_conversations: 10,
    password: '',
  });

  function statusLabel(value: string) {
    return { online: '在线', busy: '忙碌', offline: '离线' }[value] || value;
  }
  function percent(value: number) {
    return `${Math.round(Number(value || 0) * 100)}%`;
  }
  function summaryOf(id: string) {
    return summaries.value[id];
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  async function load() {
    loading.value = true;
    try {
      const [agentPayload, summaryPayload] = await Promise.all([fetchAgents(), fetchRatingSummaries()]);
      agents.value = agentPayload.agents || [];
      summaries.value = Object.fromEntries((summaryPayload.items || []).map((item: any) => [item.agent_id, item]));
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    Object.assign(form, {
      id: record?.id || '',
      account: record?.account || '',
      name: record?.name || '',
      group: record?.group || '',
      max_conversations: record?.max_conversations || 10,
      password: '',
    });
    editorVisible.value = true;
  }

  async function saveAgent() {
    if (!form.name.trim()) {
      Message.warning('请填写客服姓名');
      return;
    }
    if (form.id) {
      await updateAgent(form.id, {
        name: form.name,
        group: form.group,
        max_conversations: Number(form.max_conversations),
      });
    } else {
      await createAgent({
        account: form.account,
        name: form.name,
        group: form.group,
        max_conversations: Number(form.max_conversations),
        password: form.password,
      });
    }
    editorVisible.value = false;
    Message.success('已保存');
    await load();
  }

  function openRating(record: any) {
    ratingAgent.value = record;
    const summary = summaryOf(record.id);
    ratingValue.value =
      record.manual_satisfaction_rate != null
        ? Math.round(Number(record.manual_satisfaction_rate) * 100)
        : Math.round(Number(summary?.display_satisfaction_rate ?? summary?.satisfaction_rate ?? 1) * 100);
    ratingVisible.value = true;
  }

  async function saveRating() {
    if (!ratingAgent.value) return;
    await setAgentManualRating(ratingAgent.value.id, Number(ratingValue.value));
    ratingVisible.value = false;
    Message.success('展示好评率已设置');
    await load();
  }

  async function clearManualRating() {
    if (!ratingAgent.value) return;
    await clearAgentManualRating(ratingAgent.value.id);
    ratingVisible.value = false;
    Message.success('已清除手动好评率');
    await load();
  }

  function resetRatings() {
    if (!ratingAgent.value) return;
    Modal.confirm({
      title: '重置评价',
      content: '将清空该客服的全部真实评价与手动好评率，不可恢复。',
      onOk: async () => {
        await resetAgentRatings(ratingAgent.value.id);
        ratingVisible.value = false;
        Message.success('已重置');
        await load();
      },
    });
  }

  async function openDetail(record: any) {
    detail.value = await fetchAgentRatings(record.id);
    detailVisible.value = true;
  }

  function onResetPassword(record: any) {
    Modal.confirm({
      title: '重置密码',
      content: '留空将自动生成临时密码。',
      onOk: async () => {
        const payload = await resetAgentPassword(record.id, '');
        Message.success(payload.temporary_password ? `临时密码：${payload.temporary_password}` : '密码已重置');
        await load();
      },
    });
  }

  function onDisable(record: any) {
    Modal.confirm({
      title: '禁用客服',
      content: `禁用 ${record.name} 后其登录态立即失效。`,
      onOk: async () => {
        await disableAgent(record.id);
        Message.success('已禁用');
        await load();
      },
    });
  }

  function onDelete(record: any) {
    Modal.confirm({
      title: '删除客服',
      content: `确定永久删除 ${record.name}？`,
      onOk: async () => {
        await deleteAgent(record.id);
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>

<style scoped>
  .rating-table {
    margin-top: 16px;
  }
</style>
