<template>
  <div class="workspace">
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

    <a-card :bordered="false" class="board">
      <a-row :gutter="12">
        <a-col :span="8">
          <a-input-search v-model="keyword" placeholder="搜索访客、来源或客服" allow-clear class="search" />
          <a-list :bordered="false" class="conversations">
            <a-list-item
              v-for="item in filteredConversations"
              :key="item.id"
              :class="{ active: item.id === currentId }"
              @click="selectConversation(item.id)"
            >
              <a-list-item-meta :title="conversationName(item)" :description="item.last_message || statusLabel(item.status)">
                <template #avatar>
                  <a-badge :count="item.unread_for_agent || 0" :offset="[-2, 4]">
                    <a-avatar :style="{ backgroundColor: '#165dff' }">
                      {{ conversationName(item).slice(0, 1) }}
                    </a-avatar>
                  </a-badge>
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
                <icon-loading v-if="message.translation_status === 'pending'" class="hint-icon" />
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

    <a-modal v-model:visible="transferVisible" title="转接会话" @ok="confirmTransfer">
      <a-select v-model="transferAgentId" :options="transferOptions" placeholder="选择要转接的客服" />
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    closeConversation,
    fetchAgents,
    fetchConversations,
    fetchDashboard,
    fetchMessages,
    transferConversation,
  } from '@/api/admin';

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
  const filteredConversations = computed(() => {
    const query = keyword.value.trim().toLowerCase();
    if (!query) return conversations.value;
    return conversations.value.filter((item) =>
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
    // 客服引擎上传的相对路径需要经后台同源代理回显；绝对地址直接使用。
    return value.startsWith('/uploads/') ? `/site-admin-api/servicebridge${value}` : value;
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

  onMounted(async () => {
    await load();
    timer = window.setInterval(() => {
      load().catch(() => {});
      loadMessages().catch(() => {});
    }, 10000);
  });
  onUnmounted(() => {
    if (timer) window.clearInterval(timer);
  });
</script>

<style scoped lang="less">
  .cards {
    margin-bottom: 12px;
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
