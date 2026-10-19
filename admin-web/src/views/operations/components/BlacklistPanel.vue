<template>
  <div class="blacklist-panel" data-testid="blacklist-panel">
    <div class="panel-head">
      <a-space>
        <a-tag color="arcoblue">仅 IP</a-tag>
        <a-typography-text type="secondary">
          共 {{ entries.length }} 条 IP 黑名单
        </a-typography-text>
      </a-space>
      <a-space>
        <a-button
          :loading="loading"
          size="small"
          data-testid="blacklist-refresh"
          @click="load"
        >
          刷新
        </a-button>
        <a-button
          type="primary"
          size="small"
          data-testid="blacklist-add"
          @click="openEditor"
        >
          新增 IP
        </a-button>
      </a-space>
    </div>

    <a-alert class="tips" type="info">
      黑名单只支持 <strong>IP</strong>：命中的访客建立会话会被拒绝，并收到提示文案「您暂时无法发起会话」。
      留空过期时间表示永久生效。<strong>访客 / 账号维度已下线</strong>，避免误封正常用户。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <!-- 新增 IP 用面板内联表单（不再套嵌套弹窗，避免多层弹窗互相遮挡/误关）。 -->
    <a-card
      v-if="editorVisible"
      :bordered="false"
      class="editor-card"
      data-testid="blacklist-editor"
    >
      <a-alert v-if="saveError" class="banner" type="error" show-icon>
        {{ saveError }}
      </a-alert>
      <a-form :model="form" layout="inline" class="editor-form">
        <a-form-item label="IP 地址" required>
          <a-input
            v-model="form.value"
            placeholder="例如：203.0.113.10（支持 IPv6）"
            :input-attrs="{ 'data-testid': 'blacklist-value' }"
            style="width: 240px"
          />
        </a-form-item>
        <a-form-item label="原因">
          <a-input
            v-model="form.reason"
            placeholder="例如：广告骚扰"
            :input-attrs="{ 'data-testid': 'blacklist-reason' }"
            style="width: 180px"
          />
        </a-form-item>
        <a-form-item label="过期时间">
          <div data-testid="blacklist-expires">
            <a-date-picker
              v-model="form.expires_at"
              show-time
              allow-clear
              value-format="YYYY-MM-DD HH:mm:ss"
              format="YYYY-MM-DD HH:mm:ss"
              placeholder="留空=永久"
              style="width: 220px"
            />
          </div>
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-button
              type="primary"
              :loading="saving"
              data-testid="blacklist-save"
              @click="save"
            >
              保存
            </a-button>
            <a-button @click="closeEditor">取消</a-button>
          </a-space>
        </a-form-item>
      </a-form>
      <a-typography-text type="secondary" class="hint">
        只接受 IP；带端口或逗号分隔的取值会由后端归一化。留空过期时间表示永久生效。
      </a-typography-text>
    </a-card>

    <a-table
      :data="entries"
      :loading="loading"
      :pagination="false"
      row-key="id"
      size="small"
      data-testid="blacklist-table"
    >
      <template #columns>
        <a-table-column title="IP" :width="220">
          <template #cell="{ record }">
            <a-typography-text :title="record.value">{{ record.value }}</a-typography-text>
          </template>
        </a-table-column>
        <a-table-column title="原因">
          <template #cell="{ record }">{{ record.reason || '-' }}</template>
        </a-table-column>
        <a-table-column title="创建人" :width="130">
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
        <a-table-column title="操作" :width="80">
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
        <a-empty description="当前没有 IP 黑名单" />
      </template>
    </a-table>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createBlacklistEntry,
    deleteBlacklistEntry,
    fetchBlacklist,
  } from '@/api/admin';

  const emit = defineEmits<{ (e: 'changed', total: number): void }>();

  // 前端约束：只提交 kind=ip（后端接口本身仍保留三种 kind，这里不改后端）。
  const BLACKLIST_KIND = 'ip';

  const entries = ref<any[]>([]);
  const loading = ref(false);
  const loadError = ref('');
  const saving = ref(false);
  const saveError = ref('');
  const editorVisible = ref(false);
  const form = reactive({ value: '', reason: '', expires_at: '' });

  const IPV4 =
    /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;

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

  // 与后端 normalizeIP 对齐：取逗号首项、去 IPv6 方括号、去 IPv4 端口。
  function hostPart(value: string) {
    let raw = String(value ?? '').trim();
    if (raw.includes(',')) raw = raw.split(',')[0].trim();
    if (raw.startsWith('[')) {
      const end = raw.indexOf(']');
      raw = end === -1 ? raw.slice(1) : raw.slice(1, end);
    } else if (/^\d+\.\d+\.\d+\.\d+:\d+$/.test(raw)) {
      raw = raw.split(':')[0];
    }
    return raw;
  }

  function isIpLike(value: string) {
    const host = hostPart(value);
    if (!host) return false;
    if (IPV4.test(host)) return true;
    // IPv6：只含十六进制字符与冒号，且至少三段。
    return (
      host.includes(':') &&
      /^[0-9a-fA-F:]+$/.test(host) &&
      host.split(':').length >= 3
    );
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchBlacklist(BLACKLIST_KIND);
      entries.value = Array.isArray(result?.entries) ? result.entries : [];
      emit('changed', entries.value.length);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '黑名单加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  function openEditor() {
    saveError.value = '';
    Object.assign(form, { value: '', reason: '', expires_at: '' });
    editorVisible.value = true;
  }

  function closeEditor() {
    editorVisible.value = false;
    saveError.value = '';
  }

  // 空值不提交 expires_at（= 永久）；有值时转成 RFC3339（后端按 time.Time 解析）。
  function buildPayload() {
    const payload: Record<string, unknown> = {
      kind: BLACKLIST_KIND,
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
    const value = String(form.value ?? '').trim();
    if (!value) {
      Message.warning('请填写要加入黑名单的 IP');
      return;
    }
    if (!isIpLike(value)) {
      Message.warning('只支持 IP 地址，例如 203.0.113.10');
      return;
    }
    saving.value = true;
    saveError.value = '';
    try {
      await createBlacklistEntry(buildPayload());
      editorVisible.value = false;
      Message.success('已加入 IP 黑名单');
      await load();
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '加入黑名单失败，请稍后重试。');
    } finally {
      saving.value = false;
    }
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除 IP 黑名单',
      content: `确定删除「${record.value}」？删除后该 IP 可立即重新建会话。`,
      onOk: async () => {
        await deleteBlacklistEntry(String(record.id));
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 12px;
  }
  .hint {
    display: block;
    margin-top: 4px;
    font-size: 12px;
  }
  .editor-card {
    margin-bottom: 12px;
    background: var(--color-fill-1);
  }
  .editor-form {
    row-gap: 8px;
  }
</style>
