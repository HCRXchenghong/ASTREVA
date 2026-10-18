<template>
  <div class="media-dashboard">
    <a-spin :loading="loading" style="width: 100%">
      <div class="stat-grid">
        <article
          v-for="card in statCards"
          :key="card.label"
          class="stat-card"
          :style="{ '--accent': card.accent }"
        >
          <small>{{ card.label }}</small>
          <strong>{{ card.value }}</strong>
          <span>{{ card.note }}</span>
        </article>
      </div>

      <div class="dashboard-grid">
        <a-card :bordered="false" class="surface">
          <template #title>服务器资源</template>
          <template #extra>
            <a-tag :color="health.ok ? 'green' : health.pending ? 'gray' : 'red'">
              {{ health.pending ? '检查中' : health.ok ? '全部正常' : '存在异常' }}
            </a-tag>
          </template>
          <div class="resource-list">
            <div v-for="row in resourceRows" :key="row.key" class="resource-row">
              <div class="resource-label">
                <span>{{ row.label }}</span>
                <span class="resource-value">{{ row.text }}</span>
              </div>
              <a-progress
                :percent="row.percent / 100"
                :show-text="false"
                size="small"
                :color="row.percent > 85 ? '#e5484d' : '#0878dc'"
              />
            </div>
          </div>
        </a-card>

        <a-card :bordered="false" class="surface">
          <template #title>运行环境</template>
          <a-descriptions :column="1" size="small" bordered>
            <a-descriptions-item v-for="fact in environment" :key="fact[0]" :label="fact[0]">
              {{ fact[1] }}
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="surface">
          <template #title>分类分布</template>
          <div v-if="categories.length" class="distribution">
            <div v-for="item in categories" :key="item.name" class="distribution-row">
              <b :title="item.name">{{ item.name }}</b>
              <a-progress
                :percent="item.count / maxCategoryCount"
                :show-text="false"
                size="small"
              />
              <span>{{ item.count }}</span>
            </div>
          </div>
          <a-empty v-else description="暂无分类数据" />
        </a-card>
      </div>
    </a-spin>
  </div>
</template>

<script lang="ts" setup>
  import { computed } from 'vue';
  import {
    formatBytes,
    formatDateTime,
    formatDuration,
    ratioPercent,
  } from '@/lib/media';
  import type { MediaCategory, MediaRuntime, MediaStats } from '@/api/media';

  const props = withDefaults(
    defineProps<{
      stats?: MediaStats;
      runtime?: MediaRuntime;
      categories?: MediaCategory[];
      loading?: boolean;
    }>(),
    { stats: () => ({}), runtime: () => ({}), categories: () => [], loading: false }
  );

  const storage = computed(() => props.stats?.storage || props.runtime?.storage || {});
  const memory = computed(() => props.runtime?.memory || {});
  const disk = computed(() => props.runtime?.disk || {});
  const service = computed(() => props.runtime?.service || {});
  const storageOk = computed(() => Boolean(storage.value.ok));
  const pending = computed(() => storage.value.ok === undefined && service.value.ok === undefined);

  const health = computed(() => ({
    ok: storageOk.value && service.value.ok !== false,
    pending: pending.value,
  }));

  const statCards = computed(() => [
    {
      label: '图片总数',
      value: String(props.stats?.images || 0),
      note: `全部素材 ${props.stats?.total || 0}`,
      accent: '#0878dc',
    },
    {
      label: '存储占用',
      value: formatBytes(props.stats?.storedBytes ?? props.stats?.totalBytes),
      note: '含原图与变体',
      accent: '#16835c',
    },
    {
      label: '进程内存',
      value: formatBytes(memory.value.rss),
      note: `堆内存 ${formatBytes(memory.value.heapUsed)}`,
      accent: '#7656c4',
    },
    {
      label: '分类数量',
      value: String(props.stats?.categories || props.categories.length || 0),
      note: '支持快捷筛选',
      accent: '#d36c18',
    },
    {
      label: '运行时长',
      value: formatDuration(service.value.uptimeSeconds),
      note: `PID ${service.value.pid || '—'}`,
      accent: '#1685a5',
    },
  ]);

  const resourceRows = computed(() => {
    const systemMemoryUsed = Math.max(
      0,
      Number(memory.value.systemTotal || 0) - Number(memory.value.systemFree || 0)
    );
    const usage = Number(props.runtime?.cpu?.usagePercent || 0);
    const processUsage = Number(props.runtime?.cpu?.processUsagePercent || 0);
    return [
      {
        key: 'system-cpu',
        label: '系统 CPU',
        percent: usage,
        text: `${usage.toFixed(1)}%`,
      },
      {
        key: 'process-cpu',
        label: 'Node 进程 CPU',
        percent: processUsage,
        text: `${processUsage.toFixed(1)}%`,
      },
      {
        key: 'process-rss',
        label: 'Node 进程 RSS',
        percent: ratioPercent(memory.value.rss, memory.value.systemTotal),
        text: `${formatBytes(memory.value.rss)} / ${formatBytes(memory.value.systemTotal)}`,
      },
      {
        key: 'system-memory',
        label: '系统内存',
        percent: ratioPercent(systemMemoryUsed, memory.value.systemTotal),
        text: `${formatBytes(systemMemoryUsed)} / ${formatBytes(memory.value.systemTotal)}`,
      },
      {
        key: 'disk',
        label: '磁盘空间',
        percent: ratioPercent(disk.value.used, disk.value.total),
        text: disk.value.available
          ? `${formatBytes(disk.value.used)} / ${formatBytes(disk.value.total)}`
          : '宿主机未提供',
      },
      {
        key: 'heap',
        label: 'Node 堆内存',
        percent: ratioPercent(memory.value.heapUsed, memory.value.heapTotal),
        text: `${formatBytes(memory.value.heapUsed)} / ${formatBytes(memory.value.heapTotal)}`,
      },
    ];
  });

  const environment = computed(() => [
    ['Node.js', service.value.node || '—'],
    ['运行平台', service.value.platform || '—'],
    ['数据库', props.runtime?.database?.driver || '—'],
    ['媒体存储', storage.value.driver ? String(storage.value.driver).toUpperCase() : '—'],
    ['图片处理', props.runtime?.processing?.engine || '—'],
    ['CPU 核心', props.runtime?.cpu?.cores || '—'],
    ['Bucket', storage.value.bucket || '—'],
    ['启动时间', formatDateTime(service.value.startedAt)],
  ]);

  const maxCategoryCount = computed(() =>
    Math.max(1, ...props.categories.map((item) => Number(item.count || 0)))
  );
</script>

<style scoped>
  .stat-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }

  .stat-card {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 16px;
    background: var(--color-bg-2);
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    overflow: hidden;
  }

  .stat-card::before {
    content: '';
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--accent, #0878dc);
  }

  .stat-card small {
    color: var(--color-text-3);
  }

  .stat-card strong {
    font-size: 22px;
    line-height: 1.2;
  }

  .stat-card span {
    color: var(--color-text-3);
    font-size: 12px;
  }

  .dashboard-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: 12px;
  }

  .resource-row {
    margin-bottom: 12px;
  }

  .resource-label {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 4px;
    font-size: 13px;
  }

  .resource-value {
    color: var(--color-text-3);
  }

  .distribution-row {
    display: grid;
    grid-template-columns: 96px 1fr 40px;
    align-items: center;
    gap: 10px;
    margin-bottom: 10px;
  }

  .distribution-row b {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .distribution-row span {
    text-align: right;
    color: var(--color-text-3);
  }
</style>
