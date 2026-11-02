<template>
  <div class="workspace">
    <!-- 两个来源，两个视图：会话询盘（引擎 conversations）/ 表单线索（官网 site_leads）。 -->
    <a-tabs
      v-model:active-key="activeTab"
      class="workspace-tabs"
      data-testid="inquiries-tabs"
    >
      <a-tab-pane key="conversations" title="会话询盘" />
      <a-tab-pane key="leads" title="表单线索" />
    </a-tabs>

    <template v-if="activeTab === 'conversations'">
    <a-grid :cols="4" :col-gap="12" :row-gap="12" class="cards">
      <a-grid-item v-for="card in cards" :key="card.label">
        <a-card :bordered="false">
          <div class="stat">
            <small>{{ card.label }}</small>
            <strong :style="{ color: card.color }">
              {{ card.value }}<span v-if="card.suffix">{{ card.suffix }}</span>
            </strong>
          </div>
        </a-card>
      </a-grid-item>
    </a-grid>

    <!-- T1：策略告警提示条；点击在「全部会话 / 仅告警会话」之间切换。 -->
    <a-alert
      v-if="alerts.length"
      class="alert-banner"
      :class="{ 'alert-banner-active': onlyAlerted }"
      type="warning"
      data-testid="policy-alert-banner"
      @click="toggleAlertFilter"
    >
      <span class="alert-banner-text">当前有 {{ takeoverAlertCount }} 个待接管 / {{ timeoutAlertCount }} 个超时</span>
      <a-tag size="small" class="alert-banner-tag">{{ onlyAlerted ? '取消筛选' : '仅看告警会话' }}</a-tag>
    </a-alert>

    <a-card :bordered="false" class="board">
      <a-row :gutter="12">
        <a-col :span="8">
          <a-input-search v-model="keyword" placeholder="搜索访客、来源或客服" allow-clear class="search" />
          <a-typography-text type="secondary" class="profile-hint">
            右键或双击会话，可查看该访客画像。
          </a-typography-text>
          <a-list :bordered="false" class="conversations">
            <a-list-item
              v-for="item in filteredConversations"
              :key="item.id"
              :class="{ active: item.id === currentId }"
              @click="selectConversation(item.id)"
              @dblclick="openVisitorProfile(item)"
              @contextmenu.prevent="openVisitorProfile(item)"
            >
              <a-list-item-meta :description="item.last_message || statusLabel(item.status)">
                <template #avatar>
                  <a-badge :count="item.unread_for_agent || 0" :offset="[-2, 4]">
                    <a-avatar :style="{ backgroundColor: '#165dff' }">
                      {{ conversationName(item).slice(0, 1) }}
                    </a-avatar>
                  </a-badge>
                </template>
                <template #title>
                  <span class="conversation-title">
                    <span class="conversation-name">{{ conversationName(item) }}</span>
                    <!-- T1：待接管 / 排队 / SLA 告警徽标。 -->
                    <a-tag
                      v-for="alert in alertsFor(item.id)"
                      :key="alertKey(alert)"
                      size="small"
                      class="policy-alert-tag"
                      :color="alertColor(alert)"
                      :data-alert-kind="alert.kind"
                    >
                      {{ alertLabel(alert) }}
                    </a-tag>
                  </span>
                </template>
              </a-list-item-meta>
              <template #actions>
                <a-tag size="small" :color="statusColor(item.status)">{{ statusLabel(item.status) }}</a-tag>
              </template>
            </a-list-item>
          </a-list>
        </a-col>
        <a-col :span="16">
          <div class="chat-head">
            <div class="chat-head-main">
              <strong>{{ current ? conversationName(current) : '请选择一条会话' }}</strong>
              <!-- T1：当前打开的会话若出现在告警里，头部同样显示对应徽标。 -->
              <a-tag
                v-for="alert in alertsFor(current?.id)"
                :key="`head-${alertKey(alert)}`"
                size="small"
                class="policy-alert-tag"
                :color="alertColor(alert)"
                :data-alert-kind="alert.kind"
              >
                {{ alertLabel(alert) }}
              </a-tag>
              <!-- T4：客户显式要求「请用某语言回答」，整个会话持续生效；本轮只读展示。 -->
              <a-tooltip v-if="replyLangOverrideName" content="该要求对整个会话持续生效">
                <a-tag class="reply-lang-tag" color="arcoblue">
                  客户要求用{{ replyLangOverrideName }}回复
                </a-tag>
              </a-tooltip>
            </div>
            <a-space v-if="current && current.status !== 'closed'">
              <a-button size="small" @click="onTransfer">转接</a-button>
              <a-button size="small" status="danger" @click="onClose">结束会话</a-button>
            </a-space>
            <a-tag v-else-if="current" color="gray">已结束</a-tag>
          </div>
          <div ref="messageBox" class="messages">
            <div v-if="hasMore" class="load-older">
              <a-link @click="loadOlder">加载更早消息</a-link>
            </div>
            <div v-for="message in messages" :key="message.server_msg_id" class="message" :class="message.sender_type">
              <div class="bubble" :class="{ 'bubble-original': isShowingOriginal(message) }">
                <!-- T2：访客消息的语种标签（气泡右上角）。 -->
                <div v-if="messageLangName(message)" class="bubble-corner">
                  <a-tag size="small" class="lang-tag">{{ messageLangName(message) }}</a-tag>
                </div>
                <div class="bubble-body" dir="auto">
                  <template v-if="message.message_type === 'image' || message.message_type === 'audio'">
                    <img v-if="message.message_type === 'image'" :src="assetUrl(message.content)" :alt="message.content" class="image" />
                    <audio v-else controls :src="assetUrl(message.content)" />
                  </template>
                  <template v-else>{{ displayContent(message) }}</template>
                </div>
              </div>
              <div class="message-meta">
                <small>{{ senderLabel(message.sender_type) }} · {{ localTime(message.created_at) }}</small>
                <!-- T1：逐条独立的「原文 / 译文」开关，撤回消息不显示。 -->
                <a-button
                  v-if="canToggleOriginal(message)"
                  size="mini"
                  type="text"
                  class="original-toggle"
                  @click="toggleOriginal(message)"
                >
                  {{ isShowingOriginal(message) ? '译文' : '原文' }}
                </a-button>
              </div>
              <!-- T3：客服自己发出的消息的翻译状态。 -->
              <div v-if="translationHint(message)" class="translation-hint" :class="`translation-${message.translation_status}`">
                <template v-if="message.translation_status === 'pending'">
                  <icon-loading class="hint-icon" />
                  <span>{{ translationHint(message) }}</span>
                </template>
                <a-tag v-else-if="message.translation_status === 'failed'" size="small" color="orange">
                  {{ translationHint(message) }}
                </a-tag>
                <span v-else>{{ translationHint(message) }}</span>
              </div>
            </div>
            <a-empty v-if="!messages.length" description="暂无消息" />
          </div>
        </a-col>
      </a-row>
    </a-card>
    </template>

    <!-- 表单线索：保留原有功能与数据，只是并入询盘管理，不再单独占菜单项。 -->
    <LeadsPanel v-if="activeTab === 'leads'" />

    <a-modal v-model:visible="transferVisible" title="转接会话" @ok="confirmTransfer">
      <a-select v-model="transferAgentId" :options="transferOptions" placeholder="选择要转接的客服" />
    </a-modal>

    <!-- 右键 / 双击会话 → 小窗查看该访客画像。 -->
    <VisitorProfilePopup
      v-model:visible="profileVisible"
      :conversation="profileConversation"
    />
  </div>
