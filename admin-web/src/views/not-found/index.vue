<template>
  <div class="not-found">
    <a-result status="404" title="页面不存在" subtitle="请检查地址，或从左侧菜单重新进入。">
      <template #extra>
        <a-button type="primary" @click="router.replace('/workspace')">返回工作台</a-button>
      </template>
    </a-result>
  </div>
</template>

<script lang="ts" setup>
  import { useRouter } from 'vue-router';

  const router = useRouter();
</script>

<style scoped>
  .not-found {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 70vh;
  }
</style>
