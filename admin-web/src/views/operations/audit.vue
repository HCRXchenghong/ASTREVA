<template>
  <a-card :bordered="false">
    <template #title>审计日志</template>
    <template #extra>
      <a-space>
        <a-input-search v-model="keyword" placeholder="搜索动作、对象、操作者、IP" style="width: 260px" allow-clear />
        <a-select v-model="source" :options="sourceOptions" style="width: 140px" />
        <a-button @click="exportCsv">导出 CSV</a-button>
        <a-button @click="load">刷新</a-button>
      </a-space>
    </template>
    <a-table :data="filtered" :loading="loading" :pagination="{ pageSize: 30 }" row-key="key" @row-click="openDetail">
      <template #columns>
        <a-table-column title="时间">
          <template #cell="{ record }">{{ localTime(record.createdAt) }}</template>
        </a-table-column>
        <a-table-column title="来源">
          <template #cell="{ record }">{{ record.source === 'servicebridge' ? '客服系统' : '官网后台' }}</template>
        </a-table-column>
        <a-table-column title="操作者">
          <template #cell="{ record }">{{ record.actorEmail || record.actorKind || '-' }}</template>
        </a-table-column>
        <a-table-column title="动作">
          <template #cell="{ record }">
            <a-tag :color="riskColor(record.risk)">{{ riskLabel(record.risk) }}</a-tag>
            {{ record.action }}
          </template>
        </a-table-column>
        <a-table-column title="对象">
          <template #cell="{ record }">{{ record.resource }}{{ record.resourceId ? ` / ${record.resourceId}` : '' }}</template>
        </a-table-column>
        <a-table-column title="IP" data-index="ip" />
      </template>
    </a-table>

    <a-modal v-model:visible="detailVisible" title="审计详情" :footer="false" width="640px">
      <a-descriptions :column="1" bordered>
        <a-descriptions-item v-for="item in detailRows" :key="item[0]" :label="item[0]">{{ item[1] }}</a-descriptions-item>
      </a-descriptions>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { fetchBridgeAudit, fetchSiteAudit } from '@/api/admin';

  const events = ref<any[]>([]);
  const loading = ref(false);
  const keyword = ref('');
  const source = ref('');
  const detailVisible = ref(false);
  const detail = ref<any>(null);
  const sourceOptions = [
    { label: '全部来源', value: '' },
    { label: '客服系统', value: 'servicebridge' },
    { label: '官网后台', value: 'site-admin' },
  ];

  const filtered = computed(() => {
    const query = keyword.value.trim().toLowerCase();
    return events.value.filter((event) => {
      if (source.value && event.source !== source.value) return false;
      if (!query) return true;
      return [event.action, event.resource, event.resourceId, event.actorEmail, event.actorId, event.ip, event.description]
        .join(' ')
        .toLowerCase()
        .includes(query);
    });
  });

  const detailRows = computed(() => {
    const event = detail.value || {};
    return [
      ['时间', localTime(event.createdAt)],
      ['来源', event.source],
      ['操作者', event.actorEmail || event.actorId || event.actorKind],
      ['动作', event.action],
      ['对象', `${event.resource || '-'}${event.resourceId ? ` / ${event.resourceId}` : ''}`],
      ['IP', event.ip || '-'],
      ['User-Agent', event.userAgent || '-'],
      ['说明', event.description || '-'],
      ['详情', JSON.stringify(event.detail || {}, null, 2)],
    ];
  });

  function riskLabel(risk: string) {
    return { high: '高', medium: '中', low: '低' }[risk] || '低';
  }
  function riskColor(risk: string) {
    return { high: 'red', medium: 'orange', low: 'gray' }[risk] || 'gray';
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  function openDetail(record: any) {
    detail.value = record;
    detailVisible.value = true;
  }

  async function load() {
    loading.value = true;
    try {
      const [bridge, site] = await Promise.all([fetchBridgeAudit(200), fetchSiteAudit(500)]);
      const bridgeEvents = (bridge.events || []).map((event: any) => ({
        key: `sb-${event.id}`,
        source: 'servicebridge',
        id: event.id,
        action: event.action,
        resource: event.resource,
        resourceId: event.resource_id,
        actorKind: event.actor_kind,
        actorId: event.actor_id,
        actorEmail: event.actor_email,
        ip: event.ip_address,
        userAgent: event.user_agent,
        description: event.description,
        detail: event.detail,
        createdAt: event.created_at,
        risk: /delete|disable|reset|remove/i.test(event.action || '') ? 'high' : /update|create/i.test(event.action || '') ? 'medium' : 'low',
      }));
      const siteEvents = (site.data || []).map((event: any) => ({
        key: `site-${event.id}`,
        source: 'site-admin',
        id: event.id,
        action: event.action,
        resource: event.resource,
        resourceId: event.resourceId,
        actorKind: 'admin',
        actorEmail: event.actorEmail,
        ip: event.ipAddress,
        userAgent: event.userAgent,
        description: event.description,
        detail: event.detail,
        createdAt: event.createdAt,
        risk: /delete|disable|reset|remove/i.test(event.action || '') ? 'high' : 'medium',
      }));
      events.value = [...bridgeEvents, ...siteEvents].sort(
        (a, b) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime()
      );
    } finally {
      loading.value = false;
    }
  }

  function exportCsv() {
    const header = ['时间', '来源', '动作', '对象', '操作者', 'IP', '说明'];
    const cell = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`;
    const rows = filtered.value.map((item) => [
      item.createdAt,
      item.source,
      item.action,
      `${item.resource || ''}${item.resourceId ? `/${item.resourceId}` : ''}`,
      item.actorEmail || item.actorId,
      item.ip,
      item.description,
    ]);
    const blob = new Blob(['\uFEFF', [header, ...rows].map((row) => row.map(cell).join(',')).join('\r\n')], {
      type: 'text/csv;charset=utf-8',
    });
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = `audit-${new Date().toISOString().slice(0, 10)}.csv`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }

  onMounted(load);
</script>
