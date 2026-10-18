<template>
  <div class="dashboard">
    <div class="head">
      <div class="head-title">
        <h2>工作台</h2>
        <span class="muted">官网数据 + 服务器健康{{ refreshedAt ? ` · 更新于 ${refreshedAt}` : '' }}</span>
      </div>
      <a-space>
        <a-tag v-if="timezone" color="arcoblue">{{ timezone }}</a-tag>
        <a-button :loading="loading" @click="load()">刷新</a-button>
      </a-space>
    </div>

    <a-alert v-if="error" type="error" class="banner" show-icon>
      {{ error }}
      <template #action>
        <a-button size="mini" @click="load()">重试</a-button>
      </template>
    </a-alert>

    <template v-if="initialLoading">
      <a-grid :cols="4" :col-gap="12" :row-gap="12" class="cards">
        <a-grid-item v-for="index in 4" :key="index">
          <a-card :bordered="false"><a-skeleton :animation="true"><a-skeleton-line :rows="2" /></a-skeleton></a-card>
        </a-grid-item>
      </a-grid>
      <a-card :bordered="false" class="block">
        <a-skeleton :animation="true"><a-skeleton-line :rows="8" /></a-skeleton>
      </a-card>
    </template>

    <template v-else>
      <a-grid :cols="4" :col-gap="12" :row-gap="12" class="cards">
        <a-grid-item v-for="card in cards" :key="card.key">
          <a-card :bordered="false" class="stat-card">
            <div class="stat" :data-testid="`stat-${card.key}`">
              <small>{{ card.label }}</small>
              <strong :style="{ color: card.color }">
                {{ card.value }}<span v-if="card.suffix">{{ card.suffix }}</span>
              </strong>
              <em>{{ card.hint }}</em>
            </div>
          </a-card>
        </a-grid-item>
      </a-grid>

      <a-card :bordered="false" class="block">
        <template #title>本月访问趋势</template>
        <template #extra>
          <a-space class="legend">
            <span class="legend-item"><i class="dot pv"></i>PV 访问量</span>
            <span class="legend-item"><i class="dot uv"></i>UV 访客数</span>
            <span class="muted">{{ monthLabel }}</span>
          </a-space>
        </template>
        <a-empty v-if="!series.length" description="本月暂无访问数据" />
        <div v-else ref="chartEl" class="chart-wrap" @mouseleave="hoverIndex = -1">
          <svg
            :width="chart.width"
            :height="chart.height"
            class="trend-svg"
            data-testid="trend-svg"
            role="img"
            aria-label="本月 PV 与 UV 趋势"
          >
            <g class="grid">
              <template v-for="tick in chart.yTicks" :key="`y-${tick.value}`">
                <line :x1="chart.padL" :x2="chart.width - chart.padR" :y1="tick.y" :y2="tick.y" />
                <text :x="chart.padL - 8" :y="tick.y + 4" text-anchor="end">{{ tick.value }}</text>
              </template>
            </g>
            <polyline class="line-pv" :points="chart.pvPoints" fill="none" stroke="#165dff" stroke-width="2" />
            <polyline class="line-uv" :points="chart.uvPoints" fill="none" stroke="#00b42a" stroke-width="2" />
            <g v-if="hoverIndex >= 0" class="focus">
              <line
                :x1="chart.x(hoverIndex)"
                :x2="chart.x(hoverIndex)"
                :y1="chart.padT"
                :y2="chart.padT + chart.innerH"
                stroke="#c9cdd4"
                stroke-dasharray="4 4"
              />
              <circle :cx="chart.x(hoverIndex)" :cy="chart.y(series[hoverIndex].pv)" r="4" fill="#165dff" />
              <circle :cx="chart.x(hoverIndex)" :cy="chart.y(series[hoverIndex].uv)" r="4" fill="#00b42a" />
            </g>
            <rect
              v-for="(item, index) in series"
              :key="`hit-${item.date}`"
              :x="chart.bandX(index)"
              :y="chart.padT"
              :width="chart.bandWidth"
              :height="chart.innerH"
              fill="transparent"
              @mouseenter="hoverIndex = index"
            />
            <text
              v-for="tick in chart.xTicks"
              :key="`x-${tick.date}`"
              :x="tick.x"
              :y="chart.height - 8"
              text-anchor="middle"
              class="axis"
            >
              {{ tick.label }}
            </text>
          </svg>
          <div v-if="hoverIndex >= 0" class="tooltip" :style="{ left: `${chart.tooltipLeft(hoverIndex)}%` }">
            <strong>{{ series[hoverIndex].date }}</strong>
            <div><i class="dot pv"></i>PV {{ series[hoverIndex].pv }}</div>
            <div><i class="dot uv"></i>UV {{ series[hoverIndex].uv }}</div>
          </div>
        </div>
      </a-card>

      <a-grid :cols="2" :col-gap="12" :row-gap="12" class="blocks">
        <a-grid-item>
          <a-card :bordered="false" class="block health-card">
            <template #title>服务健康</template>
            <template #extra><span class="muted">{{ servicesOk ? '全部正常' : `${servicesFailed} 项异常` }}</span></template>
            <a-skeleton v-if="!health" :animation="true"><a-skeleton-line :rows="6" /></a-skeleton>
            <ul v-else class="services" data-testid="services">
              <li v-for="service in services" :key="service.label" :class="{ failed: !service.ok }">
                <a-tag :color="service.ok ? 'green' : 'red'" size="small">{{ service.ok ? '正常' : '异常' }}</a-tag>
                <span class="service-label">{{ service.label }}</span>
                <span class="muted">{{ service.latencyMs }} ms</span>
                <span v-if="!service.ok" class="service-error">{{ service.message || '探测失败' }}</span>
              </li>
            </ul>
          </a-card>
        </a-grid-item>

        <a-grid-item>
          <a-card :bordered="false" class="block resource-card">
            <template #title>服务器资源</template>
            <template #extra>
              <span v-if="server" class="muted">{{ server.hostname }} · Node {{ server.nodeVersion }} · 运行 {{ uptimeText }}</span>
            </template>
            <a-skeleton v-if="!health" :animation="true"><a-skeleton-line :rows="6" /></a-skeleton>
            <div v-else class="resources" data-testid="resources">
              <div class="resource">
                <div class="resource-head">
                  <span>CPU 使用率</span>
                  <span>{{ cpuPercent }}%</span>
                </div>
                <a-progress :percent="cpuPercent" :color="levelColor(cpuPercent)" :show-text="false" size="large" />
                <div class="muted">负载 1/5/15：{{ cpu.load1 }} / {{ cpu.load5 }} / {{ cpu.load15 }} · {{ cpu.cores }} 核</div>
              </div>

              <div class="resource">
                <div class="resource-head">
                  <span>内存</span>
                  <span>{{ formatBytes(memory.usedBytes) }} / {{ formatBytes(memory.totalBytes) }}</span>
                </div>
                <a-progress :percent="memoryPercent" :color="levelColor(memoryPercent)" :show-text="false" size="large" />
                <div class="muted">
                  进程 RSS {{ formatBytes(memory.process.rss) }} · 堆 {{ formatBytes(memory.process.heapUsed) }} /
                  {{ formatBytes(memory.process.heapTotal) }}
                </div>
              </div>

              <div v-for="disk in disks" :key="disk.label || disk.path" class="resource">
                <div class="resource-head">
                  <span>磁盘 {{ disk.label || disk.path }}</span>
                  <span>{{ formatBytes(disk.usedBytes) }} / {{ formatBytes(disk.totalBytes) }}</span>
                </div>
                <a-progress :percent="diskPercent(disk)" :color="levelColor(diskPercent(disk))" :show-text="false" size="large" />
                <div class="muted">
                  <template v-if="disk.error">读取失败：{{ disk.error }}</template>
                  <template v-else>{{ disk.path }}</template>
                </div>
              </div>

              <div class="resource">
                <div class="resource-head">
                  <span>数据库</span>
                  <span>{{ database.driver || '-' }}</span>
                </div>
                <div class="muted">
                  大小 {{ formatBytes(database.sizeBytes) }} · 连接 {{ database.connections ?? 0 }} /
                  {{ database.maxConnections ?? '-' }}
                  <template v-if="database.error"> · {{ database.error }}</template>
                </div>
              </div>
            </div>
          </a-card>
        </a-grid-item>
      </a-grid>
    </template>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref, watch } from 'vue';
  import { useDocumentVisibility, useElementSize, useIntervalFn } from '@vueuse/core';
  import dayjs from 'dayjs';
  import { fetchAnalyticsOverview, fetchSystemHealth } from '@/api/admin';

  const overview = ref<any>(null);
  const health = ref<any>(null);
  const loading = ref(false);
  const error = ref('');
  const hoverIndex = ref(-1);
  const refreshedAt = ref('');
  const chartEl = ref<HTMLElement>();
  const { width: chartWidth } = useElementSize(chartEl);

  const isBrowser = typeof Intl !== 'undefined' && typeof Intl.DateTimeFormat === 'function';
  const timezone = isBrowser ? Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai' : 'Asia/Shanghai';
  const month = dayjs().format('YYYY-MM');
  const monthLabel = dayjs().format('YYYY 年 M 月');

  const series = computed<any[]>(() => overview.value?.series || []);
  const initialLoading = computed(() => loading.value && !overview.value && !health.value);

  function num(value: unknown) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }

  const cards = computed(() => {
    const today = overview.value?.today || {};
    const monthData = overview.value?.month || {};
    return [
      { key: 'pv', label: '今日访问（PV）', value: num(today.pv), suffix: '', color: '#165dff', hint: `本月累计 ${num(monthData.pv)}` },
      { key: 'uv', label: '今日访客（UV）', value: num(today.uv), suffix: '', color: '#00b42a', hint: `本月累计 ${num(monthData.uv)}` },
      { key: 'leads', label: '今日询盘', value: num(today.leads), suffix: '', color: '#ff7d00', hint: `本月累计 ${num(monthData.leads)}` },
      { key: 'chats', label: '今日发起会话', value: num(today.chats), suffix: '', color: '#0fc6c2', hint: `本月累计 ${num(monthData.chats)}` },
    ];
  });

  const chart = computed(() => {
    const width = Math.max(360, Math.round(chartWidth.value || 760));
    const height = 260;
    const padL = 48;
    const padR = 20;
    const padT = 16;
    const padB = 32;
    const innerW = width - padL - padR;
    const innerH = height - padT - padB;
    const data = series.value;
    const count = data.length;
    const raw = Math.max(1, ...data.map((item) => Math.max(num(item.pv), num(item.uv))));
    const magnitude = 10 ** Math.floor(Math.log10(raw));
    const niceMax = Math.max(1, Math.ceil(raw / magnitude) * magnitude);
    const x = (index: number) =>
      count <= 1 ? padL + innerW / 2 : padL + (innerW * index) / (count - 1);
    const y = (value: number) => padT + innerH - (innerH * Math.min(num(value), niceMax)) / niceMax;
    const step = count > 1 ? innerW / (count - 1) : innerW;
    const yTicks = [0, 0.25, 0.5, 0.75, 1].map((ratio) => {
      const value = Math.round(niceMax * ratio);
      return { value, y: y(value) };
    });
    const tickEvery = Math.max(1, Math.ceil(count / 8));
    const xTicks = data
      .map((item, index) => ({ date: item.date, x: x(index), label: String(item.date).slice(5) }))
      .filter((_item, index) => index % tickEvery === 0 || index === count - 1);
    return {
      width,
      height,
      padL,
      padR,
      padT,
      innerH,
      x,
      y,
      yTicks,
      xTicks,
      tooltipLeft: (index: number) => (x(index) / width) * 100,
      bandX: (index: number) => (count > 1 ? x(index) - step / 2 : padL),
      bandWidth: count > 1 ? step : innerW,
      pvPoints: data.map((item, index) => `${x(index).toFixed(1)},${y(item.pv).toFixed(1)}`).join(' '),
      uvPoints: data.map((item, index) => `${x(index).toFixed(1)},${y(item.uv).toFixed(1)}`).join(' '),
    };
  });

  const cpu = computed(() => health.value?.cpu || { usage: 0, cores: 0, load1: 0, load5: 0, load15: 0 });
  const cpuPercent = computed(() => Math.round(num(cpu.value.usage) * 100));
  const memory = computed(
    () => health.value?.memory || { totalBytes: 0, usedBytes: 0, process: { rss: 0, heapUsed: 0, heapTotal: 0 } }
  );
  const memoryPercent = computed(() => {
    const total = num(memory.value.totalBytes);
    return total > 0 ? Math.round((num(memory.value.usedBytes) / total) * 100) : 0;
  });
  const disks = computed<any[]>(() => health.value?.disks || []);
  const database = computed(() => health.value?.database || {});
  const services = computed<any[]>(() => health.value?.services || []);
  const servicesFailed = computed(() => services.value.filter((item) => !item.ok).length);
  const servicesOk = computed(() => services.value.length > 0 && servicesFailed.value === 0);
  const server = computed(() => health.value?.server || null);
  const uptimeText = computed(() => formatDuration(num(server.value?.processUptimeSeconds)));

  function diskPercent(disk: any) {
    const total = num(disk.totalBytes);
    return total > 0 ? Math.round((num(disk.usedBytes) / total) * 100) : 0;
  }
  function levelColor(percent: number) {
    if (percent >= 90) return '#f53f3f';
    if (percent >= 75) return '#ff7d00';
    return '#165dff';
  }
  function formatBytes(value: unknown) {
    const size = num(value);
    if (size <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    const index = Math.min(units.length - 1, Math.floor(Math.log(size) / Math.log(1024)));
    const scaled = size / 1024 ** index;
    return `${scaled >= 100 || index === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[index]}`;
  }
  function formatDuration(seconds: number) {
    if (seconds <= 0) return '-';
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) return `${days} 天 ${hours} 小时`;
    if (hours > 0) return `${hours} 小时 ${minutes} 分`;
    return `${minutes} 分`;
  }

  async function load() {
    loading.value = true;
    error.value = '';
    const [overviewResult, healthResult] = await Promise.allSettled([
      fetchAnalyticsOverview({ tz: timezone, month }),
      fetchSystemHealth(),
    ]);
    const failures: string[] = [];
    if (overviewResult.status === 'fulfilled') {
      overview.value = overviewResult.value?.data || overviewResult.value || null;
    } else {
      failures.push(`访问统计：${overviewResult.reason?.message || '加载失败'}`);
    }
    if (healthResult.status === 'fulfilled') {
      health.value = healthResult.value?.data || healthResult.value || null;
    } else {
      failures.push(`服务器健康：${healthResult.reason?.message || '加载失败'}`);
    }
    error.value = failures.join('；');
    if (!failures.length) refreshedAt.value = dayjs().format('HH:mm:ss');
    loading.value = false;
  }

  const visibility = useDocumentVisibility();
  const { pause, resume } = useIntervalFn(() => {
    load();
  }, 30000, { immediate: false });

  watch(visibility, (state) => {
    if (state === 'visible') resume();
    else pause();
  });

  onMounted(async () => {
    await load();
    resume();
  });
