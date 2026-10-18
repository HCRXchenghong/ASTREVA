<template>
  <div class="workspace">
    <a-grid :cols="4" :col-gap="12" :row-gap="12" class="cards">
      <a-grid-item v-for="card in cards" :key="card.label">
        <a-card :bordered="false">
          <a-statistic :title="card.label" :value="card.value" :value-style="{ color: card.color }" />
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
            <strong>{{ current ? conversationName(current) : '请选择一条会话' }}</strong>
            <a-space v-if="current && current.status !== 'closed'">
              <a-button size="small" @click="onTransfer">转接</a-button>
              <a-button size="small" status="danger" @click="onClose">结束会话</a-button>
            </a-space>
            <a-tag v-else-if="current" color="gray">已结束</a-tag>
          </div>
          <div ref="messageBox" class="messages">
            <div v-for="message in messages" :key="message.server_msg_id" class="message" :class="message.sender_type">
              <div class="bubble">
                <template v-if="message.message_type === 'image' || message.message_type === 'audio'">
                  <img v-if="message.message_type === 'image'" :src="assetUrl(message.content)" :alt="message.content" class="image" />
                  <audio v-else controls :src="assetUrl(message.content)" />
                </template>
                <template v-else>{{ message.content }}</template>
              </div>
              <small>{{ senderLabel(message.sender_type) }} · {{ localTime(message.created_at) }}</small>
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
  let timer: number | undefined;

  const current = computed(() => conversations.value.find((item) => item.id === currentId.value));
  const filteredConversations = computed(() => {
    const query = keyword.value.trim().toLowerCase();
    if (!query) return conversations.value;
    return conversations.value.filter((item) =>
      [conversationName(item), item.source, item.last_message].join(' ').toLowerCase().includes(query)
    );
  });
  const cards = computed(() => [
    { label: '当前会话', value: dashboard.value.active_conversations || 0, color: '#165dff' },
    { label: '等待人工', value: (dashboard.value.human_requested || 0) + (dashboard.value.waiting || 0), color: '#ff7d00' },
    { label: '在线客服', value: dashboard.value.online_agents || 0, color: '#00b42a' },
    { label: '满意度', value: `${Math.round((dashboard.value.rating?.satisfaction_rate || 0) * 100)}%`, color: '#0fc6c2' },
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
    return value.startsWith('/') ? value : value;
  }

  async function load() {
    const [board, list] = await Promise.all([fetchDashboard(), fetchConversations()]);
    dashboard.value = board;
    conversations.value = list.conversations || [];
    if (!currentId.value && conversations.value.length) {
      currentId.value = conversations.value[0].id;
      await loadMessages();
    }
  }

  async function loadMessages() {
    if (!currentId.value) return;
    const payload = await fetchMessages(currentId.value, 100);
    messages.value = payload.messages || [];
    await nextTick();
    if (messageBox.value) messageBox.value.scrollTop = messageBox.value.scrollHeight;
  }

  async function selectConversation(id: string) {
    currentId.value = id;
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
    margin-bottom: 12px;
    &.agent {
      align-items: flex-end;
    }
    .bubble {
      max-width: 72%;
      padding: 8px 12px;
      border-radius: 10px;
      background: #fff;
      white-space: pre-wrap;
      word-break: break-word;
    }
    &.agent .bubble {
      background: #d6ecff;
    }
    &.system .bubble {
      background: #f2f3f5;
      color: var(--color-text-3);
      font-size: 12px;
    }
    small {
      margin-top: 4px;
      color: var(--color-text-3);
      font-size: 11px;
    }
  }
  .image {
    max-width: 240px;
    border-radius: 8px;
  }
</style>
