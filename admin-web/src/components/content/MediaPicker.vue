<template>
  <div v-if="variant === 'field'" class="media-picker">
    <a-input
      :model-value="modelValue"
      :placeholder="placeholder || '图片 / 视频地址，可上传或直接粘贴 URL'"
      :disabled="disabled"
      allow-clear
      @update:model-value="emitValue"
    >
      <template #prefix>
        <icon-link v-if="modelValue" />
      </template>
    </a-input>
    <div class="media-picker-actions">
      <a-button size="small" :disabled="disabled" @click="openModal">
        <template #icon><icon-image /></template>
        选择素材
      </a-button>
      <a-button size="small" :disabled="disabled" :loading="uploading" @click="triggerUpload">
        <template #icon><icon-upload /></template>
        上传{{ mediaType === 'video' ? '视频' : mediaType === 'file' ? '文件' : '图片' }}
      </a-button>
      <a-button v-if="modelValue" size="small" status="danger" :disabled="disabled" @click="emitValue('')">
        清空
      </a-button>
    </div>
    <div v-if="previewUrl" class="media-preview">
      <video v-if="isVideo" :src="previewUrl" controls muted />
      <img v-else :src="previewUrl" alt="" />
    </div>
    <input
      ref="fileInput"
      class="media-file-input"
      type="file"
      :accept="accept"
      :multiple="multiple"
      @change="onFiles"
    />

    <a-modal
      v-model:visible="modalVisible"
      :title="modalTitle"
      :width="860"
      :footer="false"
      unmount-on-close
    >
      <div class="media-modal-toolbar">
        <a-input-search
          v-model="keyword"
          placeholder="搜索素材名称"
          style="width: 260px"
          @search="loadMedia"
          @press-enter="loadMedia"
        />
        <a-space>
          <a-radio-group v-model="typeFilter" type="button" size="small" @change="loadMedia">
            <a-radio value="">全部</a-radio>
            <a-radio value="image">图片</a-radio>
            <a-radio value="video">视频</a-radio>
            <a-radio value="file">文件</a-radio>
          </a-radio-group>
          <a-button size="small" :loading="loading" @click="loadMedia">
            <template #icon><icon-refresh /></template>
            刷新
          </a-button>
          <a-button size="small" type="primary" :loading="uploading" @click="triggerUpload">
            <template #icon><icon-upload /></template>
            上传到素材库
          </a-button>
        </a-space>
      </div>
      <a-spin :loading="loading" style="width: 100%">
        <a-empty v-if="!mediaItems.length" description="素材库暂无匹配的素材" />
        <div v-else class="media-modal-grid">
          <button
            v-for="item in mediaItems"
            :key="item.id"
            type="button"
            class="media-card"
            :class="{ active: item.url === modelValue }"
            @click="pick(item)"
          >
            <div class="media-card-body">
              <img v-if="item.type === 'image'" :src="item.url" :alt="item.name" />
              <video v-else-if="item.type === 'video'" :src="item.url" muted />
              <div v-else class="media-card-file"><icon-file /></div>
            </div>
            <div class="media-card-name" :title="item.name">{{ item.name }}</div>
          </button>
        </div>
      </a-spin>
    </a-modal>
  </div>

  <a-button
    v-else
    size="small"
    :disabled="disabled"
    :loading="uploading"
    @click="triggerUpload"
  >
    <template #icon><icon-upload /></template>
    <slot>上传{{ mediaType === 'video' ? '视频' : '文件' }}</slot>
  </a-button>
</template>

<script lang="ts" setup>
  import { computed, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchMediaList, uploadMedia, type MediaItem } from '@/api/content';

  const props = withDefaults(
    defineProps<{
      modelValue?: string;
      accept?: string;
      mediaType?: 'image' | 'video' | 'file' | '';
      variant?: 'field' | 'button';
      multiple?: boolean;
      disabled?: boolean;
      placeholder?: string;
    }>(),
    {
      modelValue: '',
      accept: 'image/*,video/*',
      mediaType: 'image',
      variant: 'field',
      multiple: false,
      disabled: false,
      placeholder: '',
    }
  );

  const emit = defineEmits<{
    (e: 'update:modelValue', value: string): void;
    (e: 'uploaded', urls: string[]): void;
  }>();

  const fileInput = ref<HTMLInputElement>();
  const modalVisible = ref(false);
  const loading = ref(false);
  const uploading = ref(false);
  const mediaItems = ref<MediaItem[]>([]);
  const keyword = ref('');
  const typeFilter = ref('');

  const previewUrl = computed(() => String(props.modelValue || ''));
  const isVideo = computed(
    () => props.mediaType === 'video' || /\.(mp4|webm|mov|m3u8)(\?|$)/i.test(previewUrl.value)
  );
  const modalTitle = computed(() =>
    props.mediaType === 'video' ? '选择视频素材' : '选择图片素材'
  );

  function emitValue(value: string) {
    emit('update:modelValue', value || '');
  }

  function openModal() {
    typeFilter.value = ['image', 'video', 'file'].includes(String(props.mediaType))
      ? String(props.mediaType)
      : '';
    modalVisible.value = true;
    loadMedia();
  }

  async function loadMedia() {
    loading.value = true;
    try {
      const payload = await fetchMediaList({
        q: keyword.value || '',
        type: typeFilter.value || '',
        pageSize: 100,
      });
      mediaItems.value = payload.data || [];
    } catch {
      mediaItems.value = [];
    } finally {
      loading.value = false;
    }
  }

  function pick(item: MediaItem) {
    emitValue(item.url);
    modalVisible.value = false;
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
      const urls: string[] = [];
      for (const file of files) {
        // eslint-disable-next-line no-await-in-loop
        const media = await uploadMedia(file);
        if (media?.url) urls.push(media.url);
      }
      if (urls.length) {
        if (props.multiple) emit('uploaded', urls);
        else emitValue(urls[0]);
        Message.success(`已上传 ${urls.length} 个文件`);
      }
      if (modalVisible.value) await loadMedia();
    } catch {
      // 具体错误由 api/interceptor.ts 统一提示
    } finally {
      uploading.value = false;
      input.value = '';
    }
  }
</script>

<style scoped lang="less">
  .media-picker {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .media-picker-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .media-preview img,
  .media-preview video {
    max-width: 240px;
    max-height: 150px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    object-fit: cover;
    display: block;
  }
  .media-file-input {
    display: none;
  }
  .media-modal-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 14px;
  }
  .media-modal-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 12px;
    max-height: 56vh;
    overflow: auto;
  }
  .media-card {
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: #fff;
    padding: 0;
    overflow: hidden;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .media-card:hover,
  .media-card.active {
    border-color: rgb(var(--primary-6));
    box-shadow: 0 4px 14px rgba(6, 102, 216, 0.12);
  }
  .media-card-body {
    height: 110px;
    display: grid;
    place-items: center;
    background: var(--color-fill-2);
  }
  .media-card-body img,
  .media-card-body video {
    width: 100%;
    height: 110px;
    object-fit: cover;
    display: block;
  }
  .media-card-file {
    font-size: 30px;
    color: var(--color-text-3);
  }
  .media-card-name {
    padding: 6px 8px;
    font-size: 12px;
    color: var(--color-text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
