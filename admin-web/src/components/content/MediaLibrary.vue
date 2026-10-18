<template>
  <div class="media-library">
    <a-alert type="info" show-icon class="tips">
      上传成功后会立即保存到素材库。删除前会检查官网引用，仍被使用的素材不允许删除。
    </a-alert>

    <div class="toolbar">
      <a-space wrap>
        <a-input-search
          v-model="keyword"
          placeholder="搜索素材名称"
          style="width: 240px"
          @search="load"
          @press-enter="load"
        />
        <a-radio-group v-model="type" type="button" @change="load">
          <a-radio value="">全部</a-radio>
          <a-radio value="image">图片</a-radio>
          <a-radio value="video">视频</a-radio>
          <a-radio value="file">文件</a-radio>
        </a-radio-group>
        <a-button :loading="loading" @click="load">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </a-space>
      <a-button type="primary" :loading="uploading" @click="triggerUpload">
        <template #icon><icon-upload /></template>
        上传媒体
      </a-button>
      <input ref="fileInput" class="hidden-input" type="file" multiple accept="image/*,video/*,.pdf,.zip,.dwg,.dxf" @change="onFiles" />
    </div>

    <a-spin :loading="loading" style="width: 100%">
      <a-empty v-if="!items.length" description="素材库暂无素材" />
      <div v-else class="media-grid">
        <div v-for="item in items" :key="item.id" class="media-card">
          <div class="media-body">
            <img v-if="item.type === 'image'" :src="item.url" :alt="item.name" />
            <video v-else-if="item.type === 'video'" :src="item.url" controls muted />
            <div v-else class="media-file"><icon-file /> 文件</div>
          </div>
          <div class="media-meta">
            <div class="media-name" :title="item.name">{{ item.name }}</div>
            <div class="media-sub">
              {{ typeLabel(item.type) }}
              <template v-if="item.size"> · {{ formatBytes(item.size) }}</template>
            </div>
            <a-space wrap class="media-actions">
              <a-link @click="copy(item)">复制地址</a-link>
              <a-link @click="showReferences(item)">查看引用</a-link>
              <a-link status="danger" @click="confirmDelete(item)">删除</a-link>
            </a-space>
          </div>
        </div>
      </div>
    </a-spin>

    <a-modal v-model:visible="referencesVisible" title="素材引用" :footer="false" :width="620">
      <a-spin :loading="referencesLoading" style="width: 100%">
        <a-typography-paragraph>
          {{ current?.name }}
        </a-typography-paragraph>
        <a-empty v-if="!references.length" description="当前素材没有被官网内容引用" />
        <ul v-else class="reference-list">
          <li v-for="field in references" :key="field">{{ field }}</li>
        </ul>
      </a-spin>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    deleteMedia,
    fetchMediaList,
    fetchMediaReferences,
    uploadMedia,
    type MediaItem,
  } from '@/api/content';
  import { copyText, formatBytes, typeLabel } from '@/lib/media';

  const props = withDefaults(defineProps<{ media?: MediaItem[] }>(), { media: () => [] });

  const emit = defineEmits<{ (e: 'updated', items: MediaItem[]): void }>();

  const items = ref<MediaItem[]>(props.media || []);
  const loading = ref(false);
  const uploading = ref(false);
  const keyword = ref('');
  const type = ref('');
  const fileInput = ref<HTMLInputElement>();
  const referencesVisible = ref(false);
  const referencesLoading = ref(false);
  const references = ref<string[]>([]);
  const current = ref<MediaItem | null>(null);

  async function load() {
    loading.value = true;
    try {
      const payload = await fetchMediaList({
        q: keyword.value || '',
        type: type.value || '',
        pageSize: 100,
      });
      items.value = payload.data || [];
      emit('updated', items.value);
    } finally {
      loading.value = false;
    }
  }

  function triggerUpload() {
    const input = fileInput.value;
    if (!input) return;
    input.value = '';
    input.click();
  }

  async function onFiles(event: Event) {
    const input = event.target as HTMLInputElement;
    const files = [...(input.files || [])];
    if (!files.length) return;
    uploading.value = true;
    try {
      for (const file of files) {
        // eslint-disable-next-line no-await-in-loop
        await uploadMedia(file);
      }
      Message.success(`已上传 ${files.length} 个文件`);
      await load();
    } catch {
      // 错误提示由 api/interceptor.ts 统一处理
    } finally {
      uploading.value = false;
      input.value = '';
    }
  }

  async function copy(item: MediaItem) {
    const ok = await copyText(item.url);
    if (ok) Message.success('素材地址已复制');
    else Message.warning('复制失败，请手动复制');
  }

  async function showReferences(item: MediaItem) {
    current.value = item;
    references.value = [];
    referencesVisible.value = true;
    referencesLoading.value = true;
    try {
      references.value = await fetchMediaReferences(item.id);
    } catch {
      references.value = [];
    } finally {
      referencesLoading.value = false;
    }
  }

  function confirmDelete(item: MediaItem) {
    Modal.confirm({
      title: '删除媒体文件',
      content: `确认删除“${item.name}”？对象存储中的文件也会删除，此操作不可恢复。`,
      okText: '确认删除',
      cancelText: '取消',
      okButtonProps: { status: 'danger' },
      onOk: async () => {
        try {
          const used = await fetchMediaReferences(item.id);
          if (used.length) {
            Message.error(`该素材仍被引用：${used.join('、')}。请先替换这些字段。`);
            return;
          }
          await deleteMedia(item.id);
          Message.success('已删除');
          await load();
        } catch {
          // 409 / 网络错误由 interceptor 提示
        }
      },
    });
  }

  onMounted(() => {
    if (!props.media?.length) load();
  });
</script>

<style scoped lang="less">
  .media-library {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .tips {
    margin-bottom: 0;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .hidden-input {
    display: none;
  }
  .media-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
  }
  .media-card {
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    overflow: hidden;
    background: #fff;
    display: flex;
    flex-direction: column;
  }
  .media-body {
    height: 140px;
    display: grid;
    place-items: center;
    background: var(--color-fill-2);
  }
  .media-body img,
  .media-body video {
    width: 100%;
    height: 140px;
    object-fit: cover;
    display: block;
  }
  .media-file {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-3);
  }
  .media-meta {
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .media-name {
    font-weight: 700;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .media-sub {
    font-size: 12px;
    color: var(--color-text-3);
  }
  .media-actions {
    margin-top: 4px;
  }
  .reference-list {
    margin: 0;
    padding-left: 18px;
    line-height: 1.9;
    word-break: break-all;
  }
</style>
