<template>
  <div class="content-editor-page">
    <!-- 顶部状态栏 -->
    <div class="topbar">
      <div class="topbar-status">
        <a-tag :color="dirty ? 'orange' : 'green'" size="large">
          <template #icon>
            <icon-check-circle v-if="!dirty" />
            <icon-exclamation-circle v-else />
          </template>
          {{ dirty ? '有未保存修改' : '已保存' }}
        </a-tag>
        <a-tag :color="rebuildColor" size="large" class="rebuild-tag" @click="refreshStatus">
          <template #icon><icon-refresh /></template>
          重建：{{ rebuildLabel }}
        </a-tag>
        <a-typography-text v-if="lastSavedAt" type="secondary" class="saved-at">
          上次保存 {{ localTime(lastSavedAt) }}
        </a-typography-text>
      </div>
      <div class="topbar-actions">
        <a-tooltip content="打开当前模块对应的已发布官网页面">
          <a-button :disabled="!content" @click="openPublished">
            <template #icon><icon-launch /></template>
            已发布官网
          </a-button>
        </a-tooltip>
        <a-button :loading="previewing" :disabled="!content || saving" @click="previewDraft">
          <template #icon><icon-eye /></template>
          预览草稿
        </a-button>
        <a-button :loading="saving" :disabled="operationOnly || !content" @click="saveOnly">
          <template #icon><icon-save /></template>
          仅保存
        </a-button>
        <a-button
          type="primary"
          :loading="publishing || saving"
          :disabled="operationOnly || !content"
          @click="saveAndPublish"
        >
          <template #icon><icon-send /></template>
          保存并发布
        </a-button>
      </div>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="state-panel">
      <a-spin dot :size="32" />
      <p>正在加载官网内容…</p>
    </div>

    <!-- 加载失败 -->
    <div v-else-if="loadError" class="state-panel">
      <a-result status="error" title="无法加载官网内容" :subtitle="loadError">
        <template #extra>
          <a-button type="primary" :loading="loading" @click="load">重新加载</a-button>
        </template>
      </a-result>
    </div>

    <div v-else class="workspace">
      <!-- 左侧模块导航 -->
      <aside class="module-sidebar">
        <a-input-search
          v-model="moduleKeyword"
          placeholder="搜索模块"
          size="small"
          class="module-search"
          allow-clear
        />
        <a-collapse
          v-model:active-key="openGroups"
          :bordered="false"
          class="module-nav"
          expand-icon-position="right"
        >
          <a-collapse-item v-for="group in visibleGroups" :key="group.id" :header="group.label">
            <template #extra>
              <component :is="group.icon" class="group-icon" />
            </template>
            <button
              v-for="id in group.items"
              :key="id"
              type="button"
              class="module-link"
              :class="{ active: id === activeModule }"
              @click="selectModule(id)"
            >
              <span>{{ MODULE_LABELS[id] || id }}</span>
              <icon-right class="module-arrow" />
            </button>
          </a-collapse-item>
        </a-collapse>
      </aside>

      <!-- 右侧编辑区 -->
      <section class="editor-pane">
        <header class="editor-head">
          <div>
            <h2>{{ MODULE_LABELS[activeModule] || activeModule }}</h2>
            <p class="editor-desc">{{ activeGroup?.description || '' }}</p>
          </div>
          <a-space>
            <a-tag v-if="!operationOnly" color="gray">保存模块：{{ sectionLabel }}</a-tag>
            <a-link @click="openPublished">
              预览位置 {{ MODULE_PREVIEW_PATHS[activeModule] || '/' }}
            </a-link>
          </a-space>
        </header>

        <div class="editor-body">
          <!-- 开发者高级：完整 JSON -->
          <JsonEditor
            v-if="activeModule === 'allContent'"
            :model="content"
            :disabled="saving"
            @apply="onApplyJson"
          />

          <!-- 素材库 -->
          <MediaLibrary
            v-else-if="activeModule === 'media'"
            :media="mediaItems"
            @updated="onMediaUpdated"
          />

          <!-- 降级版本 -->
          <FallbackVersions
            v-else-if="activeModule === 'fallbackVersions'"
            :versions="fallbackVersions"
            :active-id="fallbackActiveId"
            :busy="fallbackBusy"
            @create="onCreateFallback"
            @activate="onActivateFallback"
            @remove="onDeleteFallback"
          />

          <!-- 客户咨询 -->
          <LeadPanel
            v-else-if="activeModule === 'leads'"
            :leads="leads"
            :saving="saving"
            @save="onSaveLead"
          />

          <!-- SEO 设置（含 AI 一键生成） -->
          <template v-else-if="activeModule === 'seo'">
            <a-card :bordered="false" class="seo-ai-card">
              <template #title>AI SEO 生成</template>
              <p class="hint">
                根据当前已保存的网站内容，为全站、首页、产品中心、服务页、全部品类、产品和静态页生成中文
                SEO，并自动排队生成英文与阿拉伯语版本。现有分享图会保留，空白分享图自动使用页面主图或
                ASTREVA Logo。
              </p>
              <a-space direction="vertical">
                <a-button
                  type="primary"
                  :loading="seoRunning"
                  :disabled="dirty || saving"
                  @click="runSeo"
                >
                  一键 AI 生成三语 SEO
                </a-button>
                <a-typography-text v-if="dirty" type="warning">
                  当前还有未保存修改，请先保存后再生成 SEO。
                </a-typography-text>
                <a-typography-text v-if="seoMessage" type="success">
                  {{ seoMessage }}
                </a-typography-text>
              </a-space>
            </a-card>
            <a-card :bordered="false" class="seo-card">
              <template #title>站点 SEO 字段</template>
              <SchemaForm :model="content" :fields="seoFields" :disabled="saving" />
            </a-card>
          </template>

          <!-- 品类 / 产品 / FAQ -->
          <CollectionEditor
            v-else-if="collectionKind"
            :module-id="collectionKind.module"
            :items="collectionItems"
            :categories="content.categories"
            :selected-index="collectionIndex"
            :disabled="saving"
            @update:selected-index="(value: number) => (collectionIndex = value)"
            @create="createCollectionItem"
            @delete="deleteCollectionItem"
          />

          <!-- 静态页 -->
          <SchemaForm
            v-else-if="staticSlug"
            :model="staticPageModel"
            :fields="staticPageFields(staticSlug)"
            :disabled="saving"
          />

          <!-- 普通页面 / 全局组件 -->
          <SchemaForm v-else :model="content" :fields="pageFields" :disabled="saving" />
        </div>

        <footer v-if="!operationOnly" class="editor-footer">
          <a-space>
            <a-button :loading="saving" @click="saveOnly">保存当前模块</a-button>
            <a-button type="primary" :loading="publishing" @click="saveAndPublish">
              保存并发布
            </a-button>
          </a-space>
          <a-typography-text type="secondary">
            分模块保存：只写入当前模块字段，提交时携带内容修订号；其余模块的改动会保留在编辑器中。
          </a-typography-text>
        </footer>
      </section>
    </div>
  </div>
