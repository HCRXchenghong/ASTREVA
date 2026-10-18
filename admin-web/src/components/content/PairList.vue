<template>
  <div class="pair-list">
    <a-empty v-if="!list.length" description="暂无条目" class="pair-empty" />
    <div v-for="(item, index) in list" :key="index" class="pair-card">
      <div class="pair-card-head">
        <span>条目 {{ index + 1 }}</span>
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
      <div class="pair-inputs">
        <a-input
          :model-value="item?.[keyA]"
          :placeholder="labelA"
          :disabled="disabled"
          @update:model-value="(value: string) => update(index, keyA, value)"
        />
        <a-input
          :model-value="item?.[keyB]"
          :placeholder="labelB"
          :disabled="disabled"
          @update:model-value="(value: string) => update(index, keyB, value)"
        />
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
      keyA?: string;
      keyB?: string;
      labelA?: string;
      labelB?: string;
      disabled?: boolean;
    }>(),
    {
      modelValue: () => [],
      keyA: 'label',
      keyB: 'href',
      labelA: '文字',
      labelB: '链接',
      disabled: false,
    }
  );

  const emit = defineEmits<{ (e: 'update:modelValue', value: Record<string, any>[]): void }>();

  const list = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : []));

  function commit(next: Record<string, any>[]) {
    emit('update:modelValue', next);
  }

  function update(index: number, key: string, value: any) {
    const next = list.value.map((item, i) => (i === index ? { ...item, [key]: value } : item));
    commit(next);
  }

  function add() {
    commit([...list.value, { [props.keyA]: '', [props.keyB]: '' }]);
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
  .pair-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .pair-empty {
    padding: 8px 0;
  }
  .pair-card {
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: var(--color-fill-1);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .pair-card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-weight: 700;
    font-size: 13px;
    color: var(--color-text-2);
  }
  .pair-inputs {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  @media (max-width: 720px) {
    .pair-inputs {
      grid-template-columns: 1fr;
    }
  }
</style>
