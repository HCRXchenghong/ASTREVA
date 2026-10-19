<template>
  <a-card :bordered="false" class="system-settings-page">
    <template #title>系统设置</template>
    <template #extra>
      <a-typography-text type="secondary">
        每个分区独立保存，互不影响。
      </a-typography-text>
    </template>

    <a-alert class="tips" type="info">
      原「营业时间与联系方式」「推送设置」「提醒设置」已合并到本页，用上方 Tab 切换，每个分区各自保存。
      原「系统 → 访客画像」设置与「运营中心 → 访客画像 → 画像设置」重复，已删除，请到运营中心维护。
    </a-alert>

    <a-tabs v-model:active-key="activeTab" lazy-load>
      <a-tab-pane key="general" title="营业时间与联系方式">
        <GeneralSettings />
      </a-tab-pane>
      <a-tab-pane key="push" title="推送设置">
        <PushSettings />
      </a-tab-pane>
      <a-tab-pane key="alerts" title="提醒设置">
        <AlertSettings />
      </a-tab-pane>
    </a-tabs>
  </a-card>
</template>

<script lang="ts" setup>
  import { ref, watch } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import GeneralSettings from './general.vue';
  import PushSettings from './push.vue';
  import AlertSettings from './alerts.vue';

  // Tab key 与 ?tab= 查询参数双向同步，便于旧地址 /system/{general,push,alerts} 重定向后直接落到对应分区。
  const TAB_KEYS = ['general', 'push', 'alerts'];
  const route = useRoute();
  const router = useRouter();

  function normalizeTab(value: unknown) {
    const key = String(value || '');
    return TAB_KEYS.includes(key) ? key : 'general';
  }

  const activeTab = ref(normalizeTab(route.query.tab));

  watch(activeTab, (key) => {
    if (normalizeTab(route.query.tab) === key) return;
    const query = { ...route.query };
    if (key === 'general') {
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
</script>

<style scoped lang="less">
  .tips {
    margin-bottom: 14px;
  }
</style>
