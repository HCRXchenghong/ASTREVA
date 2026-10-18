<template>
  <a-modal
    :visible="visible"
    title="上传素材"
    width="760px"
    :mask-closable="!uploading"
    :esc-to-close="!uploading"
    :closable="!uploading"
    :on-before-ok="runUpload"
    ok-text="开始上传"
    @update:visible="(value: boolean) => emit('update:visible', value)"
  >
    <p class="modal-subtitle">支持智能处理、原文件入库和 R2 预签名直传。</p>

    <a-radio-group v-model="mode" type="button" class="mode-tabs" :disabled="uploading">
      <a-radio value="smart">智能处理</a-radio>
      <a-radio value="original">原文件上传</a-radio>
      <a-radio value="direct" :disabled="!directAvailable">R2 直传</a-radio>
    </a-radio-group>

    <a-alert v-if="!directAvailable" type="info" class="mode-tip">
      当前存储驱动为本地模式，预签名直传仅在 S3/R2 存储下可用。
    </a-alert>

    <div class="field-row">
      <div class="field">
        <label>归属分类</label>
        <a-select v-model="category" style="width: 100%">
          <a-option v-for="item in categoryOptions" :key="item" :value="item">
            {{ item }}
          </a-option>
        </a-select>
      </div>
      <div v-if="mode !== 'original'" class="field">
        <label>处理选项</label>
        <a-checkbox v-model="preserveOriginal" :disabled="mode !== 'smart'">
          保留原图（智能模式）
        </a-checkbox>
      </div>
    </div>

    <label
      class="drop-zone"
      :class="{ drag: dragging }"
      @dragenter.prevent="dragging = true"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="onDrop"
    >
      <input
        ref="inputRef"
        type="file"
        multiple
        :accept="accept"
        :disabled="uploading"
        @change="onPicked"
      />
      <strong>＋ 点击选择或拖入文件</strong>
      <small>{{ modeHint }}</small>
    </label>

    <div v-if="queue.length" class="upload-queue">
      <div v-for="(item, index) in queue" :key="item.id" class="queue-item">
        <b :title="item.file.name">{{ item.file.name }}</b>
        <span>{{ formatBytes(item.file.size) }}</span>
        <a-tag :color="statusColor(item.status)" size="small">{{ item.message }}</a-tag>
        <a-link v-if="item.status !== 'uploading'" @click="removeQueue(index)">移除</a-link>
      </div>
    </div>

    <a-space v-if="queue.length" class="queue-actions">
      <a-button size="mini" :disabled="uploading" @click="queue = []">清空队列</a-button>
    </a-space>

    <p v-if="progressText" class="form-state" :class="progressState">{{ progressText }}</p>
  </a-modal>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    confirmMediaUpload,
    presignMediaUpload,
    putToStorage,
    uploadMedia,
  } from '@/api/media';
  import type { MediaCategory, MediaSettings } from '@/api/media';
  import { declaredMime, formatBytes, isRasterImage } from '@/lib/media';

  type UploadMode = 'smart' | 'original' | 'direct';
  type QueueStatus = 'waiting' | 'uploading' | 'done' | 'error';
  interface QueueItem {
    id: string;
    file: File;
    status: QueueStatus;
    message: string;
    // 超过单文件上限：只展示错误，不参与上传队列。
    oversized?: boolean;
  }

  const props = withDefaults(
    defineProps<{
      visible: boolean;
      categories?: MediaCategory[];
      settings?: MediaSettings | null;
    }>(),
    { categories: () => [], settings: null }
  );
  const emit = defineEmits<{
    (e: 'update:visible', value: boolean): void;
    (e: 'uploaded'): void;
  }>();

  // 超过该体积时，原文件模式自动改用 R2 预签名直传，避免占用应用进程。
  const LARGE_UPLOAD_BYTES = 32 * 1024 * 1024;

  const mode = ref<UploadMode>('smart');
  const queue = ref<QueueItem[]>([]);
  const category = ref('未分类');
  const preserveOriginal = ref(true);
  const uploading = ref(false);
  const progressText = ref('');
  const progressState = ref('');
  const dragging = ref(false);
  const inputRef = ref<HTMLInputElement | null>(null);

  const image = computed(() => props.settings?.config?.image || {});
  const storageDriver = computed(
    () =>
      props.settings?.storageHealth?.driver ||
      props.settings?.config?.storage?.driver ||
      ''
  );
  const directAvailable = computed(
    () => !storageDriver.value || storageDriver.value === 's3'
  );
  const allowedMimeTypes = computed(() =>
    String(image.value.allowedMimeTypes || '')
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
  );
  const accept = computed(() => {
    const allowed = allowedMimeTypes.value.join(',');
    if (mode.value === 'smart') {
      return allowedMimeTypes.value.filter((mime) => mime.startsWith('image/')).join(',');
    }
    return allowed;
  });
  const modeHint = computed(() => {
    if (mode.value === 'smart') return '智能模式会校验位图并生成 WebP 主图与多尺寸变体。';
    if (mode.value === 'direct') return '浏览器直接 PUT 到 R2，完成后由后台校验大小、类型和魔数。';
    return `保留文件原始内容，适合视频、文档或无需变体的图片；超过 ${formatBytes(
      LARGE_UPLOAD_BYTES
    )} 自动改用 R2 直传。`;
  });
  const categoryOptions = computed(() => {
    const names = props.categories.map((item) => item.name);
    return names.includes('未分类') ? names : ['未分类', ...names];
  });

  watch(
    () => props.visible,
    (visible) => {
      if (!visible) return;
      mode.value = 'smart';
      queue.value = [];
      progressText.value = '';
      progressState.value = '';
      preserveOriginal.value = image.value.keepOriginal !== false;
      if (!categoryOptions.value.includes(category.value)) category.value = '未分类';
    }
  );

  function statusColor(status: QueueStatus) {
    return { waiting: 'gray', uploading: 'blue', done: 'green', error: 'red' }[status];
  }

  function addFiles(files: File[]) {
    if (!files.length) return;
    const maxBytes = Number(image.value.maxUploadBytes || Infinity);
    for (const file of files) {
      if (maxBytes && file.size > maxBytes) {
        queue.value.push({
          id: `${Date.now()}-${Math.random()}`,
          file,
          status: 'error',
          oversized: true,
          message: `超过单文件上限 ${formatBytes(maxBytes)}`,
        });
        continue;
      }
      queue.value.push({
        id: `${Date.now()}-${Math.random()}`,
        file,
        status: 'waiting',
        message: '等待上传',
      });
    }
    if (inputRef.value) inputRef.value.value = '';
  }

  function onPicked(event: Event) {
    const input = event.target as HTMLInputElement;
    addFiles([...(input.files || [])]);
  }

  function onDrop(event: DragEvent) {
    dragging.value = false;
    addFiles([...(event.dataTransfer?.files || [])]);
  }

  function removeQueue(index: number) {
    queue.value.splice(index, 1);
  }

  async function uploadOne(file: File) {
    const currentMode = mode.value;
    const useDirect =
      currentMode === 'direct' ||
      (currentMode === 'original' && directAvailable.value && file.size > LARGE_UPLOAD_BYTES);
    if (useDirect) {
      const mime = declaredMime(file);
      if (!mime) throw new Error('无法识别该文件的类型，请改用原文件模式上传。');
      const direct = (
        await presignMediaUpload({
          name: file.name,
          mime,
          mimeType: mime,
          size: file.size,
          category: category.value,
          mode: 'original',
          preserveOriginal: false,
        })
      ).data;
      await putToStorage(direct, file);
      return (await confirmMediaUpload(direct.token)).data;
    }
    return (
      await uploadMedia({
        file,
        mode: currentMode === 'smart' ? 'smart' : 'original',
        category: category.value,
        preserveOriginal: preserveOriginal.value,
      })
    ).data;
  }

  async function runUpload(): Promise<boolean> {
    const oversized = queue.value.filter((item) => item.oversized).length;
    const pending = queue.value.filter(
      (item) => ['waiting', 'error'].includes(item.status) && !item.oversized
    );
    if (!pending.length) {
      Message.warning(
        oversized ? '队列中的文件都超过单文件上限，请先移除。' : '请先选择要上传的文件。'
      );
      return false;
    }
    if (mode.value === 'smart' && pending.some((item) => !isRasterImage(item.file))) {
      Message.warning('智能模式只允许位图，视频和文档请选择原文件模式。');
      return false;
    }
    uploading.value = true;
    progressState.value = '';
    progressText.value = '上传队列处理中…';
    try {
      for (const item of pending) {
        item.status = 'uploading';
        item.message = '上传中';
        try {
          await uploadOne(item.file);
          item.status = 'done';
          item.message = '已完成';
        } catch (error: any) {
          item.status = 'error';
          item.message = error?.message || '上传失败';
        }
      }
      const done = pending.filter((item) => item.status === 'done').length;
      const failed = pending.length - done;
      progressText.value = `完成 ${done} 个，失败 ${failed} 个。`;
      progressState.value = failed ? 'error' : 'success';
      if (failed) {
        Message.error(`上传完成：成功 ${done} 个，失败 ${failed} 个。`);
      } else {
        Message.success(`上传完成：成功 ${done} 个。`);
      }
      emit('uploaded');
      return failed === 0;
    } catch (error: any) {
      progressState.value = 'error';
      progressText.value = error?.message || '上传失败。';
      return false;
    } finally {
      uploading.value = false;
    }
  }