</template>

<script lang="ts" setup>
  import {
    computed,
    onActivated,
    onBeforeUnmount,
    onDeactivated,
    onMounted,
    reactive,
    ref,
    watch,
  } from 'vue';
  import { onBeforeRouteLeave, useRoute } from 'vue-router';
  import { Message, Modal } from '@arco-design/web-vue';
  import cloneDeep from 'lodash/cloneDeep';
  import isEqual from 'lodash/isEqual';
  import {
    activateFallbackVersion,
    createFallbackVersion,
    deleteFallbackVersion,
    fetchContentBootstrap,
    fetchSiteStatus,
    generateSeo,
    requestDraftPreview,
    requestPublish,
    saveContentSection,
    type BootstrapPayload,
    type FallbackVersion,
    type MediaItem,
    type SiteStatus,
  } from '@/api/content';
  import {
    CONTENT_MODULE_GROUPS,
    MODULE_LABELS,
    MODULE_PREVIEW_PATHS,
    STATIC_MODULE_SLUGS,
    pageFieldsForModule,
    staticPageFields,
  } from '@/components/content/schema';
  import SchemaForm from '@/components/content/SchemaForm.vue';
  import JsonEditor from '@/components/content/JsonEditor.vue';
  import MediaLibrary from '@/components/content/MediaLibrary.vue';
  import FallbackVersions from '@/components/content/FallbackVersions.vue';
  import LeadPanel from '@/components/content/LeadPanel.vue';
  import CollectionEditor from '@/components/content/CollectionEditor.vue';

  const route = useRoute();

  // ---------- 状态 ----------
  const loading = ref(true);
  const loadError = ref('');
  const saving = ref(false);
  const publishing = ref(false);
  const previewing = ref(false);
  const seoRunning = ref(false);
  const seoMessage = ref('');
  const fallbackBusy = ref(false);
  const lastSavedAt = ref('');

  const content = ref<any>(null);
  const baseline = ref<any>(null);
  const revision = ref(0);
  const previewUrl = ref('');
  const leads = ref<Record<string, any>[]>([]);
  const mediaItems = ref<MediaItem[]>([]);
  const fallbackVersions = ref<FallbackVersion[]>([]);
  const fallbackActiveId = ref('');
  const status = ref<SiteStatus | null>(null);

  const activeModule = ref('homeHero');
  const moduleKeyword = ref('');
  const openGroups = ref<string[]>(['pages']);
  const selection = reactive({ category: 0, product: 0, faq: 0 });

  // ---------- 计算属性 ----------
  const dirty = computed(
    () => Boolean(content.value && baseline.value) && !isEqual(content.value, baseline.value)
  );

  const operationOnly = computed(() => ['media', 'fallbackVersions'].includes(activeModule.value));

  const collectionKind = computed(() => {
    if (activeModule.value === 'categories') {
      return { module: 'categories' as const, key: 'category' as const, list: 'categories' };
    }
    if (activeModule.value === 'products') {
      return { module: 'products' as const, key: 'product' as const, list: 'products' };
    }
    if (activeModule.value === 'faq') {
      return { module: 'faq' as const, key: 'faq' as const, list: 'faqs' };
    }
    return null;
  });

  const collectionItems = computed<Record<string, any>[]>(() =>
    collectionKind.value ? content.value?.[collectionKind.value.list] || [] : []
  );

  const collectionIndex = computed({
    get: () => (collectionKind.value ? selection[collectionKind.value.key] : 0),
    set: (value: number) => {
      if (collectionKind.value) selection[collectionKind.value.key] = value;
    },
  });

  const staticSlug = computed(() => STATIC_MODULE_SLUGS[activeModule.value] || '');

  // 静态页对象由 watch(activeModule) 保证存在，这里只做只读查找。
  const staticPageModel = computed<Record<string, any>>(() => {
    if (!staticSlug.value || !content.value) return {};
    return (
      content.value.staticPages.find((item: any) => item.slug === staticSlug.value) || {}
    );
  });

  const pageFields = computed(() => pageFieldsForModule(activeModule.value));
  const seoFields = computed(() => pageFieldsForModule('seo'));

  const activeGroup = computed(() =>
    CONTENT_MODULE_GROUPS.find((group) => group.items.includes(activeModule.value))
  );

  const visibleGroups = computed(() => {
    const keyword = moduleKeyword.value.trim().toLowerCase();
    if (!keyword) return CONTENT_MODULE_GROUPS;
    return CONTENT_MODULE_GROUPS.map((group) => ({
      ...group,
      items: group.items.filter((id) =>
        String(MODULE_LABELS[id] || id)
          .toLowerCase()
          .includes(keyword)
      ),
    })).filter((group) => group.items.length);
  });

  const sectionLabel = computed(() => {
    if (['allContent', 'seo'].includes(activeModule.value)) return 'content（整份内容 + 修订号）';
    if (activeModule.value.startsWith('home')) return 'home';
    if (activeModule.value === 'productCenter') return 'productCenter';
    if (activeModule.value === 'support') return 'support';
    if (['site', 'navigation', 'footer', 'social', 'cookie', 'chat'].includes(activeModule.value)) {
      return 'site';
    }
    if (staticSlug.value) return `staticPage（${staticSlug.value}）`;
    if (activeModule.value === 'categories') return 'category';
    if (activeModule.value === 'products') return 'product';
    if (activeModule.value === 'faq') return 'faq';
    if (activeModule.value === 'leads') return 'lead';
    return activeModule.value;
  });

  const rebuildState = computed(() => status.value?.rebuild || null);
  const rebuildLabel = computed(() => {
    const rebuild = rebuildState.value;
    if (!rebuild) return '读取中';
    if (!rebuild.ok) return '重建服务未启动';
    if (rebuild.running) return '构建中';
    if (rebuild.queued) return '等待构建';
    if (rebuild.lastBuild?.status === 'succeeded') return '已同步前台';
    if (rebuild.lastBuild?.status === 'failed') return '上次构建失败';
    return rebuild.message || '可发布';
  });
  const rebuildColor = computed(() => {
    const rebuild = rebuildState.value;
    if (!rebuild) return 'gray';
    if (!rebuild.ok || rebuild.lastBuild?.status === 'failed') return 'red';
    if (rebuild.running || rebuild.queued) return 'orange';
    if (rebuild.lastBuild?.status === 'succeeded') return 'green';
    return 'arcoblue';
  });

  // ---------- 工具 ----------
  function localTime(value?: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '';
  }

  const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

  function clampSelections() {
    if (!content.value) return;
    selection.category = Math.min(
      selection.category,
      Math.max(content.value.categories.length - 1, 0)
    );
    selection.product = Math.min(selection.product, Math.max(content.value.products.length - 1, 0));
    selection.faq = Math.min(selection.faq, Math.max(content.value.faqs.length - 1, 0));
  }

  function applyBootstrap(payload: BootstrapPayload) {
    const data = payload?.data;
    if (!data?.content) return;
    content.value = cloneDeep(data.content);
    baseline.value = cloneDeep(data.content);
    revision.value = Number(data.contentRevision || 0);
    leads.value = data.leads || [];
    mediaItems.value = data.media || [];
    fallbackVersions.value = data.fallbackVersions || [];
    fallbackActiveId.value = data.fallbackActiveId || '';
    if (payload.meta?.previewUrl) previewUrl.value = payload.meta.previewUrl;
    clampSelections();
  }

  // ---------- 加载 ----------
  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const payload = await fetchContentBootstrap();
      applyBootstrap(payload);
      const requested = String(route.query.section || '');
      if (requested && MODULE_LABELS[requested]) activeModule.value = requested;
      const group = CONTENT_MODULE_GROUPS.find((item) =>
        item.items.includes(activeModule.value)
      );
      openGroups.value = [group?.id || CONTENT_MODULE_GROUPS[0].id];
      await refreshStatus();
    } catch (error: any) {
      loadError.value = error?.message || '请求 /site-admin-api/bootstrap 失败';
    } finally {
      loading.value = false;
    }
  }

  async function refreshStatus() {
    try {
      status.value = await fetchSiteStatus();
      return status.value;
    } catch {
      status.value = null;
      return null;
    }
  }

  // ---------- 模块切换 ----------
  function selectModule(id: string) {
    activeModule.value = id;
    const group = CONTENT_MODULE_GROUPS.find((item) => item.items.includes(id));
    if (group && !openGroups.value.includes(group.id)) {
      openGroups.value = [...openGroups.value, group.id];
    }
  }

  watch(activeModule, (id) => {
    const slug = STATIC_MODULE_SLUGS[id];
    if (!slug || !content.value) return;
    if (!content.value.staticPages.some((item: any) => item.slug === slug)) {
      content.value.staticPages.push({
        slug,
        title: slug,
        hero: { title: slug, description: '' },
        body: '',
        sections: {},
        seo: { title: `${slug} | 星渡ASTREVA`, description: slug },
      });
    }
  });

  // ---------- 保存 ----------
  function sectionPayload(): { section: string; data: any } | null {
    const id = activeModule.value;
    if (!content.value) return null;
    if (id === 'allContent' || id === 'seo') {
      return { section: 'content', data: cloneDeep(content.value) };
    }
    if (id.startsWith('home')) return { section: 'home', data: cloneDeep(content.value.home) };
    if (id === 'productCenter') {
      return { section: 'productCenter', data: cloneDeep(content.value.productCenter) };
    }
    if (id === 'support') return { section: 'support', data: cloneDeep(content.value.support) };
    if (['site', 'navigation', 'footer', 'social', 'cookie', 'chat'].includes(id)) {
      return { section: 'site', data: cloneDeep(content.value.site) };
    }
    if (STATIC_MODULE_SLUGS[id]) {
      const page = content.value.staticPages.find(
        (item: any) => item.slug === STATIC_MODULE_SLUGS[id]
      );
      if (!page) return null;
      return { section: 'staticPage', data: cloneDeep(page) };
    }
    if (id === 'categories') {
      const item = content.value.categories[selection.category];
      return item ? { section: 'category', data: cloneDeep(item) } : null;
    }
    if (id === 'products') {
      const item = content.value.products[selection.product];
      return item ? { section: 'product', data: cloneDeep(item) } : null;
    }
    if (id === 'faq') {
      const item = content.value.faqs[selection.faq];
      return item ? { section: 'faq', data: cloneDeep(item) } : null;
    }
    return null;
  }

  function showConflict() {
    Modal.warning({
      title: '保存冲突',
      content: '内容已被其他页面更新，请刷新后重试。',
      okText: '刷新内容',
      hideCancel: true,
      onOk: () => load(),
    });
  }

  async function performSave(section: string, data: unknown): Promise<boolean> {
    saving.value = true;
    try {
      const payload = await saveContentSection({ section, data, revision: revision.value });
      applyBootstrap(payload);
      lastSavedAt.value = new Date().toISOString();
      return true;
    } catch (error: any) {
      if (error?.response?.status === 409) showConflict();
      return false;
    } finally {
      saving.value = false;
    }
  }

  async function saveModule(): Promise<boolean> {
    if (operationOnly.value) {
      Message.warning('当前模块的操作会立即保存，无需手动保存。');
      return false;
    }
    const payload = sectionPayload();
    if (!payload) {
      Message.warning('请先选择或新增一条记录再保存。');
      return false;
    }
    const ok = await performSave(payload.section, payload.data);
    if (ok) Message.success('已保存');
    return ok;
  }

  async function saveOnly() {
    if (saving.value || publishing.value) return;
    await saveModule();
  }

  async function saveAndPublish() {
    if (saving.value || publishing.value) return;
    if (operationOnly.value) {
      Message.warning('当前模块的操作会立即保存，无需整体发布。');
      return;
    }
    const payload = sectionPayload();
    if (!payload) {
      Message.warning('请先选择或新增一条记录再保存。');
      return;
    }
    const ok = await performSave(payload.section, payload.data);
    if (!ok) return;
    if (payload.section === 'lead') {
      Message.success('咨询单已保存（无需重新发布官网）');
      return;
    }
    await publishAndWait('current');
  }

  // ---------- 发布 / 预览 ----------
  function showBuildFailure(message: string) {
    const rebuild = rebuildState.value;
    Modal.error({
      title: '官网构建未完成',
      content: `${message}\n重建状态接口：${rebuild?.url || '未配置'}\n上次构建结果：${
        rebuild?.lastBuild?.status || '未知'
      }`,
      okText: '知道了',
      hideCancel: true,
    });
  }

  async function waitForBuild(maxTries = 90) {
    for (let index = 0; index < maxTries; index += 1) {
      // eslint-disable-next-line no-await-in-loop
      const next = await refreshStatus();
      const rebuild = next?.rebuild;
      if (rebuild && !rebuild.running && !rebuild.queued) {
        if (rebuild.lastBuild?.status === 'failed') {
          throw new Error('官网静态构建失败，请查看重建日志。');
        }
        if (rebuild.lastBuild?.status === 'succeeded') return next;
      }
      // eslint-disable-next-line no-await-in-loop
      await sleep(800);
    }
    throw new Error('官网静态构建超时，请稍后查看重建状态。');
  }

  async function publishAndWait(source: 'current' | 'fallback') {
    publishing.value = true;
    try {
      await requestPublish(source);
      await waitForBuild();
      Message.success('发布完成，官网已同步');
    } catch (error: any) {
      showBuildFailure(error?.message || '发布失败');
    } finally {
      publishing.value = false;
      await refreshStatus();
    }
  }

  function siteOrigin() {
    return previewUrl.value || status.value?.preview?.url || '';
  }

  function buildPublishedUrl(path: string) {
    const raw = path && path.startsWith('/') ? path : `/${path || ''}`;
    const hashIndex = raw.indexOf('#');
    const beforeHash = hashIndex === -1 ? raw : raw.slice(0, hashIndex);
    const hash = hashIndex === -1 ? '' : raw.slice(hashIndex);
    const separator = beforeHash.includes('?') ? '&' : '?';
    return `${siteOrigin()}${beforeHash}${separator}adminPreview=${Date.now()}${hash}`;
  }

  function openPublished() {
    const path = MODULE_PREVIEW_PATHS[activeModule.value] || '/';
    window.open(buildPublishedUrl(path), '_blank');
  }

  async function previewDraft() {
    if (!content.value || previewing.value) return;
    previewing.value = true;
    try {
      await requestDraftPreview(cloneDeep(content.value));
      const finalStatus = await waitForBuild();
      const draftUrl = finalStatus?.preview?.draftUrl || status.value?.preview?.draftUrl || '';
      if (draftUrl) window.open(draftUrl, '_blank');
      Message.success('草稿预览构建完成，已在新窗口打开');
    } catch (error: any) {
      showBuildFailure(error?.message || '草稿预览构建失败');
    } finally {
      previewing.value = false;
      await refreshStatus();
    }
  }

  // ---------- AI SEO ----------
  function runSeo() {
    if (dirty.value) {
      Message.warning('当前页面还有未保存修改，请先保存后再生成 SEO。');
      return;
    }
    Modal.confirm({
      title: '一键 AI 生成三语 SEO',
      content: 'AI 将覆盖全站所有页面现有的 SEO 标题、描述和关键词。现有分享图会保留。确认继续？',
      okText: '开始生成',
      cancelText: '取消',
      onOk: async () => {
        seoRunning.value = true;
        seoMessage.value = '';
        try {
          const payload = await generateSeo(revision.value);
          applyBootstrap(payload);
          const count = Number(payload.meta?.generatedCount || 0);
          seoMessage.value = `已生成并保存 ${count} 个页面的中文 SEO；英文和阿拉伯语已进入自动翻译队列。`;
          lastSavedAt.value = new Date().toISOString();
          Message.success(`AI SEO 生成完成，共 ${count} 个页面`);
        } catch (error: any) {
          if (error?.response?.status === 409) showConflict();
        } finally {
          seoRunning.value = false;
          await refreshStatus();
        }
      },
    });
  }

  // ---------- 客户咨询 ----------
  async function onSaveLead(payload: {
    id: string;
    status: string;
    priority: string;
    internalNotes: string;
  }) {
    const ok = await performSave('lead', payload);
    if (ok) Message.success('咨询单已保存');
  }

  // ---------- 集合编辑 ----------
  function createCollectionItem() {
    if (!collectionKind.value || !content.value) return;
    if (collectionKind.value.module === 'categories') {
      content.value.categories.push({
        id: '',
        name: '新产品品类',
        slug: '',
        description: '',
        coverImage: '',
        heroVideo: '',
        sortOrder: 0,
        seo: { title: '', description: '' },
      });
      selection.category = content.value.categories.length - 1;
    } else if (collectionKind.value.module === 'products') {
      content.value.products.push({
        id: '',
        name: '新产品',
        slug: '',
        categorySlug: content.value.categories[0]?.slug || '',
        coverImage: '',
        galleryImages: [],
        videoUrl: '',
        summary: '',
        features: [],
        specifications: [],
        downloads: [],
        body: '',
        isFeatured: false,
        sortOrder: 0,
        seo: { title: '', description: '' },
      });
      selection.product = content.value.products.length - 1;
    } else {
      content.value.faqs.push({
        id: '',
        question: '',
        answer: '',
        sortOrder: 0,
        enabled: true,
      });
      selection.faq = content.value.faqs.length - 1;
    }
    Message.info('已新增条目，填写后点击“保存当前模块”');
  }

  function deleteCollectionItem(index: number) {
    const kind = collectionKind.value;
    if (!kind || !content.value) return;
    const list = content.value[kind.list] as Record<string, any>[];
    const item = list[index];
    if (!item) return;
    let warning = '确认删除当前条目？此操作不可恢复。';
    if (kind.module === 'categories') {
      const linked = content.value.products.filter(
        (product: any) => product.categorySlug === item.slug
      ).length;
      warning = linked
        ? `确认删除品类“${item.name || ''}”及其关联的 ${linked} 个产品？此操作不可恢复。`
        : `确认删除品类“${item.name || ''}”？此操作不可恢复。`;
    } else if (kind.module === 'products') {
      warning = `确认删除产品“${item.name || ''}”？此操作不可恢复。`;
    } else {
      warning = `确认删除 FAQ“${item.question || ''}”？此操作不可恢复。`;
    }
    Modal.confirm({
      title: '删除确认',
      content: warning,
      okText: '确认删除',
      cancelText: '取消',
      okButtonProps: { status: 'danger' },
      onOk: async () => {
        const section =
          kind.module === 'categories' ? 'category' : kind.module === 'products' ? 'product' : 'faq';
        const ok = await performSave(section, { _delete: true, id: item.id });
        if (!ok) return;
        Message.success('已删除，正在同步官网');
        await publishAndWait('current');
      },
    });
  }

  // ---------- 降级版本 ----------
  async function reloadFallbackVersions() {
    try {
      const payload = await fetchContentBootstrap();
      fallbackVersions.value = payload.data.fallbackVersions || [];
      fallbackActiveId.value = payload.data.fallbackActiveId || '';
    } catch {
      // 忽略；下次 bootstrap 会刷新
    }
  }

  async function onCreateFallback(name: string) {
    if (fallbackBusy.value) return;
    fallbackBusy.value = true;
    try {
      await createFallbackVersion(name);
      await reloadFallbackVersions();
      Message.success('已从当前官网内容创建降级版本');
    } finally {
      fallbackBusy.value = false;
    }
  }

  async function onActivateFallback(id: string) {
    if (fallbackBusy.value) return;
    fallbackBusy.value = true;
    try {
      await activateFallbackVersion(id);
      await reloadFallbackVersions();
      Message.success('降级版本已启用，正在重新发布官网');
      fallbackBusy.value = false;
      await publishAndWait('fallback');
    } finally {
      fallbackBusy.value = false;
    }
  }

  async function onDeleteFallback(id: string) {
    if (fallbackBusy.value) return;
    fallbackBusy.value = true;
    try {
      const result = await deleteFallbackVersion(id);
      await reloadFallbackVersions();
      Message.success('降级版本已删除');
      fallbackBusy.value = false;
      if (result?.activeChanged) {
        Message.info('删除的是当前启用版本，正在自动切换并重新发布');
        await publishAndWait('fallback');
      }
    } finally {
      fallbackBusy.value = false;
    }
  }

  // ---------- 其它 ----------
  function onApplyJson(next: Record<string, any>) {
    content.value = next;
  }

  function onMediaUpdated(items: MediaItem[]) {
    mediaItems.value = items;
  }

  // ---------- 未保存离开提示 ----------
  function onBeforeUnload(event: BeforeUnloadEvent) {
    if (!dirty.value) return;
    event.preventDefault();
    event.returnValue = '';
  }

  onBeforeRouteLeave(() => {
    if (!dirty.value) return true;
    return new Promise<boolean>((resolve) => {
      let settled = false;
      const done = (value: boolean) => {
        if (settled) return;
        settled = true;
        resolve(value);
      };
      Modal.confirm({
        title: '有未保存修改',
        content: '当前模块还有未保存修改，确定放弃并离开吗？',
        okText: '放弃并离开',
        cancelText: '继续编辑',
        okButtonProps: { status: 'danger' },
        onOk: () => done(true),
        onCancel: () => done(false),
        onClose: () => done(false),
      });
    });
  });

  // ---------- 生命周期 ----------
  let timer: ReturnType<typeof setInterval> | null = null;
  function startPolling() {
    if (timer) return;
    timer = setInterval(() => {
      refreshStatus();
    }, 8000);
  }
  function stopPolling() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  onMounted(() => {
    window.addEventListener('beforeunload', onBeforeUnload);
    startPolling();
    load();
  });

  onActivated(() => {
    startPolling();
    refreshStatus();
  });

  onDeactivated(stopPolling);

  onBeforeUnmount(() => {
    window.removeEventListener('beforeunload', onBeforeUnload);
    stopPolling();
  });
