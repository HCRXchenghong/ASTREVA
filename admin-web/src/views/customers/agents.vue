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

    <!-- 提醒渠道：列表弹窗 + 新增/编辑弹窗（一个客服可混合配置 1–3 个渠道）。 -->
    <a-modal
      v-model:visible="alertVisible"
      :title="`提醒渠道：${alertAgent?.name || ''}`"
      :footer="false"
      width="780px"
      unmount-on-close
      data-testid="alert-channel-modal"
    >
      <a-alert class="alert-tip" type="info">
        新会话 / 排队超时 / 被转接等事件会推送到该客服已启用的渠道；一个客服最多
        {{ alertMaxChannels }} 个，类型可混合（Telegram / Bark / 邮箱）。
      </a-alert>

      <div class="alert-toolbar">
        <a-typography-text type="secondary" data-testid="alert-channel-count">
          已配置 {{ alertChannels.length }} / {{ alertMaxChannels }}
        </a-typography-text>
        <a-button
          type="primary"
          :disabled="alertAtCap"
          data-testid="alert-channel-new"
          @click="startCreateAlertChannel"
        >
          新增渠道
        </a-button>
      </div>

      <a-alert
        v-if="alertAtCap"
        class="alert-tip"
        type="warning"
        show-icon
        data-testid="alert-cap-tip"
      >
        已达到 {{ alertMaxChannels }} 个上限，请先删除已有渠道后再新增。
      </a-alert>

      <a-alert v-if="alertListError" class="alert-tip" type="error" show-icon>
        {{ alertListError }}
        <template #action>
          <a-button size="mini" data-testid="alert-channel-reload" @click="loadAlertChannels">
            重试
          </a-button>
        </template>
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
            <a-table-column title="操作" :width="200">
              <template #cell="{ record }">
                <a-space>
                  <a-link
                    :data-testid="`alert-channel-edit-${record.id}`"
                    @click="startEditAlertChannel(record)"
                  >
                    编辑
                  </a-link>
                  <a-link
                    :loading="alertTestingId === String(record.id)"
                    :data-testid="`alert-channel-test-${record.id}`"
                    @click="testSavedChannel(record)"
                  >
                    发送测试
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
      </a-spin>
    </a-modal>

    <a-modal
      v-model:visible="alertEditorVisible"
      :title="alertEditingId ? '编辑提醒渠道' : '新增提醒渠道'"
      :mask-closable="false"
      width="560px"
      unmount-on-close
      data-testid="alert-channel-editor"
    >
      <a-alert v-if="alertSaveError" class="alert-tip" type="error" show-icon>
        {{ alertSaveError }}
      </a-alert>
      <a-alert
        v-if="alertTestResult"
        class="alert-tip"
        :type="alertTestResult.ok ? 'success' : 'error'"
        show-icon
        data-testid="alert-test-result"
      >
        {{ alertTestResult.text }}
      </a-alert>

      <a-form layout="vertical">
        <a-form-item label="渠道类型">
          <a-select
            v-model="alertDraft.kind"
            :options="channelKindOptions"
            :disabled="Boolean(alertEditingId)"
            data-testid="alert-channel-kind"
            style="width: 220px"
          />
          <a-typography-text v-if="alertEditingId" type="secondary" class="alert-hint">
            渠道类型创建后不可更改，需要换类型请删除后重建。
          </a-typography-text>
        </a-form-item>
        <a-form-item
          v-for="field in alertFields"
          :key="field.key"
          :label="field.label"
        >
          <!-- 能枚举的字段一律用下拉（允许自定义输入），避免手填不该手填的内容。 -->
          <a-select
            v-if="field.options"
            v-model="alertConfig[field.key]"
            :options="field.options"
            allow-create
            allow-search
            :placeholder="field.placeholder"
            :data-testid="`alert-channel-${field.key}`"
          />
          <a-input-password
            v-else-if="field.secret"
            v-model="alertConfig[field.key]"
            :placeholder="alertEditingId ? '留空不修改（已保存的值不回显）' : field.placeholder"
            :input-attrs="{ 'data-testid': `alert-channel-${field.key}` }"
          />
          <a-input
            v-else
            v-model="alertConfig[field.key]"
            :placeholder="field.placeholder"
            :input-attrs="{ 'data-testid': `alert-channel-${field.key}` }"
          />
          <a-typography-text v-if="field.hint" type="secondary" class="alert-hint">
            {{ field.hint }}
          </a-typography-text>
        </a-form-item>
      </a-form>

      <template #footer>
        <a-space>
          <a-button
            :loading="alertTesting"
            data-testid="alert-channel-test"
            @click="testAlertEditorDraft"
          >
            发送测试
          </a-button>
          <a-button @click="closeAlertEditor">取消</a-button>
          <a-button
            type="primary"
            :loading="alertSaving"
            data-testid="alert-channel-add"
            @click="saveAlertChannel"
          >
            {{ alertEditingId ? '保存修改' : '新增渠道' }}
          </a-button>
        </a-space>
      </template>
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
    fetchAlertSmtp,
    fetchKnowledgeBases,
    fetchRatingSummaries,
    resetAgentPassword,
    resetAgentRatings,
    setAgentManualRating,
    testAlertChannel,
    testSavedAlertChannel,
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

  // ---------- 提醒渠道（Telegram / Bark / 邮箱） ----------
  // 契约冻结：每客服上限 3 个，类型可混合；config 字段 telegram→token+chat_id、
  // bark→key+server?、email→address。PATCH 的 config 是合并语义：未出现的键保留旧值。
  const MAX_ALERT_CHANNELS = 3;
  const CHANNEL_KINDS = [
    { label: 'Telegram', value: 'telegram' },
    { label: 'Bark', value: 'bark' },
    { label: '邮箱', value: 'email' },
  ];
  // 渠道类型下拉。此前模板引用了未定义的 channelKindOptions，导致下拉是空的、
  // 只显示原始值 telegram —— 这里补上定义（修复"提醒渠道逻辑有问题"的一部分）。
  const channelKindOptions = CHANNEL_KINDS;
  const REQUIRED_CHANNEL_FIELDS: Record<string, string[]> = {
    telegram: ['token', 'chat_id'],
    bark: ['key'],
    email: ['address'],
  };
  // 密钥字段后端永不回显；编辑时留空 = 不修改（PATCH 合并语义）。
  const SECRET_CHANNEL_FIELDS: Record<string, string[]> = {
    telegram: ['token'],
    bark: ['key'],
    email: ['address'],
  };
  const BARK_SERVER_PRESETS = ['https://api.day.app'];
  const EMPTY_CHANNEL_CONFIG = {
    token: '',
    chat_id: '',
    key: '',
    server: '',
    address: '',
  };

  const alertVisible = ref(false);
  const alertEditorVisible = ref(false);
  const alertLoading = ref(false);
  const alertSaving = ref(false);
  const alertTesting = ref(false);
  const alertTestingId = ref('');
  const alertAgent = ref<any>(null);
  const alertChannels = ref<any[]>([]);
  const alertEditingId = ref('');
  const alertListError = ref('');
  const alertSaveError = ref('');
  const alertTestResult = ref<{ ok: boolean; text: string } | null>(null);
  // 上限默认 3，服务端返回 max_channels 时以其为准。
  const alertMaxChannels = ref(MAX_ALERT_CHANNELS);
  const alertDraft = reactive({ kind: 'telegram' });
  const alertConfig = reactive({ ...EMPTY_CHANNEL_CONFIG });
  // 全局 SMTP 发件地址：只用于邮箱渠道的下拉候选（不参与保存）。
  const smtpFromAddress = ref('');
  const alertAtCap = computed(
    () => alertChannels.value.length >= alertMaxChannels.value
  );

  function uniqueOptions(values: unknown[]) {
    const seen = new Set<string>();
    const options: Array<{ label: string; value: string }> = [];
    for (const raw of values) {
      const value = String(raw ?? '').trim();
      if (!value || seen.has(value)) continue;
      seen.add(value);
      options.push({ label: value, value });
    }
    return options;
  }

  // 下拉候选来自「已有的同类配置」：chat_id / server 属于非密钥字段，后端原样返回；
  // 邮箱候选来自全局 SMTP 发件地址。所有下拉都允许直接输入，不强制只选预设。
  const chatIdOptions = computed(() =>
    uniqueOptions(
      alertChannels.value
        .filter((item) => item?.kind === 'telegram')
        .map((item) => item?.config_masked?.chat_id)
    )
  );
  const barkServerOptions = computed(() =>
    uniqueOptions([
      ...BARK_SERVER_PRESETS,
      ...alertChannels.value
        .filter((item) => item?.kind === 'bark')
        .map((item) => item?.config_masked?.server),
    ])
  );
  const emailAddressOptions = computed(() =>
    uniqueOptions([smtpFromAddress.value])
  );

  const alertFields = computed(() => {
    const kind = alertDraft.kind;
    if (kind === 'telegram') {
      return [
        {
          key: 'token',
          label: 'Bot Token',
          secret: true,
          placeholder: '例如：123456:ABC-DEF...',
          hint: '密钥不会回显；编辑时留空表示不修改。',
        },
        {
          key: 'chat_id',
          label: 'Chat ID',
          placeholder: '例如：-1001234567890',
          options: chatIdOptions.value,
          hint: '可从该客服已有 Telegram 渠道中选择，也可直接输入新的 Chat ID。',
        },
      ];
    }
    if (kind === 'bark') {
      return [
        {
          key: 'key',
          label: 'Bark Key',
          secret: true,
          placeholder: '例如：abcdEFGH1234',
          hint: '密钥不会回显；编辑时留空表示不修改。',
        },
        {
          key: 'server',
          label: '服务器地址（可选）',
          placeholder: '默认 https://api.day.app',
          options: barkServerOptions.value,
          hint: '下拉选择常用服务地址，或直接输入自定义地址。',
        },
      ];
    }
    return [
      {
        key: 'address',
        label: '收件邮箱',
        placeholder: '例如：agent@example.com',
        options: emailAddressOptions.value,
        hint: '可从全局 SMTP 发件地址中选择，也可直接输入收件邮箱。',
      },
    ];
  });

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function channelLabel(kind: string) {
    return CHANNEL_KINDS.find((item) => item.value === kind)?.label || kind;
  }
  function channelColor(kind: string) {
    return (
      { telegram: 'arcoblue', bark: 'orange', email: 'green' }[kind] || 'gray'
    );
  }
  // 配置展示：优先列出 config_masked 的全部字段（密钥是掩码、chat_id/server 是明文，
  // 便于客服识别渠道）；没有 config_masked 时回落到 masked 主密钥掩码。
  function channelMasked(record: any) {
    const masked = record?.config_masked;
    if (masked && typeof masked === 'object') {
      const parts = Object.entries(masked)
        .filter(([, value]) => String(value ?? '').length)
        .map(([key, value]) => `${key}=${value}`);
      if (parts.length) return parts.join(' · ');
    }
    const plain = String(record?.masked ?? '').trim();
    return plain || '-';
  }
  function resetAlertDraft(kind = 'telegram') {
    alertEditingId.value = '';
    alertDraft.kind = kind;
    Object.assign(alertConfig, EMPTY_CHANNEL_CONFIG);
    alertSaveError.value = '';
    alertTestResult.value = null;
  }
  function draftConfig() {
    const config: Record<string, string> = {};
    for (const field of alertFields.value) {
      const value = String(alertConfig[field.key] ?? '').trim();
      // 空值不提交：PATCH 里空字符串等于删除该键，会触发必填校验失败。
      if (value) config[field.key] = value;
    }
    return config;
  }
  function missingChannelFields(kind = alertDraft.kind) {
    return (REQUIRED_CHANNEL_FIELDS[kind] || []).filter(
      (key) => !String(alertConfig[key] ?? '').trim()
    );
  }

  async function loadAlertChannels() {
    if (!alertAgent.value) return;
    alertLoading.value = true;
    alertListError.value = '';
    try {
      const payload = await fetchAgentAlertChannels(alertAgent.value.id);
      alertChannels.value = Array.isArray(payload?.channels)
        ? payload.channels
        : [];
      const max = Number(payload?.max_channels);
      if (Number.isFinite(max) && max > 0) {
        alertMaxChannels.value = Math.trunc(max);
      }
    } catch (reason: any) {
      alertChannels.value = [];
      alertListError.value = backendMessage(
        reason,
        '提醒渠道加载失败，请稍后重试。'
      );
    } finally {
      alertLoading.value = false;
    }
  }

  // 全局 SMTP 发件地址只作为邮箱下拉候选；失败静默，不影响渠道管理。
  async function loadAlertSmtp() {
    try {
      const payload = await fetchAlertSmtp();
      const settings = payload?.settings ?? payload ?? {};
      smtpFromAddress.value = String(settings.from_address ?? '').trim();
    } catch {
      smtpFromAddress.value = '';
    }
  }

  async function openAlertChannels(record: any) {
    alertAgent.value = record;
    alertChannels.value = [];
    alertListError.value = '';
    resetAlertDraft('telegram');
    alertVisible.value = true;
    await Promise.all([loadAlertChannels(), loadAlertSmtp()]);
  }

  // 「新增渠道」是按钮 + 弹窗；达到上限时明确拒绝并给出提示。
  function startCreateAlertChannel() {
    if (alertAtCap.value) {
      Message.warning(
        `每个客服最多 ${alertMaxChannels.value} 个提醒渠道，请先删除后再新增`
      );
      return;
    }
    resetAlertDraft('telegram');
    alertEditorVisible.value = true;
  }

  function startEditAlertChannel(record: any) {
    const kind = record?.kind || 'telegram';
    resetAlertDraft(kind);
    alertEditingId.value = String(record?.id || '');
    // 非密钥字段（chat_id / server）后端原样返回，直接回填；密钥字段留空表示不修改。
    const masked =
      record?.config_masked && typeof record.config_masked === 'object'
        ? record.config_masked
        : {};
    const secretKeys = SECRET_CHANNEL_FIELDS[kind] || [];
    for (const field of alertFields.value) {
      if (secretKeys.includes(field.key)) continue;
      const value = String((masked as any)[field.key] ?? '').trim();
      if (value) alertConfig[field.key] = value;
    }
    alertEditorVisible.value = true;
  }

  function closeAlertEditor() {
    alertEditorVisible.value = false;
    resetAlertDraft('telegram');
  }

  async function saveAlertChannel() {
    if (!alertAgent.value) return;
    const config = draftConfig();
    alertSaveError.value = '';
    alertTestResult.value = null;

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
        closeAlertEditor();
        await loadAlertChannels();
      } catch (reason: any) {
        alertSaveError.value = backendMessage(
          reason,
          '提醒渠道更新失败，请稍后重试。'
        );
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
    const missing = missingChannelFields(alertDraft.kind);
    if (missing.length) {
      alertSaveError.value = `缺少必填项：${missing.join('、')}`;
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
      closeAlertEditor();
      await loadAlertChannels();
    } catch (reason: any) {
      alertSaveError.value = backendMessage(
        reason,
        '提醒渠道新增失败，请稍后重试。'
      );
    } finally {
      alertSaving.value = false;
    }
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
      content: `确定删除该 ${channelLabel(record.kind)} 提醒渠道？删除后立即停止向该渠道推送。`,
      onOk: async () => {
        try {
          await deleteAgentAlertChannel(alertAgent.value.id, record.id);
          if (alertEditingId.value === String(record.id)) closeAlertEditor();
          Message.success('提醒渠道已删除');
          await loadAlertChannels();
        } catch {
          // 失败提示由拦截器给出，保持列表状态即可。
        }
      },
    });
  }

  function applyTestResult(result: any, fallbackOk = false) {
    const ok = result?.ok === undefined ? fallbackOk : Boolean(result.ok);
    const text = String(
      result?.message || (ok ? '测试消息已发送' : '测试失败，请检查渠道配置')
    ).trim();
    alertTestResult.value = { ok, text };
    if (ok) Message.success(text);
    else Message.error(text);
  }

  // 列表中「发送测试」：按 id 让服务端读取已保存配置（密钥不回前端也能测）。
  async function testSavedChannel(record: any) {
    if (!alertAgent.value) return;
    alertTestingId.value = String(record?.id || '');
    try {
      const result = await testSavedAlertChannel(String(record.id));
      applyTestResult(result);
    } catch (reason: any) {
      Message.error(backendMessage(reason, '测试失败，请稍后重试。'));
    } finally {
      alertTestingId.value = '';
    }
  }

  async function testAlertEditorDraft() {
    if (!alertAgent.value) return;
    alertTestResult.value = null;
    // 编辑已保存渠道：按 id 测试，不必重新填写密钥；
    // 新增渠道：用当前草稿的即时配置测试。
    if (!alertEditingId.value && missingChannelFields(alertDraft.kind).length) {
      Message.warning('请先填写完整的渠道配置再发送测试');
      return;
    }
    alertTesting.value = true;
    try {
      const result = alertEditingId.value
        ? await testSavedAlertChannel(alertEditingId.value)
        : await testAlertChannel({
            kind: alertDraft.kind,
            config: draftConfig(),
          });
      applyTestResult(result, true);
    } catch (reason: any) {
      const text = backendMessage(reason, '测试失败，请稍后重试。');
      alertTestResult.value = { ok: false, text };
      Message.error(text);
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
  .alert-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .alert-hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
    line-height: 18px;
  }
</style>
