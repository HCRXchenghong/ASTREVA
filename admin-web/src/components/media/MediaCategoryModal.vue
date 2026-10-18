<template>
  <a-modal
    :visible="visible"
    title="分类管理"
    width="520px"
    :footer="false"
    @update:visible="(value: boolean) => emit('update:visible', value)"
  >
    <p class="modal-subtitle">删除分类后，其素材会自动移入“未分类”。</p>

    <a-form layout="inline" class="category-form" @submit-success="createCategory">
      <a-form-item>
        <a-input
          v-model="name"
          :max-length="80"
          placeholder="新分类名称"
          style="width: 240px"
          @press-enter="createCategory"
        />
      </a-form-item>
      <a-form-item>
        <a-button type="primary" :loading="creating" @click="createCategory">
          新建分类
        </a-button>
      </a-form-item>
    </a-form>

    <a-spin :loading="loading" style="width: 100%">
      <div v-if="categories.length" class="category-list">
        <div v-for="item in categories" :key="item.name" class="category-item">
          <div>
            <b>{{ item.name }}</b>
            <span>{{ item.count }} 个素材</span>
          </div>
          <a-tag v-if="item.name === '未分类'" size="small">默认分类</a-tag>
          <a-link v-else status="danger" @click="removeCategory(item)">删除</a-link>
        </div>
      </div>
      <a-empty v-else description="暂无分类" />
    </a-spin>
  </a-modal>
</template>

<script lang="ts" setup>
  import { ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { createMediaCategory, deleteMediaCategory } from '@/api/media';
  import type { MediaCategory } from '@/api/media';
  import { reportError, runModalTask } from '@/lib/media';

  withDefaults(defineProps<{ visible: boolean; categories?: MediaCategory[]; loading?: boolean }>(), {
    categories: () => [],
    loading: false,
  });
  const emit = defineEmits<{
    (e: 'update:visible', value: boolean): void;
    (e: 'changed'): void;
  }>();

  const name = ref('');
  const creating = ref(false);

  async function createCategory() {
    const value = name.value.trim();
    if (!value) {
      Message.warning('请输入分类名称。');
      return;
    }
    creating.value = true;
    try {
      await createMediaCategory(value);
      name.value = '';
      Message.success('分类已创建。');
      emit('changed');
    } catch (error: any) {
      reportError(error, '分类创建失败。');
    } finally {
      creating.value = false;
    }
  }

  function removeCategory(item: MediaCategory) {
    Modal.confirm({
      title: '删除分类',
      content: `确认删除分类“${item.name}”？其素材会移入“未分类”。`,
      okButtonProps: { status: 'danger' },
      onOk: runModalTask(async () => {
        const result = (await deleteMediaCategory(item.name)).data;
        Message.success(`分类已删除，${result.reassigned} 个素材已重新归类。`);
        emit('changed');
      }),
    });
  }
</script>

<style scoped>
  .modal-subtitle {
    margin: 0 0 12px;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .category-form {
    margin-bottom: 12px;
  }

  .category-list {
    max-height: 320px;
    overflow: auto;
  }

  .category-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 0;
    border-bottom: 1px solid var(--color-border-1);
  }

  .category-item div {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .category-item span {
    color: var(--color-text-3);
    font-size: 12px;
  }
</style>