</template>

<script lang="ts" setup>
  import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue';
  import { useRoute } from 'vue-router';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    closeConversation,
    fetchAgents,
    fetchConversations,
    fetchDashboard,
    fetchMessages,
    fetchPolicyAlerts,
    transferConversation,
  } from '@/api/admin';
  import LeadsPanel from './components/LeadsPanel.vue';
  import VisitorProfilePopup from './components/VisitorProfilePopup.vue';

  // 两个视图共用一条路由：会话询盘（引擎会话）/ 表单线索（官网表单）。
  const activeTab = ref<'conversations' | 'leads'>('conversations');
  const profileVisible = ref(false);
  const profileConversation = ref<any>(null);

  const dashboard = ref<any>({});
  const conversations = ref<any[]>([]);
  const agents = ref<any[]>([]);
  const messages = ref<any[]>([]);
  const currentId = ref('');
  const keyword = ref('');
  const messageBox = ref<HTMLElement>();
  const transferVisible = ref(false);
  const transferAgentId = ref('');
  const transferOptions = ref<Array<{ label: string; value: string }>>([]);
  const hasMore = ref(false);
  const nextBefore = ref('');
  // T1（询盘告警）：接管 / SLA 告警由既有 10s 定时器一并轮询，不再新增独立定时器。
  const alerts = ref<any[]>([]);
  const onlyAlerted = ref(false);
  // T1：原文/译文切换按消息独立记录，key 为 server_msg_id，刷新消息列表不丢状态。
  const originalShownIds = ref<Set<string>>(new Set());
  let timer: number | undefined;

  // 语种基础码 → 中文名（T2/T4 共用）；未知码原样大写展示。
  const LANGUAGE_NAMES: Record<string, string> = {
    zh: '中文',
    en: '英语',
    ar: '阿拉伯语',
    es: '西班牙语',
    fr: '法语',
    de: '德语',
    pt: '葡萄牙语',
    it: '意大利语',
    nl: '荷兰语',
    id: '印尼语',
    vi: '越南语',
    tr: '土耳其语',
    pl: '波兰语',
    ro: '罗马尼亚语',
    ru: '俄语',
    ja: '日语',
    ko: '韩语',
    th: '泰语',
    he: '希伯来语',
    hi: '印地语',
    el: '希腊语',
  };

  function languageName(code?: string) {
    const raw = String(code || '').trim().toLowerCase();
    if (!raw) return '';
    // 兼容 es-MX / es_MX 这类带地区的取值，只按基础码查表。
    const base = raw.split(/[-_]/)[0];
    return LANGUAGE_NAMES[base] || base.toUpperCase();
  }

  const current = computed(() => conversations.value.find((item) => item.id === currentId.value));
  // T1：告警按 conversation_id 归并；顶部提示条只统计两类（待接管 / 超时）。
  const alertConversationIds = computed(
    () => new Set(alerts.value.map((item) => String(item?.conversation_id || '')).filter(Boolean))
  );
  const takeoverAlertCount = computed(() => alerts.value.filter((item) => item?.kind === 'takeover').length);
  const timeoutAlertCount = computed(
    () => alerts.value.filter((item) => item?.kind === 'sla_first_response' || item?.kind === 'sla_idle').length
  );
  const filteredConversations = computed(() => {
    const query = keyword.value.trim().toLowerCase();
    const scoped = onlyAlerted.value
      ? conversations.value.filter((item) => alertConversationIds.value.has(String(item.id)))
      : conversations.value;
    if (!query) return scoped;
    return scoped.filter((item) =>
      [conversationName(item), item.source, item.last_message].join(' ').toLowerCase().includes(query)
    );
  });
  const cards = computed(() => [
    { label: '当前会话', value: Number(dashboard.value.active_conversations || 0), color: '#165dff', suffix: '' },
    {
      label: '等待人工',
      value: Number(dashboard.value.human_requested || 0) + Number(dashboard.value.waiting || 0),
      color: '#ff7d00',
      suffix: '',
    },
    { label: '在线客服', value: Number(dashboard.value.online_agents || 0), color: '#00b42a', suffix: '' },
    {
      label: '满意度',
      value: Math.round(Number(dashboard.value.rating?.satisfaction_rate || 0) * 100),
      color: '#0fc6c2',
      suffix: '%',
    },
  ]);

  function conversationName(item: any) {
    return item?.visitor_remark || item?.visitor_ip || item?.id || '未知访客';
  }
  function statusLabel(value: string) {
    return (
      {
        online: '在线',
        busy: '忙碌',
        offline: '离线',
        waiting: '待接入',
        ai_serving: 'AI 接待',
        human_requested: '等待人工',
        assigned: '人工服务',
        closed: '已结束',
      }[value] || value || '-'
    );
  }
  function statusColor(value: string) {
    return (
      { waiting: 'orange', human_requested: 'orangered', assigned: 'green', ai_serving: 'arcoblue', closed: 'gray' }[value] || 'gray'
    );
  }
  function senderLabel(value: string) {
    return { visitor: '访客', agent: '人工客服', ai: 'AI', system: '系统' }[value] || value;
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }
  function assetUrl(value: string) {
    if (!value) return '';
    // 只回显客服引擎托管的上传路径；不再直接加载访客给出的任意外部 URL。
    return value.startsWith('/uploads/') ? `/site-admin-api/servicebridge${value}` : '';
  }

  // ---------- T1：策略告警（接管 / 排队 / SLA） ----------
  function alertsFor(conversationId?: string) {
    const id = String(conversationId || '');
    if (!id) return [] as any[];
    return alerts.value.filter((item) => String(item?.conversation_id || '') === id);
  }

  function alertMinutes(value: any) {
    const minutes = Math.round(Number(value));
    return Number.isFinite(minutes) && minutes > 0 ? String(minutes) : '0';
  }

  function alertLabel(alert: any) {
    const kind = String(alert?.kind || '');
    if (kind === 'takeover') {
      const position = Number(alert?.queue_position);
      return Number.isFinite(position) && position > 0 ? `排队中 第 ${position} 位` : '待接管';
    }
    if (kind === 'sla_first_response') return `首响超时 ${alertMinutes(alert?.minutes)} 分钟`;
    if (kind === 'sla_idle') return `待回复超时 ${alertMinutes(alert?.minutes)} 分钟`;
    return '';
  }

  // 首响超时=红色，接管/待回复超时=橙色。
  function alertColor(alert: any) {
    return String(alert?.kind || '') === 'sla_first_response' ? 'red' : 'orange';
  }

  function alertKey(alert: any) {
    return [alert?.kind, alert?.conversation_id, alert?.minutes, alert?.queue_position].join('-');
  }

  function toggleAlertFilter() {
    onlyAlerted.value = !onlyAlerted.value;
  }

  function pageHidden() {
    if (typeof document === 'undefined') return false;
    return document.hidden || document.visibilityState === 'hidden';
  }

  async function loadAlerts() {
    try {
      const payload = await fetchPolicyAlerts();
      const list = Array.isArray(payload?.alerts) ? payload.alerts : [];
      alerts.value = list;
      // 空数组时清空全部徽标；筛选态下无告警可看时自动退出筛选，避免列表空白。
      if (!list.length) onlyAlerted.value = false;
    } catch {
      // 静默降级：告警接口失败不弹错、不阻塞既有会话刷新，保留上一次徽标。
    }
  }

  // 页面隐藏时整体暂停轮询（会话 + 消息 + 告警共用一个 10s 定时器）；
  // 切到「表单线索」视图时也不轮询会话数据。
  function refreshTick() {
    if (pageHidden() || activeTab.value !== 'conversations') return;
    load().catch(() => {});
    loadMessages().catch(() => {});
    loadAlerts();
  }

  function onVisibilityChange() {
    if (!pageHidden()) refreshTick();
  }

  function isRevoked(message: any) {
    return message?.message_type === 'revoked';
  }

  // T2：只有访客消息带语种标签；撤回消息不再提示语种。
  function messageLangName(message: any) {
    if (!message || isRevoked(message) || message.sender_type !== 'visitor') return '';
    return languageName(message.detected_lang);
  }

  // T1：仅访客消息、且确实发生了翻译（content 被替换成中文译文）时才有原文可看。
  function canToggleOriginal(message: any) {
    if (!message || isRevoked(message) || message.sender_type !== 'visitor') return false;
    if (message.message_type === 'image' || message.message_type === 'audio') return false;
    const original = String(message.original_content || '').trim();
    return original !== '' && original !== String(message.content || '').trim();
  }

  function isShowingOriginal(message: any) {
    return Boolean(message?.server_msg_id) && originalShownIds.value.has(message.server_msg_id);
  }

  function toggleOriginal(message: any) {
    const id = message?.server_msg_id;
    if (!id || !canToggleOriginal(message)) return;
    const next = new Set(originalShownIds.value);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    originalShownIds.value = next;
  }

  function displayContent(message: any) {
    if (isShowingOriginal(message) && message.original_content) return message.original_content;
    return message.content;
  }

  // T3：客服自己发出的消息的翻译状态文案（skipped 不展示）。
  function translationHint(message: any) {
    if (!message || isRevoked(message) || message.sender_type !== 'agent') return '';
    return (
      {
        pending: '翻译中…',
        failed: '翻译失败（已发送原文）',
        done: '已翻译',
      }[message.translation_status as string] || ''
    );
  }

  // T4：会话级「请用某语言回答我」要求，只读展示。
  const replyLangOverrideName = computed(() => languageName(current.value?.reply_lang_override));

  async function load() {
    const [board, list] = await Promise.all([fetchDashboard(), fetchConversations()]);
    dashboard.value = board;
    conversations.value = list.conversations || [];
    if (!currentId.value && conversations.value.length) {
      currentId.value = conversations.value[0].id;
      originalShownIds.value = new Set();
      await loadMessages();
    }
  }

  async function loadMessages() {
    if (!currentId.value) return;
    const payload = await fetchMessages(currentId.value, 100);
    messages.value = payload.messages || [];
    hasMore.value = Boolean(payload.has_more);
    nextBefore.value = payload.next_before || '';
    await nextTick();
    if (messageBox.value) messageBox.value.scrollTop = messageBox.value.scrollHeight;
  }

  async function loadOlder() {
    if (!currentId.value || !hasMore.value || !nextBefore.value) return;
    const payload = await fetchMessages(currentId.value, 100, nextBefore.value);
    messages.value = [...(payload.messages || []), ...messages.value];
    hasMore.value = Boolean(payload.has_more);
    nextBefore.value = payload.next_before || '';
  }

  async function selectConversation(id: string) {
    currentId.value = id;
    // 原文/译文开关按会话隔离，切会话时重置（不影响其它会话的即时刷新）。
    originalShownIds.value = new Set();
    await loadMessages();
  }

  // 右键 / 双击一行 → 打开该访客画像小窗；画像关联键由弹窗内部按后端 ProfileKey 规则取。
  function openVisitorProfile(item: any) {
    if (!item) return;
    profileConversation.value = item;
    profileVisible.value = true;
  }

  async function onClose() {
    if (!currentId.value) return;
    Modal.confirm({
      title: '结束会话',
      content: '结束后访客不能再发送消息，确定继续吗？',
      onOk: async () => {
        await closeConversation(currentId.value);
        Message.success('会话已结束');
        await load();
        await loadMessages();
      },
    });
  }

  async function onTransfer() {
    if (!currentId.value) return;
    if (!agents.value.length) agents.value = (await fetchAgents()).agents || [];
    transferOptions.value = agents.value
      .filter((agent) => !agent.disabled_at)
      .map((agent) => ({ label: `${agent.name}（${agent.account}）`, value: agent.id }));
    if (!transferOptions.value.length) {
      Message.warning('没有可转接的客服账号');
      return;
    }
    transferAgentId.value = transferOptions.value[0].value;
    transferVisible.value = true;
  }

  async function confirmTransfer() {
    if (!currentId.value || !transferAgentId.value) return;
    await transferConversation(currentId.value, transferAgentId.value);
    transferVisible.value = false;
    Message.success('会话已转接');
    await load();
  }

  const route = useRoute();

  onMounted(async () => {
    // 深链：/operations/inquiries?tab=leads 直接落到表单线索；默认会话询盘。
    const tab = typeof route.query.tab === 'string' ? route.query.tab.trim() : '';
    if (tab === 'leads') activeTab.value = 'leads';
    await load();
    await loadAlerts();
    // 支持从「排队队列」等页面深链：/operations/inquiries?conversation=<id>
    const deepLinkId = typeof route.query.conversation === 'string' ? route.query.conversation.trim() : '';
    if (deepLinkId) {
      activeTab.value = 'conversations';
      await selectConversation(deepLinkId).catch(() => {
        Message.warning('未能加载指定会话，可能已结束或无权访问');
      });
    }
    timer = window.setInterval(refreshTick, 10000);
    // 页面重新可见时立即补一次刷新（隐藏期间定时器整体跳过）。
    document.addEventListener('visibilitychange', onVisibilityChange);
  });
  onUnmounted(() => {
    if (timer) window.clearInterval(timer);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  });
