<template>
  <a-card :bordered="false">
    <template #title>客服账号</template>
    <template #extra>
      <a-button type="primary" @click="openEditor()">新增客服账号</a-button>
    </template>
    <a-table :data="agents" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="账号 / 姓名">
          <template #cell="{ record }">
            <div><strong>{{ record.name }}</strong></div>
            <a-typography-text type="secondary">{{ record.account }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="分组" data-index="group" />
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.disabled_at ? 'red' : record.status === 'online' ? 'green' : 'gray'">
              {{ record.disabled_at ? '已禁用' : statusLabel(record.status) }}
            </a-tag>
          </template>
        </a-table-column>
        <a-table-column title="当前会话">
          <template #cell="{ record }">{{ record.current_conversations }} / {{ record.max_conversations }}</template>
        </a-table-column>
        <a-table-column title="真实好评率">
          <template #cell="{ record }">{{ summaryOf(record.id)?.satisfaction_rate != null ? percent(summaryOf(record.id).satisfaction_rate) : '-' }}</template>
        </a-table-column>
        <a-table-column title="展示好评率">
          <template #cell="{ record }">{{ record.manual_satisfaction_rate != null ? Math.round(record.manual_satisfaction_rate * 100) + '%' : '跟随真实' }}</template>
        </a-table-column>
        <a-table-column title="操作" :width="330">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link @click="openPreferences(record)">翻译偏好</a-link>
              <a-link @click="openAlertChannels(record)">提醒渠道</a-link>
              <a-link @click="openRating(record)">好评率</a-link>
              <a-link @click="openDetail(record)">详情</a-link>
              <a-link @click="onResetPassword(record)">重置密码</a-link>
              <a-link v-if="!record.disabled_at" status="warning" @click="onDisable(record)">禁用</a-link>
              <a-link status="danger" @click="onDelete(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-modal v-model:visible="editorVisible" :title="form.id ? '编辑客服' : '新增客服账号'" @ok="saveAgent">
      <a-form :model="form" layout="vertical">
        <a-form-item label="登录账号" required>
          <a-input v-model="form.account" :disabled="Boolean(form.id)" />
        </a-form-item>
        <a-form-item label="客服姓名" required>
          <a-input v-model="form.name" />
        </a-form-item>
        <a-form-item label="分组">
          <a-input v-model="form.group" />
        </a-form-item>
        <a-form-item label="最大并发会话" required>
          <a-input-number v-model="form.max_conversations" :min="1" />
        </a-form-item>
        <a-form-item v-if="!form.id" label="初始密码（至少 10 位）" required>
          <a-input-password v-model="form.password" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="ratingVisible" title="设置展示好评率" @ok="saveRating">
      <a-form layout="vertical">
        <a-form-item label="展示好评率（0-100）">
          <a-input-number v-model="ratingValue" :min="0" :max="100" />
        </a-form-item>
        <a-space>
          <a-button size="small" @click="clearManualRating">清除手动值</a-button>
          <a-button size="small" status="danger" @click="resetRatings">重置该客服全部评价</a-button>
        </a-space>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="prefVisible"
      :title="`翻译偏好：${prefAgent?.name || ''}`"
      :ok-loading="prefSaving"
      @ok="savePreferences"
    >
      <a-alert class="pref-tip" type="info">
        这些开关决定该客服收发消息时的翻译与 AI 辅助行为；客服本人也可在客服 App 的「我的偏好」里自行修改。
      </a-alert>
      <a-spin :loading="prefLoading" class="pref-body">
        <a-form :model="prefs" layout="vertical">
          <a-form-item label="访客消息自动译成中文">
            <a-switch v-model="prefs.auto_translate_incoming" />
          </a-form-item>
          <a-form-item label="我的消息自动译成访客语言">
            <a-switch v-model="prefs.auto_translate_outgoing" />
          </a-form-item>
          <a-form-item label="发送前确认 / 编辑译文">
            <a-switch v-model="prefs.review_translation_before_send" />
          </a-form-item>
          <a-form-item label="翻译方言模式">
            <a-radio-group v-model="prefs.translation_dialect_mode" type="button">
              <a-radio value="standard">标准语</a-radio>
              <a-radio value="dialect">跟随访客方言</a-radio>
            </a-radio-group>
          </a-form-item>
          <a-form-item label="AI 推荐发送内容">
            <a-switch v-model="prefs.ai_suggest_replies" />
          </a-form-item>
          <a-divider class="pref-divider">AI 知识推荐</a-divider>
          <a-form-item label="启用知识库推荐">
            <a-switch
              v-model="prefs.knowledge_suggest_enabled"
              data-testid="pref-knowledge-suggest"
            />
            <a-typography-text type="secondary" class="pref-hint">
              关闭后该客服不使用 AI 推荐发送内容（含知识库检索命中）。
            </a-typography-text>
          </a-form-item>
          <a-form-item label="可用的知识库">
            <a-select
              v-model="prefs.knowledge_base_ids"
              multiple
              allow-clear
              allow-search
              :options="knowledgeBaseOptions"
              :loading="knowledgeBasesLoading"
              placeholder="留空表示使用全部知识库"
              data-testid="pref-knowledge-bases"
            />
            <a-typography-text type="secondary" class="pref-hint">
              可多选；留空表示该客服可以使用全部知识库。
            </a-typography-text>
          </a-form-item>
        </a-form>
        <a-button size="small" :loading="prefSaving" @click="resetPreferences">
          恢复默认
        </a-button>
      </a-spin>
    </a-modal>

    <a-modal
      v-model:visible="alertVisible"
      :title="`提醒渠道：${alertAgent?.name || ''}`"
      :footer="false"
      width="760px"
    >
      <a-alert class="alert-tip" type="info">
        新会话 / 排队超时 / 被转接等事件会推送到该客服已启用的渠道；每个客服最多 {{ alertMaxChannels }} 个提醒渠道。
      </a-alert>
      <a-alert
        v-if="alertAtCap"
        class="alert-tip"
        type="warning"
        show-icon
        data-testid="alert-cap-tip"
      >
        已达到 {{ alertMaxChannels }} 个上限，请先删除已有渠道后再新增。
      </a-alert>
      <a-spin :loading="alertLoading" style="width: 100%">
        <a-table
          :data="alertChannels"
          :pagination="false"
          row-key="id"
          size="small"
          data-testid="alert-channel-table"
        >
          <template #columns>
            <a-table-column title="类型" :width="110">
              <template #cell="{ record }">
                <a-tag :color="channelColor(record.kind)">{{ channelLabel(record.kind) }}</a-tag>
              </template>
            </a-table-column>
            <a-table-column title="配置（掩码）">
              <template #cell="{ record }">{{ channelMasked(record) }}</template>
            </a-table-column>
            <a-table-column title="启用" :width="90">
              <template #cell="{ record }">
                <a-switch
                  :model-value="Boolean(record.enabled)"
                  :data-testid="`alert-channel-toggle-${record.id}`"
                  @change="(value: boolean | string | number) => toggleAlertChannel(record, Boolean(value))"
                />
              </template>
            </a-table-column>
            <a-table-column title="操作" :width="140">
              <template #cell="{ record }">
                <a-space>
                  <a-link
                    :data-testid="`alert-channel-edit-${record.id}`"
                    @click="startEditAlertChannel(record)"
                  >
                    编辑
                  </a-link>
                  <a-link
                    status="danger"
                    :data-testid="`alert-channel-delete-${record.id}`"
                    @click="removeAlertChannel(record)"
                  >
                    删除
                  </a-link>
                </a-space>
              </template>
            </a-table-column>
          </template>
          <template #empty>
            <a-empty description="还没有提醒渠道" />
          </template>
        </a-table>

        <a-divider class="alert-divider" orientation="left">
          {{ alertEditingId ? '编辑渠道' : '新增渠道' }}
        </a-divider>
        <a-form layout="vertical">
          <a-form-item label="渠道类型">
            <a-select
              v-model="alertDraft.kind"
              :options="channelKindOptions"
              :disabled="Boolean(alertEditingId)"
              data-testid="alert-channel-kind"
              style="width: 220px"
            />
          </a-form-item>
          <a-form-item
            v-for="field in alertFields"
            :key="field.key"
            :label="field.label"
          >
            <a-input-password
              v-if="field.secret"
              v-model="alertConfig[field.key]"
              :placeholder="alertEditingId ? '留空不修改' : field.placeholder"
              :input-attrs="{ 'data-testid': `alert-channel-${field.key}` }"
            />
            <a-input
              v-else
              v-model="alertConfig[field.key]"
              :placeholder="field.placeholder"
              :input-attrs="{ 'data-testid': `alert-channel-${field.key}` }"
            />
          </a-form-item>
          <a-space wrap>
            <a-button
              type="primary"
              :loading="alertSaving"
              :disabled="alertAtCap && !alertEditingId"
              data-testid="alert-channel-add"
              @click="saveAlertChannel"
            >
              {{ alertEditingId ? '保存修改' : '新增渠道' }}
            </a-button>
            <a-button :loading="alertTesting" data-testid="alert-channel-test" @click="testAlertChannelDraft">
              发送测试
            </a-button>
            <a-button v-if="alertEditingId" @click="cancelEditAlertChannel">取消编辑</a-button>
            <a-typography-text v-if="alertAtCap && !alertEditingId" type="secondary">
              已达上限，请先删除后再新增。
            </a-typography-text>
          </a-space>
        </a-form>
      </a-spin>
    </a-modal>

    <a-drawer v-model:visible="detailVisible" :width="520" title="客服详情">
      <a-descriptions :column="1" bordered>
        <a-descriptions-item label="账号">{{ detail?.agent?.account }}</a-descriptions-item>
        <a-descriptions-item label="姓名">{{ detail?.agent?.name }}</a-descriptions-item>
        <a-descriptions-item label="真实好评率">{{ detail?.summary ? percent(detail.summary.satisfaction_rate) : '-' }}</a-descriptions-item>
        <a-descriptions-item label="评价总数">{{ detail?.summary?.total || 0 }}</a-descriptions-item>
      </a-descriptions>
      <a-table :data="detail?.ratings || []" :pagination="false" row-key="id" class="rating-table">
        <template #columns>
          <a-table-column title="评分" data-index="score" />
          <a-table-column title="标签">
            <template #cell="{ record }">{{ (record.tags || []).join('、') || '-' }}</template>
          </a-table-column>
          <a-table-column title="评价" data-index="comment" />
          <a-table-column title="时间">
            <template #cell="{ record }">{{ localTime(record.created_at) }}</template>
          </a-table-column>
        </template>
      </a-table>
    </a-drawer>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createAgent,
    clearAgentManualRating,
    createAgentAlertChannel,
    deleteAgent,
    deleteAgentAlertChannel,
    disableAgent,
    fetchAgentAlertChannels,
    fetchAgentPreferences,
    fetchAgentRatings,
    fetchAgents,
    fetchKnowledgeBases,
    fetchRatingSummaries,
    resetAgentPassword,
    resetAgentRatings,
    setAgentManualRating,
    testAlertChannel,
    updateAgent,
    updateAgentAlertChannel,
    updateAgentPreferences,
  } from '@/api/admin';

  // 偏好默认值：既有 5 项翻译偏好 + 2 项知识推荐偏好（与服务端默认一致）。
  // 原有 5 项保持全开 + 标准语；知识推荐默认开启，知识库留空=全部库。
  const DEFAULT_PREFS = {
    auto_translate_incoming: true,
    auto_translate_outgoing: true,
    review_translation_before_send: true,
    translation_dialect_mode: 'standard',
    ai_suggest_replies: true,
    knowledge_suggest_enabled: true,
    knowledge_base_ids: [] as string[],
  };

  const agents = ref<any[]>([]);
  const loading = ref(false);
  const summaries = ref<Record<string, any>>({});
  const editorVisible = ref(false);
  const ratingVisible = ref(false);
  const detailVisible = ref(false);
  const prefVisible = ref(false);
  const prefLoading = ref(false);
  const prefSaving = ref(false);
  const prefAgent = ref<any>(null);
  const knowledgeBases = ref<any[]>([]);
  const knowledgeBasesLoading = ref(false);
  const ratingValue = ref(100);
  const ratingAgent = ref<any>(null);
  const detail = ref<any>(null);
  const prefs = reactive({ ...DEFAULT_PREFS });
  // 偏好弹窗里的知识库多选项；知识库接口不可用时保持空列表，不影响既有偏好。
  const knowledgeBaseOptions = computed(() =>
    knowledgeBases.value.map((item) => ({ label: item.name, value: item.id }))
  );
  const form = reactive({
    id: '',
    account: '',
    name: '',
    group: '',
    max_conversations: 10,
    password: '',
  });

  function statusLabel(value: string) {
    return { online: '在线', busy: '忙碌', offline: '离线' }[value] || value;
  }
  function percent(value: number) {
    return `${Math.round(Number(value || 0) * 100)}%`;
  }
  function summaryOf(id: string) {
    return summaries.value[id];
  }
  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }

  async function load() {
    loading.value = true;
    try {
      const [agentPayload, summaryPayload] = await Promise.all([fetchAgents(), fetchRatingSummaries()]);
      agents.value = agentPayload.agents || [];
      summaries.value = Object.fromEntries((summaryPayload.items || []).map((item: any) => [item.agent_id, item]));
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    Object.assign(form, {
      id: record?.id || '',
      account: record?.account || '',
      name: record?.name || '',
      group: record?.group || '',
      max_conversations: record?.max_conversations || 10,
      password: '',
    });
    editorVisible.value = true;
  }

  async function saveAgent() {
    if (!form.name.trim()) {
      Message.warning('请填写客服姓名');
      return;
    }
    if (form.id) {
      await updateAgent(form.id, {
        name: form.name,
        group: form.group,
        max_conversations: Number(form.max_conversations),
      });
    } else {
      await createAgent({
        account: form.account,
        name: form.name,
        group: form.group,
        max_conversations: Number(form.max_conversations),
        password: form.password,
      });
    }
    editorVisible.value = false;
    Message.success('已保存');
    await load();
  }

  function openRating(record: any) {
    ratingAgent.value = record;
    const summary = summaryOf(record.id);
    ratingValue.value =
      record.manual_satisfaction_rate != null
        ? Math.round(Number(record.manual_satisfaction_rate) * 100)
        : Math.round(Number(summary?.display_satisfaction_rate ?? summary?.satisfaction_rate ?? 1) * 100);
    ratingVisible.value = true;
  }

  async function saveRating() {
    if (!ratingAgent.value) return;
    await setAgentManualRating(ratingAgent.value.id, Number(ratingValue.value));
    ratingVisible.value = false;
    Message.success('展示好评率已设置');
    await load();
  }

  async function clearManualRating() {
    if (!ratingAgent.value) return;
    await clearAgentManualRating(ratingAgent.value.id);
    ratingVisible.value = false;
    Message.success('已清除手动好评率');
    await load();
  }

  function resetRatings() {
    if (!ratingAgent.value) return;
    Modal.confirm({
      title: '重置评价',
      content: '将清空该客服的全部真实评价与手动好评率，不可恢复。',
      onOk: async () => {
        await resetAgentRatings(ratingAgent.value.id);
        ratingVisible.value = false;
        Message.success('已重置');
        await load();
      },
    });
  }

  async function openDetail(record: any) {
    detail.value = await fetchAgentRatings(record.id);
    detailVisible.value = true;
  }

  function applyPreferences(value: any) {
    Object.assign(prefs, {
      auto_translate_incoming: value?.auto_translate_incoming ?? true,
      auto_translate_outgoing: value?.auto_translate_outgoing ?? true,
      review_translation_before_send:
        value?.review_translation_before_send ?? true,
      translation_dialect_mode:
        value?.translation_dialect_mode === 'dialect' ? 'dialect' : 'standard',
      ai_suggest_replies: value?.ai_suggest_replies ?? true,
      knowledge_suggest_enabled: value?.knowledge_suggest_enabled ?? true,
      knowledge_base_ids: Array.isArray(value?.knowledge_base_ids)
        ? [...value.knowledge_base_ids]
        : [],
    });
  }

  // 知识库列表只用于多选下拉的选项，失败时保持空列表并允许留空=全部库。
  async function loadKnowledgeBases() {
    knowledgeBasesLoading.value = true;
    try {
      const payload = await fetchKnowledgeBases();
      knowledgeBases.value = payload?.bases || [];
    } catch {
      knowledgeBases.value = [];
    } finally {
      knowledgeBasesLoading.value = false;
    }
  }

  async function openPreferences(record: any) {
    prefAgent.value = record;
    applyPreferences(DEFAULT_PREFS);
    prefVisible.value = true;
    prefLoading.value = true;
    loadKnowledgeBases();
    try {
      const payload = await fetchAgentPreferences(record.id);
      applyPreferences(payload?.preferences);
    } catch {
      // 失败提示由 axios 拦截器统一给出，这里保持默认值，避免未捕获的 Promise。
      applyPreferences(DEFAULT_PREFS);
    } finally {
      prefLoading.value = false;
    }
  }

  // 局部 PATCH：这里一次提交全部偏好（既有 5 项 + 知识推荐 2 项），恢复默认也走同一条路径。
  async function persistPreferences() {
    if (!prefAgent.value) return false;
    prefSaving.value = true;
    try {
      const result = await updateAgentPreferences(prefAgent.value.id, {
        auto_translate_incoming: prefs.auto_translate_incoming,
        auto_translate_outgoing: prefs.auto_translate_outgoing,
        review_translation_before_send: prefs.review_translation_before_send,
        translation_dialect_mode: prefs.translation_dialect_mode,
        ai_suggest_replies: prefs.ai_suggest_replies,
        knowledge_suggest_enabled: prefs.knowledge_suggest_enabled,
        knowledge_base_ids: Array.isArray(prefs.knowledge_base_ids)
          ? [...prefs.knowledge_base_ids]
          : [],
      });
      if (result?.preferences) applyPreferences(result.preferences);
      return true;
    } finally {
      prefSaving.value = false;
    }
  }

  async function savePreferences() {
    if (!(await persistPreferences())) return;
    prefVisible.value = false;
    Message.success('翻译偏好已保存');
  }

  async function resetPreferences() {
    applyPreferences(DEFAULT_PREFS);
    if (await persistPreferences()) {
      Message.success('已恢复默认翻译偏好');
    }
  }

  // ---------- 提醒渠道（SMTP / Telegram / Bark / 邮箱） ----------
  // 契约冻结：每客服上限 3 个；渠道 config 字段 telegram→token+chat_id、bark→key+server?、email→address。
  const MAX_ALERT_CHANNELS = 3;
  const CHANNEL_KINDS = [
    { label: 'Telegram', value: 'telegram' },
    { label: 'Bark', value: 'bark' },
    { label: '邮箱', value: 'email' },
  ];
  const CHANNEL_FIELDS: Record<
    string,
    Array<{ key: string; label: string; secret?: boolean; placeholder: string }>
  > = {
    telegram: [
      { key: 'token', label: 'Bot Token', secret: true, placeholder: '例如：123456:ABC-DEF...' },
      { key: 'chat_id', label: 'Chat ID', placeholder: '例如：-1001234567890' },
    ],
    bark: [
      { key: 'key', label: 'Bark Key', secret: true, placeholder: '例如：abcdEFGH1234' },
      { key: 'server', label: '服务器地址（可选）', placeholder: '默认 https://api.day.app' },
    ],
    email: [
      { key: 'address', label: '收件邮箱', placeholder: '例如：agent@example.com' },
    ],
  };
  const REQUIRED_CHANNEL_FIELDS: Record<string, string[]> = {
    telegram: ['token', 'chat_id'],
    bark: ['key'],
    email: ['address'],
  };
  const EMPTY_CHANNEL_CONFIG = {
    token: '',
    chat_id: '',
    key: '',
    server: '',
    address: '',
  };

  const alertVisible = ref(false);
  const alertLoading = ref(false);
  const alertSaving = ref(false);
  const alertTesting = ref(false);
  const alertAgent = ref<any>(null);
  const alertChannels = ref<any[]>([]);
  const alertEditingId = ref('');
  // 上限默认 3，服务端返回 max_channels 时以其为准。
  const alertMaxChannels = ref(MAX_ALERT_CHANNELS);
  const alertDraft = reactive({ kind: 'telegram' });
  const alertConfig = reactive({ ...EMPTY_CHANNEL_CONFIG });
  const alertFields = computed(() => CHANNEL_FIELDS[alertDraft.kind] || []);
  const alertAtCap = computed(
    () => alertChannels.value.length >= alertMaxChannels.value
  );

  function channelLabel(kind: string) {
    return CHANNEL_KINDS.find((item) => item.value === kind)?.label || kind;
  }
  function channelColor(kind: string) {
    return (
      { telegram: 'arcoblue', bark: 'orange', email: 'green' }[kind] || 'gray'
    );
  }
  // 响应可能给 masked 字符串（冻结契约）或 config_masked 映射（当前实现），两种都兼容。
  function channelMasked(record: any) {
    const plain = String(record?.masked ?? '').trim();
    if (plain) return plain;
    const masked = record?.config_masked;
    if (masked && typeof masked === 'object') {
      const parts = Object.entries(masked)
        .filter(([, value]) => String(value ?? '').length)
        .map(([key, value]) => `${key}=${value}`);
      if (parts.length) return parts.join(' · ');
    }
    return '-';
  }
  // 密钥永不回显：编辑时全部字段清空，留空即不修改。
  function resetAlertDraft(kind = 'telegram') {
    alertEditingId.value = '';
    alertDraft.kind = kind;
    Object.assign(alertConfig, EMPTY_CHANNEL_CONFIG);
  }
  function draftConfig() {
    const config: Record<string, string> = {};
    for (const field of alertFields.value) {
      const value = String(alertConfig[field.key] ?? '').trim();
      if (value) config[field.key] = value;
    }
    return config;
  }
  function missingChannelFields() {
    return (REQUIRED_CHANNEL_FIELDS[alertDraft.kind] || []).filter(
      (key) => !String(alertConfig[key] ?? '').trim()
    );
  }

  async function loadAlertChannels() {
    if (!alertAgent.value) return;
    alertLoading.value = true;
    try {
      const payload = await fetchAgentAlertChannels(alertAgent.value.id);
      alertChannels.value = Array.isArray(payload?.channels)
        ? payload.channels
        : [];
      const max = Number(payload?.max_channels);
      if (Number.isFinite(max) && max > 0) {
        alertMaxChannels.value = Math.trunc(max);
      }
    } catch {
      // 失败提示由 axios 拦截器统一给出，这里保持空列表，避免未捕获的 Promise。
      alertChannels.value = [];
    } finally {
      alertLoading.value = false;
    }
  }

  async function openAlertChannels(record: any) {
    alertAgent.value = record;
    alertChannels.value = [];
    resetAlertDraft('telegram');
    alertVisible.value = true;
    await loadAlertChannels();
  }

  async function saveAlertChannel() {
    if (!alertAgent.value) return;
    const config = draftConfig();
    if (alertEditingId.value) {
      if (!Object.keys(config).length) {
        Message.warning('请至少填写一项要修改的配置');
        return;
      }
      alertSaving.value = true;
      try {
        await updateAgentAlertChannel(
          alertAgent.value.id,
          alertEditingId.value,
          { config }
        );
        Message.success('提醒渠道已更新');
        resetAlertDraft();
        await loadAlertChannels();
      } finally {
        alertSaving.value = false;
      }
      return;
    }
    if (alertAtCap.value) {
      Message.warning(
        `每个客服最多 ${alertMaxChannels.value} 个提醒渠道，请先删除后再新增`
      );
      return;
    }
    if (missingChannelFields().length) {
      Message.warning('请填写完整的渠道配置');
      return;
    }
    alertSaving.value = true;
    try {
      await createAgentAlertChannel(alertAgent.value.id, {
        kind: alertDraft.kind,
        config,
      });
      Message.success('提醒渠道已新增');
      resetAlertDraft();
      await loadAlertChannels();
    } finally {
      alertSaving.value = false;
    }
  }

  function startEditAlertChannel(record: any) {
    alertEditingId.value = String(record?.id || '');
    alertDraft.kind = record?.kind || 'telegram';
    Object.assign(alertConfig, EMPTY_CHANNEL_CONFIG);
    Message.info('密钥不会回显，留空表示不修改');
  }

  function cancelEditAlertChannel() {
    resetAlertDraft();
  }

  async function toggleAlertChannel(record: any, enabled: boolean) {
    if (!alertAgent.value) return;
    try {
      await updateAgentAlertChannel(alertAgent.value.id, record.id, {
        enabled,
      });
    } finally {
      // 成功或失败都以服务端状态为准回填开关。
      await loadAlertChannels();
    }
  }

  function removeAlertChannel(record: any) {
    if (!alertAgent.value) return;
    Modal.confirm({
      title: '删除提醒渠道',
      content: `确定删除该 ${channelLabel(record.kind)} 提醒渠道？`,
      onOk: async () => {
        try {
          await deleteAgentAlertChannel(alertAgent.value.id, record.id);
          if (alertEditingId.value === String(record.id)) resetAlertDraft();
          Message.success('提醒渠道已删除');
          await loadAlertChannels();
        } catch {
          // 失败提示由拦截器给出，保持弹窗状态即可。
        }
      },
    });
  }

  async function testAlertChannelDraft() {
    if (missingChannelFields().length) {
      Message.warning('请先填写完整的渠道配置再发送测试');
      return;
    }
    alertTesting.value = true;
    try {
      const result = await testAlertChannel({
        kind: alertDraft.kind,
        config: draftConfig(),
      });
      const text = result?.message || (result?.ok ? '测试消息已发送' : '测试失败');
      if (result?.ok) Message.success(text);
      else Message.error(text);
    } catch {
      // 失败提示由拦截器给出。
    } finally {
      alertTesting.value = false;
    }
  }

  function onResetPassword(record: any) {
    Modal.confirm({
      title: '重置密码',
      content: '留空将自动生成临时密码。',
      onOk: async () => {
        const payload = await resetAgentPassword(record.id, '');
        Message.success(payload.temporary_password ? `临时密码：${payload.temporary_password}` : '密码已重置');
        await load();
      },
    });
  }

  function onDisable(record: any) {
    Modal.confirm({
      title: '禁用客服',
      content: `禁用 ${record.name} 后其登录态立即失效。`,
      onOk: async () => {
        await disableAgent(record.id);
        Message.success('已禁用');
        await load();
      },
    });
  }

  function onDelete(record: any) {
    Modal.confirm({
      title: '删除客服',
      content: `确定永久删除 ${record.name}？`,
      onOk: async () => {
        await deleteAgent(record.id);
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>

<style scoped>
  .rating-table {
    margin-top: 16px;
  }
  .pref-tip {
    margin-bottom: 12px;
  }
  .pref-body {
    display: block;
    min-height: 80px;
  }
  .pref-divider {
    margin: 4px 0 14px;
    font-size: 12px;
    color: var(--color-text-3);
  }
  .pref-hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
  }
  .alert-tip {
    margin-bottom: 12px;
  }
  .alert-divider {
    margin: 16px 0 12px;
    font-size: 12px;
    color: var(--color-text-3);
  }
</style>
