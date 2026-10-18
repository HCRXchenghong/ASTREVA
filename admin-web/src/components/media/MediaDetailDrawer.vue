<template>
  <a-drawer
    :visible="visible"
    :width="680"
    title="素材详情"
    :footer="false"
    unmount-on-close
    @update:visible="(value: boolean) => emit('update:visible', value)"
  >
    <a-spin :loading="loading" style="width: 100%">
      <template v-if="item">
        <p class="drawer-subtitle">链接、变体、引用保护与稳定 URL 替换。</p>

        <div class="detail-preview">
          <a-image v-if="item.type === 'image'" :src="item.url" :alt="item.name" width="100%" />
          <video v-else-if="item.type === 'video'" :src="item.url" controls />
          <span v-else class="detail-file">▤</span>
        </div>

        <h3 class="detail-name">{{ item.name }}</h3>
        <p class="detail-key">{{ item.key }}</p>

        <a-descriptions :column="1" size="small" bordered class="detail-facts">
          <a-descriptions-item label="分类">
            <a-space>
              <a-select v-model="editCategory" size="small" style="width: 180px">
                <a-option v-for="name in categoryOptions" :key="name" :value="name">
                  {{ name }}
                </a-option>
              </a-select>
              <a-button
                size="mini"
                :loading="savingCategory"
                :disabled="editCategory === (item.category || '未分类')"
                @click="changeCategory"
              >
                保存分类
              </a-button>
            </a-space>
          </a-descriptions-item>
          <a-descriptions-item label="类型">{{ item.mime }} / {{ typeLabel(item.type) }}</a-descriptions-item>
          <a-descriptions-item label="主图大小">{{ formatBytes(item.size) }}</a-descriptions-item>
          <a-descriptions-item label="来源">{{ sourceLabel(item.sourceType) }}</a-descriptions-item>
          <a-descriptions-item label="更新时间">{{ formatDateTime(item.updatedAt) }}</a-descriptions-item>
          <a-descriptions-item label="SHA-256">
            <span class="mono">{{ item.checksumSha256 || '—' }}</span>
          </a-descriptions-item>
        </a-descriptions>

        <a-space wrap class="detail-actions">
          <a-button size="small" @click="copyUrl">复制链接</a-button>
          <a-button size="small" @click="copyMarkdown">复制 Markdown</a-button>
          <a-button size="small" :disabled="!srcset" @click="copySrcset">复制 srcset</a-button>
          <a-button size="small" :loading="referencesLoading" @click="loadReferences">
            查看官网引用
          </a-button>
          <a-button size="small" type="primary" @click="replaceItem">替换（URL 不变）</a-button>
          <a-button size="small" status="danger" @click="removeItem">删除</a-button>
        </a-space>

        <a-alert
          v-if="referencesLoaded"
          class="reference-alert"
          :type="references.length ? 'warning' : 'success'"
        >
          <template #title>
            {{ references.length ? '该素材已被官网内容引用' : '该素材没有被官网内容引用' }}
          </template>
          <ul v-if="references.length" class="reference-list">
            <li v-for="field in references" :key="field">{{ field }}</li>
          </ul>
        </a-alert>

        <h3 class="variant-title">文件与多尺寸变体</h3>
        <a-table :data="variants" :pagination="false" row-key="id" size="small">
          <template #columns>
            <a-table-column title="文件" data-index="label" />
            <a-table-column title="格式" data-index="mime" />
            <a-table-column title="尺寸" data-index="dimensions" />
            <a-table-column title="大小">
              <template #cell="{ record }">{{ formatBytes(record.size) }}</template>
            </a-table-column>
            <a-table-column title="操作" :width="80">
              <template #cell="{ record }">
                <a-link @click="copyVariant(record.url)">复制</a-link>
              </template>
            </a-table-column>
          </template>
        </a-table>
      </template>
      <a-empty v-else-if="!loading" description="素材不存在或已被删除" />
    </a-spin>
  </a-drawer>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    deleteMedia,
    fetchMedia,
    fetchMediaReferences,
    replaceMedia,
  } from '@/api/media';
  import type { MediaCategory, MediaItem, MediaSettings } from '@/api/media';
  import {
    copyText,
    formatBytes,
    formatDateTime,
    pickFile,
    preformatted,
    replaceAccept,
    runModalTask,
    sourceLabel,
    srcsetOf,
    typeLabel,
  } from '@/lib/media';

  // 单文件读取上限：避免为了改分类把超大视频整体读进浏览器内存。
  const MAX_REWRITE_BYTES = 64 * 1024 * 1024;

  const props = withDefaults(
    defineProps<{
      visible: boolean;
      mediaId?: string;
      categories?: MediaCategory[];
      settings?: MediaSettings | null;
    }>(),
    { mediaId: '', categories: () => [], settings: null }
  );
  const emit = defineEmits<{
    (e: 'update:visible', value: boolean): void;
    (e: 'changed'): void;
  }>();

  const item = ref<MediaItem | null>(null);
  const loading = ref(false);
  const references = ref<string[]>([]);
  const referencesLoading = ref(false);
  const referencesLoaded = ref(false);
  const editCategory = ref('未分类');
  const savingCategory = ref(false);

  const categoryOptions = computed(() => {
    const names = props.categories.map((entry) => entry.name);
    const fallback = item.value?.category || '未分类';
    return names.includes(fallback) ? names : [fallback, ...names];
  });

  const srcset = computed(() => (item.value ? srcsetOf(item.value) : ''));

  const variants = computed(() => {
    const current = item.value;
    if (!current) return [];
    const list = [
      {
        id: 'main',
        label: 'WebP 主图',
        url: current.url,
        mime: current.mime || '—',
        size: current.size,
        dimensions:
          current.width && current.height ? `${current.width} × ${current.height}` : '—',
      },
      ...(Array.isArray(current.variants?.webp) ? current.variants!.webp! : []).map(
        (entry, index) => ({
          id: `webp-${index}`,
          label: `${entry.width}px 变体`,
          url: entry.url,
          mime: entry.mime || 'image/webp',
          size: entry.size,
          dimensions: `${entry.width} × ${entry.height}`,
        })
      ),
    ];
    if (current.variants?.original) {
      const original = current.variants.original;
      list.push({
        id: 'original',
        label: '保留原图',
        url: original.url,
        mime: original.mime || '—',
        size: original.size,
        dimensions: '原始尺寸',
      });
    }
    return list;
  });

  async function load() {
    if (!props.mediaId) return;
    loading.value = true;
    references.value = [];
    referencesLoaded.value = false;
    try {
      const payload = await fetchMedia(props.mediaId);
      item.value = payload.data;
      editCategory.value = payload.data?.category || '未分类';
      loadReferences();
    } catch {
      item.value = null;
      // 错误提示已由响应拦截器统一处理。
    } finally {
      loading.value = false;
    }
  }

  watch(() => [props.visible, props.mediaId], () => {
    if (props.visible && props.mediaId) load();
  });

  async function loadReferences() {
    const current = item.value;
    if (!current) return;
    referencesLoading.value = true;
    try {
      references.value = (await fetchMediaReferences(current.id)).data?.references || [];
      referencesLoaded.value = true;
    } catch {
      // 错误提示已由响应拦截器统一处理，避免未处理的 Promise 报错。
    } finally {
      referencesLoading.value = false;
    }
  }

  async function copyVariant(url: string) {
    const ok = await copyText(url);
    if (ok) Message.success('已复制到剪贴板。');
    else Message.error('浏览器拒绝访问剪贴板，请手动复制。');
  }

  function copyUrl() {
    if (item.value) copyVariant(item.value.url);
  }

  function copyMarkdown() {
    if (item.value) copyVariant(`![${item.value.name}](${item.value.url})`);
  }

  function copySrcset() {
    if (srcset.value) copyVariant(srcset.value);
  }

  function replaceItem() {
    const current = item.value;
    if (!current) return;
    pickFile(replaceAccept(current)).then((files) => {
      const file = files[0];
      if (!file) return;
      Modal.confirm({
        title: '稳定替换',
        content: `确认用“${file.name}”替换“${current.name}”？主 URL 与对象 Key 会保持不变。`,
        onOk: runModalTask(async () => {
          const payload = await replaceMedia(current.id, {
            file,
            mode: current.type === 'image' ? 'smart' : 'original',
            category: current.category || '未分类',
            preserveOriginal: props.settings?.config?.image?.keepOriginal !== false,
          });
          item.value = payload.data;
          editCategory.value = payload.data.category || '未分类';
          Message.success('素材已替换，主 URL 未变。');
          emit('changed');
        }),
      });
    });
  }

  async function readCurrentAsset(current: MediaItem): Promise<Blob> {
    if (Number(current.size || 0) > MAX_REWRITE_BYTES) {
      throw new Error('文件过大，无法通过浏览器改写分类。请使用「替换」上传文件并选择新分类。');
    }
    let response: Response;
    try {
      response = await fetch(current.url);
    } catch {
      throw new Error('浏览器无法读取当前素材（公开域名可能未开启 CORS）。请使用「替换」上传文件并选择新分类。');
    }
    if (!response.ok) {
      throw new Error(`读取当前素材失败（HTTP ${response.status}）。请使用「替换」上传文件并选择新分类。`);
    }
    return response.blob();
  }

  function changeCategory() {
    const current = item.value;
    const next = editCategory.value;
    if (!current || !next || next === (current.category || '未分类')) return;
    Modal.confirm({
      title: '修改分类',
      content: preformatted(
        `将把“${current.name}”归入“${next}”。\n\n后端没有单独的分类更新接口，需要通过稳定替换写回同一对象：URL 与对象 Key 不变，但图片会按当前主图重新生成 WebP 变体，文件体积可能变化。是否继续？`
      ),
      onOk: runModalTask(async () => {
        savingCategory.value = true;
        try {
          const blob = await readCurrentAsset(current);
          const file = new File([blob], current.name, {
            type: blob.type || current.mime || 'application/octet-stream',
          });
          const payload = await replaceMedia(current.id, {
            file,
            mode: current.type === 'image' ? 'smart' : 'original',
            category: next,
            preserveOriginal: props.settings?.config?.image?.keepOriginal !== false,
          });
          item.value = payload.data;
          editCategory.value = payload.data.category || '未分类';
          Message.success('分类已更新，素材 URL 未变。');
          emit('changed');
        } finally {
          savingCategory.value = false;
        }
      }),
    });
  }

  async function removeItem() {
    const current = item.value;
    if (!current) return;
    let used: string[] = [];
    try {
      used = (await fetchMediaReferences(current.id)).data?.references || [];
    } catch {
      return;
    }
    if (used.length) {
      references.value = used;
      referencesLoaded.value = true;
      Modal.warning({
        title: '无法删除：素材仍被引用',
        content: preformatted(
          `“${current.name}”被以下官网字段引用，请先替换对应字段：\n${used.join('\n')}`
        ),
      });
      return;
    }
    Modal.confirm({
      title: '删除素材',
      content: `确认删除“${current.name}”及对象存储中的全部变体？该操作不可恢复。`,
      okButtonProps: { status: 'danger' },
      onOk: runModalTask(async () => {
        await deleteMedia(current.id);
        Message.success('素材已删除。');
        emit('update:visible', false);
        emit('changed');
      }),
    });
  }
</script>

<style scoped>
  .drawer-subtitle {
    margin: 0 0 12px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .detail-preview {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 180px;
    padding: 8px;
    border-radius: 8px;
    background: var(--color-fill-1);
    overflow: hidden;
  }

  .detail-preview video {
    max-width: 100%;
    max-height: 320px;
  }

  .detail-file {
    font-size: 48px;
    color: var(--color-text-3);
  }

  .detail-name {
    margin: 14px 0 4px;
    font-size: 16px;
  }

  .detail-key {
    margin: 0 0 12px;
    color: var(--color-text-3);
    font-size: 12px;
    word-break: break-all;
  }

  .detail-facts {
    margin-bottom: 12px;
  }

  .mono {
    font-family: monospace;
    word-break: break-all;
  }

  .detail-actions {
    margin-bottom: 12px;
  }

  .reference-alert {
    margin-bottom: 12px;
  }

  .reference-list {
    margin: 4px 0 0;
    padding-left: 18px;
  }

  .variant-title {
    margin: 4px 0 10px;
    font-size: 14px;
  }
</style>
