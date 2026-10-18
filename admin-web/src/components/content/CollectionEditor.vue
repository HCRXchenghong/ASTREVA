<template>
  <div class="collection-editor">
    <div class="collection-list">
      <a-empty v-if="!items.length" description="暂无条目，请先新增" />
      <template v-else>
        <button
          v-for="(item, index) in items"
          :key="item.id || index"
          type="button"
          class="collection-item"
          :class="{ active: index === selectedIndex }"
          @click="emit('update:selectedIndex', index)"
        >
          <span class="collection-item-title">{{ labelOf(item, index) }}</span>
          <span v-if="item.slug" class="collection-item-sub">{{ item.slug }}</span>
        </button>
      </template>
      <a-button type="dashed" long class="collection-add" :disabled="disabled" @click="emit('create')">
        <template #icon><icon-plus /></template>
        新增{{ createLabel }}
      </a-button>
    </div>

    <div class="collection-detail">
      <template v-if="current">
        <div class="collection-head">
          <strong>{{ labelOf(current, selectedIndex) }}</strong>
          <a-button size="small" status="danger" :disabled="disabled" @click="emit('delete', selectedIndex)">
            删除当前{{ createLabel }}
          </a-button>
        </div>
        <SchemaForm
          :model="current"
          :fields="fields"
          :disabled="disabled"
          @change="emit('change')"
        />
      </template>
      <a-empty v-else description="选择左侧条目开始编辑，或先新增一条" />
    </div>
  </div>
</template>

<script lang="ts" setup>
  import { computed } from 'vue';
  import { CATEGORY_FIELDS, FAQ_FIELDS, productFields, type FieldDef } from './schema';
  import SchemaForm from './SchemaForm.vue';

  const props = withDefaults(
    defineProps<{
      moduleId: 'categories' | 'products' | 'faq';
      items: Record<string, any>[];
      categories?: Record<string, any>[];
      selectedIndex?: number;
      disabled?: boolean;
    }>(),
    { categories: () => [], selectedIndex: 0, disabled: false }
  );

  const emit = defineEmits<{
    (e: 'update:selectedIndex', value: number): void;
    (e: 'create'): void;
    (e: 'delete', index: number): void;
    (e: 'change'): void;
  }>();

  const createLabel = computed(() =>
    props.moduleId === 'categories' ? '品类' : props.moduleId === 'products' ? '产品' : 'FAQ'
  );

  const fields = computed<FieldDef[]>(() => {
    if (props.moduleId === 'categories') return CATEGORY_FIELDS;
    if (props.moduleId === 'products') return productFields(props.categories);
    return FAQ_FIELDS;
  });

  const current = computed(() => props.items[props.selectedIndex] || null);

  function labelOf(item: Record<string, any>, index: number) {
    return item?.name || item?.question || `条目 ${index + 1}`;
  }
</script>

<style scoped lang="less">
  .collection-editor {
    display: grid;
    grid-template-columns: 240px minmax(0, 1fr);
    gap: 18px;
    align-items: start;
  }
  .collection-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: 68vh;
    overflow: auto;
    padding-right: 4px;
  }
  .collection-item {
    display: flex;
    flex-direction: column;
    gap: 3px;
    text-align: left;
    padding: 9px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: #fff;
    cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
  }
  .collection-item:hover {
    border-color: rgb(var(--primary-5));
  }
  .collection-item.active {
    border-color: rgb(var(--primary-6));
    background: rgb(var(--primary-1));
  }
  .collection-item-title {
    font-weight: 700;
    font-size: 13px;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .collection-item-sub {
    font-size: 12px;
    color: var(--color-text-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .collection-add {
    margin-top: 2px;
  }
  .collection-detail {
    min-width: 0;
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    padding: 16px;
    background: #fff;
  }
  .collection-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
    padding-bottom: 10px;
    border-bottom: 1px solid var(--color-border-1);
  }
  @media (max-width: 1000px) {
    .collection-editor {
      grid-template-columns: 1fr;
    }
    .collection-list {
      max-height: 240px;
    }
  }
</style>
