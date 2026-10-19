<template>
  <a-card :bordered="false" class="logs-page">
    <template #title>系统日志</template>
    <template #extra>
      <a-space v-if="activeTab === 'system'">
        <a-button :loading="loading" @click="load">刷新</a-button>
        <a-button :disabled="!rows.length" @click="exportCsv">导出 CSV</a-button>
      </a-space>
    </template>

    <a-tabs v-model:active-key="activeTab" lazy-load>
      <a-tab-pane key="system" title="系统日志">
        <div class="retention-panel">
          <a-form :model="retention" layout="inline" class="retention-form">
            <a-form-item label="事件保留天数">
              <a-input-number
                v-model="retention.eventDays"
                :min="30"
                :max="3650"
                :precision="0"
                :disabled="retentionLoading"
                :input-attrs="{ id: 'retention-event-days' }"
                placeholder="180"
                style="width: 130px"
              />
            </a-form-item>
            <a-form-item label="访问明细保留天数">
              <a-input-number
                v-model="retention.visitDays"
                :min="1"
                :max="365"
                :precision="0"
                :disabled="retentionLoading"
                :input-attrs="{ id: 'retention-visit-days' }"
                placeholder="30"
                style="width: 130px"
              />
            </a-form-item>
            <a-form-item>
              <a-button
                status="danger"
                :loading="purging"
                :disabled="retentionLoading"
                @click="confirmPurge"
              >
                立即清理
              </a-button>
            </a-form-item>
          </a-form>
          <a-typography-text type="secondary" class="retention-hint">
            默认 180 天 / 30 天，满足等保 ≥6 个月。事件保留天数范围 30–3650，访问明细 1–365；点击「立即清理」会按当前填写的天数删除过期数据，删除后不可恢复。
          </a-typography-text>
          <a-alert v-if="retentionError" class="banner" type="error" show-icon>
            {{ retentionError }}
            <template #action>
              <a-button size="mini" @click="loadRetention">重试</a-button>
            </template>
          </a-alert>
        </div>

        <a-form class="filters" :model="filters" layout="inline">
          <a-form-item label="等级">
            <a-select v-model="filters.level" :options="levelOptions" style="width: 120px" />
          </a-form-item>
          <a-form-item label="分类">
            <a-select v-model="filters.category" :options="categoryOptions" style="width: 140px" />
          </a-form-item>
          <a-form-item label="IP">
            <a-input v-model="filters.ip" placeholder="精确匹配 IP" allow-clear style="width: 160px" />
          </a-form-item>
          <a-form-item label="关键词">
            <a-input-search
              v-model="filters.q"
              placeholder="动作 / 消息 / 路径"
              allow-clear
              style="width: 220px"
              @search="load"
            />
          </a-form-item>
          <a-form-item label="时间范围">
            <a-select v-model="filters.range" :options="rangeOptions" style="width: 130px" />
          </a-form-item>
          <a-form-item v-if="filters.range === 'custom'" label="自定义">
            <a-range-picker v-model="customRange" value-format="YYYY-MM-DD" style="width: 240px" />
          </a-form-item>
          <a-form-item label="包含访客访问">
            <a-switch v-model="filters.includeVisits" />
          </a-form-item>
          <a-form-item label="包含爬虫">
            <a-switch v-model="filters.includeBots" />
          </a-form-item>
          <a-form-item>
            <a-button type="primary" @click="load">查询</a-button>
          </a-form-item>
        </a-form>

        <a-alert v-if="error" type="error" class="banner" show-icon>{{ error }}</a-alert>

        <a-table
          :data="rows"
          :loading="loading"
          :pagination="{ pageSize: 20, showTotal: true }"
          :row-class="rowClass"
          :scroll="{ x: 1280 }"
          row-key="id"
          @row-click="openDetail"
        >
          <template #columns>
            <a-table-column title="时间" :width="180">
              <template #cell="{ record }">{{ localTime(record.occurredAt) }}</template>
            </a-table-column>
            <a-table-column title="来源" :width="110">
              <template #cell="{ record }">
                <a-tag :color="record.source === 'visit' ? 'arcoblue' : 'purple'" size="small">
                  {{ sourceLabel(record.source) }}
                </a-tag>
              </template>
            </a-table-column>
            <a-table-column title="等级" :width="90">
              <template #cell="{ record }">
                <a-tag :color="levelColor(record.level)" size="small">{{ levelLabel(record.level) }}</a-tag>
              </template>
            </a-table-column>
            <a-table-column title="分类" :width="110">
              <template #cell="{ record }">{{ categoryLabel(record.category) }}</template>
            </a-table-column>
            <a-table-column title="动作" :width="130" data-index="action" />
            <a-table-column title="消息" :width="260" ellipsis tooltip>
              <template #cell="{ record }">{{ record.message || '-' }}</template>
            </a-table-column>
            <a-table-column title="IP" :width="140" data-index="ip" />
            <a-table-column title="浏览器 / 系统" :width="200" ellipsis tooltip>
              <template #cell="{ record }">{{ browserText(record) }}</template>
            </a-table-column>
          </template>
          <template #empty>
            <a-empty description="没有匹配的日志" />
          </template>
        </a-table>
      </a-tab-pane>

      <a-tab-pane key="audit" title="审计日志">
        <AuditLogPanel />
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:visible="detailVisible" title="日志详情" :footer="false" width="760px">
      <a-spin :loading="detailLoading" style="width: 100%">
        <template v-if="detail">
          <a-descriptions :column="1" bordered size="small">
            <a-descriptions-item v-for="item in detailRows" :key="item.label" :label="item.label">
              <pre v-if="item.pre" class="detail-pre">{{ item.value }}</pre>
              <span v-else class="detail-text">{{ item.value }}</span>
            </a-descriptions-item>
          </a-descriptions>
          <div class="detail-actions">
            <a-space>
              <a-button size="small" @click="copyIp">复制 IP</a-button>
              <a-button size="small" @click="filterByIp">只看该 IP</a-button>
            </a-space>
          </div>
        </template>
        <a-empty v-else-if="!detailLoading" description="详情不可用" />
      </a-spin>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import { Message, Modal } from '@arco-design/web-vue';
  import dayjs from 'dayjs';
  import { debounce } from 'lodash';
  import {
    fetchRetentionPolicy,
    fetchSystemEvent,
    fetchSystemEvents,
    purgeSystemEvents,
  } from '@/api/admin';
  // 审计日志与系统日志是两种数据，合并在同一页的两个视图里。
  import AuditLogPanel from '@/views/operations/audit.vue';

  const route = useRoute();
  const router = useRouter();

  const rows = ref<any[]>([]);
  const loading = ref(false);
  const error = ref('');
  const detailVisible = ref(false);
  const detailLoading = ref(false);
  const detail = ref<any>(null);
  const customRange = ref<string[]>([]);

  // Tab 与 ?tab= 查询参数同步，旧地址 /system/audit 重定向后直接落在审计视图。
  const TAB_KEYS = ['system', 'audit'];
  function normalizeTab(value: unknown) {
    const key = String(value || '');
    return TAB_KEYS.includes(key) ? key : 'system';
  }
  const activeTab = ref(normalizeTab(route.query.tab));

  watch(activeTab, (key) => {
    if (normalizeTab(route.query.tab) === key) return;
    const query = { ...route.query };
    if (key === 'system') {
      delete query.tab;
    } else {
      query.tab = key;
    }
    router.replace({ query });
  });

  watch(
    () => route.query.tab,
    (value) => {
      const key = normalizeTab(value);
      if (key !== activeTab.value) activeTab.value = key;
    }
  );

  // 保留策略：默认事件 180 天 / 访问明细 30 天，由 /system/retention 回填。
  const retention = reactive<{ eventDays: number | undefined; visitDays: number | undefined }>({
    eventDays: undefined,
    visitDays: undefined,
  });
  const retentionLoading = ref(false);
  const retentionError = ref('');
  const purging = ref(false);
  const EVENT_DAYS_RANGE = { min: 30, max: 3650, fallback: 180 };
  const VISIT_DAYS_RANGE = { min: 1, max: 365, fallback: 30 };

  const filters = reactive({
    level: '',
    category: '',
    ip: '',
    q: '',
    range: '7d',
    includeVisits: true,
    includeBots: false,
  });

  const levelOptions = [
    { label: '全部等级', value: '' },
    { label: '信息', value: 'info' },
    { label: '警告', value: 'warning' },
    { label: '危险', value: 'danger' },
  ];
  const categoryOptions = [
    { label: '全部分类', value: '' },
    { label: '官网访问', value: 'visit' },
    { label: '登录认证', value: 'auth' },
    { label: '接口调用', value: 'api' },
    { label: '安全防护', value: 'security' },
    { label: '后台管理', value: 'admin' },
    { label: '系统事件', value: 'system' },
    { label: '服务运行', value: 'server' },
  ];
  const rangeOptions = [
    { label: '今天', value: 'today' },
    { label: '近 7 天', value: '7d' },
    { label: '近 30 天', value: '30d' },
    { label: '自定义', value: 'custom' },
  ];

  const CATEGORY_LABELS: Record<string, string> = {
    visit: '官网访问',
    auth: '登录认证',
    api: '接口调用',
    security: '安全防护',
    admin: '后台管理',
    system: '系统事件',
    server: '服务运行',
  };

  const detailRows = computed(() => {
    const record = detail.value || {};
    const nested = record.detail && typeof record.detail === 'object' ? record.detail : {};
    const pick = (...names: string[]) => {
      for (const name of names) {
        const value = record[name] ?? nested[name];
        if (value !== undefined && value !== null && value !== '') return value;
      }
      return '';
    };
    const actor = [pick('actorKind'), pick('actorId'), pick('actorEmail')]
      .filter(Boolean)
      .join(' · ');
    return [
      { label: '时间', value: localTime(pick('occurredAt')) },
      { label: '来源', value: sourceLabel(pick('source') || (record.id ? 'event' : '')) },
      { label: '等级', value: levelLabel(pick('level')) },
      { label: '分类', value: categoryLabel(pick('category')) },
      { label: '动作', value: String(pick('action') || '-') },
      { label: '消息', value: String(pick('message') || '-') },
      { label: 'IP', value: String(pick('ip') || '-') },
      { label: 'IP 来源', value: String(pick('ipSource') || '-') },
      { label: '国家', value: String(pick('country') || '-') },
      { label: '操作者', value: actor || '-' },
      { label: '方法', value: String(pick('method') || '-') },
      { label: '路径', value: String(pick('path') || '-') },
      { label: '状态码', value: String(pick('statusCode') || '-') },
      { label: '浏览器', value: [pick('browser'), pick('browserVersion')].filter(Boolean).join(' ') || '-' },
      { label: '系统 / 设备', value: [pick('os'), pick('device')].filter(Boolean).join(' · ') || '-' },
      { label: '来源 referrer', value: String(nested.referrer ?? record.referrer ?? '-') },
      { label: 'visitor_id', value: String(nested.visitorId ?? record.visitorId ?? '-') },
      { label: 'session_id', value: String(nested.sessionId ?? record.sessionId ?? '-') },
      { label: '是否爬虫', value: (nested.isBot ?? record.isBot) ? '是' : '否' },
      { label: 'User-Agent', value: String(pick('userAgent') || '-'), pre: true },
      {
        label: '详情（detail）',
        value: JSON.stringify(nested, null, 2) || '{}',
        pre: true,
      },
    ];
  });

  function sourceLabel(source: string) {
    return source === 'visit' ? '官网访问' : '系统事件';
  }
  function levelLabel(level: string) {
    return { info: '信息', warning: '警告', danger: '危险' }[level] || level || '-';
  }
  function levelColor(level: string) {
    return { danger: 'red', warning: 'orange', info: 'blue' }[level] || 'gray';
  }
  function categoryLabel(category: string) {
    return CATEGORY_LABELS[category] || category || '-';
  }
  function browserText(record: any) {
    const browser = [record.browser, record.browserVersion].filter(Boolean).join(' ');
    return [browser, record.os].filter(Boolean).join(' · ') || '-';
  }
  function localTime(value: string) {
    return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '-';
  }
  function rowClass(record: any) {
    return record?.level === 'danger' ? 'log-danger-row' : '';
  }

  function sinceISO() {
    if (filters.range === 'today') return dayjs().startOf('day').toISOString();
    if (filters.range === '7d') return dayjs().subtract(6, 'day').startOf('day').toISOString();
    if (filters.range === '30d') return dayjs().subtract(29, 'day').startOf('day').toISOString();
    if (filters.range === 'custom' && customRange.value?.length === 2) {
      return dayjs(String(customRange.value[0])).startOf('day').toISOString();
    }
    return '';
  }

  function queryParams() {
    return {
      limit: 200,
      level: filters.level,
      category: filters.category,
      ip: filters.ip.trim(),
      q: filters.q.trim(),
      since: sinceISO(),
      include_visits: filters.includeVisits ? '1' : '0',
      include_bots: filters.includeBots ? '1' : '0',
    };
  }

  async function load() {
    loading.value = true;
    error.value = '';
    try {
      const result = await fetchSystemEvents(queryParams());
      rows.value = result?.data || [];
    } catch (reason: any) {
      error.value =
        reason?.response?.data?.error?.message ||
        reason?.message ||
        '日志加载失败，请稍后重试。';
      rows.value = [];
    } finally {
      loading.value = false;
    }
  }

  const debouncedLoad = debounce(() => load(), 400);

  watch(
    () => [filters.level, filters.category, filters.range, filters.includeVisits, filters.includeBots],
    () => load()
  );
  watch(() => [filters.ip, filters.q, customRange.value], () => debouncedLoad());

  async function openDetail(record: any) {
    detail.value = record;
    detailVisible.value = true;
    detailLoading.value = true;
    try {
      const result = await fetchSystemEvent(String(record.id));
      detail.value = result?.data || record;
    } catch {
      detail.value = record;
      Message.warning('完整详情读取失败，已展示列表中的字段');
    } finally {
      detailLoading.value = false;
    }
  }

  async function copyIp() {
    const ip = String(detail.value?.ip || '');
    if (!ip) {
      Message.warning('该记录没有 IP');
      return;
    }
    try {
      await navigator.clipboard.writeText(ip);
      Message.success('IP 已复制');
    } catch {
      Message.error('复制失败，请手动选择复制');
    }
  }

  function filterByIp() {
    const ip = String(detail.value?.ip || '');
    if (!ip) {
      Message.warning('该记录没有 IP');
      return;
    }
    filters.ip = ip;
    detailVisible.value = false;
    load();
  }

  function exportCsv() {
    const header = [
      '时间',
      '来源',
      '等级',
      '分类',
      '动作',
      '消息',
      'IP',
      '路径',
      '状态码',
      '浏览器',
      '系统',
      'User-Agent',
    ];
    const cell = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`;
    const data = rows.value.map((item) => [
      item.occurredAt,
      sourceLabel(item.source),
      levelLabel(item.level),
      categoryLabel(item.category),
      item.action,
      item.message,
      item.ip,
      item.path,
      item.statusCode,
      item.browser,
      item.os,
      item.userAgent,
    ]);
    const blob = new Blob(['\uFEFF', [header, ...data].map((row) => row.map(cell).join(',')).join('\r\n')], {
      type: 'text/csv;charset=utf-8',
    });
    const link = document.createElement('a');
    link.href = URL.createObjectURL(blob);
    link.download = `system-logs-${dayjs().format('YYYY-MM-DD-HHmm')}.csv`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }

  function clampRetention(
    value: unknown,
    range: { min: number; max: number; fallback: number }
  ) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) return range.fallback;
    return Math.min(Math.max(Math.trunc(parsed), range.min), range.max);
  }

  async function loadRetention() {
    retentionLoading.value = true;
    retentionError.value = '';
    try {
      const result = await fetchRetentionPolicy();
      const data = result?.data || {};
      retention.eventDays = clampRetention(data.eventDays, EVENT_DAYS_RANGE);
      retention.visitDays = clampRetention(data.visitDays, VISIT_DAYS_RANGE);
    } catch (reason: any) {
      retention.eventDays = EVENT_DAYS_RANGE.fallback;
      retention.visitDays = VISIT_DAYS_RANGE.fallback;
      retentionError.value =
        reason?.response?.data?.error?.message ||
        reason?.response?.data?.message ||
        reason?.message ||
        '保留策略读取失败，已使用默认 180 / 30 天。';
    } finally {
      retentionLoading.value = false;
    }
  }

  function confirmPurge() {
    // 提交前把天数收敛到后端允许范围，确认框里展示的就是最终会下发的天数。
    const eventDays = clampRetention(retention.eventDays, EVENT_DAYS_RANGE);
    const visitDays = clampRetention(retention.visitDays, VISIT_DAYS_RANGE);
    retention.eventDays = eventDays;
    retention.visitDays = visitDays;
    Modal.confirm({
      title: '保留策略清理',
      content: `将删除 ${eventDays} 天前的系统事件、${visitDays} 天前的官网访问明细，删除后不可恢复，确定继续吗？`,
      okText: '确认清理',
      cancelText: '取消',
      onOk: async () => {
        purging.value = true;
        try {
          const result = await purgeSystemEvents({
            event_days: eventDays,
            visit_days: visitDays,
          });
          const payload = result?.data || {};
          Message.success(
            `已删除系统事件 ${payload.removedEvents ?? 0} 条、访问记录 ${payload.removedVisits ?? 0} 条`
          );
          // 回填后端收敛后的实际天数。
          if (payload.eventDays !== undefined) {
            retention.eventDays = clampRetention(payload.eventDays, EVENT_DAYS_RANGE);
          }
          if (payload.visitDays !== undefined) {
            retention.visitDays = clampRetention(payload.visitDays, VISIT_DAYS_RANGE);
          }
          await load();
        } finally {
          purging.value = false;
        }
      },
    });
  }

  onMounted(() => {
    load();
    loadRetention();
  });
</script>

<style scoped lang="less">
  .retention-panel {
    margin-bottom: 12px;
    padding: 12px;
    border: 1px solid var(--color-border-2, #e5e6eb);
    border-radius: 4px;
    background: var(--color-fill-1, #f7f8fa);
  }
  .retention-form {
    row-gap: 12px;
    :deep(.arco-form-item) {
      margin-right: 12px;
      margin-bottom: 8px;
    }
  }
  .retention-hint {
    display: block;
    font-size: 12px;
    line-height: 18px;
  }
  .filters {
    margin-bottom: 12px;
    row-gap: 12px;
    :deep(.arco-form-item) {
      margin-right: 12px;
      margin-bottom: 12px;
    }
  }
  .banner {
    margin-bottom: 12px;
  }
  .logs-page :deep(.log-danger-row) td {
    background: var(--color-danger-light-1, #ffece8);
  }
  .detail-pre {
    margin: 0;
    max-height: 220px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
    font-size: 12px;
    line-height: 18px;
  }
  .detail-text {
    word-break: break-all;
  }
  .detail-actions {
    margin-top: 12px;
    text-align: right;
  }
</style>
