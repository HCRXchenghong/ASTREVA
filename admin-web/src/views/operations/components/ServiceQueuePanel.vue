<template>
  <div class="queue-panel" data-testid="queue-panel">
    <div class="panel-head">
      <a-space>
        <a-typography-text type="secondary" data-testid="queue-total">
          排队中 {{ items.length }} 个会话
        </a-typography-text>
        <a-typography-text type="secondary" data-testid="queue-max-wait">
          最长等待 {{ formatDuration(maxWaitingSeconds) }}
        </a-typography-text>
      </a-space>
      <a-button :loading="loading" size="small" data-testid="queue-refresh" @click="load">
        刷新
      </a-button>
    </div>

    <a-alert class="tips" type="info">
      列表每 10 秒自动刷新（弹窗关闭或页面切到后台时暂停）；等待时长按进入队列时间实时计算。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-table
      :data="items"
      :loading="loading"
      :pagination="false"
      row-key="conversation_id"
      size="small"
      data-testid="queue-table"
    >
      <template #columns>
        <a-table-column title="会话 ID" :width="200">
          <template #cell="{ record }">
            <a-typography-text :title="record.conversation_id">
              {{ shortId(record.conversation_id) }}
            </a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="访客名">
          <template #cell="{ record }">{{ visitorName(record) }}</template>
        </a-table-column>
        <a-table-column title="分组">
          <template #cell="{ record }">{{ record.group_name || '未分组' }}</template>
        </a-table-column>
        <a-table-column title="优先级" data-index="priority" :width="90" />
        <a-table-column title="进入队列时间" :width="170">
          <template #cell="{ record }">{{ localTime(record.enqueued_at) }}</template>
        </a-table-column>
        <a-table-column title="已等待时长" :width="130">
          <template #cell="{ record }">
            <!-- 显式依赖 now，保证每秒重新渲染等待时长 -->
            <span :data-testid="`queue-waiting-${record.conversation_id}`">
              {{ formatDuration(waitedSeconds(record, now)) }}
            </span>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="160">
          <template #cell="{ record }">
            <a-space>
              <a-link @click="openConversation(record)">查看会话</a-link>
              <a-link @click="copyId(record)">复制 ID</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="当前没有排队会话" />
      </template>
    </a-table>

    <a-typography-text type="secondary" class="footnote">
      提示：点「查看会话」会跳到「运营中心 → 询盘管理」并自动选中该会话；若会话已结束则无法打开。
    </a-typography-text>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, onUnmounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchServiceQueue } from '@/api/admin';

  const emit = defineEmits<{ (e: 'navigate', conversationId: string): void }>();

  // 自动刷新间隔；页面隐藏（document.hidden）时跳过本轮，回到前台后自然恢复。
  const REFRESH_MS = 10_000;

  const items = ref<any[]>([]);
  const loading = ref(false);
  const loadError = ref('');
  const now = ref(Date.now());
  // 每次拉取的时间戳：后端未给 enqueued_at 时，用 waiting_seconds 加上本地已流逝的时间兜底。
  const fetchedAt = ref(Date.now());
  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let tickTimer: ReturnType<typeof setInterval> | null = null;

  const maxWaitingSeconds = computed(() =>
    items.value.reduce(
      (max, item) => Math.max(max, waitedSeconds(item, now.value)),
      0
    )
  );

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function shortId(value: unknown) {
    const id = String(value ?? '');
    if (!id) return '-';
    return id.length > 14 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
  }

  function visitorName(record: any) {
    const name = String(record?.visitor_name ?? '').trim();
    return name || '访客';
  }

  function localTime(value: unknown) {
    const raw = String(value ?? '');
    if (!raw) return '-';
    const time = Date.parse(raw);
    if (!Number.isFinite(time)) return raw;
    return new Date(time).toLocaleString('zh-CN', { hour12: false });
  }

  function waitedSeconds(record: any, nowMs = Date.now()) {
    const elapsed = Math.max(0, Math.floor((nowMs - fetchedAt.value) / 1000));
    // 后端同时给出 waiting_seconds（契约）与 wait_seconds（当前实现），优先用服务端秒数 + 本地流逝秒。
    const base = Number(record?.waiting_seconds ?? record?.wait_seconds);
    if (Number.isFinite(base) && base >= 0) return Math.floor(base) + elapsed;
    const enqueued = Date.parse(String(record?.enqueued_at ?? ''));
    if (Number.isFinite(enqueued)) {
      const seconds = Math.floor((nowMs - enqueued) / 1000);
      // 防御服务端零值时间（0001-01-01T00:00:00Z）导致的超大时长。
      if (seconds >= 0 && seconds < 366 * 24 * 3600) return seconds;
    }
    return elapsed;
  }

  function formatDuration(seconds: number) {
    const total = Math.max(0, Math.floor(Number(seconds) || 0));
    const minutes = Math.floor(total / 60);
    if (!minutes) return `${total} 秒`;
    return `${minutes} 分 ${total % 60} 秒`;
  }

  function openConversation(record: any) {
    const id = String(record?.conversation_id ?? '');
    if (!id) {
      Message.warning('该会话没有 ID');
      return;
    }
    emit('navigate', id);
  }

  async function copyId(record: any) {
    const id = String(record?.conversation_id ?? '');
    if (!id) {
      Message.warning('该会话没有 ID');
      return;
    }
    try {
      await navigator.clipboard.writeText(id);
      Message.success('会话 ID 已复制');
    } catch {
      Message.error('复制失败，请手动选择复制');
    }
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchServiceQueue();
      items.value = Array.isArray(result?.queue) ? result.queue : [];
      fetchedAt.value = Date.now();
      now.value = Date.now();
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '排队队列加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  onMounted(() => {
    load();
    tickTimer = setInterval(() => {
      now.value = Date.now();
    }, 1000);
    refreshTimer = setInterval(() => {
      if (document.hidden) return;
      load();
    }, REFRESH_MS);
  });

  onUnmounted(() => {
    if (tickTimer) clearInterval(tickTimer);
    if (refreshTimer) clearInterval(refreshTimer);
    tickTimer = null;
    refreshTimer = null;
  });
</script>

<style scoped lang="less">
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 10px;
  }
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 14px;
  }
  .footnote {
    display: block;
    margin-top: 12px;
    font-size: 12px;
  }
</style>
