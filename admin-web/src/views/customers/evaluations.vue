<template>
  <a-card :bordered="false">
    <template #title>服务评价</template>
    <a-grid :cols="4" :col-gap="12" :row-gap="12" class="cards">
      <a-grid-item>
        <div class="stat">
          <small>整体好评率</small>
          <strong>{{ Math.round(Number(summary.satisfaction_rate || 0) * 100) }}<span>%</span></strong>
        </div>
      </a-grid-item>
      <a-grid-item>
        <a-statistic title="评价总数" :value="summary.total || 0" />
      </a-grid-item>
      <a-grid-item>
        <a-statistic title="非常满意" :value="summary.positive || 0" />
      </a-grid-item>
      <a-grid-item>
        <a-statistic title="不满意" :value="summary.negative || 0" />
      </a-grid-item>
    </a-grid>

    <a-typography-title :heading="6" class="section">按客服汇总</a-typography-title>
    <a-table :data="byAgent" :loading="loading" :pagination="false" row-key="agent_id">
      <template #columns>
        <a-table-column title="客服">
          <template #cell="{ record }">{{ agentName(record.agent_id) }}</template>
        </a-table-column>
        <a-table-column title="真实好评率">
          <template #cell="{ record }">{{ percent(record.satisfaction_rate) }}</template>
        </a-table-column>
        <a-table-column title="展示好评率">
          <template #cell="{ record }">{{ record.display_satisfaction_rate != null ? percent(record.display_satisfaction_rate) : '跟随真实' }}</template>
        </a-table-column>
        <a-table-column title="评价数" data-index="total" />
      </template>
    </a-table>

    <a-typography-title :heading="6" class="section">最近评价</a-typography-title>
    <a-table :data="recent" :loading="loading" :pagination="{ pageSize: 20 }" row-key="id">
      <template #columns>
        <a-table-column title="评分">
          <template #cell="{ record }">
            <a-rate :model-value="Number(record.score || 0)" readonly />
          </template>
        </a-table-column>
        <a-table-column title="标签">
          <template #cell="{ record }">{{ (record.tags || []).join('、') || '-' }}</template>
        </a-table-column>
        <a-table-column title="评价内容">
          <template #cell="{ record }">
            <span :class="{ negative: Number(record.score || 0) <= 3 }">{{ record.comment || '访客未填写文字评价' }}</span>
          </template>
        </a-table-column>
        <a-table-column title="会话" data-index="conversation_id" />
        <a-table-column title="时间">
          <template #cell="{ record }">{{ localTime(record.created_at) }}</template>
        </a-table-column>
      </template>
    </a-table>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import { fetchAgents, fetchRatingSummaries, fetchRatingSummary, fetchRecentRatings } from '@/api/admin';

  const loading = ref(false);
  const summary = ref<any>({});
  const byAgent = ref<any[]>([]);
  const recent = ref<any[]>([]);
  const agents = ref<any[]>([]);

  function percent(value: number) {
    return `${Math.round(Number(value || 0) * 100)}%`;
  }
  function agentName(id: string) {
    const agent = agents.value.find((item) => item.id === id);
    return agent ? `${agent.name}（${agent.account}）` : id || '-';
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  onMounted(async () => {
    loading.value = true;
    try {
      const [overall, byAgentPayload, recentPayload, agentPayload] = await Promise.all([
        fetchRatingSummary(),
        fetchRatingSummaries(),
        fetchRecentRatings(200),
        fetchAgents(),
      ]);
      summary.value = overall;
      byAgent.value = byAgentPayload.items || [];
      recent.value = recentPayload.ratings || [];
      agents.value = agentPayload.agents || [];
    } finally {
      loading.value = false;
    }
  });
</script>

<style scoped>
  .cards {
    margin-bottom: 18px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    small {
      color: var(--color-text-3);
      font-size: 13px;
    }
    strong {
      color: var(--color-text-1);
      font-size: 26px;
      font-weight: 700;
      span {
        margin-left: 2px;
        font-size: 16px;
      }
    }
  }
  .section {
    margin: 16px 0 8px;
  }
  .negative {
    color: rgb(var(--red-6));
    font-weight: 600;
  }
</style>
