<template>
  <a-card :bordered="false">
    <a-tabs v-model:active-key="activeTab">
      <a-tab-pane key="workbench" title="生成图片">
        <MediaAiWorkbench
          :categories="categories"
          :settings="settings"
          @changed="onGenerated"
        />
      </a-tab-pane>
      <a-tab-pane key="settings" title="生图配置">
        <AiImageSettings :settings="settings" @saved="loadSettings" />
      </a-tab-pane>
    </a-tabs>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import type { MediaCategory, MediaSettings } from '@/api/media';
  import { fetchMediaCategories, fetchMediaSettings } from '@/api/media';
  import MediaAiWorkbench from '@/components/media/MediaAiWorkbench.vue';
  import AiImageSettings from '@/components/ai/AiImageSettings.vue';

  const activeTab = ref('workbench');
  const categories = ref<MediaCategory[]>([]);
  const settings = ref<MediaSettings | null>(null);

  async function loadSettings() {
    settings.value = (await fetchMediaSettings()).data || null;
  }

  async function onGenerated() {
    // 生成入库会新增素材，重新拉一次配置以刷新容量/统计展示。
    await loadSettings();
  }

  onMounted(async () => {
    const [categoryPayload] = await Promise.allSettled([fetchMediaCategories(), loadSettings()]);
    if (categoryPayload.status === 'fulfilled') {
      categories.value = (categoryPayload.value as { data?: MediaCategory[] }).data || [];
    }
  });
</script>
