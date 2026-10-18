<template>
  <div class="legacy-frame">
    <a-alert type="warning" class="notice">
      该模块正在迁移到 Arco 控制台，暂时沿用既有页面（功能不受影响）。
    </a-alert>
    <iframe :src="src" :title="title" class="frame" />
  </div>
</template>

<script lang="ts" setup>
  defineProps<{ src: string; title: string }>();
</script>

<style scoped>
  .legacy-frame {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 120px);
  }
  .notice {
    margin-bottom: 10px;
  }
  .frame {
    flex: 1;
    width: 100%;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: #fff;
  }
</style>
