<script lang="ts" setup>
  import { useRoute, useRouter } from 'vue-router';

  const router = useRouter();
  const route = useRoute();
  const { params, query } = route;
  const { path } = params;
  router.replace({ path: `/${Array.isArray(path) ? path.join('/') : path}`, query });
</script>

<template>
  <div />
</template>
