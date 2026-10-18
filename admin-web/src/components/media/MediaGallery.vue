<template>
  <div class="media-gallery">
    <a-card :bordered="false" class="gallery-toolbar">
      <div class="search-row">
        <a-input
          v-model="query"
          allow-clear
          placeholder="搜索名称、对象 Key 或元数据"
          style="max-width: 320px"
        >
          <template #prefix><icon-search /></template>
        </a-input>
        <a-select v-model="type" style="width: 140px" placeholder="全部类型">
          <a-option value="">全部类型</a-option>
          <a-option value="image">图片</a-option>
          <a-option value="video">视频</a-option>
          <a-option value="file">文件</a-option>
        </a-select>
        <a-button @click="resetFilters">重置</a-button>
      </div>
      <a-radio-group v-model="category" type="button" class="category-tabs">
        <a-radio value="">全部（{{ statsTotal }}）</a-radio>
        <a-radio v-for="item in categories" :key="item.name" :value="item.name">
          {{ item.name }}（{{ item.count }}）
        </a-radio>
      </a-radio-group>
    </a-card>

    <div class="gallery-summary">
      <span>
        共 {{ meta.total || 0 }} 个素材{{ selected.length ? `，已选 ${selected.length} 个` : '' }}
      </span>
      <a-space>
        <a-button status="danger" :disabled="!selected.length" @click="onBatchDelete">
          删除已选
        </a-button>
        <a-radio-group v-model="view" type="button" size="small">
          <a-radio value="grid">网格</a-radio>
          <a-radio value="list">列表</a-radio>
        </a-radio-group>
      </a-space>
    </div>

    <a-spin :loading="loading" style="width: 100%">
      <a-alert v-if="errorText" type="error" class="list-error">
        <template #title>素材列表加载失败</template>
        {{ errorText }}
        <a-link class="retry-link" @click="load">重试</a-link>
      </a-alert>

      <a-empty v-if="!items.length && !loading" class="empty-state">
        <p>没有找到素材</p>
        <p class="empty-hint">调整筛选条件，或上传新的图片、视频和文档。</p>
      </a-empty>

      <div v-else class="media-grid" :class="{ list: view === 'list' }">
        <article
          v-for="item in items"
          :key="item.id"
          class="media-card"
          :class="{ selected: selected.includes(item.id) }"
        >
          <div class="media-preview" @click="openDetail(item.id)">
            <span class="media-select" @click.stop>
              <a-checkbox
                :model-value="selected.includes(item.id)"
                @change="() => toggleSelect(item.id)"
              />
            </span>
            <img
              v-if="item.type === 'image'"
              :src="item.url"
              :alt="item.name"
              loading="lazy"
              decoding="async"
            />
            <video v-else-if="item.type === 'video'" :src="item.url" preload="metadata" muted />
            <span v-else class="file-icon">▤</span>
            <a-tag class="media-type" size="small">{{ typeLabel(item.type) }}</a-tag>
          </div>
          <div class="media-info">
            <h3 :title="item.name">{{ item.name }}</h3>
            <p>
              <span>{{ item.category || '未分类' }}</span>
              <span>{{ formatBytes(item.size) }}</span>
            </p>
          </div>
          <div class="media-actions">
            <a-link @click="openDetail(item.id)">详情</a-link>
            <a-link @click="copyUrl(item)">复制链接</a-link>
            <a-link status="danger" @click="removeOne(item)">删除</a-link>
          </div>
        </article>
      </div>
    </a-spin>

    <div class="pagination">
      <a-pagination
        :current="meta.page || 1"
        :page-size="pageSize"
        :total="meta.total || 0"
        show-total
        @change="onPageChange"
      />
    </div>

    <MediaDetailDrawer
      v-model:visible="detailVisible"
      :media-id="detailId"
      :categories="categories"
      :settings="settings"
      @changed="onDetailChanged"
    />
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    batchDeleteMedia,
    deleteMedia,
    fetchMediaList,
    fetchMediaReferences,
  } from '@/api/media';
  import type { MediaCategory, MediaItem, MediaMeta, MediaSettings } from '@/api/media';
  import { copyText, formatBytes, preformatted, runModalTask, typeLabel } from '@/lib/media';
  import MediaDetailDrawer from './MediaDetailDrawer.vue';

  const props = withDefaults(
    defineProps<{
      categories?: MediaCategory[];
      statsTotal?: number;
      settings?: MediaSettings | null;
    }>(),
    { categories: () => [], statsTotal: 0, settings: null }
  );
  const emit = defineEmits<{ (e: 'changed'): void }>();

  const pageSize = 24;
  const items = ref<MediaItem[]>([]);
  const meta = ref<MediaMeta>({ total: 0, page: 1, pageSize, pages: 0 });
  const loading = ref(false);
  const query = ref('');
  const type = ref('');
  const category = ref('');
  const view = ref<'grid' | 'list'>('grid');
  const selected = ref<string[]>([]);
  const errorText = ref('');
  const detailVisible = ref(false);
  const detailId = ref('');

  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  async function load() {
    loading.value = true;
    errorText.value = '';
    try {
      const payload = await fetchMediaList({
        page: meta.value.page || 1,
        pageSize,
        q: query.value.trim() || undefined,
        type: type.value || undefined,
        category: category.value || undefined,
      });
      items.value = payload.data || [];
      meta.value = payload.meta || { total: 0, page: 1, pageSize, pages: 0 };
      const visible = new Set(items.value.map((item) => item.id));
      selected.value = selected.value.filter((id) => visible.has(id));
    } catch (error: any) {
      // 这里吞掉异常，避免 watcher / 定时器里的调用产生未处理的 Promise 报错。
      errorText.value =
        error?.response?.data?.error?.message || error?.message || '素材列表加载失败。';
    } finally {
      loading.value = false;
    }
  }

  function reload() {
    return load();
  }

  defineExpose({ reload });

  function resetFilters() {
    query.value = '';
    type.value = '';
    category.value = '';
    meta.value.page = 1;
    load();
  }

  watch(query, () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      meta.value.page = 1;
      load();
    }, 300);
  });

  watch(type, () => {
    meta.value.page = 1;
    load();
  });

  watch(category, () => {
    meta.value.page = 1;
    load();
  });

  watch(
    () => props.categories,
    (list) => {
      if (category.value && !list.some((item) => item.name === category.value)) {
        category.value = '';
      }
    }
  );

  function onPageChange(current: number) {
    meta.value.page = current;
    load();
  }

  function toggleSelect(id: string) {
    selected.value = selected.value.includes(id)
      ? selected.value.filter((item) => item !== id)
      : [...selected.value, id];
  }

  function openDetail(id: string) {
    detailId.value = id;
    detailVisible.value = true;
  }

  async function copyUrl(item: MediaItem) {
    const ok = await copyText(item.url);
    if (ok) Message.success('已复制到剪贴板。');
    else Message.error('浏览器拒绝访问剪贴板，请手动复制。');
  }

  async function removeOne(item: MediaItem) {
    let references: string[] = [];
    try {
      references = (await fetchMediaReferences(item.id)).data?.references || [];
    } catch {
      return;
    }
    if (references.length) {
      Modal.warning({
        title: '无法删除：素材仍被引用',
        content: preformatted(
          `“${item.name}”被以下官网字段引用，请先替换对应字段：\n${references.join('\n')}`
        ),
      });
      return;
    }
    Modal.confirm({
      title: '删除素材',
      content: `确认删除“${item.name}”及对象存储中的全部变体？该操作不可恢复。`,
      okButtonProps: { status: 'danger' },
      onOk: runModalTask(async () => {
        await deleteMedia(item.id);
        Message.success('素材已删除。');
        if (detailId.value === item.id) detailVisible.value = false;
        await load();
        emit('changed');
      }),
    });
  }

  async function onBatchDelete() {
    const count = selected.value.length;
    if (!count) return;
    Modal.confirm({
      title: '批量删除素材',
      content: `确认删除已选的 ${count} 个素材？被官网引用的素材会被自动阻止。`,
      okButtonProps: { status: 'danger' },
      onOk: runModalTask(async () => {
        const result = (await batchDeleteMedia([...selected.value])).data;
        selected.value = [];
        const summary = `已删除 ${result.deleted.length} 个，引用阻止 ${result.blocked.length} 个，失败 ${result.failed.length} 个。`;
        if (result.blocked.length) {
          Modal.info({
            title: '部分素材被引用阻止',
            content: preformatted(
              `${summary}\n\n${result.blocked
                .map((item) => `${item.name}：${item.references.join('、')}`)
                .join('\n')}`
            ),
          });
        } else if (result.failed.length) {
          Modal.error({
            title: '部分素材删除失败',
            content: preformatted(
              `${summary}\n\n${result.failed
                .map((item) => `${item.id}：${item.message}`)
                .join('\n')}`
            ),
          });
        } else {
          Message.success(summary);
        }
        await load();
        emit('changed');
      }),
    });
  }

  async function onDetailChanged() {
    await load();
    emit('changed');
  }

  onMounted(load);
