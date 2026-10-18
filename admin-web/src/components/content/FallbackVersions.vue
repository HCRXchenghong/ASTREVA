<template>
  <div class="fallback-versions">
    <a-alert type="info" show-icon class="tips">
      当后台内容接口不可用时，官网构建会使用当前启用的快照。版本从已保存的官网内容创建；
      <strong>启用</strong>或删除当前启用版本都会自动重新发布官网。
    </a-alert>

    <a-card :bordered="false" class="create-card">
      <div class="create-row">
        <a-input
          v-model="name"
          placeholder="例如：2026-08-22 发布前备份"
          :disabled="busy"
          @press-enter="submitCreate"
        />
        <a-button type="primary" :loading="busy" :disabled="busy" @click="submitCreate">
          <template #icon><icon-plus /></template>
          从当前官网内容创建版本
        </a-button>
      </div>
      <p class="hint">
        版本名最长 80 个字符；未填写时服务端会使用默认名称。当前共
        {{ versions.length }} 个版本。
      </p>
    </a-card>

    <a-spin :loading="loading" style="width: 100%">
      <a-empty v-if="!versions.length" description="暂无降级版本" />
      <div v-else class="version-list">
        <div v-for="version in versions" :key="version.id" class="version-card">
          <div class="version-head">
            <div>
              <strong>{{ version.name }}</strong>
              <a-tag v-if="version.id === activeId" color="arcoblue" class="active-tag">
                当前启用
              </a-tag>
            </div>
            <span class="version-time">{{ localTime(version.createdAt) }}</span>
          </div>
          <p class="version-id">ID：{{ version.id }}</p>
          <a-space wrap>
            <a-button
              v-if="version.id !== activeId"
              size="small"
              type="primary"
              :disabled="busy"
              @click="confirmActivate(version)"
            >
              启用并发布
            </a-button>
            <a-button
              size="small"
              status="danger"
              :disabled="busy || versions.length <= 1"
              :title="versions.length <= 1 ? '最后一个降级版本不可删除' : ''"
              @click="confirmDelete(version)"
            >
              删除版本
            </a-button>
          </a-space>
        </div>
      </div>
    </a-spin>
  </div>
</template>

<script lang="ts" setup>
  import { ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import type { FallbackVersion } from '@/api/content';

  const props = withDefaults(
    defineProps<{
      versions?: FallbackVersion[];
      activeId?: string;
      busy?: boolean;
      loading?: boolean;
    }>(),
    { versions: () => [], activeId: '', busy: false, loading: false }
  );

  const emit = defineEmits<{
    (e: 'create', name: string): void;
    (e: 'activate', id: string): void;
    (e: 'remove', id: string): void;
    (e: 'refresh'): void;
  }>();

  const name = ref('');

  function localTime(value?: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '未知时间';
  }

  function submitCreate() {
    if (props.busy) return;
    emit('create', name.value.trim());
    name.value = '';
  }

  function confirmActivate(version: FallbackVersion) {
    Modal.confirm({
      title: '启用降级版本',
      content: `确认启用“${version.name}”？启用后会上线该快照并自动重新发布官网。`,
      okText: '启用并发布',
      cancelText: '取消',
      onOk: () => emit('activate', version.id),
    });
  }

  function confirmDelete(version: FallbackVersion) {
    if (props.versions.length <= 1) {
      Message.warning('至少需要保留一个降级版本，最后一个版本不可删除。');
      return;
    }
    const isActive = version.id === props.activeId;
    Modal.confirm({
      title: '删除降级版本',
      content: isActive
        ? `“${version.name}”是当前启用版本。删除后系统会自动切换到其他版本并重新发布官网，确认继续？`
        : `确认删除降级版本“${version.name}”？此操作不可恢复。`,
      okText: '确认删除',
      cancelText: '取消',
      okButtonProps: { status: 'danger' },
      onOk: () => emit('remove', version.id),
    });
  }
</script>

<style scoped lang="less">
  .fallback-versions {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .tips {
    margin-bottom: 0;
  }
  .create-card {
    border: 1px solid var(--color-border-2);
  }
  .create-row {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .create-row .arco-input-wrapper {
    flex: 1;
    min-width: 240px;
  }
  .hint {
    margin: 10px 0 0;
    font-size: 12px;
    color: var(--color-text-3);
  }
  .version-list {
    display: grid;
    gap: 12px;
  }
  .version-card {
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    padding: 14px 16px;
    background: #fff;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .version-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .active-tag {
    margin-left: 8px;
  }
  .version-time {
    font-size: 12px;
    color: var(--color-text-3);
  }
  .version-id {
    margin: 0;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
  }
</style>
