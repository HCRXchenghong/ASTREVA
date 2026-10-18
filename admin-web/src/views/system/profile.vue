<template>
  <a-card :bordered="false">
    <template #title>访客画像</template>
    <VisitorProfileSettings />
  </a-card>
</template>

<script lang="ts" setup>
  import VisitorProfileSettings from '@/views/operations/components/VisitorProfileSettings.vue';
</script>
