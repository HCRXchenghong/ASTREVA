<template>
  <a-config-provider :locale="locale">
    <router-view />
  </a-config-provider>
</template>

<script lang="ts" setup>
  import { computed } from 'vue';
  import zhCN from '@arco-design/web-vue/es/locale/lang/zh-cn';

  const locale = computed(() => zhCN);
</script>
