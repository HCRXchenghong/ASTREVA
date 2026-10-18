<template>
  <div class="json-editor">
    <a-alert type="error" show-icon class="danger">
      <template #title>高风险操作</template>
      完整 JSON 直接对应官网内容模型。字段缺失、类型写错或删掉必填根节点都会导致保存失败，
      也可能让前台页面渲染异常。仅在批量核对或前端新增字段时使用。
    </a-alert>

    <div class="json-toolbar">
      <a-space wrap>
        <a-button size="small" :disabled="disabled" @click="format">
          <template #icon><icon-code /></template>
          格式化
        </a-button>
        <a-button size="small" :disabled="disabled" @click="reset">从当前内容重新载入</a-button>
        <a-button size="small" :disabled="disabled" @click="copy">复制 JSON</a-button>
        <a-button
          size="small"
          type="primary"
          status="danger"
          :disabled="disabled"
          @click="apply"
        >
          校验并应用 JSON
        </a-button>
      </a-space>
      <a-typography-text type="secondary">
        当前内容：{{ summary }}
      </a-typography-text>
    </div>

    <a-textarea
      v-model="text"
      class="json-textarea"
      :disabled="disabled"
      spellcheck="false"
      :auto-size="{ minRows: 22, maxRows: 40 }"
    />

    <p class="json-hint">
      应用后仍需点击顶部“仅保存”或“保存并发布”才会写入官网内容。保存时服务端会再次校验
      品类 / 产品 / FAQ / 静态页的唯一性与引用关系。
    </p>
  </div>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { copyText } from '@/lib/media';

  const props = withDefaults(
    defineProps<{
      model: Record<string, any>;
      disabled?: boolean;
    }>(),
    { disabled: false }
  );

  const emit = defineEmits<{ (e: 'apply', content: Record<string, any>): void }>();

  const text = ref(JSON.stringify(props.model || {}, null, 2));

  watch(
    () => props.model,
    (value) => {
      text.value = JSON.stringify(value || {}, null, 2);
    }
  );

  const summary = computed(() => {
    const content = props.model || {};
    return [
      `品类 ${content.categories?.length ?? 0}`,
      `产品 ${content.products?.length ?? 0}`,
      `FAQ ${content.faqs?.length ?? 0}`,
      `静态页 ${content.staticPages?.length ?? 0}`,
    ].join(' · ');
  });

  function format() {
    try {
      text.value = JSON.stringify(JSON.parse(text.value), null, 2);
    } catch (error: any) {
      Message.error(`JSON 格式错误：${error?.message || error}`);
    }
  }

  function reset() {
    text.value = JSON.stringify(props.model || {}, null, 2);
    Message.info('已从当前内容重新载入');
  }

  async function copy() {
    const ok = await copyText(text.value);
    if (ok) Message.success('已复制到剪贴板');
    else Message.warning('复制失败，请手动选择文本');
  }

  function parseContent() {
    const next = JSON.parse(text.value);
    if (
      !next ||
      typeof next !== 'object' ||
      Array.isArray(next) ||
      !next.site ||
      !next.home ||
      !next.productCenter ||
      !next.support ||
      !Array.isArray(next.categories) ||
      !Array.isArray(next.products) ||
      !Array.isArray(next.faqs) ||
      !Array.isArray(next.staticPages)
    ) {
      throw new Error('完整 JSON 缺少官网内容的必填根节点。');
    }
    return next;
  }

  function apply() {
    let next: Record<string, any>;
    try {
      next = parseContent();
    } catch (error: any) {
      Message.error(error?.message || 'JSON 解析失败');
      return;
    }
    Modal.confirm({
      title: '应用完整内容 JSON',
      content:
        '这会用文本框里的内容整体替换编辑器中的官网内容，未保存的修改将被覆盖。确认继续？',
      okText: '确认应用',
      cancelText: '取消',
      okButtonProps: { status: 'danger' },
      onOk: () => {
        emit('apply', next);
        Message.success('已应用 JSON，请记得保存');
      },
    });
  }
</script>

<style scoped lang="less">
  .json-editor {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .danger {
    margin-bottom: 4px;
  }
  .json-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .json-textarea :deep(textarea) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    line-height: 1.6;
  }
  .json-hint {
    margin: 0;
    font-size: 12px;
    line-height: 1.7;
    color: var(--color-text-3);
  }
</style>
