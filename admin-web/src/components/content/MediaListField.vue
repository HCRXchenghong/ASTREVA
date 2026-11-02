<template>
  <div class="media-list-field">
    <div class="media-list-actions">
      <a-button size="small" :disabled="disabled" @click="add('image')">新增图片</a-button>
      <a-button size="small" :disabled="disabled" @click="add('video')">新增视频</a-button>
      <a-button v-if="!items.length && legacyItems.length" size="small" :disabled="disabled" @click="update(legacyItems)">导入原有媒体</a-button>
      <a-button v-if="defaultImages.length" size="small" :disabled="disabled" @click="update([...items, ...defaultImages.map((url) => ({ type: 'image', url, alt: '', playback: 'click' }))])">加入默认素材</a-button>
      <a-button size="small" status="danger" :disabled="disabled" @click="update([])">使用默认占位</a-button>
    </div>
    <p class="media-list-hint">当前 {{ items.length }} 项。旧媒体未导入时仍会显示旧配置；点击“使用默认占位”并保存即可清空旧媒体的展示。移除这里只解除引用，不删除素材库文件。</p>
    <div v-if="defaultImages.length" class="default-media-options">
      <button v-for="url in defaultImages" :key="url" type="button" :disabled="disabled" :title="`加入素材：${url}`" @click="update([...items, { type: 'image', url, alt: '', playback: 'click' }])">
        <img :src="url" alt="默认备选素材" loading="lazy" />
        <span>加入此图</span>
      </button>
    </div>
    <div v-for="(item, index) in items" :key="index" class="media-list-card">
      <div class="media-list-card-head">
        <strong>第 {{ index + 1 }} 项</strong>
        <a-space>
          <a-button size="mini" :disabled="disabled || index === 0" @click="move(index, -1)">上移</a-button>
          <a-button size="mini" :disabled="disabled || index === items.length - 1" @click="move(index, 1)">下移</a-button>
          <a-button size="mini" status="danger" :disabled="disabled" @click="remove(index)">移除</a-button>
        </a-space>
      </div>
      <a-radio-group :model-value="item.type" :disabled="disabled" type="button" size="small" @update:model-value="(value: string) => change(index, { type: value, url: '', poster: '' })">
        <a-radio value="image">图片</a-radio><a-radio value="video">视频</a-radio>
      </a-radio-group>
      <MediaPicker :model-value="item.url" :accept="item.type === 'video' ? 'video/*' : 'image/*'" :media-type="item.type" :disabled="disabled" @update:model-value="(value: string) => change(index, { url: value })" />
      <a-input :model-value="item.alt || ''" placeholder="图片替代文字 / 视频标题" :disabled="disabled" @update:model-value="(value: string) => change(index, { alt: value })" />
      <template v-if="item.type === 'video'">
        <MediaPicker :model-value="item.poster || ''" accept="image/*" media-type="image" placeholder="视频封面 URL（可选）" :disabled="disabled" @update:model-value="(value: string) => change(index, { poster: value })" />
        <a-select :model-value="item.playback || 'click'" :disabled="disabled" :options="[{ label: '点击播放', value: 'click' }, { label: '静音自动播放', value: 'muted-autoplay' }]" @update:model-value="(value: string) => change(index, { playback: value })" />
        <p class="media-list-hint">支持 MP4 / WebM 文件和 YouTube watch、shorts、youtu.be 链接；自动播放受浏览器策略限制时显示播放按钮。</p>
      </template>
      <p v-if="item.url && !valid(item)" class="media-list-error">媒体链接无效或类型不匹配，请使用站内路径、HTTP(S) 图片、可播放视频文件或 YouTube 链接。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import MediaPicker from './MediaPicker.vue';

type Item = { type: 'image' | 'video'; url: string; poster?: string; alt?: string; playback?: 'click' | 'muted-autoplay' };
const props = withDefaults(defineProps<{ modelValue?: Item[]; legacyImage?: string | string[]; legacyVideo?: string | string[]; defaultImages?: string[]; disabled?: boolean }>(), { modelValue: () => [], defaultImages: () => [], disabled: false });
const emit = defineEmits<{ (e: 'update:modelValue', value: Item[]): void }>();
const items = computed(() => Array.isArray(props.modelValue) ? props.modelValue : []);
const asUrls = (value?: string | string[]) => (Array.isArray(value) ? value : [value]).map((url) => String(url || '').trim()).filter(Boolean);
const legacyItems = computed<Item[]>(() => [
  ...asUrls(props.legacyVideo).map((url): Item => ({ type: 'video', url, poster: asUrls(props.legacyImage)[0], playback: 'click' })),
  ...asUrls(props.legacyImage).map((url): Item => ({ type: 'image', url, playback: 'click' }))
]);
function update(value: Item[]) { emit('update:modelValue', value); }
function add(type: Item['type']) { update([...items.value, { type, url: '', alt: '', playback: 'click' }]); }
function change(index: number, patch: Partial<Item>) { update(items.value.map((item, i) => i === index ? { ...item, ...patch } as Item : item)); }
function remove(index: number) { update(items.value.filter((_, i) => i !== index)); }
function move(index: number, direction: number) { const next = [...items.value]; [next[index], next[index + direction]] = [next[index + direction], next[index]]; update(next); }
function valid(item: Item) {
  try {
    if (!item.url.startsWith('/') && !/^https?:\/\//i.test(item.url)) return false;
    const url = new URL(item.url, window.location.origin);
    if (!['http:', 'https:'].includes(url.protocol) || item.url.startsWith('//')) return false;
    if (item.type === 'image') return !/\.(mp4|webm|mov|ogg)$/i.test(url.pathname) && !/(^|\.)(?:youtube(?:-nocookie)?\.com|youtu\.be)$/i.test(url.hostname);
    const host = url.hostname.toLowerCase();
    if (/\.(mp4|webm|ogg|ogv|m4v|mov)$/i.test(url.pathname)) return true;
    if (!['youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com', 'youtu.be', 'www.youtu.be', 'youtube-nocookie.com', 'www.youtube-nocookie.com'].includes(host)) return false;
    const parts = url.pathname.split('/').filter(Boolean);
    const id = host.endsWith('youtu.be') ? parts[0] : ['embed', 'shorts', 'live', 'v'].includes(parts[0]) ? parts[1] : url.searchParams.get('v');
    return /^[a-zA-Z0-9_-]{6,}$/.test(String(id || ''));
  } catch { return false; }
}
</script>

<style scoped lang="less">
.media-list-field { display: grid; gap: 12px; }
.media-list-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.media-list-hint { margin: 0; color: var(--color-text-3); font-size: 12px; line-height: 1.5; }
.media-list-card { display: grid; gap: 10px; border: 1px solid var(--color-border-2); border-radius: 8px; padding: 14px; }
.media-list-card-head { display: flex; justify-content: space-between; align-items: center; }
.media-list-error { margin: 0; color: rgb(var(--danger-6)); font-size: 12px; }
.default-media-options { display: flex; flex-wrap: wrap; gap: 8px; }
.default-media-options button { border: 1px solid var(--color-border-2); border-radius: 6px; overflow: hidden; background: var(--color-bg-2); color: var(--color-text-2); cursor: pointer; padding: 0; width: 110px; }
.default-media-options button:hover { border-color: rgb(var(--primary-6)); }
.default-media-options button:disabled { cursor: not-allowed; opacity: .5; }
.default-media-options img { display: block; width: 100%; height: 66px; object-fit: cover; }
.default-media-options span { display: block; padding: 4px; font-size: 11px; }
</style>
