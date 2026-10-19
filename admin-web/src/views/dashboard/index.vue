<template>
  <div class="dashboard">
    <div class="head">
      <div class="head-title">
        <h2>工作台</h2>
        <span class="muted">官网访问 + 客服引擎 + 服务器健康{{ refreshedAt ? ` · 更新于 ${refreshedAt}` : '' }}</span>
      </div>
      <a-space>
        <a-tag v-if="timezone" color="arcoblue">{{ timezone }}</a-tag>
        <a-button :loading="loading" @click="load()">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
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
              <div class="stat-label">
                <small>{{ card.label }}</small>
                <a-tooltip :content="card.tooltip" position="top">
                  <icon-question-circle class="help-icon" />
                </a-tooltip>
              </div>
              <strong :style="{ color: card.color }" :data-testid="`stat-${card.key}-value`">
                {{ card.display }}<span v-if="card.suffix">{{ card.suffix }}</span>
              </strong>
              <em>{{ card.hint }}</em>
            </div>
          </a-card>
        </a-grid-item>
      </a-grid>

      <div class="metric-note muted">
        口径：PV = 浏览次数（刷新或重复打开都计次）；UV = 去重访客数；发起会话 = 访客打开聊天窗口的次数；询盘 =
        当天有访客消息的会话数（同一会话当天只计 1 次，不是官网表单提交）。
      </div>

      <a-card :bordered="false" class="block trend-card">
        <template #title>本月访问趋势</template>
        <template #extra>
          <a-space class="legend">
            <span class="legend-item"><i class="dot pv"></i>PV 访问量</span>
            <span class="legend-item"><i class="dot uv"></i>UV 访客数</span>
            <span class="muted">{{ monthLabel }}</span>
          </a-space>
        </template>
        <a-empty v-if="!series.length" description="本月暂无访问数据" />
        <template v-else>
          <div ref="chartEl" class="chart-wrap" @mouseleave="hoverIndex = -1">
            <svg
              :width="chart.width"
              :height="chart.height"
              class="trend-svg"
              data-testid="trend-svg"
              role="img"
              aria-label="本月 PV 与 UV 柱状趋势图"
            >
              <g class="grid">
                <template v-for="tick in chart.yTicks" :key="`y-${tick.value}`">
                  <line :x1="chart.padL" :x2="chart.width - chart.padR" :y1="tick.y" :y2="tick.y" />
                  <text :x="chart.padL - 8" :y="tick.y + 4" text-anchor="end">{{ tick.value }}</text>
                </template>
              </g>
              <line
                v-if="hoverIndex >= 0"
                :x1="chart.bandCenter(hoverIndex)"
                :x2="chart.bandCenter(hoverIndex)"
                :y1="chart.padT"
                :y2="chart.padT + chart.innerH"
                stroke="#c9cdd4"
                stroke-dasharray="4 4"
              />
              <g class="bars">
                <template v-for="(item, index) in series" :key="`bars-${item.date}`">
                  <rect
                    class="bar pv"
                    :x="chart.pvBarX(index)"
                    :y="chart.barY(item.pv)"
                    :width="chart.barWidth"
                    :height="chart.barHeight(item.pv)"
                    rx="2"
                    fill="#165dff"
                  />
                  <rect
                    class="bar uv"
                    :x="chart.uvBarX(index)"
                    :y="chart.barY(item.uv)"
                    :width="chart.barWidth"
                    :height="chart.barHeight(item.uv)"
                    rx="2"
                    fill="#00b42a"
                  />
                </template>
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
              <div><i class="dot pv"></i>PV {{ formatNumber(series[hoverIndex].pv) }}</div>
              <div><i class="dot uv"></i>UV {{ formatNumber(series[hoverIndex].uv) }}</div>
            </div>
          </div>
          <div class="chart-foot muted">
            本月 PV 合计 {{ formatNumber(monthData.pv) }} · UV 合计 {{ formatNumber(monthData.uv) }} ·
            数据按 {{ timezone }} 自然日汇总
          </div>
        </template>
      </a-card>

      <a-grid :cols="2" :col-gap="12" :row-gap="12" class="blocks">
        <a-grid-item>
          <a-card :bordered="false" class="block health-card">
            <template #title>服务健康</template>
            <template #extra><span class="muted">最近检查 {{ healthCheckedAt }}</span></template>

            <div class="status-overall" :class="overall.level" data-testid="health-overall">
              <span class="status-dot" :class="overall.level"></span>
              <div class="status-overall-text">
                <strong>{{ overall.title }}</strong>
                <small>{{ overall.summary }}</small>
              </div>
            </div>

            <ul class="services" data-testid="services">
              <li v-for="row in serviceRows" :key="row.label" class="service-row" :data-testid="`service-${row.key}`">
                <span class="status-dot" :class="row.level"></span>
                <div class="service-main">
                  <div class="service-name">
                    <span class="service-label">{{ row.label }}</span>
                    <a-tag :color="row.tagColor" size="small">{{ row.statusLabel }}</a-tag>
                  </div>
                  <div class="service-desc">{{ row.description }}</div>
                </div>
                <div class="service-side">
                  <span v-if="row.latencyMs !== null" class="muted">{{ row.latencyMs }} ms</span>
                  <span class="muted">{{ row.checkedAt }}</span>
                </div>
              </li>
            </ul>
          </a-card>
        </a-grid-item>

        <a-grid-item>
          <a-card :bordered="false" class="block resource-card">
            <template #title>服务器资源</template>
            <template #extra>
              <span v-if="server" class="muted">
                {{ server.hostname }} · Node {{ server.nodeVersion }} · 运行 {{ uptimeText }}
              </span>
            </template>

            <a-empty v-if="!health" description="未获取到服务器资源数据" />
            <template v-else>
              <div class="rings" data-testid="resources">
                <div v-for="ring in resourceRings" :key="ring.key" class="ring" :data-testid="`resource-${ring.key}`">
                  <div class="ring-visual">
                    <svg viewBox="0 0 120 120" class="ring-svg" role="img" :aria-label="`${ring.label} 使用率 ${ring.display}`">
                      <circle class="ring-track" cx="60" cy="60" r="52" />
                      <circle
                        v-if="ring.percent !== null && ring.percent > 0"
                        class="ring-value"
                        cx="60"
                        cy="60"
                        r="52"
                        pathLength="100"
                        :stroke="ring.color"
                        :stroke-dasharray="`${ring.percent} 100`"
                        transform="rotate(-90 60 60)"
                      />
                    </svg>
                    <div class="ring-center">
                      <strong :style="{ color: ring.color }">{{ ring.display }}</strong>
                    </div>
                  </div>
                  <div class="ring-label">{{ ring.label }}</div>
                  <div class="ring-meta">{{ ring.meta }}</div>
                </div>
              </div>

              <div class="resource-details">
                <div v-for="disk in disks" :key="disk.label || disk.path" class="resource-line">
                  <span class="resource-line-label">磁盘 · {{ disk.label || disk.path }}</span>
                  <span class="resource-line-value">
                    {{ formatBytes(disk.usedBytes) }} / {{ formatBytes(disk.totalBytes) }}
                  </span>
                  <span class="muted resource-line-extra">
                    <template v-if="disk.error">读取失败：{{ disk.error }}</template>
                    <template v-else>可用 {{ formatBytes(disk.freeBytes) }} · {{ disk.path }}</template>
                  </span>
                </div>
                <div class="resource-line">
                  <span class="resource-line-label">数据库</span>
                  <span class="resource-line-value">{{ database.driver || '-' }} · {{ formatBytes(database.sizeBytes) }}</span>
                  <span class="muted resource-line-extra">
                    连接 {{ database.connections ?? 0 }} / {{ database.maxConnections ?? '-' }}
                    <template v-if="database.error"> · {{ database.error }}</template>
                  </span>
                </div>
                <div class="resource-line">
                  <span class="resource-line-label">进程内存</span>
                  <span class="resource-line-value">RSS {{ formatBytes(memory.process.rss) }}</span>
                  <span class="muted resource-line-extra">
                    堆 {{ formatBytes(memory.process.heapUsed) }} / {{ formatBytes(memory.process.heapTotal) }}
                  </span>
                </div>
              </div>
            </template>
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
  import client, { serviceBridgePrefix } from '@/api/client';
  import { fetchAnalyticsOverview, fetchSystemHealth } from '@/api/admin';

  type StatusLevel = 'ok' | 'degraded' | 'down' | 'unknown';

  interface ServiceRow {
    key: string;
    label: string;
    level: StatusLevel;
    statusLabel: string;
    tagColor: string;
    description: string;
    latencyMs: number | null;
    checkedAt: string;
  }

  interface ResourceRing {
    key: string;
    label: string;
    percent: number | null;
    display: string;
    color: string;
    meta: string;
  }

  const overview = ref<any>(null);
  const health = ref<any>(null);
  const loading = ref(false);
  const error = ref('');
  const hoverIndex = ref(-1);
  const refreshedAt = ref('');
  const chartEl = ref<HTMLElement>();

  // 「询盘」来自客服引擎：新版 admin-server 会在 overview 里返回 inquiries；
  // 旧进程尚未重启时为空，这里通过现有的 servicebridge 代理用引擎接口即时聚合。
  const inquiryToday = ref<number | null>(null);
  const inquiryMonth = ref<number | null>(null);

  const { width: chartWidth } = useElementSize(chartEl);

  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai';
  const monthLabel = dayjs().format('YYYY 年 M 月');

  const KNOWN_SERVICE_LABELS = ['控制台服务', '客服引擎', '构建服务', '对象存储', 'AI 生图', '网站翻译'];
  const STATUS_LABELS: Record<StatusLevel, string> = {
    ok: '正常',
    degraded: '降级',
    down: '故障',
    unknown: '未知',
  };
  const STATUS_COLORS: Record<StatusLevel, string> = {
    ok: 'green',
    degraded: 'orange',
    down: 'red',
    unknown: 'gray',
  };

  const series = computed<any[]>(() => overview.value?.series || []);
  const monthData = computed(() => overview.value?.month || {});
  const initialLoading = computed(() => loading.value && !overview.value && !health.value);

  function num(value: unknown) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }

  function formatNumber(value: unknown) {
    return num(value).toLocaleString('zh-CN');
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

  function levelColor(percent: number) {
    if (percent >= 90) return '#f53f3f';
    if (percent >= 75) return '#ff7d00';
    return '#165dff';
  }

  const cards = computed(() => {
    const today = overview.value?.today || {};
    const monthStats = overview.value?.month || {};
    const inquiryKnown = inquiryToday.value !== null;
    const inquiryMonthKnown = inquiryMonth.value !== null;
    const inquiryTruncated = overview.value?.inquiry?.truncated === true;
    const inquiryTooltip = inquiryKnown
      ? `${
          overview.value?.inquiry?.definition ||
          '询盘=当天有访客消息的会话数；同一会话当天多条消息只计 1 次；不含官网表单提交。'
        }${inquiryTruncated ? ' 当前引擎未提供聚合接口，兼容路径最多统计最近 500 个会话，结果可能不完整。' : ''}`
      : `客服引擎询盘数据不可用${overview.value?.inquiry?.message ? `：${overview.value.inquiry.message}` : ''}`;

    return [
      {
        key: 'pv',
        label: '今日访问（PV）',
        display: formatNumber(today.pv),
        suffix: '',
        color: '#165dff',
        hint: `本月累计 ${formatNumber(monthStats.pv)}`,
        tooltip: 'PV = 浏览次数：同一访客刷新或重复打开页面都会各计 1 次。',
      },
      {
        key: 'uv',
        label: '今日访客（UV）',
        display: formatNumber(today.uv),
        suffix: '',
        color: '#00b42a',
        hint: `本月累计 ${formatNumber(monthStats.uv)}`,
        tooltip: 'UV = 去重访客数：同一访客当天多次访问只计 1 次。',
      },
      {
        key: 'chats',
        label: '今日发起会话',
        display: formatNumber(today.chats),
        suffix: '',
        color: '#0fc6c2',
        hint: `本月累计 ${formatNumber(monthStats.chats)}`,
        tooltip: '访客打开聊天窗口的次数（chat_open 埋点）；重复打开会重复计数。',
      },
      {
        key: 'inquiries',
        label: '今日询盘',
        display: inquiryKnown ? formatNumber(inquiryToday.value) : '—',
        suffix: '',
        color: '#ff7d00',
        hint: inquiryKnown
          ? inquiryMonthKnown
            ? `本月累计 ${formatNumber(inquiryMonth.value)}`
            : '本月累计 —'
          : '客服引擎数据不可用',
        tooltip: inquiryTooltip,
      },
    ];
  });

  const chart = computed(() => {
    const width = Math.max(420, Math.round(chartWidth.value || 860));
    const height = 280;
    const padL = 46;
    const padR = 18;
    const padT = 18;
    const padB = 34;
    const innerW = width - padL - padR;
    const innerH = height - padT - padB;
    const data = series.value;
    const count = data.length;
    const rawMax = Math.max(1, ...data.map((item) => Math.max(num(item.pv), num(item.uv))));
    const magnitude = 10 ** Math.floor(Math.log10(rawMax));
    const niceMax = Math.max(4, Math.ceil(rawMax / magnitude) * magnitude);
    const y = (value: number) => padT + innerH - (innerH * Math.min(num(value), niceMax)) / niceMax;
    const bandWidth = innerW / Math.max(1, count);
    const bandCenter = (index: number) => padL + bandWidth * (index + 0.5);
    const barWidth = Math.max(3, Math.min(14, bandWidth * 0.28));
    const barHeight = (value: number) => (innerH * Math.min(num(value), niceMax)) / niceMax;
    const barY = (value: number) => padT + innerH - barHeight(value);
    const yTicks = [0, 0.25, 0.5, 0.75, 1].map((ratio) => {
      const value = Math.round(niceMax * ratio);
      return { value, y: y(value) };
    });
    const tickEvery = Math.max(1, Math.ceil(count / 8));
    const xTicks = data
      .map((item, index) => ({ date: item.date, x: bandCenter(index), label: String(item.date).slice(5) }))
      .filter((_item, index) => index % tickEvery === 0 || index === count - 1);
    return {
      width,
      height,
      padL,
      padR,
      padT,
      innerH,
      yTicks,
      xTicks,
      bandWidth,
      bandCenter,
      barWidth,
      barHeight,
      barY,
      tooltipLeft: (index: number) => Math.min(94, Math.max(6, (bandCenter(index) / width) * 100)),
      bandX: (index: number) => padL + bandWidth * index,
      pvBarX: (index: number) => bandCenter(index) - barWidth - 1,
      uvBarX: (index: number) => bandCenter(index) + 1,
    };
  });

  const cpu = computed(() => health.value?.cpu || {});
  const memory = computed(
    () => health.value?.memory || { totalBytes: 0, usedBytes: 0, freeBytes: 0, process: { rss: 0, heapUsed: 0, heapTotal: 0 } }
  );
  const disks = computed<any[]>(() => health.value?.disks || []);
  const database = computed(() => health.value?.database || {});
  const services = computed<any[]>(() => health.value?.services || []);
  const server = computed(() => health.value?.server || null);
  const uptimeText = computed(() => formatDuration(num(server.value?.processUptimeSeconds)));
  const healthCheckedAt = computed(() =>
    health.value?.generatedAt ? dayjs(health.value.generatedAt).format('HH:mm:ss') : '—'
  );

  function diskPercent(disk: any) {
    const total = num(disk?.totalBytes);
    return total > 0 ? Math.round((num(disk.usedBytes) / total) * 100) : 0;
  }

  const resourceRings = computed<ResourceRing[]>(() => {
    if (!health.value) return [];
    const cpuPercent = Math.min(100, Math.max(0, Math.round(num(cpu.value.usage) * 100)));
    const memoryTotal = num(memory.value.totalBytes);
    const memoryUsed = num(memory.value.usedBytes);
    const memoryPercent =
      memoryTotal > 0 ? Math.min(100, Math.max(0, Math.round((memoryUsed / memoryTotal) * 100))) : 0;
    const memoryFree = num(memory.value.freeBytes) || Math.max(0, memoryTotal - memoryUsed);
    const primaryDisk = disks.value[0];
    const storagePercent = primaryDisk ? Math.min(100, Math.max(0, diskPercent(primaryDisk))) : null;

    return [
      {
        key: 'cpu',
        label: 'CPU 使用率',
        percent: cpu.value.usage === undefined || cpu.value.usage === null ? null : cpuPercent,
        display: cpu.value.usage === undefined || cpu.value.usage === null ? '—' : `${cpuPercent}%`,
        color: levelColor(cpuPercent),
        meta: `负载 ${num(cpu.value.load1)} / ${num(cpu.value.load5)} / ${num(cpu.value.load15)}\n${num(
          cpu.value.cores
        )} 核`,
      },
      {
        key: 'memory',
        label: '内存使用率',
        percent: memoryTotal > 0 ? memoryPercent : null,
        display: memoryTotal > 0 ? `${memoryPercent}%` : '—',
        color: levelColor(memoryPercent),
        meta:
          memoryTotal > 0
            ? `${formatBytes(memoryUsed)} / ${formatBytes(memoryTotal)}\n可用 ${formatBytes(memoryFree)}`
            : '未获取到内存信息',
      },
      {
        key: 'storage',
        label: '存储使用率',
        percent: storagePercent,
        display: storagePercent === null ? '—' : `${storagePercent}%`,
        color: levelColor(storagePercent || 0),
        meta: primaryDisk
          ? `${formatBytes(primaryDisk.usedBytes)} / ${formatBytes(primaryDisk.totalBytes)}\n可用 ${formatBytes(
              primaryDisk.freeBytes
            )}`
          : '未获取到磁盘信息',
      },
    ];
  });

  function serviceLevel(item: any): StatusLevel {
    if (!item || typeof item.ok !== 'boolean') return 'unknown';
    if (!item.ok) return 'down';
    const detail = item.detail || {};
    if (item.label === '客服引擎' && detail.database && detail.database.ok === false) return 'degraded';
    if (item.label === '构建服务') {
      if (detail.rebuild && detail.rebuild.ok === false) return 'degraded';
      if (detail.preview && detail.preview.ok === false) return 'degraded';
      // 构建服务可达、但最近一次构建失败：这是降级而不是正常。
      if (detail.rebuild?.lastBuild?.status === 'failed') return 'degraded';
    }
    if (item.label === '对象存储' && detail.ok === false) return 'down';
    // 未配置 API Key 时探测本身是「成功」的，但能力并未真正可用：
    // 如实显示「未知」，不要伪装成正常。
    if (item.label === 'AI 生图' && detail.configured === false) return 'unknown';
    if (item.label === '网站翻译' && detail.apiKeyConfigured === false) return 'unknown';
    return 'ok';
  }

  function serviceDescription(item: any, level: StatusLevel): string {
    if (!item) return '健康接口未返回该项状态';
    if (item.ok === false) return item.message || '探测失败';
    const detail = item.detail || {};
    switch (item.label) {
      case '控制台服务':
        return '统一后台 Node 服务在线';
      case '客服引擎': {
        if (detail.database && detail.database.ok === false) {
          return `引擎可达，但数据库异常：${detail.database.message || '未知错误'}`;
        }
        const ws = detail.websocket || {};
        const clients = num(ws.Visitors) + num(ws.Agents) + num(ws.Admins);
        return `引擎与数据库正常 · 实时连接 ${clients} 个`;
      }
      case '构建服务': {
        if (detail.rebuild && detail.rebuild.ok === false) {
          return `构建服务不可达：${detail.rebuild.message || '未知错误'}`;
        }
        if (detail.preview && detail.preview.ok === false) {
          return `官网预览不可达：${detail.preview.message || '未知错误'}`;
        }
        const lastStatus = detail.rebuild?.lastBuild?.status;
        if (lastStatus === 'failed') return '构建服务可达，但最近一次构建失败';
        return `预览正常 · 构建服务就绪${lastStatus ? ` · 最近构建 ${lastStatus}` : ''}`;
      }
      case '对象存储':
        return `${detail.driver || '存储'} · ${detail.bucket || 'default'}${detail.ok === false ? ' · 连接失败' : ''}`;
      case 'AI 生图':
        return detail.configured
          ? `已配置 · 默认模型 ${detail.defaultModel || '-'}`
          : '未配置 API Key，能力未启用';
      case '网站翻译':
        return detail.apiKeyConfigured ? `已配置 · 模型 ${detail.model || '默认'}` : '未配置 API Key，翻译未启用';
      default:
        return level === 'unknown' ? '状态未知' : '探测正常';
    }
  }

  const serviceRows = computed<ServiceRow[]>(() => {
    const items = services.value;
    const labels = [
      ...KNOWN_SERVICE_LABELS,
      ...items.map((item) => String(item?.label || '')).filter((label) => label && !KNOWN_SERVICE_LABELS.includes(label)),
    ];
    const byLabel = new Map(items.map((item) => [String(item?.label || ''), item]));
    return labels.map((label, index) => {
      const item = byLabel.get(label);
      const level = serviceLevel(item);
      return {
        key: `s${index}`,
        label,
        level,
        statusLabel: STATUS_LABELS[level],
        tagColor: STATUS_COLORS[level],
        description: serviceDescription(item, level),
        latencyMs: item && Number.isFinite(Number(item.latencyMs)) ? Number(item.latencyMs) : null,
        checkedAt: healthCheckedAt.value,
      };
    });
  });

  const overall = computed(() => {
    const rows = serviceRows.value;
    if (!rows.length) {
      return { level: 'unknown' as StatusLevel, title: '状态未知', summary: '健康接口未返回服务状态' };
    }
    const down = rows.filter((row) => row.level === 'down').length;
    const degraded = rows.filter((row) => row.level === 'degraded').length;
    const unknown = rows.filter((row) => row.level === 'unknown').length;
    if (down > 0) {
      return {
        level: 'down' as StatusLevel,
        title: '部分服务故障',
        summary: `${down} 项故障${degraded > 0 ? `，${degraded} 项降级` : ''}${unknown > 0 ? `，${unknown} 项未知` : ''}`,
      };
    }
    if (degraded > 0) {
      return {
        level: 'degraded' as StatusLevel,
        title: '部分服务降级',
        summary: `${degraded} 项降级${unknown > 0 ? `，${unknown} 项未知` : ''}`,
      };
    }
    if (unknown > 0) {
      return {
        level: 'unknown' as StatusLevel,
        title: '部分服务状态未知',
        summary: `${unknown} 项未配置或状态未知`,
      };
    }
    return { level: 'ok' as StatusLevel, title: '所有系统正常', summary: `${rows.length} 项服务检查通过` };
  });

  /** 兼容旧版 admin-server：经现有 servicebridge 代理用引擎接口聚合「有访客消息的会话数」。 */
  async function conversationInquiries(conversationId: string, boundaries: Record<string, number>) {
    let before = '';
    let today = false;
    let month = false;
    for (let page = 0; page < 50; page += 1) {
      const response = await client.get(
        `${serviceBridgePrefix}/api/conversations/${encodeURIComponent(conversationId)}/messages`,
        { params: { limit: 100, ...(before ? { before } : {}) } }
      );
      const payload = response.data || {};
      const messages: any[] = Array.isArray(payload.messages) ? payload.messages : [];
      if (!messages.length) break;
      let oldest = Infinity;
      for (const message of messages) {
        if (message?.sender_type !== 'visitor') continue;
        const createdAt = Date.parse(message.created_at || '');
        if (!Number.isFinite(createdAt)) continue;
        if (createdAt < oldest) oldest = createdAt;
        if (createdAt >= boundaries.todayStart) today = true;
        if (createdAt >= boundaries.monthStart && createdAt < boundaries.monthEnd) month = true;
      }
      if (!payload.has_more || !payload.next_before) break;
      if (oldest < boundaries.monthStart) break;
      before = payload.next_before;
    }
    return { today, month };
  }

  async function loadInquiriesFromEngine(range: any) {
    const todayStart = Date.parse(range?.todayStart || '');
    const monthStart = Date.parse(range?.monthStart || '');
    const monthEnd = Date.parse(range?.monthEnd || '');
    if (![todayStart, monthStart, monthEnd].every(Number.isFinite)) return null;
    const response = await client.get(`${serviceBridgePrefix}/api/admin/conversations`);
    const conversations: any[] = Array.isArray(response.data?.conversations) ? response.data.conversations : [];
    const candidates = conversations.filter((conversation) => {
      const updatedAt = Date.parse(conversation?.updated_at || conversation?.updatedAt || '');
      return Number.isFinite(updatedAt) && updatedAt >= monthStart;
    });
    const boundaries = { todayStart, monthStart, monthEnd };
    let today = 0;
    let month = 0;
    for (let index = 0; index < candidates.length; index += 4) {
      const chunk = candidates.slice(index, index + 4);
      const results = await Promise.all(
        chunk.map((conversation) =>
          conversationInquiries(conversation.id, boundaries).catch(() => ({ today: false, month: false }))
        )
      );
      for (const result of results) {
        if (result.today) today += 1;
        if (result.month) month += 1;
      }
    }
    return { today, month };
  }

  async function load() {
    loading.value = true;
    error.value = '';
    const [overviewResult, healthResult] = await Promise.allSettled([
      // 看板只展示当前月；不传 month 让后端按看板时区取「本月」，
      // 避免旧进程对显式 month 的区间计算偏差影响趋势图。
      fetchAnalyticsOverview({ tz: timezone }),
      fetchSystemHealth(),
    ]);
    const failures: string[] = [];

    if (overviewResult.status === 'fulfilled') {
      const data = overviewResult.value?.data || overviewResult.value || null;
      overview.value = data;
      // 新版 overview 一定带 inquiries 字段（引擎不可用时为 null）。只有旧版
      // admin-server 完全缺少该字段时，才走上面的 servicebridge 兼容聚合，
      // 避免引擎故障时同一份数据被重复请求两次。
      const hasInquiryField = Boolean(
        data?.today && Object.prototype.hasOwnProperty.call(data.today, 'inquiries')
      );
      if (hasInquiryField) {
        const todayInquiries = data.today.inquiries;
        const monthInquiries = data?.month?.inquiries;
        inquiryToday.value =
          todayInquiries === null || todayInquiries === undefined || !Number.isFinite(Number(todayInquiries))
            ? null
            : Number(todayInquiries);
        inquiryMonth.value =
          monthInquiries === null || monthInquiries === undefined || !Number.isFinite(Number(monthInquiries))
            ? null
            : Number(monthInquiries);
      } else {
        try {
          const computedInquiries = await loadInquiriesFromEngine(data?.range);
          inquiryToday.value = computedInquiries ? computedInquiries.today : null;
          inquiryMonth.value = computedInquiries ? computedInquiries.month : null;
        } catch {
          inquiryToday.value = null;
          inquiryMonth.value = null;
        }
      }
    } else {
      failures.push(`访问统计：${overviewResult.reason?.message || '加载失败'}`);
      inquiryToday.value = null;
      inquiryMonth.value = null;
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
  const { pause, resume } = useIntervalFn(
    () => {
      load();
    },
    30000,
    { immediate: false }
  );

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
    margin-bottom: 8px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    .stat-label {
      display: flex;
      align-items: center;
      gap: 4px;
      color: var(--color-text-3);
      font-size: 13px;
    }
    .help-icon {
      color: var(--color-text-4);
      font-size: 13px;
      cursor: help;
    }
    strong {
      font-size: 28px;
      font-weight: 700;
      line-height: 1.2;
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
  .metric-note {
    margin: 0 0 12px;
    line-height: 18px;
  }
  .block {
    margin-bottom: 12px;
  }
  .trend-card {
    .legend {
      flex-wrap: wrap;
    }
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
  .chart-foot {
    margin-top: 8px;
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
  .status-overall {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 8px;
    padding: 10px 12px;
    border-radius: 8px;
    background: var(--color-fill-1);
    &.ok {
      background: rgba(0, 180, 42, 0.08);
    }
    &.degraded {
      background: rgba(255, 125, 0, 0.1);
    }
    &.down {
      background: rgba(245, 63, 63, 0.1);
    }
    &.unknown {
      background: var(--color-fill-2);
    }
  }
  .status-overall-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    strong {
      font-size: 14px;
    }
    small {
      color: var(--color-text-3);
      font-size: 12px;
    }
  }
  .status-dot {
    flex: 0 0 auto;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #86909c;
    &.ok {
      background: #00b42a;
      box-shadow: 0 0 0 3px rgba(0, 180, 42, 0.12);
    }
    &.degraded {
      background: #ff7d00;
      box-shadow: 0 0 0 3px rgba(255, 125, 0, 0.14);
    }
    &.down {
      background: #f53f3f;
      box-shadow: 0 0 0 3px rgba(245, 63, 63, 0.14);
    }
    &.unknown {
      background: #86909c;
      box-shadow: 0 0 0 3px rgba(134, 144, 156, 0.14);
    }
  }
  .services {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .service-row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 11px 2px;
    border-bottom: 1px solid var(--color-fill-2);
    &:last-child {
      border-bottom: none;
    }
    .status-dot {
      margin-top: 6px;
    }
  }
  .service-main {
    min-width: 0;
    flex: 1;
  }
  .service-name {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .service-label {
    font-weight: 500;
  }
  .service-desc {
    margin-top: 2px;
    color: var(--color-text-3);
    font-size: 12px;
    line-height: 18px;
    word-break: break-all;
  }
  .service-side {
    display: flex;
    flex: 0 0 auto;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    padding-top: 1px;
  }
  .rings {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 12px;
    margin-bottom: 12px;
  }
  .ring {
    display: flex;
    flex-direction: column;
    align-items: center;
    min-width: 0;
    padding: 6px 2px;
  }
  .ring-visual {
    position: relative;
    width: 108px;
    height: 108px;
  }
  .ring-svg {
    display: block;
    width: 108px;
    height: 108px;
    .ring-track {
      fill: none;
      stroke: var(--color-fill-2);
      stroke-width: 10;
    }
    .ring-value {
      fill: none;
      stroke-width: 10;
      stroke-linecap: round;
      transition: stroke-dasharray 240ms ease;
    }
  }
  .ring-center {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    strong {
      font-size: 20px;
      font-weight: 700;
    }
  }
  .ring-label {
    margin-top: 4px;
    color: var(--color-text-2);
    font-size: 13px;
    font-weight: 500;
  }
  .ring-meta {
    margin-top: 2px;
    color: var(--color-text-3);
    font-size: 12px;
    line-height: 17px;
    text-align: center;
    white-space: pre-line;
    word-break: normal;
    overflow-wrap: anywhere;
  }
  .resource-details {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 10px;
    border-top: 1px solid var(--color-fill-2);
  }
  .resource-line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 6px 10px;
    font-size: 12px;
    line-height: 18px;
  }
  .resource-line-label {
    flex: 0 0 110px;
    color: var(--color-text-2);
    font-weight: 500;
  }
  .resource-line-value {
    color: var(--color-text-1);
  }
  .resource-line-extra {
    min-width: 0;
    word-break: break-all;
  }
  @media (max-width: 900px) {
    .rings {
      grid-template-columns: 1fr;
    }
    .ring {
      flex-direction: row;
      gap: 12px;
    }
    .ring-meta {
      text-align: left;
    }
  }
</style>
