<template>
  <div class="translation-page">
    <a-card :bordered="false" title="网站翻译">
      <template #extra>
        <a-space>
          <a-tag v-if="polling" color="arcoblue">
            <template #icon><icon-refresh /></template>
            自动刷新中
          </a-tag>
          <a-button :loading="loading" @click="refreshAll">
            <template #icon><icon-refresh /></template>
            刷新状态
          </a-button>
        </a-space>
      </template>

      <a-alert class="intro" type="info" show-icon>
        后台只维护中文；每次保存官网内容后，系统会把同一修订版自动翻译为英语和阿拉伯语。
      </a-alert>

      <a-skeleton v-if="initialLoading" :animation="true" class="skeleton">
        <a-skeleton-line :rows="8" />
      </a-skeleton>

      <a-result
        v-else-if="!overview"
        status="error"
        title="无法加载翻译状态"
        :subtitle="loadError || '请检查网络连接后重试。'"
      >
        <template #extra>
          <a-button type="primary" @click="refreshAll">重新加载</a-button>
        </template>
      </a-result>

      <template v-else>
        <a-alert
          v-if="loadError"
          class="intro"
          type="warning"
          show-icon
          :title="`状态刷新失败：${loadError}`"
        />

        <a-row :gutter="[16, 16]" align="stretch">
          <a-col v-for="card in localeCards" :key="card.locale" :xs="24" :md="8">
            <a-card class="locale-card">
              <template #title>
                <a-space :size="6">
                  <span>{{ card.name }}</span>
                  <a-tag size="small" :color="card.locale === 'zh-CN' ? 'gray' : 'arcoblue'">
                    {{ card.locale }}
                  </a-tag>
                  <a-tag v-if="card.locale === 'zh-CN'" size="small" color="green">源语言</a-tag>
                </a-space>
              </template>
              <template #extra>
                <a-tag :color="statusMeta(card).color">{{ statusMeta(card).label }}</a-tag>
              </template>

              <div v-if="isActive(card)" class="progress">
                <a-progress
                  type="line"
                  size="large"
                  :show-text="false"
                  :percent="progressRatio(card)"
                />
                <div class="progress-text">
                  {{
                    card.progress
                      ? `已翻译 ${card.progress.done} / ${card.progress.total} 段文案 · ${progressPercent(card)}%`
                      : '等待队列分配进度…'
                  }}
                </div>
              </div>

              <a-descriptions :column="1" size="small" :label-style="{ width: '90px' }">
                <a-descriptions-item label="源修订版">{{ card.revision }}</a-descriptions-item>
                <a-descriptions-item label="是否过期">
                  <template v-if="card.stale">
                    <a-tag size="small" color="orange">已过期</a-tag>
                    <span class="stale-tip">不会对外提供</span>
                  </template>
                  <span v-else>否</span>
                </a-descriptions-item>
                <a-descriptions-item label="更新时间">
                  {{ localTime(card.updatedAt) }}
                </a-descriptions-item>
                <a-descriptions-item v-if="card.lastError" label="失败原因">
                  <a-typography-text type="danger">{{ card.lastError }}</a-typography-text>
                </a-descriptions-item>
              </a-descriptions>

              <a-typography-text
                v-if="card.locale !== 'zh-CN' && !card.updatedAt && !card.lastError"
                type="secondary"
                class="card-hint"
              >
                尚未生成该语言版本。
              </a-typography-text>
            </a-card>
          </a-col>
        </a-row>

        <a-card class="panel" title="自动翻译上游">
          <template #extra>
            <a-tag :color="form.apiKeyConfigured ? 'green' : 'gray'">
              {{ form.apiKeyConfigured ? 'Key 已配置（加密存储）' : 'Key 未配置' }}
            </a-tag>
          </template>

          <a-form :model="form" layout="vertical" @submit="saveSettings">
            <a-row :gutter="16">
              <a-col :span="24">
                <a-form-item>
                  <a-space>
                    <a-switch v-model="form.enabled" />
                    <span>中文保存后自动翻译</span>
                  </a-space>
                </a-form-item>
              </a-col>

              <a-col :xs="24" :md="12">
                <a-form-item label="API 类型">
                  <a-select v-model="form.apiType" :options="apiTypeOptions" />
                </a-form-item>
              </a-col>
              <a-col :xs="24" :md="12">
                <a-form-item label="上游地址">
                  <a-input
                    v-model="form.baseUrl"
                    placeholder="https://api.openai.com/v1"
                    allow-clear
                  />
                </a-form-item>
              </a-col>

              <a-col :xs="24" :md="12">
                <a-form-item label="API Key">
                  <a-input-password
                    v-model="form.apiKey"
                    autocomplete="new-password"
                    allow-clear
                    :placeholder="form.apiKeyConfigured ? '留空表示保留现有 Key' : '请输入上游 API Key'"
                  />
                  <template #extra>
                    {{
                      form.apiKeyConfigured
                        ? `已配置（${form.apiKeyMasked}），页面与接口都不会回显明文`
                        : '尚未配置，填写并保存后即可启用自动翻译'
                    }}
                  </template>
                </a-form-item>
              </a-col>

              <a-col :xs="24" :md="12">
                <a-form-item label="翻译模型">
                  <div class="model-row">
                    <a-select
                      v-model="form.model"
                      class="model-select"
                      :options="modelOptions"
                      allow-create
                      allow-search
                      placeholder="请选择或输入模型"
                    />
                    <a-button
                      :loading="fetchingModels"
                      :disabled="busy && !fetchingModels"
                      @click="loadModels"
                    >
                      从上游获取模型
                    </a-button>
                  </div>
                  <template #extra>
                    共 {{ modelOptions.length }} 个可选模型，可直接输入自定义模型名。
                  </template>
                </a-form-item>
              </a-col>

              <a-col :xs="12" :md="6">
                <a-form-item label="每批文案数">
                  <a-input-number v-model="form.batchSize" :min="5" :max="100" style="width: 100%" />
                </a-form-item>
              </a-col>
              <a-col :xs="12" :md="6">
                <a-form-item label="超时（秒）">
                  <a-input-number
                    v-model="form.timeoutSeconds"
                    :min="10"
                    :max="600"
                    style="width: 100%"
                  />
                </a-form-item>
              </a-col>

              <a-col :span="24">
                <a-space wrap>
                  <a-button
                    type="primary"
                    :loading="saving"
                    :disabled="busy && !saving"
                    @click="saveSettings"
                  >
                    保存翻译配置
                  </a-button>
                  <a-button
                    status="danger"
                    :loading="clearingKey"
                    :disabled="(busy && !clearingKey) || !form.apiKeyConfigured"
                    @click="confirmClearKey"
                  >
                    清除 Key
                  </a-button>
                </a-space>
              </a-col>
            </a-row>
          </a-form>
        </a-card>

        <a-card class="panel" title="翻译任务">
          <template #extra>
            <a-typography-text type="secondary">
              重译会以当前中文修订版为准覆盖对应译文，不会改动中文。
            </a-typography-text>
          </template>

          <a-space wrap>
            <a-button
              type="primary"
              :loading="retranslating === 'all'"
              :disabled="busy && retranslating !== 'all'"
              @click="retranslate('all')"
            >
              重新翻译英语 + 阿拉伯语
            </a-button>
            <a-button
              :loading="retranslating === 'en'"
              :disabled="busy && retranslating !== 'en'"
              @click="retranslate('en')"
            >
              只重译英语
            </a-button>
            <a-button
              :loading="retranslating === 'ar'"
              :disabled="busy && retranslating !== 'ar'"
              @click="retranslate('ar')"
            >
              只重译阿拉伯语
            </a-button>
          </a-space>

          <a-typography-text type="secondary" class="task-hint">
            提交后任务立即进入后台队列，本页每 2 秒自动刷新进度，可以随时离开。
          </a-typography-text>
        </a-card>
      </template>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    fetchTranslationModels,
    fetchTranslations,
    requestRetranslation,
    saveTranslationSettings,
  } from '@/api/translation';
  import type {
    TranslationLocale,
    TranslationProgress,
    TranslationSettings,
    TranslationSettingsPayload,
  } from '@/api/translation';

  interface LocaleCard {
    locale: string;
    name: string;
    revision: number;
    status: string;
    stale: boolean;
    lastError: string;
    updatedAt: string | null;
    progress: TranslationProgress | null;
  }

  const LOCALE_NAMES: Record<string, string> = {
    'zh-CN': '中文',
    en: '英语',
    ar: '阿拉伯语',
  };

  const STATUS_META: Record<string, { label: string; color: string }> = {
    pending: { label: '排队中', color: 'orange' },
    running: { label: '翻译中', color: 'arcoblue' },
    ready: { label: '已同步', color: 'green' },
    failed: { label: '失败', color: 'red' },
    unconfigured: { label: '未配置', color: 'gray' },
  };

  const apiTypeOptions = [
    { label: 'Chat Completions', value: 'chat_completions' },
    { label: 'Responses API', value: 'responses' },
    { label: 'Anthropic Messages', value: 'anthropic_messages' },
  ];

  const overview = ref<any>(null);
  const loadError = ref('');
  const initialLoading = ref(true);
  const loading = ref(false);
  const polling = ref(false);

  const saving = ref(false);
  const fetchingModels = ref(false);
  const clearingKey = ref(false);
  const retranslating = ref<'all' | TranslationLocale | null>(null);
  const busy = computed(
    () =>
      saving.value ||
      fetchingModels.value ||
      clearingKey.value ||
      retranslating.value !== null
  );

  const form = reactive({
    enabled: true,
    apiType: 'chat_completions',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    model: '',
    batchSize: 40,
    timeoutSeconds: 120,
    configuredModels: [] as string[],
    apiKeyConfigured: false,
    apiKeyMasked: '',
  });

  let pollTimer: number | null = null;

  const modelOptions = computed(() => {
    const values = [...new Set([...form.configuredModels, form.model].filter(Boolean))];
    return values.map((value) => ({ label: value, value }));
  });

  // 上游配置完整（Key + 模型）时才可能真正产生翻译任务。
  const upstreamReady = computed(() => Boolean(form.apiKeyConfigured && form.model));

  const localeCards = computed<LocaleCard[]>(() => {
    const data = overview.value;
    if (!data) return [];
    const source = data.source || {};
    const cards: LocaleCard[] = [
      {
        locale: 'zh-CN',
        name: LOCALE_NAMES['zh-CN'],
        revision: Number(source.revision) || 0,
        status: 'ready',
        stale: false,
        lastError: '',
        updatedAt: source.updatedAt || null,
        progress: null,
      },
    ];
    for (const item of Array.isArray(data.translations) ? data.translations : []) {
      cards.push({
        locale: item.locale,
        name: LOCALE_NAMES[item.locale] || item.locale,
        revision: Number(item.sourceRevision) || 0,
        status: item.status || 'pending',
        stale: Boolean(item.stale),
        lastError: item.lastError || '',
        updatedAt: item.updatedAt || null,
        progress: item.progress || null,
      });
    }
    // 后端固定返回 en / ar；此处兜底，确保三语卡片始终齐全。
    for (const locale of ['en', 'ar'] as TranslationLocale[]) {
      if (!cards.some((card) => card.locale === locale)) {
        cards.push({
          locale,
          name: LOCALE_NAMES[locale],
          revision: 0,
          status: 'pending',
          stale: true,
          lastError: '',
          updatedAt: null,
          progress: null,
        });
      }
    }
    return cards;
  });

  function cardStatus(card: LocaleCard): string {
    if (card.locale === 'zh-CN') return 'ready';
    if (card.status === 'pending') {
      // 从未翻译过、没有进度、且上游未配置时，显示“未配置”，而不是无限“排队中”。
      const untouched = !card.updatedAt && !card.lastError && !card.progress;
      return untouched && !upstreamReady.value ? 'unconfigured' : 'pending';
    }
    return card.status || 'unconfigured';
  }

  function statusMeta(card: LocaleCard) {
    const status = cardStatus(card);
    return STATUS_META[status] || { label: status, color: 'gray' };
  }

  function isActive(card: LocaleCard) {
    const status = cardStatus(card);
    return status === 'pending' || status === 'running';
  }

  // a-progress 的 percent 需要 0 - 1 的小数。
  function progressRatio(card: LocaleCard) {
    const total = Number(card.progress?.total) || 0;
    if (!total) return 0;
    const done = Number(card.progress?.done) || 0;
    return Math.min(1, Math.max(0, done / total));
  }

  function progressPercent(card: LocaleCard) {
    return Math.round(progressRatio(card) * 100);
  }

  function localTime(value?: string | null) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  function errorMessage(error: unknown) {
    const failure = error as any;
    return (
      failure?.response?.data?.error?.message ||
      failure?.response?.data?.message ||
      failure?.message ||
      '请求失败'
    );
  }

  function applySettings(settings?: TranslationSettings | null) {
    if (!settings) return;
    form.enabled = settings.enabled !== false;
    form.apiType = settings.apiType || 'chat_completions';
    form.baseUrl = settings.baseUrl || '';
    form.model = settings.model || '';
    form.batchSize = Number(settings.batchSize) || 40;
    form.timeoutSeconds = Math.max(
      10,
      Math.round((Number(settings.timeoutMs) || 120000) / 1000)
    );
    form.configuredModels = Array.isArray(settings.configuredModels)
      ? [...settings.configuredModels]
      : [];
    form.apiKeyConfigured = Boolean(settings.apiKeyConfigured);
    form.apiKeyMasked = settings.apiKeyMasked || '';
  }

  function settingsPayload(
    overrides: { apiKey?: string; clearApiKey?: boolean } = {}
  ): TranslationSettingsPayload {
    const payload: TranslationSettingsPayload = {
      enabled: form.enabled,
      apiType: form.apiType,
      baseUrl: form.baseUrl.trim(),
      apiKey: overrides.apiKey !== undefined ? overrides.apiKey : form.apiKey.trim(),
      model: form.model,
      batchSize: Number(form.batchSize),
      timeoutMs: Math.round(Number(form.timeoutSeconds) * 1000),
      configuredModels: [...new Set([...form.configuredModels, form.model].filter(Boolean))],
    };
    if (overrides.clearApiKey) payload.clearApiKey = true;
    return payload;
  }

  function stopPolling() {
    if (pollTimer !== null) {
      window.clearInterval(pollTimer);
      pollTimer = null;
    }
    polling.value = false;
  }

  // 有 pending / running 任务时每 2 秒轮询，全部结束后自动停止。
  function syncPolling() {
    const active = localeCards.value.some((card) => isActive(card));
    if (active && pollTimer === null) {
      pollTimer = window.setInterval(() => {
        refreshStatus().catch((error) => {
          loadError.value = errorMessage(error);
          stopPolling();
        });
      }, 2000);
      polling.value = true;
      return;
    }
    if (!active && pollTimer !== null) stopPolling();
  }

  async function refreshStatus(options: { syncForm?: boolean } = {}) {
    const payload = await fetchTranslations();
    overview.value = payload;
    if (options.syncForm) applySettings(payload.settings);
    loadError.value = '';
    syncPolling();
  }

  async function refreshAll() {
    loading.value = true;
    try {
      await refreshStatus({ syncForm: true });
    } catch (error) {
      loadError.value = errorMessage(error);
    } finally {
      loading.value = false;
      initialLoading.value = false;
    }
  }

  async function saveSettings() {
    if (saving.value) return;
    if (fetchingModels.value || clearingKey.value || retranslating.value !== null) return;
    if (!form.baseUrl.trim()) {
      Message.warning('请填写上游地址');
      return;
    }
    if (!form.model) {
      Message.warning('请选择或输入翻译模型');
      return;
    }
    const batchSize = Number(form.batchSize);
    if (!Number.isFinite(batchSize) || batchSize < 5 || batchSize > 100) {
      Message.warning('每批文案数需在 5 - 100 之间');
      return;
    }
    const seconds = Number(form.timeoutSeconds);
    if (!Number.isFinite(seconds) || seconds < 10 || seconds > 600) {
      Message.warning('超时需在 10 - 600 秒之间');
      return;
    }

    saving.value = true;
    try {
      const settings = await saveTranslationSettings(settingsPayload());
      if (overview.value) overview.value.settings = settings;
      applySettings(settings);
      form.apiKey = '';
      Message.success('翻译配置已加密保存');
      await refreshStatus();
    } catch {
      /* 错误提示由全局响应拦截器统一给出 */
    } finally {
      saving.value = false;
    }
  }

  async function loadModels() {
    if (fetchingModels.value) return;
    if (saving.value || clearingKey.value || retranslating.value !== null) return;
    fetchingModels.value = true;
    try {
      const result = await fetchTranslationModels(settingsPayload());
      const models = Array.isArray(result?.models) ? result.models : [];
      form.configuredModels = [...new Set([...models, form.model].filter(Boolean))];
      if (!form.model && models.length) form.model = models[0];
      Message.success(`已获取 ${models.length} 个上游模型`);
    } catch {
      /* 错误提示由全局响应拦截器统一给出 */
    } finally {
      fetchingModels.value = false;
    }
  }

  async function clearKey() {
    clearingKey.value = true;
    try {
      const settings = await saveTranslationSettings(
        settingsPayload({ apiKey: '', clearApiKey: true })
      );
      if (overview.value) overview.value.settings = settings;
      applySettings(settings);
      form.apiKey = '';
      Message.success('API Key 已清除');
    } finally {
      clearingKey.value = false;
    }
  }

  function confirmClearKey() {
    Modal.warning({
      title: '清除翻译 API Key',
      content: '确认清除已加密保存的翻译 API Key？清除后需要重新配置才能继续自动翻译。',
      hideCancel: false,
      okText: '确认清除',
      cancelText: '取消',
      okButtonProps: { status: 'danger' },
      onBeforeOk: async () => {
        try {
          await clearKey();
          return true;
        } catch {
          return false;
        }
      },
    });
  }

  async function retranslate(target: 'all' | TranslationLocale) {
    if (retranslating.value) return;
    retranslating.value = target;
    try {
      await requestRetranslation(target === 'all' ? undefined : target);
      const scope = target === 'all' ? '英语 + 阿拉伯语' : LOCALE_NAMES[target];
      Message.success(`翻译任务已进入队列（${scope}）`);
      await refreshStatus();
    } catch {
      /* 错误提示由全局响应拦截器统一给出 */
    } finally {
      retranslating.value = null;
      syncPolling();
    }
  }

  onMounted(refreshAll);
  onUnmounted(stopPolling);
</script>

<style scoped lang="less">
  .translation-page {
    .intro {
      margin-bottom: 16px;
    }

    .skeleton {
      padding: 8px 0 16px;
    }

    .locale-card {
      height: 100%;
    }

    .panel {
      margin-top: 16px;
    }

    .progress {
      margin-bottom: 12px;

      .progress-text {
        margin-top: 4px;
        color: var(--color-text-3);
        font-size: 12px;
      }
    }

    .stale-tip {
      margin-left: 8px;
      color: var(--color-text-3);
      font-size: 12px;
    }

    .card-hint {
      display: block;
      margin-top: 8px;
      font-size: 12px;
    }

    .model-row {
      display: flex;
      align-items: center;
      gap: 8px;

      .model-select {
        flex: 1;
        min-width: 0;
      }
    }

    .task-hint {
      display: block;
      margin-top: 12px;
      font-size: 12px;
    }
  }
</style>