</script>

<style scoped>
  .modal-subtitle {
    margin: 0 0 12px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .mode-tabs {
    margin-bottom: 12px;
  }

  .mode-tip {
    margin-bottom: 12px;
  }

  .field-row {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
    margin-bottom: 12px;
  }

  .field {
    flex: 1 1 220px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field label {
    color: var(--color-text-2);
    font-size: 13px;
  }

  .drop-zone {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 28px 16px;
    border: 1px dashed var(--color-border-3);
    border-radius: 8px;
    background: var(--color-fill-1);
    cursor: pointer;
    text-align: center;
    transition: border-color 0.2s, background 0.2s;
  }

  .drop-zone.drag {
    border-color: rgb(var(--primary-6));
    background: var(--color-fill-2);
  }

  .drop-zone input {
    display: none;
  }

  .drop-zone small {
    color: var(--color-text-3);
  }

  .upload-queue {
    margin-top: 12px;
    max-height: 220px;
    overflow: auto;
  }

  .queue-item {
    display: grid;
    grid-template-columns: 1fr auto auto auto;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
    border-bottom: 1px solid var(--color-border-1);
    font-size: 13px;
  }

  .queue-item b {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }

  .queue-actions {
    margin-top: 8px;
  }

  .form-state {
    margin: 12px 0 0;
    font-size: 13px;
    color: var(--color-text-3);
  }

  .form-state.success {
    color: rgb(var(--success-6));
  }

  .form-state.error {
    color: rgb(var(--danger-6));
  }
</style>