</script>

<style scoped lang="less">
  .dashboard {
    padding-bottom: 16px;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .head-title {
    display: flex;
    align-items: baseline;
    gap: 12px;
    h2 {
      margin: 0;
      font-size: 20px;
      font-weight: 700;
    }
  }
  .muted {
    color: var(--color-text-3);
    font-size: 12px;
  }
  .banner {
    margin-bottom: 12px;
  }
  .cards {
    margin-bottom: 12px;
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
      font-size: 28px;
      font-weight: 700;
      span {
        margin-left: 2px;
        font-size: 16px;
      }
    }
    em {
      color: var(--color-text-3);
      font-size: 12px;
      font-style: normal;
    }
  }
  .block {
    margin-bottom: 12px;
  }
  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-2);
    font-size: 12px;
  }
  .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: 6px;
    &.pv {
      background: #165dff;
    }
    &.uv {
      background: #00b42a;
    }
  }
  .chart-wrap {
    position: relative;
    width: 100%;
  }
  .trend-svg {
    display: block;
    max-width: 100%;
    .grid line {
      stroke: var(--color-fill-2);
      stroke-width: 1;
    }
    .grid text,
    .axis {
      fill: var(--color-text-3);
      font-size: 11px;
    }
  }
  .tooltip {
    position: absolute;
    top: 4px;
    transform: translateX(-50%);
    min-width: 120px;
    padding: 6px 10px;
    border-radius: 6px;
    background: rgba(29, 33, 41, 0.92);
    color: #fff;
    font-size: 12px;
    line-height: 18px;
    pointer-events: none;
    strong {
      display: block;
      margin-bottom: 2px;
    }
  }
  .services {
    margin: 0;
    padding: 0;
    list-style: none;
    li {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 10px 0;
      border-bottom: 1px solid var(--color-fill-2);
      &:last-child {
        border-bottom: none;
      }
    }
    .service-label {
      flex: 0 0 110px;
      font-weight: 500;
    }
    .service-error {
      color: #f53f3f;
      font-size: 12px;
      word-break: break-all;
    }
  }
  .resources {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .resource-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 6px;
    font-size: 13px;
  }
</style>
