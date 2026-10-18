<template>
  <div class="object-list">
    <a-empty v-if="!list.length" description="暂无条目，可新增。" class="object-empty" />
    <div v-for="(item, index) in list" :key="index" class="object-card">
      <div class="object-card-head">
        <span>{{ label }} {{ index + 1 }}</span>
        <a-space>
          <a-button size="mini" :disabled="disabled || index === 0" @click="move(index, -1)">
            <template #icon><icon-up /></template>
          </a-button>
          <a-button
            size="mini"
            :disabled="disabled || index === list.length - 1"
            @click="move(index, 1)"
          >
            <template #icon><icon-down /></template>
          </a-button>
          <a-button size="mini" status="danger" :disabled="disabled" @click="remove(index)">
            删除
          </a-button>
        </a-space>
      </div>
      <div class="object-card-body">
        <slot name="item" :item="item" :index="index" />
      </div>
    </div>
    <a-button size="small" type="dashed" long :disabled="disabled" @click="add">
      <template #icon><icon-plus /></template>
      新增条目
    </a-button>
  </div>
</template>

<script lang="ts" setup>
  import { computed } from 'vue';

  const props = withDefaults(
    defineProps<{
      modelValue?: Record<string, any>[];
      label?: string;
      blank?: Record<string, any> | (() => Record<string, any>);
      disabled?: boolean;
    }>(),
    {
      modelValue: () => [],
      label: '条目',
      blank: () => ({}),
      disabled: false,
    }
  );

  const emit = defineEmits<{ (e: 'update:modelValue', value: Record<string, any>[]): void }>();

  const list = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []));

  function commit(next: Record<string, any>[]) {
    emit('update:modelValue', next);
  }

  function blankItem() {
    return typeof props.blank === 'function' ? { ...props.blank() } : { ...props.blank };
  }

  function add() {
    commit([...list.value, blankItem()]);
  }

  function remove(index: number) {
    const next = list.value.slice();
    next.splice(index, 1);
    commit(next);
  }

  function move(index: number, delta: number) {
    const target = index + delta;
    if (target < 0 || target >= list.value.length) return;
    const next = list.value.slice();
    const [item] = next.splice(index, 1);
    next.splice(target, 0, item);
    commit(next);
  }
</script>

<style scoped lang="less">
  .object-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .object-empty {
    padding: 8px 0;
  }
  .object-card {
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: var(--color-fill-1);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .object-card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-weight: 700;
    font-size: 13px;
    color: var(--color-text-2);
  }
  .object-card-body {
    display: block;
  }
</style>
