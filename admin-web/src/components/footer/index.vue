<template>
  <a-layout-footer class="footer">
    © {{ year }} ASTREVA · 统一控制台
  </a-layout-footer>
</template>

<script lang="ts" setup>
  const year = new Date().getFullYear();
</script>

<style lang="less" scoped>
  .footer {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 40px;
    color: var(--color-text-2);
    text-align: center;
  }
</style>
