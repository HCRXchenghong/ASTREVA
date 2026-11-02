<template>
  <a-row :gutter="16">
    <a-col :span="14">
      <a-card :bordered="false" title="官网正式域名" class="domain-card">
        <a-alert type="info" class="tip">
          正式域名确定后在这里填写。保存会自动重建官网，并更新三语 canonical、hreflang、robots.txt 和 sitemap。域名需先在部署环境完成 DNS 与 HTTPS 配置。
        </a-alert>
        <a-form :model="domainForm" layout="vertical">
          <a-form-item label="官网域名（不含 https:// 和路径）">
            <a-input v-model="domainForm.domain" placeholder="例如 www.astreva.com" allow-clear />
          </a-form-item>
          <a-space>
            <a-button type="primary" :loading="savingDomain" @click="saveDomain">保存并重建官网</a-button>
            <a-button :loading="loadingDomain" @click="loadDomain">刷新</a-button>
          </a-space>
        </a-form>
        <p class="domain-origin">当前 SEO 地址：{{ domainSetting?.effectiveOrigin || '读取中' }}</p>
        <p class="domain-origin">留空保存可恢复部署环境的默认域名。</p>
        <a-alert v-if="domainError" type="error" class="tip">{{ domainError }}</a-alert>
        <a-alert v-if="domainResult" type="success" class="tip">{{ domainResult }}</a-alert>
      </a-card>
      <a-card :bordered="false" title="AI 生成全站 SEO">
        <a-alert type="info" class="tip">
          为全站页面（首页、产品分类、产品详情、解决方案、支持页、静态页）生成 SEO。
          中文由 SEO AI 直接生成；英语和阿拉伯语会在正文翻译完成后，基于各自语言内容再次由 SEO AI 独立生成。
          正文保存后会自动排队生成；三种语言均保存到对应内容版本，并在生成成功后自动构建前台页面。
        </a-alert>
        <a-descriptions :column="1" bordered class="info">
          <a-descriptions-item label="当前内容修订号">
            {{ revision || '—' }}
          </a-descriptions-item>
          <a-descriptions-item label="上次生成">
            {{ lastRun || '尚未在本页生成' }}
          </a-descriptions-item>
          <a-descriptions-item label="自动生成状态">
            {{ autoSeoLabel }}
          </a-descriptions-item>
        </a-descriptions>
        <a-space class="actions">
          <a-button type="primary" :loading="generating" @click="generate">
            一键生成三语独立 SEO
          </a-button>
          <a-button :loading="loading" @click="load">刷新修订号</a-button>
        </a-space>
        <a-alert v-if="error" type="error" class="tip">{{ error }}</a-alert>
        <a-alert v-if="autoSeo?.status === 'failed'" type="error" class="tip">自动生成失败：{{ autoSeo.error }}。可以点击“一键生成”重试。</a-alert>
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
          中文 SEO 与英/阿 SEO 使用 AI 控制台绑定的 seo 能力；正文翻译使用 translation 能力。若任一语言的 SEO 生成失败，该语言不会标记为已就绪，后台可查看失败状态并重试。
        </a-typography-paragraph>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { computed, onMounted, onUnmounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchBootstrap } from '@/api/admin';
  import client from '@/api/client';

  const revision = ref<number | null>(null);
  const generating = ref(false);
  const loading = ref(false);
  const error = ref('');
  const result = ref('');
  const lastRun = ref('');
  const domainForm = ref({ domain: '' });
  const domainSetting = ref<{ domain: string; effectiveOrigin: string; source: string } | null>(null);
  const savingDomain = ref(false);
  const loadingDomain = ref(false);
  const domainError = ref('');
  const domainResult = ref('');

  async function loadDomain() {
    loadingDomain.value = true;
    try {
      const response = await client.get('/site-admin-api/seo/site-domain');
      domainSetting.value = response.data?.data || null;
      domainForm.value.domain = domainSetting.value?.domain || '';
      domainError.value = '';
    } catch (err: any) {
      domainError.value = err?.response?.data?.error?.message || err?.message || '读取域名失败';
    } finally {
      loadingDomain.value = false;
    }
  }

  async function saveDomain() {
    savingDomain.value = true;
    domainError.value = '';
    domainResult.value = '';
    try {
      const response = await client.put('/site-admin-api/seo/site-domain', { domain: domainForm.value.domain });
      domainSetting.value = response.data?.data || null;
      domainForm.value.domain = domainSetting.value?.domain || '';
      domainResult.value = response.data?.meta?.rebuildError
        ? `域名已保存，但官网重建未排队：${response.data.meta.rebuildError}。请在内容管理中重新发布。`
        : domainSetting.value?.changed ? '域名已保存，官网重建已排队。' : '域名没有变化。';
      Message.success('官网域名已保存');
    } catch (err: any) {
      domainError.value = err?.response?.data?.error?.message || err?.message || '保存域名失败';
    } finally {
      savingDomain.value = false;
    }
  }
  const autoSeo = ref<{ status: string; sourceRevision: number; resultRevision?: number; error?: string; generatedAt?: string } | null>(null);
  const autoSeoLabel = computed(() => {
    if (!autoSeo.value) return '尚无自动任务';
    const label: Record<string, string> = { pending: '生成中', ready: '已完成', failed: '失败', superseded: '已被较新内容取代' };
    return `${label[autoSeo.value.status] || autoSeo.value.status} · 内容第 ${autoSeo.value.sourceRevision} 版`;
  });
  let statusTimer: ReturnType<typeof setInterval> | undefined;

  async function loadAutoSeo() {
    const response = await client.get('/site-admin-api/seo/auto-status');
    autoSeo.value = response.data?.data || null;
  }

  async function load() {
    loading.value = true;
    try {
      const payload = await fetchBootstrap();
      revision.value = payload?.data?.contentRevision ?? null;
      await loadAutoSeo();
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
      result.value = `已保存 ${count} 个页面的中文 SEO；英语和阿拉伯语正在翻译正文，并将分别生成本语言 SEO，完成后自动构建前台。`;
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

  onMounted(() => {
    void load();
    void loadDomain();
    statusTimer = setInterval(() => { void loadAutoSeo().catch(() => {}); }, 5000);
  });
  onUnmounted(() => { if (statusTimer) clearInterval(statusTimer); });
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
  .domain-card {
    margin-bottom: 16px;
  }
  .domain-origin {
    margin-top: 12px;
    color: var(--color-text-2);
  }
</style>