</script>

<style scoped lang="less">
  .content-editor-page {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-height: calc(100vh - 140px);
  }

  .topbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
    background: #fff;
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    position: sticky;
    top: 0;
    z-index: 10;
  }
  .topbar-status,
  .topbar-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }
  .rebuild-tag {
    cursor: pointer;
  }
  .saved-at {
    font-size: 12px;
  }

  .state-panel {
    display: grid;
    place-items: center;
    gap: 12px;
    padding: 80px 0;
    background: #fff;
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    color: var(--color-text-3);
  }

  .workspace {
    display: grid;
    grid-template-columns: 236px minmax(0, 1fr);
    gap: 12px;
    align-items: start;
  }

  .module-sidebar {
    background: #fff;
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    padding: 10px;
    position: sticky;
    top: 74px;
    max-height: calc(100vh - 170px);
    overflow: auto;
  }
  .module-search {
    margin-bottom: 8px;
  }
  .module-nav :deep(.arco-collapse-item-header) {
    padding: 8px 4px;
    font-weight: 700;
  }
  .module-nav :deep(.arco-collapse-item-content) {
    padding: 0 0 6px;
    background: transparent;
  }
  .group-icon {
    font-size: 14px;
    color: var(--color-text-3);
  }
  .module-link {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 7px 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-2);
    font-size: 13px;
    text-align: left;
    cursor: pointer;
  }
  .module-link:hover {
    background: var(--color-fill-2);
    color: var(--color-text-1);
  }
  .module-link.active {
    background: rgb(var(--primary-1));
    color: rgb(var(--primary-6));
    font-weight: 700;
  }
  .module-arrow {
    font-size: 12px;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .module-link:hover .module-arrow,
  .module-link.active .module-arrow {
    opacity: 1;
  }

  .editor-pane {
    min-width: 0;
    background: #fff;
    border: 1px solid var(--color-border-2);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
  }
  .editor-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 16px 18px;
    border-bottom: 1px solid var(--color-border-1);
  }
  .editor-head h2 {
    margin: 0 0 4px;
    font-size: 18px;
  }
  .editor-desc {
    margin: 0;
    font-size: 12px;
    color: var(--color-text-3);
  }
  .editor-body {
    padding: 18px;
    min-height: 320px;
  }
  .editor-footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 18px;
    border-top: 1px solid var(--color-border-1);
    background: var(--color-fill-1);
    border-radius: 0 0 10px 10px;
  }
  .seo-ai-card,
  .seo-card {
    border: 1px solid var(--color-border-2);
    margin-bottom: 14px;
  }
  .hint {
    margin: 0 0 12px;
    font-size: 12px;
    line-height: 1.8;
    color: var(--color-text-3);
  }

  @media (max-width: 1000px) {
    .workspace {
      grid-template-columns: 1fr;
    }
    .module-sidebar {
      position: static;
      max-height: 320px;
    }
  }
</style>
