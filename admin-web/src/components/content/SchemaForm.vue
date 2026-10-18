<template>
  <div class="schema-form" :class="{ compact }">
    <template v-for="field in visibleFields" :key="field.key">
      <a-divider v-if="field.type === 'group'" orientation="left" class="schema-group">
        {{ field.label }}
      </a-divider>
      <div v-else class="schema-field" :class="{ full: isFull(field) }">
        <div class="schema-label">
          <span>{{ field.label }}</span>
          <a-tag v-if="field.type === 'media'" size="small" color="gray">
            {{ field.mediaType === 'video' ? '视频' : field.mediaType === 'file' ? '文件' : '图片' }}
          </a-tag>
        </div>

        <a-input
          v-if="!field.type || field.type === 'text'"
          :model-value="getPath(field.key)"
          :placeholder="field.placeholder || `请输入${field.label}`"
          :disabled="disabled"
          allow-clear
          @update:model-value="(value: string) => setValue(field, value)"
        />

        <a-textarea
          v-else-if="field.type === 'textarea'"
          :model-value="getPath(field.key)"
          :placeholder="field.placeholder || `请输入${field.label}`"
          :disabled="disabled"
          :auto-size="{ minRows: field.rows || 3, maxRows: Math.max(field.rows || 3, 14) }"
          @update:model-value="(value: string) => setValue(field, value)"
        />

        <a-input-number
          v-else-if="field.type === 'number'"
          :model-value="getPath(field.key)"
          :disabled="disabled"
          :placeholder="field.placeholder"
          style="width: 100%"
          @update:model-value="(value: number | undefined) => setValue(field, value ?? 0)"
        />

        <a-checkbox
          v-else-if="field.type === 'checkbox'"
          :model-value="Boolean(getPath(field.key))"
          :disabled="disabled"
          @update:model-value="(value: boolean) => setValue(field, value)"
        >
          {{ field.placeholder || field.label }}
        </a-checkbox>

        <a-select
          v-else-if="field.type === 'select'"
          :model-value="getPath(field.key)"
          :options="field.options || []"
          :disabled="disabled"
          :placeholder="field.placeholder || `请选择${field.label}`"
          allow-clear
          @update:model-value="(value: any) => setValue(field, value)"
        />

        <MediaPicker
          v-else-if="field.type === 'media'"
          :model-value="getPath(field.key) || ''"
          :accept="field.accept || 'image/*,video/*'"
          :media-type="field.mediaType || 'image'"
          :disabled="disabled"
          @update:model-value="(value: string) => setValue(field, value)"
        />

        <template v-else-if="field.type === 'listText'">
          <a-textarea
            :model-value="listTextValue(field)"
            :disabled="disabled"
            :placeholder="field.placeholder || '每行一条'"
            :auto-size="{ minRows: 4, maxRows: 12 }"
            @update:model-value="(value: string) => onListTextInput(field, value)"
          />
          <div v-if="field.appendUpload" class="schema-inline-actions">
            <MediaPicker
              variant="button"
              multiple
              :accept="field.appendAccept || 'image/*'"
              :media-type="field.appendAccept === 'video/*' ? 'video' : 'image'"
              :disabled="disabled"
              @uploaded="(urls: string[]) => onListTextAppend(field, urls)"
            >
              上传并追加
            </MediaPicker>
          </div>
        </template>

        <PairList
          v-else-if="field.type === 'pairList'"
          :model-value="getPath(field.key) || []"
          :key-a="field.keyA"
          :key-b="field.keyB"
          :label-a="field.labelA"
          :label-b="field.labelB"
          :disabled="disabled"
          @update:model-value="(value: any) => setValue(field, value)"
        />

        <ObjectList
          v-else-if="field.type === 'objectList'"
          :model-value="getPath(field.key) || []"
          :label="field.label"
          :blank="() => blankItem(field)"
          :disabled="disabled"
          @update:model-value="(value: any) => setValue(field, value)"
        >
          <template #item="{ item }">
            <SchemaForm
              :model="item"
              :fields="field.schema || []"
              :disabled="disabled"
              compact
              @change="$emit('change', $event)"
            />
          </template>
        </ObjectList>

        <p v-if="field.hint" class="schema-hint">{{ field.hint }}</p>
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
  import { computed, reactive, watch } from 'vue';
  import type { FieldDef } from './schema';
  import MediaPicker from './MediaPicker.vue';
  import PairList from './PairList.vue';
  import ObjectList from './ObjectList.vue';

  const props = withDefaults(
    defineProps<{
      model: Record<string, any>;
      fields: FieldDef[];
      disabled?: boolean;
      compact?: boolean;
    }>(),
    { disabled: false, compact: false }
  );

  const emit = defineEmits<{ (e: 'change', payload: { key: string; value: any }): void }>();

  defineOptions({ name: 'SchemaForm' });

  // listText 需要保留用户正在输入的空行，避免每敲一次换行就回写数组导致光标跳动。
  const listDrafts = reactive<Record<string, string>>({});

  const visibleFields = computed(() => props.fields || []);

  watch(
    () => props.model,
    () => {
      Object.keys(listDrafts).forEach((key) => delete listDrafts[key]);
    }
  );

  function getPath(path: string) {
    return path
      .split('.')
      .reduce<any>((acc, key) => (acc == null ? undefined : acc[key]), props.model);
  }

  function setPath(path: string, value: any) {
    const keys = path.split('.');
    let current: any = props.model;
    keys.slice(0, -1).forEach((key) => {
      if (current[key] == null || typeof current[key] !== 'object') current[key] = {};
      current = current[key];
    });
    current[keys[keys.length - 1]] = value;
  }

  function setValue(field: FieldDef, value: any) {
    setPath(field.key, value);
    if (field.type === 'listText') delete listDrafts[field.key];
    emit('change', { key: field.key, value });
  }

  function isFull(field: FieldDef) {
    if (field.full) return true;
    return ['textarea', 'listText', 'objectList', 'pairList'].includes(String(field.type));
  }

  function listTextValue(field: FieldDef) {
    if (field.key in listDrafts) return listDrafts[field.key];
    const value = getPath(field.key);
    return Array.isArray(value) ? value.join('\n') : '';
  }

  function linesOf(text: string) {
    return String(text || '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);
  }

  function onListTextInput(field: FieldDef, value: string) {
    listDrafts[field.key] = value;
    setPath(field.key, linesOf(value));
    emit('change', { key: field.key, value: linesOf(value) });
  }

  function onListTextAppend(field: FieldDef, urls: string[]) {
    const current = listTextValue(field);
    const next = [current, ...urls].filter(Boolean).join('\n');
    listDrafts[field.key] = next;
    setPath(field.key, linesOf(next));
    emit('change', { key: field.key, value: linesOf(next) });
  }

  function blankItem(field: FieldDef) {
    const blank: Record<string, any> = {};
    (field.schema || []).forEach((child) => {
      const key = child.key.split('.').pop() as string;
      if (child.type === 'checkbox') blank[key] = false;
      else if (child.type === 'number') blank[key] = 0;
      else if (child.type === 'listText' || child.type === 'objectList' || child.type === 'pairList')
        blank[key] = [];
      else blank[key] = '';
    });
    return blank;
  }
</script>

<style scoped lang="less">
  .schema-form {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px 16px;
  }
  .schema-form.compact {
    gap: 10px 12px;
  }
  .schema-field {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .schema-field.full {
    grid-column: 1 / -1;
  }
  .schema-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    font-weight: 700;
    color: var(--color-text-1);
  }
  .schema-hint {
    margin: 0;
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
  }
  .schema-inline-actions {
    display: flex;
    gap: 8px;
  }
  .schema-group {
    grid-column: 1 / -1;
    margin: 4px 0 0;
  }
  @media (max-width: 900px) {
    .schema-form {
      grid-template-columns: 1fr;
    }
  }
</style>
