<template>
  <a-card :bordered="false" class="ai-manage">
    <template #title>AI 管理</template>
    <template #extra>
      <a-typography-text type="secondary" class="head-hint">
        各能力的绑定与参数、故障转移候选链、限额都在下面的页签里；模型供应商与知识库见左侧子导航。
      </a-typography-text>
    </template>

    <a-tabs v-model:active-key="activeTab" lazy-load type="line" size="large">
      <a-tab-pane key="reception" title="接待 AI">
        <AiReceptionPanel />
      </a-tab-pane>
      <a-tab-pane key="translation" title="翻译 AI">
        <AiTranslationPanel />
      </a-tab-pane>
      <a-tab-pane key="seo" title="AI SEO">
        <AiSeoPanel />
      </a-tab-pane>
      <a-tab-pane key="image" title="AI 生图">
        <AiImagePanel />
      </a-tab-pane>
      <a-tab-pane key="bindings" title="能力绑定">
        <AiBindingsPanel />
      </a-tab-pane>
      <a-tab-pane key="limits" title="限额">
        <AiLimitsPanel />
      </a-tab-pane>
    </a-tabs>
  </a-card>
</template>

<script lang="ts" setup>
  import { ref, watch } from 'vue';
  import { useRoute } from 'vue-router';
  import AiReceptionPanel from './reception.vue';
  import AiTranslationPanel from './translation.vue';
  import AiSeoPanel from './seo.vue';
  import AiImagePanel from './image.vue';
  import AiBindingsPanel from './bindings.vue';
  import AiLimitsPanel from './limits.vue';

  // 页签顺序与左侧子导航一致；能力参数按「每个能力一份配置」组织。
  const TAB_KEYS = ['reception', 'translation', 'seo', 'image', 'bindings', 'limits'];

  const route = useRoute();

  // 兼容旧入口：/ai/reception、/ai/image 等别名路径落到同名页签；也支持 ?tab=xxx 直达。
  // 普通 /ai/manage（无 tab 参数）保持当前页签，避免点侧边栏「AI 管理」时被打回第一个页签。
  function tabFromRoute(current: string): string {
    const queryTab = route.query?.tab;
    if (typeof queryTab === 'string' && TAB_KEYS.includes(queryTab)) {
      return queryTab;
    }
    const segment = route.path.split('/').filter(Boolean).pop() || '';
    return segment !== 'manage' && TAB_KEYS.includes(segment) ? segment : current;
  }

  const activeTab = ref(tabFromRoute('reception'));

  watch(
    () => route.fullPath,
    () => {
      const next = tabFromRoute(activeTab.value);
      if (next !== activeTab.value) activeTab.value = next;
    }
  );
</script>

<style scoped>
  .head-hint {
    font-size: 12px;
  }
</style>
