<template>
  <a-row :gutter="16">
    <a-col :span="14">
      <a-card :bordered="false" title="AI 生成全站 SEO">
        <a-alert type="info" class="tip">
          为全站页面（首页、产品分类、产品详情、解决方案、支持页、静态页）批量生成中文
          title / description / keywords；生成结果直接保存为中文草稿，英语与阿拉伯语随后进入翻译队列。
        </a-alert>
        <a-descriptions :column="1" bordered class="info">
          <a-descriptions-item label="当前内容修订号">
            {{ revision || '—' }}
          </a-descriptions-item>
          <a-descriptions-item label="上次生成">
            {{ lastRun || '尚未在本页生成' }}
          </a-descriptions-item>
        </a-descriptions>
        <a-space class="actions">
          <a-button type="primary" :loading="generating" @click="generate">
            一键生成三语 SEO
          </a-button>
          <a-button :loading="loading" @click="load">刷新修订号</a-button>
        </a-space>
        <a-alert v-if="error" type="error" class="tip">{{ error }}</a-alert>
        <a-alert v-if="result" type="success" class="tip">{{ result }}</a-alert>
      </a-card>
    </a-col>
    <a-col :span="10">
      <a-card :bordered="false" title="说明">
        <a-typography-paragraph>
          生成会覆盖现有 SEO 标题、描述和关键词，分享图保持不变；执行前请确认官网内容没有未保存的修改。
        </a-typography-paragraph>
        <a-typography-paragraph>
          在「内容管理 → 官网内容」里编辑时会带修订号生成，能避免覆盖他人刚保存的内容；本页会先读取最新修订号再生成，冲突时会提示刷新。
        </a-typography-paragraph>
        <a-typography-paragraph type="secondary">
          上游模型使用「AI 管理 → 翻译 AI」里配置的文本模型与密钥（与网站翻译共用同一上游）。
        </a-typography-paragraph>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchBootstrap } from '@/api/admin';
  import client from '@/api/client';

  const revision = ref<number | null>(null);
  const generating = ref(false);
  const loading = ref(false);
  const error = ref('');
  const result = ref('');
  const lastRun = ref('');

  async function load() {
    loading.value = true;
    try {
      const payload = await fetchBootstrap();
      revision.value = payload?.data?.contentRevision ?? null;
      error.value = '';
    } catch (err: any) {
      error.value = err?.response?.data?.error?.message || err?.message || '读取内容修订号失败';
    } finally {
      loading.value = false;
    }
  }

  async function generate() {
    if (revision.value == null) {
      await load();
    }
    generating.value = true;
    error.value = '';
    result.value = '';
    try {
      const response = await client.post('/site-admin-api/seo/ai-generate', {
        revision: revision.value,
      });
      const count = response.data?.meta?.generatedCount ?? 0;
      result.value = `已生成并保存 ${count} 个页面的中文 SEO；英语与阿拉伯语已进入翻译队列。`;
      lastRun.value = new Date().toLocaleString('zh-CN', { hour12: false });
      Message.success('SEO 生成完成');
      await load();
    } catch (err: any) {
      const message = err?.response?.data?.error?.message || err?.message || '生成失败';
      error.value = message;
    } finally {
      generating.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped>
  .tip {
    margin-bottom: 12px;
  }
  .info {
    margin-bottom: 12px;
  }
  .actions {
    margin-top: 4px;
  }
</style>
