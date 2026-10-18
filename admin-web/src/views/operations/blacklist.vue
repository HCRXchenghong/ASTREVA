<template>
  <a-card :bordered="false" class="blacklist-page">
    <template #title>黑名单</template>
    <template #extra>
      <a-space>
        <a-button :loading="loading" @click="load">刷新</a-button>
        <a-button type="primary" data-testid="blacklist-add" @click="openEditor">
          新增黑名单
        </a-button>
      </a-space>
    </template>

    <a-alert class="tips" type="info">
      命中黑名单的访客建会话会被拒绝，并收到提示文案「您暂时无法发起会话」；IP / 账号黑名单在建会话与登录时同时生效。留空过期时间表示永久生效。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-tabs v-model:active-key="kind">
      <a-tab-pane v-for="tab in TABS" :key="tab.value" :title="tab.title" />
    </a-tabs>

    <a-table
      :data="entries"
      :loading="loading"
      :pagination="false"
      row-key="id"
      data-testid="blacklist-table"
    >
      <template #columns>
        <a-table-column title="值" :width="260">
          <template #cell="{ record }">
            <a-typography-text :title="record.value">{{ record.value }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="原因">
          <template #cell="{ record }">{{ record.reason || '-' }}</template>
        </a-table-column>
        <a-table-column title="创建人" :width="140">
          <template #cell="{ record }">{{ record.created_by || '-' }}</template>
        </a-table-column>
        <a-table-column title="过期时间" :width="180">
          <template #cell="{ record }">
            {{ record.expires_at ? localTime(record.expires_at) : '永久' }}
          </template>
        </a-table-column>
        <a-table-column title="创建时间" :width="180">
          <template #cell="{ record }">{{ localTime(record.created_at) }}</template>
        </a-table-column>
        <a-table-column title="操作" :width="90">
          <template #cell="{ record }">
            <a-link
              status="danger"
              :data-testid="`blacklist-delete-${record.id}`"
              @click="remove(record)"
            >
              删除
            </a-link>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="当前分类下没有黑名单" />
      </template>
    </a-table>

    <a-modal
      v-model:visible="editorVisible"
      title="新增黑名单"
      :ok-loading="saving"
      @ok="save"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="类型" required>
          <a-select
            v-model="form.kind"
            :options="kindOptions"
            data-testid="blacklist-kind"
          />
        </a-form-item>
        <a-form-item label="值" required>
          <a-input
            v-model="form.value"
            :placeholder="valuePlaceholder"
            :input-attrs="{ 'data-testid': 'blacklist-value' }"
          />
        </a-form-item>
        <a-form-item label="原因">
          <a-input
            v-model="form.reason"
            placeholder="例如：广告骚扰"
            :input-attrs="{ 'data-testid': 'blacklist-reason' }"
          />
        </a-form-item>
        <a-form-item label="过期时间（留空=永久）">
          <!-- DatePicker 不透传 input-attrs，这里用外层容器挂测试标记 -->
          <div data-testid="blacklist-expires">
            <a-date-picker
              v-model="form.expires_at"
              show-time
              allow-clear
              value-format="YYYY-MM-DD HH:mm:ss"
              format="YYYY-MM-DD HH:mm:ss"
              style="width: 260px"
            />
          </div>
        </a-form-item>
      </a-form>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createBlacklistEntry,
    deleteBlacklistEntry,
    fetchBlacklist,
  } from '@/api/admin';

  type Kind = 'visitor' | 'ip' | 'account';

  const TABS: Array<{ value: Kind; title: string }> = [
    { value: 'visitor', title: '访客' },
    { value: 'ip', title: 'IP' },
    { value: 'account', title: '账号' },
  ];

  const entries = ref<any[]>([]);
  const kind = ref<Kind>('visitor');
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref('');
  const editorVisible = ref(false);
  const form = reactive({
    kind: 'visitor' as Kind,
    value: '',
    reason: '',
    expires_at: '',
  });

  const kindOptions = TABS.map((tab) => ({ label: tab.title, value: tab.value }));
  const valuePlaceholder = computed(() => {
    if (form.kind === 'ip') return '例如：203.0.113.10';
    if (form.kind === 'account') return '例如：agent01';
    return '例如：访客 ID / 手机号 / 邮箱';
  });

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function localTime(value: unknown) {
    const raw = String(value ?? '');
    if (!raw) return '-';
    const time = Date.parse(raw);
    if (!Number.isFinite(time)) return raw;
    return new Date(time).toLocaleString('zh-CN', { hour12: false });
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchBlacklist(kind.value);
      entries.value = Array.isArray(result?.entries) ? result.entries : [];
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '黑名单加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  function openEditor() {
    Object.assign(form, {
      kind: kind.value,
      value: '',
      reason: '',
      expires_at: '',
    });
    editorVisible.value = true;
  }

  // 空值不提交 expires_at（= 永久）；有值时转成 RFC3339（后端按 time.Time 解析）。
  function buildPayload() {
    const payload: Record<string, unknown> = {
      kind: form.kind,
      value: String(form.value ?? '').trim(),
      reason: String(form.reason ?? '').trim(),
    };
    const expires = String(form.expires_at ?? '').trim();
    if (expires) {
      const parsed = new Date(expires.replace(' ', 'T'));
      payload.expires_at = Number.isFinite(parsed.getTime())
        ? parsed.toISOString()
        : expires;
    }
    return payload;
  }

  async function save() {
    if (!String(form.value ?? '').trim()) {
      Message.warning('请填写要加入黑名单的值');
      return;
    }
    saving.value = true;
    try {
      await createBlacklistEntry(buildPayload());
      editorVisible.value = false;
      Message.success('已加入黑名单');
      kind.value = form.kind;
      await load();
    } finally {
      saving.value = false;
    }
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除黑名单项',
      content: `确定删除「${record.value}」？删除后该访客/IP/账号可立即重新建会话。`,
      onOk: async () => {
        await deleteBlacklistEntry(String(record.id));
        Message.success('已删除');
        await load();
      },
    });
  }

  watch(kind, load);
  onMounted(load);
</script>

<style scoped lang="less">
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 14px;
  }
</style>
