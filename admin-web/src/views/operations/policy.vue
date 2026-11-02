<template>
  <div class="policy-page" data-testid="policy-page">
    <a-alert class="tips" type="info">
      <template #title>客服策略</template>
      这里的策略对全部客服与访客会话生效。每个卡片是一个独立弹窗，<strong>只保存该弹窗负责的字段</strong>，
      不会互相覆盖；保存后立即下发到客服引擎，无需重启。数字项 0 表示「不限制 / 不启用该判定」。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" data-testid="policy-retry" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-row :gutter="[16, 16]">
        <a-col
          v-for="card in cards"
          :key="card.key"
          :xs="24"
          :md="12"
          :xl="8"
        >
          <a-card
            :bordered="false"
            class="section-card"
            :data-testid="`policy-card-${card.key}`"
          >
            <div class="section-head">
              <div class="section-title">
                <icon-settings class="section-icon" />
                {{ card.title }}
              </div>
              <a-tag v-if="card.tag" size="small" :color="card.tagColor || 'arcoblue'">
                {{ card.tag }}
              </a-tag>
            </div>
            <a-typography-text type="secondary" class="section-desc">
              {{ card.desc }}
            </a-typography-text>
            <div class="section-rows">
              <div v-for="row in card.rows" :key="row.label" class="section-row">
                <span class="row-label">{{ row.label }}</span>
                <span class="row-value">{{ row.value }}</span>
              </div>
            </div>
            <a-button
              type="primary"
              long
              :data-testid="`policy-open-${card.key}`"
              @click="openSection(card.key)"
            >
              {{ card.action || '打开配置' }}
            </a-button>
          </a-card>
        </a-col>
      </a-row>
    </a-spin>

    <!-- ===== 策略弹窗：只提交各自负责的字段 ===== -->
    <a-modal
      v-model:visible="queueVisible"
      title="排队与技能组"
      :ok-loading="saving"
      :mask-closable="false"
      @ok="saveQueue"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="queueDraft" layout="vertical">
        <a-form-item label="启用排队">
          <a-switch
            v-model="queueDraft.queue_enabled"
            data-testid="policy-queue_enabled"
          />
          <template #extra>
            开启后访客会话先进入排队，按技能组与优先级分配客服；关闭则直接按现有逻辑分配。
          </template>
        </a-form-item>
        <a-form-item label="最长等待时间（秒）">
          <a-input-number
            v-model="queueDraft.max_wait_seconds"
            :min="0"
            :precision="0"
            :input-attrs="{ 'data-testid': 'policy-max_wait_seconds' }"
            style="width: 200px"
          />
          <template #extra>
            排队超过该秒数仍未接入客服时，超时转无人值守兜底回复。0 表示不限时。
          </template>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="slaVisible"
      title="SLA 与超时"
      :ok-loading="saving"
      :mask-closable="false"
      @ok="saveSla"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="slaDraft" layout="vertical">
        <a-form-item label="首次回复时限（秒）">
          <a-input-number
            v-model="slaDraft.sla_first_response_seconds"
            :min="0"
            :precision="0"
            :input-attrs="{ 'data-testid': 'policy-sla_first_response_seconds' }"
            style="width: 200px"
          />
          <template #extra>
            客服接入会话后，超过该秒数仍未首次回复即记为超时，用于统计与提醒。0 表示不考核。
          </template>
        </a-form-item>
        <a-form-item label="客服空闲判定（分钟）">
          <a-input-number
            v-model="slaDraft.sla_idle_minutes"
            :min="0"
            :precision="0"
            :input-attrs="{ 'data-testid': 'policy-sla_idle_minutes' }"
            style="width: 200px"
          />
          <template #extra>
            客服连续多少分钟未回复访客即视为空闲，随后按下面的动作处理。0 表示不判定。
          </template>
        </a-form-item>
        <a-form-item label="空闲处理动作">
          <a-radio-group
            v-model="slaDraft.sla_idle_action"
            type="button"
            data-testid="policy-sla_idle_action"
          >
            <a-radio value="ai">交回 AI</a-radio>
            <a-radio value="reassign">转其他客服</a-radio>
          </a-radio-group>
          <template #extra>
            交回 AI：结束人工接待并由 AI 继续应答；转其他客服：重新排队分配给空闲客服。
          </template>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="autoCloseVisible"
      title="超时自动关闭"
      :ok-loading="saving"
      :mask-closable="false"
      @ok="saveAutoClose"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="autoCloseDraft" layout="vertical">
        <a-form-item label="静默自动结束（分钟）">
          <a-input-number
            v-model="autoCloseDraft.auto_close_idle_minutes"
            :min="0"
            :precision="0"
            :input-attrs="{ 'data-testid': 'policy-auto_close_idle_minutes' }"
            style="width: 200px"
          />
          <template #extra>
            会话达到该静默时长且没有新消息后自动结束，包括等待分配的会话。0 表示不自动结束。
          </template>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="priorityVisible"
      title="AI 与关键词优先级"
      :ok-loading="saving"
      :mask-closable="false"
      width="600px"
      @ok="savePriority"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="priorityDraft" layout="vertical">
        <a-form-item label="关键词与 AI 的优先关系">
          <a-radio-group
            v-model="priorityDraft.ai_keyword_priority"
            data-testid="policy-ai_keyword_priority"
          >
            <a-radio value="keyword_first">
              关键词优先（现状）
              <a-typography-text type="secondary" class="option-hint">
                先匹配关键词规则并回复，未命中才交给 AI。
              </a-typography-text>
            </a-radio>
            <a-radio value="ai_first">
              AI 优先
              <a-typography-text type="secondary" class="option-hint">
                先交给 AI 生成回复，只有 AI 不可用或拒答时才用关键词规则。
              </a-typography-text>
            </a-radio>
            <a-radio value="keyword_only">
              仅关键词
              <a-typography-text type="secondary" class="option-hint">
                只按关键词规则回复，完全不调用 AI。
              </a-typography-text>
            </a-radio>
            <a-radio value="ai_only">
              仅 AI
              <a-typography-text type="secondary" class="option-hint">
                全部交给 AI 回复，忽略关键词规则（快捷回复按钮仍可用）。
              </a-typography-text>
            </a-radio>
          </a-radio-group>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="unattendedVisible"
      title="无人值守兜底"
      :ok-loading="saving"
      :mask-closable="false"
      width="600px"
      @ok="saveUnattended"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="unattendedDraft" layout="vertical">
        <a-form-item label="启用无人值守兜底">
          <a-switch
            v-model="unattendedDraft.unattended_enabled"
            data-testid="policy-unattended_enabled"
          />
          <template #extra>
            开启后，排队超时或非营业时间的会话由无人值守话术兜底；关闭则不做任何自动回复。
          </template>
        </a-form-item>
        <a-form-item label="无人值守话术">
          <a-textarea
            v-model="unattendedDraft.unattended_reply"
            :auto-size="{ minRows: 3, maxRows: 6 }"
            :textarea-attrs="{ 'data-testid': 'policy-unattended_reply' }"
            placeholder="例如：您好，客服当前繁忙，请留言，我们会尽快回复您。"
          />
          <template #extra>
            按访客语言自动翻译后下发；每个会话只发一次，避免重复打扰。
          </template>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="takeoverVisible"
      title="接管提示"
      :ok-loading="saving"
      :mask-closable="false"
      @ok="saveTakeover"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="takeoverDraft" layout="vertical">
        <a-form-item label="接管弹窗提示">
          <a-switch
            v-model="takeoverDraft.takeover_popup_enabled"
            data-testid="policy-takeover_popup_enabled"
          />
          <template #extra>
            关闭后客服 App 不再弹接管提示，AI 继续接待该会话（客服仍可在会话列表手动接入）。
          </template>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- ===== 并入客服策略的运营面板（各自维护数据，不再单独占菜单项） ===== -->
    <a-modal
      v-model:visible="keywordsVisible"
      title="关键词规则"
      :footer="false"
      width="960px"
      unmount-on-close
      data-testid="policy-keywords-modal"
    >
      <KeywordRulesPanel
        v-if="keywordsVisible"
        @changed="(total: number) => (keywordCount = total)"
      />
    </a-modal>

    <a-modal
      v-model:visible="blacklistVisible"
      title="黑名单（仅 IP）"
      :footer="false"
      width="860px"
      unmount-on-close
      data-testid="policy-blacklist-modal"
    >
      <BlacklistPanel
        v-if="blacklistVisible"
        @changed="(total: number) => (blacklistCount = total)"
      />
    </a-modal>

    <a-modal
      v-model:visible="queueListVisible"
      title="排队队列"
      :footer="false"
      width="900px"
      unmount-on-close
      data-testid="policy-queue-modal"
    >
      <ServiceQueuePanel v-if="queueListVisible" @navigate="onQueueNavigate" />
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useRouter } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchBlacklist,
    fetchKeywordRules,
    fetchServicePolicy,
    fetchServiceQueue,
    updateServicePolicy,
  } from '@/api/admin';
  import KeywordRulesPanel from './components/KeywordRulesPanel.vue';
  import BlacklistPanel from './components/BlacklistPanel.vue';
  import ServiceQueuePanel from './components/ServiceQueuePanel.vue';

  type SectionKey =
    | 'queue'
    | 'sla'
    | 'autoclose'
    | 'priority'
    | 'unattended'
    | 'takeover'
    | 'keywords'
    | 'blacklist'
    | 'queueList';

  // 与后端冻结契约一一对应：service-policy 的 policy 共 10 个字段。
  const DEFAULT_POLICY = {
    queue_enabled: true,
    max_wait_seconds: 0,
    sla_first_response_seconds: 0,
    sla_idle_minutes: 0,
    sla_idle_action: 'ai',
    auto_close_idle_minutes: 30,
    ai_keyword_priority: 'keyword_first',
    unattended_enabled: true,
    unattended_reply: '',
    takeover_popup_enabled: true,
  };

  const PRIORITY_VALUES = ['keyword_first', 'ai_first', 'keyword_only', 'ai_only'];
  const IDLE_ACTION_VALUES = ['ai', 'reassign'];
  const PRIORITY_LABELS: Record<string, string> = {
    keyword_first: '关键词优先',
    ai_first: 'AI 优先',
    keyword_only: '仅关键词',
    ai_only: '仅 AI',
  };

  const router = useRouter();

  const policy = reactive({ ...DEFAULT_POLICY });
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref('');
  // 保存失败时在弹窗内展示，避免只有一个全局 toast 而看不见失败原因。
  const saveError = ref('');

  const queueVisible = ref(false);
  const slaVisible = ref(false);
  const autoCloseVisible = ref(false);
  const priorityVisible = ref(false);
  const unattendedVisible = ref(false);
  const takeoverVisible = ref(false);
  const keywordsVisible = ref(false);
  const blacklistVisible = ref(false);
  const queueListVisible = ref(false);

  // 各弹窗独立草稿：只提交自己负责的字段（服务端 PATCH 为指针语义的局部更新）。
  const queueDraft = reactive({
    queue_enabled: DEFAULT_POLICY.queue_enabled,
    max_wait_seconds: DEFAULT_POLICY.max_wait_seconds,
  });
  const slaDraft = reactive({
    sla_first_response_seconds: DEFAULT_POLICY.sla_first_response_seconds,
    sla_idle_minutes: DEFAULT_POLICY.sla_idle_minutes,
    sla_idle_action: DEFAULT_POLICY.sla_idle_action,
  });
  const autoCloseDraft = reactive({
    auto_close_idle_minutes: DEFAULT_POLICY.auto_close_idle_minutes,
  });
  const priorityDraft = reactive({
    ai_keyword_priority: DEFAULT_POLICY.ai_keyword_priority,
  });
  const unattendedDraft = reactive({
    unattended_enabled: DEFAULT_POLICY.unattended_enabled,
    unattended_reply: DEFAULT_POLICY.unattended_reply,
  });
  const takeoverDraft = reactive({
    takeover_popup_enabled: DEFAULT_POLICY.takeover_popup_enabled,
  });

  // 概览计数：取不到时显示「-」，不影响策略本身的编辑。
  const keywordCount = ref<number | null>(null);
  const blacklistCount = ref<number | null>(null);
  const queueCount = ref<number | null>(null);
  const queueMaxWait = ref<number | null>(null);

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  // 数字字段统一归一化：空值/null 视为 0，避免提交 null 让后端解析失败。
  function toPolicyNumber(value: unknown): number {
    if (value === '' || value === null || value === undefined) return 0;
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) return 0;
    return Math.max(0, Math.trunc(parsed));
  }

  function applyPolicy(next: any) {
    const source = next && typeof next === 'object' ? next : {};
    policy.queue_enabled =
      typeof source.queue_enabled === 'boolean'
        ? source.queue_enabled
        : DEFAULT_POLICY.queue_enabled;
    policy.max_wait_seconds = toPolicyNumber(source.max_wait_seconds);
    policy.sla_first_response_seconds = toPolicyNumber(
      source.sla_first_response_seconds
    );
    policy.sla_idle_minutes = toPolicyNumber(source.sla_idle_minutes);
    policy.sla_idle_action = IDLE_ACTION_VALUES.includes(source.sla_idle_action)
      ? source.sla_idle_action
      : DEFAULT_POLICY.sla_idle_action;
    policy.auto_close_idle_minutes = toPolicyNumber(source.auto_close_idle_minutes);
    policy.ai_keyword_priority = PRIORITY_VALUES.includes(
      source.ai_keyword_priority
    )
      ? source.ai_keyword_priority
      : DEFAULT_POLICY.ai_keyword_priority;
    policy.unattended_enabled =
      typeof source.unattended_enabled === 'boolean'
        ? source.unattended_enabled
        : DEFAULT_POLICY.unattended_enabled;
    policy.unattended_reply =
      source.unattended_reply === null || source.unattended_reply === undefined
        ? ''
        : String(source.unattended_reply);
    policy.takeover_popup_enabled =
      typeof source.takeover_popup_enabled === 'boolean'
        ? source.takeover_popup_enabled
        : DEFAULT_POLICY.takeover_popup_enabled;
  }

  function onOff(value: boolean, on = '已启用', off = '未启用') {
    return value ? on : off;
  }
  function secondsText(value: number) {
    const seconds = toPolicyNumber(value);
    return seconds ? `${seconds} 秒` : '不限制';
  }
  function minutesText(value: number) {
    const minutes = toPolicyNumber(value);
    return minutes ? `${minutes} 分钟` : '不启用';
  }
  function countText(value: number | null) {
    return value === null ? '-' : `${value} 条`;
  }
  function replySummary(value: string) {
    const raw = String(value ?? '').trim();
    if (!raw) return '未填写';
    return raw.length > 28 ? `${raw.slice(0, 28)}…` : raw;
  }

  const cards = computed(() => [
    {
      key: 'queue' as SectionKey,
      title: '排队与技能组',
      desc: '开启排队后按技能组与优先级分配客服，并限制最长等待时间。',
      rows: [
        { label: '启用排队', value: onOff(policy.queue_enabled) },
        { label: '最长等待', value: secondsText(policy.max_wait_seconds) },
      ],
      action: '配置排队',
    },
    {
      key: 'sla' as SectionKey,
      title: 'SLA 与超时',
      desc: '首响时限与客服空闲判定，用于统计、提醒与自动处置。',
      rows: [
        {
          label: '首响时限',
          value: policy.sla_first_response_seconds
            ? `${toPolicyNumber(policy.sla_first_response_seconds)} 秒`
            : '不考核',
        },
        { label: '空闲判定', value: minutesText(policy.sla_idle_minutes) },
        {
          label: '空闲动作',
          value: policy.sla_idle_action === 'reassign' ? '转其他客服' : '交回 AI',
        },
      ],
      action: '配置 SLA',
    },
    {
      key: 'autoclose' as SectionKey,
      title: '超时自动关闭',
      desc: '双方静默超时后自动结束会话，避免僵尸会话堆积。',
      rows: [
        {
          label: '静默自动结束',
          value: minutesText(policy.auto_close_idle_minutes),
        },
      ],
      action: '配置自动关闭',
    },
    {
      key: 'priority' as SectionKey,
      title: 'AI 与关键词优先级',
      desc: '决定关键词规则与 AI 回复的先后关系。',
      rows: [
        {
          label: '当前优先级',
          value: PRIORITY_LABELS[policy.ai_keyword_priority] || policy.ai_keyword_priority,
        },
      ],
      action: '配置优先级',
    },
    {
      key: 'unattended' as SectionKey,
      title: '无人值守兜底',
      desc: '排队超时或非营业时间时，自动下发兜底话术。',
      rows: [
        { label: '启用兜底', value: onOff(policy.unattended_enabled) },
        { label: '兜底话术', value: replySummary(policy.unattended_reply) },
      ],
      action: '配置兜底',
    },
    {
      key: 'takeover' as SectionKey,
      title: '接管提示',
      desc: '客服 App 是否弹出接管提示，直接关系到响应速度。',
      rows: [
        { label: '接管弹窗', value: onOff(policy.takeover_popup_enabled) },
      ],
      action: '配置接管',
    },
    {
      key: 'keywords' as SectionKey,
      title: '关键词规则',
      desc: '命中关键词后自动回复或转人工，可配快捷回复按钮。',
      tag: countText(keywordCount.value),
      rows: [
        { label: '规则数量', value: countText(keywordCount.value) },
      ],
      action: '管理关键词规则',
    },
    {
      key: 'blacklist' as SectionKey,
      title: '黑名单（仅 IP）',
      desc: '只允许按 IP 封禁；访客 / 账号维度已下线，避免误封正常用户。',
      tag: countText(blacklistCount.value),
      tagColor: 'orange',
      rows: [{ label: 'IP 条目', value: countText(blacklistCount.value) }],
      action: '管理 IP 黑名单',
    },
    {
      key: 'queueList' as SectionKey,
      title: '排队队列',
      desc: '实时查看排队中的会话、等待时长与所属分组。',
      tag: queueCount.value === null ? '-' : `${queueCount.value} 个排队`,
      tagColor: 'green',
      rows: [
        { label: '排队中', value: countText(queueCount.value) },
        {
          label: '最长等待',
          value:
            queueMaxWait.value === null
              ? '-'
              : `${Math.max(0, Math.floor(queueMaxWait.value))} 秒`,
        },
      ],
      action: '查看队列',
    },
  ]);

  function openSection(key: SectionKey) {
    saveError.value = '';
    if (key === 'queue') {
      queueDraft.queue_enabled = policy.queue_enabled;
      queueDraft.max_wait_seconds = toPolicyNumber(policy.max_wait_seconds);
      queueVisible.value = true;
      return;
    }
    if (key === 'sla') {
      slaDraft.sla_first_response_seconds = toPolicyNumber(
        policy.sla_first_response_seconds
      );
      slaDraft.sla_idle_minutes = toPolicyNumber(policy.sla_idle_minutes);
      slaDraft.sla_idle_action = policy.sla_idle_action;
      slaVisible.value = true;
      return;
    }
    if (key === 'autoclose') {
      autoCloseDraft.auto_close_idle_minutes = toPolicyNumber(
        policy.auto_close_idle_minutes
      );
      autoCloseVisible.value = true;
      return;
    }
    if (key === 'priority') {
      priorityDraft.ai_keyword_priority = policy.ai_keyword_priority;
      priorityVisible.value = true;
      return;
    }
    if (key === 'unattended') {
      unattendedDraft.unattended_enabled = policy.unattended_enabled;
      unattendedDraft.unattended_reply = policy.unattended_reply;
      unattendedVisible.value = true;
      return;
    }
    if (key === 'takeover') {
      takeoverDraft.takeover_popup_enabled = policy.takeover_popup_enabled;
      takeoverVisible.value = true;
      return;
    }
    if (key === 'keywords') {
      keywordsVisible.value = true;
      return;
    }
    if (key === 'blacklist') {
      blacklistVisible.value = true;
      return;
    }
    queueListVisible.value = true;
    loadQueueSummary();
  }

  // 独立保存：只提交该弹窗负责的字段；成功后按服务端返回值回填全部概览。
  async function submitSection(
    patch: Record<string, unknown>,
    successText: string,
    close: () => void
  ) {
    saving.value = true;
    saveError.value = '';
    try {
      const result = await updateServicePolicy(patch);
      applyPolicy(result?.policy ?? result);
      Message.success(successText);
      close();
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '保存失败，请稍后重试。');
    } finally {
      saving.value = false;
    }
  }

  function saveQueue() {
    return submitSection(
      {
        queue_enabled: Boolean(queueDraft.queue_enabled),
        max_wait_seconds: toPolicyNumber(queueDraft.max_wait_seconds),
      },
      '排队与技能组已保存',
      () => {
        queueVisible.value = false;
      }
    );
  }

  function saveSla() {
    return submitSection(
      {
        sla_first_response_seconds: toPolicyNumber(
          slaDraft.sla_first_response_seconds
        ),
        sla_idle_minutes: toPolicyNumber(slaDraft.sla_idle_minutes),
        sla_idle_action: slaDraft.sla_idle_action,
      },
      'SLA 与超时已保存',
      () => {
        slaVisible.value = false;
      }
    );
  }

  function saveAutoClose() {
    return submitSection(
      {
        auto_close_idle_minutes: toPolicyNumber(
          autoCloseDraft.auto_close_idle_minutes
        ),
      },
      '超时自动关闭已保存',
      () => {
        autoCloseVisible.value = false;
      }
    );
  }

  function savePriority() {
    return submitSection(
      { ai_keyword_priority: priorityDraft.ai_keyword_priority },
      'AI 与关键词优先级已保存',
      () => {
        priorityVisible.value = false;
      }
    );
  }

  function saveUnattended() {
    return submitSection(
      {
        unattended_enabled: Boolean(unattendedDraft.unattended_enabled),
        unattended_reply: String(unattendedDraft.unattended_reply ?? ''),
      },
      '无人值守兜底已保存',
      () => {
        unattendedVisible.value = false;
      }
    );
  }

  function saveTakeover() {
    return submitSection(
      { takeover_popup_enabled: Boolean(takeoverDraft.takeover_popup_enabled) },
      '接管提示已保存',
      () => {
        takeoverVisible.value = false;
      }
    );
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchServicePolicy();
      applyPolicy(result?.policy ?? result);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '客服策略加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  async function loadQueueSummary() {
    try {
      const result = await fetchServiceQueue();
      const queue = Array.isArray(result?.queue) ? result.queue : [];
      queueCount.value = queue.length;
      queueMaxWait.value = queue.reduce((max: number, item: any) => {
        const base = Number(item?.waiting_seconds ?? item?.wait_seconds);
        return Math.max(max, Number.isFinite(base) ? base : 0);
      }, 0);
    } catch {
      queueCount.value = null;
      queueMaxWait.value = null;
    }
  }

  async function loadCounts() {
    const [keywords, blacklist, queue] = await Promise.allSettled([
      fetchKeywordRules(),
      fetchBlacklist('ip'),
      fetchServiceQueue(),
    ]);
    keywordCount.value =
      keywords.status === 'fulfilled' && Array.isArray(keywords.value?.rules)
        ? keywords.value.rules.length
        : null;
    blacklistCount.value =
      blacklist.status === 'fulfilled' && Array.isArray(blacklist.value?.entries)
        ? blacklist.value.entries.length
        : null;
    if (queue.status === 'fulfilled') {
      const list = Array.isArray(queue.value?.queue) ? queue.value.queue : [];
      queueCount.value = list.length;
      queueMaxWait.value = list.reduce((max: number, item: any) => {
        const base = Number(item?.waiting_seconds ?? item?.wait_seconds);
        return Math.max(max, Number.isFinite(base) ? base : 0);
      }, 0);
    } else {
      queueCount.value = null;
      queueMaxWait.value = null;
    }
  }

  function onQueueNavigate(conversationId: string) {
    queueListVisible.value = false;
    router.push({
      path: '/operations/inquiries',
      query: { conversation: conversationId },
    });
  }

  onMounted(() => {
    load();
    loadCounts();
  });
</script>

<style scoped lang="less">
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 14px;
  }
  .section-card {
    height: 100%;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .section-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 15px;
    font-weight: 600;
  }
  .section-icon {
    color: rgb(var(--primary-6));
  }
  .section-desc {
    display: block;
    min-height: 38px;
    margin: 6px 0 10px;
    font-size: 12px;
    line-height: 18px;
  }
  .section-rows {
    min-height: 62px;
    margin-bottom: 12px;
    padding: 8px 10px;
    background: var(--color-fill-1);
    border-radius: 8px;
  }
  .section-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    line-height: 22px;
  }
  .row-label {
    flex: none;
    color: var(--color-text-3);
    font-size: 12px;
  }
  .row-value {
    min-width: 0;
    font-size: 13px;
    text-align: right;
    word-break: break-all;
  }
  .option-hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
  }
</style>
