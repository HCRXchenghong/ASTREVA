<template>
  <a-card :bordered="false" class="media-page">
    <template #title>
      <a-space>
        <span>R2 素材库</span>
        <a-tag :color="storageTagColor">{{ storageTagText }}</a-tag>
      </a-space>
    </template>
    <template #extra>
      <a-space wrap :size="8">
        <a-tooltip v-if="activeTab === 'dashboard'" content="仪表盘自动刷新服务器资源与素材统计">
          <a-space :size="6" class="auto-refresh">
            <a-switch v-model="autoRefresh" size="small" />
            <span>实时刷新</span>
          </a-space>
        </a-tooltip>
        <a-button v-if="activeTab === 'gallery'" size="small" @click="categoryVisible = true">
          分类管理
        </a-button>
        <a-button v-if="activeTab === 'gallery'" size="small" type="primary" @click="uploadVisible = true">
          上传素材
        </a-button>
        <a-button size="small" @click="openDrawer('settings')">系统设置</a-button>
        <a-button size="small" @click="openDrawer('guide')">接入指南</a-button>
        <a-button size="small" :loading="refreshing" @click="refreshCurrent">刷新</a-button>
      </a-space>
    </template>

    <a-alert v-if="initError" type="error" class="init-error">
      <template #title>素材数据加载失败</template>
      {{ initError }}
      <a-link class="retry-link" @click="initialize">重试</a-link>
    </a-alert>

    <a-tabs v-model:active-key="activeTab" lazy-load>
      <a-tab-pane key="dashboard" title="仪表盘">
        <MediaDashboard
          :stats="stats"
          :runtime="runtime"
          :categories="categories"
          :loading="loadingDashboard"
        />
      </a-tab-pane>

      <a-tab-pane key="gallery" title="素材管理">
        <MediaGallery
          ref="galleryRef"
          :categories="categories"
          :stats-total="Number(stats.total || 0)"
          :settings="settings"
          @changed="onGalleryChanged"
        />
      </a-tab-pane>

      <a-tab-pane key="ai" title="AI 生图工作站">
        <MediaAiWorkbench
          :categories="categories"
          :settings="settings"
          @changed="onAiChanged"
        />
      </a-tab-pane>
    </a-tabs>

    <MediaUploadModal
      v-model:visible="uploadVisible"
      :categories="categories"
      :settings="settings"
      @uploaded="onUploaded"
    />

    <MediaCategoryModal
      v-model:visible="categoryVisible"
      :categories="categories"
      @changed="onCategoriesChanged"
    />

    <!-- 设置与接入指南不再各占一个标签页，收进按钮 + 抽屉，页面只留三块主要工作区。 -->
    <a-drawer
      v-model:visible="drawerVisible"
      :width="720"
      :title="drawerTitle"
      :footer="false"
      unmount-on-close
    >
      <MediaSettingsPanel
        v-if="drawerKind === 'settings'"
        :settings="settings"
        :runtime="runtime"
        :stats="stats"
        :loading="loadingSettings"
        :error="settingsError"
        @saved="loadSettings"
      />
      <MediaGuide v-else />
    </a-drawer>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchMediaCategories, fetchMediaRuntime, fetchMediaSettings, fetchMediaStats } from '@/api/media';
  import type { MediaCategory, MediaRuntime, MediaSettings, MediaStats } from '@/api/media';
  import MediaDashboard from '@/components/media/MediaDashboard.vue';
  import MediaGallery from '@/components/media/MediaGallery.vue';
  import MediaUploadModal from '@/components/media/MediaUploadModal.vue';
  import MediaCategoryModal from '@/components/media/MediaCategoryModal.vue';
  import MediaAiWorkbench from '@/components/media/MediaAiWorkbench.vue';
  import MediaSettingsPanel from '@/components/media/MediaSettingsPanel.vue';
  import MediaGuide from '@/components/media/MediaGuide.vue';

  const activeTab = ref('dashboard');
  const stats = ref<MediaStats>({});
  const runtime = ref<MediaRuntime>({});
  const categories = ref<MediaCategory[]>([]);
  const settings = ref<MediaSettings | null>(null);
  const loadingDashboard = ref(false);
  const loadingSettings = ref(false);
  const refreshing = ref(false);
  const initError = ref('');
  const settingsError = ref('');
  const uploadVisible = ref(false);
  const categoryVisible = ref(false);
  const galleryRef = ref<InstanceType<typeof MediaGallery> | null>(null);

  // 系统设置 / 接入指南共用一个抽屉。
  const drawerVisible = ref(false);
  const drawerKind = ref<'settings' | 'guide'>('settings');
  const drawerTitle = computed(() => (drawerKind.value === 'settings' ? '系统设置' : 'R2 接入指南'));

  const storage = computed(() => stats.value.storage || runtime.value.storage || {});
  const storageTagText = computed(() => {
    if (storage.value.ok === undefined) return '存储检查中';
    return storage.value.ok ? `${storage.value.bucket || 'S3'} 已连通` : '存储连接异常';
  });
  const storageTagColor = computed(() => {
    if (storage.value.ok === undefined) return 'gray';
    return storage.value.ok ? 'green' : 'red';
  });

  async function loadStats() {
    stats.value = (await fetchMediaStats()).data || {};
  }

  async function loadRuntime() {
    runtime.value = (await fetchMediaRuntime()).data || {};
  }

  async function loadCategories() {
    categories.value = (await fetchMediaCategories()).data || [];
    // 分类被删除后，素材会移入“未分类”，同步刷新统计数量。
  }

  async function loadSettings() {
    settings.value = (await fetchMediaSettings()).data || null;
  }

  async function initialize() {
    initError.value = '';
    settingsError.value = '';
    loadingDashboard.value = true;
    loadingSettings.value = true;
    const results = await Promise.allSettled([
      loadStats(),
      loadRuntime(),
      loadCategories(),
      loadSettings(),
    ]);
    const failed = results.slice(0, 3).find((result) => result.status === 'rejected') as
      | PromiseRejectedResult
      | undefined;
    if (failed) {
      initError.value =
        failed.reason?.response?.data?.error?.message ||
        failed.reason?.message ||
        '部分素材数据加载失败。';
    }
    const settingsFailure = results[3] as PromiseSettledResult<unknown>;
    if (settingsFailure.status === 'rejected') {
      settingsError.value =
        (settingsFailure.reason as any)?.response?.data?.error?.message ||
        (settingsFailure.reason as any)?.message ||
        '运行配置加载失败。';
    }
    loadingDashboard.value = false;
    loadingSettings.value = false;
  }

  function openDrawer(kind: 'settings' | 'guide') {
    drawerKind.value = kind;
    drawerVisible.value = true;
    if (kind === 'settings') {
      loadingSettings.value = true;
      Promise.allSettled([loadSettings(), loadRuntime()]).finally(() => {
        loadingSettings.value = false;
      });
    }
  }

  async function refreshCurrent() {
    refreshing.value = true;
    try {
      if (activeTab.value === 'gallery') {
        await Promise.allSettled([loadStats(), loadCategories(), galleryRef.value?.reload()]);
      } else {
        await Promise.allSettled([loadStats(), loadRuntime(), loadCategories()]);
      }
      if (drawerVisible.value && drawerKind.value === 'settings') {
        await Promise.allSettled([loadSettings(), loadRuntime()]);
      }
    } finally {
      refreshing.value = false;
    }
  }

  async function afterMutation() {
    await Promise.allSettled([loadStats(), loadCategories(), galleryRef.value?.reload()]);
  }

  function onUploaded() {
    afterMutation();
  }

  function onGalleryChanged() {
    Promise.allSettled([loadStats(), loadCategories()]);
  }

  function onCategoriesChanged() {
    afterMutation();
  }

  function onAiChanged() {
    afterMutation();
  }

  // 仪表盘实时刷新：沿用旧页面的 2 秒运行时 / 15 秒统计节奏。
  // 轮询失败时自动暂停，避免 axios 拦截器持续弹窗。
  const autoRefresh = ref(true);
  let runtimeTimer: ReturnType<typeof setInterval> | undefined;
  let statsTimer: ReturnType<typeof setInterval> | undefined;

  function stopPolling() {
    if (runtimeTimer) clearInterval(runtimeTimer);
    if (statsTimer) clearInterval(statsTimer);
    runtimeTimer = undefined;
    statsTimer = undefined;
  }

  function startPolling() {
    stopPolling();
    if (!autoRefresh.value) return;
    runtimeTimer = setInterval(() => {
      if (document.visibilityState !== 'visible' || activeTab.value !== 'dashboard') return;
      loadRuntime().catch(() => pausePolling('无法读取运行状态'));
    }, 2000);
    statsTimer = setInterval(() => {
      if (document.visibilityState !== 'visible' || activeTab.value !== 'dashboard') return;
      Promise.all([loadStats(), loadCategories()]).catch(() =>
        pausePolling('无法读取素材统计')
      );
    }, 15000);
  }

  function pausePolling(reason: string) {
    autoRefresh.value = false;
    stopPolling();
    Message.warning(`实时刷新已暂停：${reason}。`);
  }

  watch([activeTab, autoRefresh], () => {
    if (activeTab.value === 'dashboard') startPolling();
    else stopPolling();
  });

  onMounted(() => {
    initialize();
    startPolling();
  });

  onBeforeUnmount(stopPolling);
</script>

<style scoped>
  .media-page :deep(.arco-card-header) {
    border-bottom: none;
  }

  .init-error {
    margin-bottom: 12px;
  }

  .auto-refresh {
    color: var(--color-text-2);
    font-size: 13px;
    cursor: pointer;
  }

  .retry-link {
    margin-left: 8px;
  }
</style>