</script>

<style scoped lang="less">
  .cards {
    margin-bottom: 12px;
  }
  .workspace-tabs {
    margin-bottom: 4px;
  }
  .profile-hint {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    small {
      color: var(--color-text-3);
      font-size: 13px;
    }
    strong {
      font-size: 26px;
      font-weight: 700;
      span {
        margin-left: 2px;
        font-size: 16px;
      }
    }
  }
  .board {
    min-height: 560px;
  }
  // T1：顶部告警提示条，整条可点击切换「仅看告警会话」。
  .alert-banner {
    margin-bottom: 12px;
    cursor: pointer;
  }
  .alert-banner-active {
    outline: 1px solid rgb(var(--warning-6));
  }
  .alert-banner-text {
    font-weight: 600;
  }
  .alert-banner-tag {
    margin-left: 8px;
  }
  .conversation-title {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
  }
  .policy-alert-tag {
    flex: none;
  }
  .search {
    margin-bottom: 8px;
  }
  .conversations {
    max-height: 560px;
    overflow: auto;
    :deep(.arco-list-item) {
      cursor: pointer;
      padding: 10px 8px;
      border-radius: 8px;
    }
    :deep(.arco-list-item.active) {
      background: var(--color-fill-2);
    }
  }
  .chat-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 10px;
  }
  .chat-head-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .reply-lang-tag {
    cursor: default;
  }
  .load-older {
    margin-bottom: 8px;
    text-align: center;
  }
  .messages {
    height: 500px;
    overflow: auto;
    padding: 8px;
    background: var(--color-fill-1);
    border-radius: 8px;
  }
  .message {
    display: flex;
    flex-direction: column;
    margin-bottom: 12px;    &.agent {
      align-items: flex-end;
    }
    .bubble {
      display: flex;
      flex-direction: column;
      max-width: 72%;
      min-width: 0;
      padding: 8px 12px;
      border-radius: 10px;
      background: #fff;
      // 译文态加一道浅边，与原文态（灰底虚线）轻区分。
      border: 1px solid #eceef2;
      white-space: pre-wrap;
      word-break: break-word;
      overflow-wrap: anywhere;
    }
    .bubble-body {
      min-width: 0;
      white-space: pre-wrap;
      word-break: break-word;
      overflow-wrap: anywhere;
    }
    .bubble-corner {
      display: flex;
      justify-content: flex-end;
      margin-bottom: 4px;
    }
    .lang-tag {
      background: var(--color-fill-2);
      color: var(--color-text-3);
      font-size: 11px;
      line-height: 18px;
    }
    &.agent .bubble {
      background: #d6ecff;
      border-color: #c2e0fb;
    }
    &.system .bubble {
      background: #f2f3f5;
      border-color: #e9eaec;
      color: var(--color-text-3);
      font-size: 12px;
    }
    // 原文态：略灰背景 + 虚线边框，一眼区分「这是原文」。
    &.visitor .bubble-original {
      background: #f7f8fa;
      border: 1px dashed #d7dae0;
    }
    .message-meta {
      display: flex;
      align-items: center;
      gap: 6px;
      margin-top: 4px;
    }
    .original-toggle {
      height: 20px;
      padding: 0 4px;
      color: rgb(var(--primary-6));
      font-size: 11px;
    }
    .translation-hint {
      display: flex;
      align-items: center;
      gap: 4px;
      margin-top: 2px;
      color: var(--color-text-3);
      font-size: 11px;
      .hint-icon {
        font-size: 12px;
      }
      &.translation-done {
        color: rgb(var(--success-6));
      }
    }
    small {
      margin-top: 0;
      color: var(--color-text-3);
      font-size: 11px;
    }
  }
  .image {
    max-width: 240px;
    border-radius: 8px;
  }
</style>