</script>

<style scoped>
  .gallery-toolbar {
    margin-bottom: 12px;
  }

  .search-row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }

  .category-tabs {
    margin-top: 12px;
    flex-wrap: wrap;
  }

  .gallery-summary {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
    color: var(--color-text-2);
  }

  .media-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
    min-height: 120px;
  }

  .media-grid.list {
    grid-template-columns: 1fr;
  }

  .media-card {
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    overflow: hidden;
    transition: border-color 0.2s, box-shadow 0.2s;
  }

  .media-card.selected {
    border-color: rgb(var(--primary-6));
    box-shadow: 0 0 0 1px rgb(var(--primary-6));
  }

  .media-preview {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    height: 150px;
    background: var(--color-fill-1);
    cursor: pointer;
    overflow: hidden;
  }

  .media-grid.list .media-preview {
    height: 96px;
  }

  .media-preview img,
  .media-preview video {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .file-icon {
    font-size: 32px;
    color: var(--color-text-3);
  }

  .media-select {
    position: absolute;
    top: 8px;
    left: 8px;
    z-index: 2;
    padding: 2px 4px;
    border-radius: 4px;
    background: var(--color-bg-2);
  }

  .media-type {
    position: absolute;
    top: 8px;
    right: 8px;
    z-index: 2;
  }

  .media-info {
    padding: 10px 12px 0;
  }

  .media-info h3 {
    margin: 0 0 6px;
    font-size: 14px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .media-info p {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    margin: 0;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .media-actions {
    display: flex;
    gap: 12px;
    padding: 10px 12px 12px;
  }

  .pagination {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }

  .empty-state {
    padding: 40px 0;
  }

  .list-error {
    margin-bottom: 12px;
  }

  .retry-link {
    margin-left: 8px;
  }

  .empty-hint {
    color: var(--color-text-3);
    font-size: 12px;
  }
</style>
