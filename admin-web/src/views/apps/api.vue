<template>
  <a-card :bordered="false">
    <template #title>API 管理</template>
    <template #extra>
      <a-space>
        <a-button @click="openDocs">接入说明</a-button>
        <a-button type="primary" @click="openEditor()">新增授权</a-button>
      </a-space>
    </template>
    <a-alert class="tips" type="info">
      第三方平台通过 Open API 接入客服引擎：创建会话、代用户收发消息，并用 WebSocket 实时接收 AI 与人工客服回复。
      Key 只在创建/重置时显示一次，系统仅保存 SHA-256 哈希。
    </a-alert>
    <a-alert class="tips" type="warning">
      开放 API 总开关已移到「系统设置 → 安全策略」；默认关闭，关闭时 /open/v1/* 返回 503。
    </a-alert>

    <!-- T2：开放接口按 scope 校验的显著说明。 -->
    <a-alert class="tips" type="warning" title="开放接口按权限范围（scope）校验">
      <div>
        调用方持有的 Key 只有落在该应用被授予的 scope 内才能调用对应接口；缺少范围时接口返回
        <code>403</code>，并在响应里提示缺少哪一个 scope。
      </div>
      <div>
        应用可随时调用 <code>GET /open/v1/me</code> 查看自身当前生效的 scope；该接口由
        <code>me:read</code> 覆盖，始终保留、不可取消。
      </div>
      <div>
        兼容语义：<b>空的 scope 数组＝未限制（等同全部范围）</b>，用于兼容历史应用；建议按需收窄为具体范围。
      </div>
    </a-alert>
    <div class="scope-help">
      <div class="scope-help-title">每个 scope 的用途说明</div>
      <ul class="scope-help-list">
        <li v-for="scope in API_SCOPES" :key="scope.value">
          <code>{{ scope.value }}</code>
          <span class="scope-help-label">{{ scope.label }}</span>
          <span class="scope-help-desc">{{ scope.description }}</span>
        </li>
      </ul>
    </div>

    <a-table :data="apps" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="平台名称" data-index="name" />
        <a-table-column title="Key 前缀">
          <template #cell="{ record }"><code>{{ record.key_prefix }}…</code></template>
        </a-table-column>
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">{{ record.enabled ? '已启用' : '已禁用' }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="权限范围" :width="200">
          <template #cell="{ record }">
            <a-tooltip :content="scopesTooltip(record.scopes)">
              <span
                class="scope-summary"
                :class="{ 'scope-summary--open': isUnrestricted(record.scopes) }"
                :data-scopes="scopesAttr(record.scopes)"
              >
                {{ summarizeScopes(record.scopes) }}
              </span>
            </a-tooltip>
          </template>
        </a-table-column>
        <a-table-column title="限速/分钟">
          <template #cell="{ record }">{{ Number(record.rate_limit) > 0 ? record.rate_limit : '默认 300' }}</template>
        </a-table-column>
        <a-table-column title="累计调用" data-index="request_count" />
        <a-table-column title="最近使用">
          <template #cell="{ record }">{{ localTime(record.last_used_at) }}</template>
        </a-table-column>
        <a-table-column title="备注" data-index="note" />
        <a-table-column title="操作" :width="240">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="openEditor(record)">编辑</a-link>
              <a-link @click="resetKey(record)">重置 Key</a-link>
              <a-link @click="toggle(record)">{{ record.enabled ? '禁用' : '启用' }}</a-link>
              <a-link status="danger" @click="remove(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
    </a-table>

    <a-modal
      v-model:visible="editorVisible"
      :title="form.id ? '编辑授权' : '新增授权'"
      :width="680"
      :on-before-ok="save"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="平台名称" required>
          <a-input v-model="form.name" placeholder="例如：Shopify 独立站" />
        </a-form-item>
        <a-form-item label="限速（次/分钟，0=默认 300）">
          <a-input-number v-model="form.rate_limit" :min="0" />
        </a-form-item>
        <a-form-item label="备注">
          <a-textarea v-model="form.note" :auto-size="{ minRows: 2 }" />
        </a-form-item>
        <a-form-item>
          <a-checkbox v-model="form.enabled">启用授权</a-checkbox>
        </a-form-item>

        <a-divider orientation="left">权限范围（scope）</a-divider>
        <a-alert v-if="!form.id" class="tips" type="info">
          新建应用默认授予「全功能」（7 项），可按需取消勾选或用预置组合收窄。
        </a-alert>
        <a-form-item label="权限模式">
          <a-space wrap>
            <a-switch v-model="form.unrestricted" />
            <span class="mode-text">{{ form.unrestricted ? '未限制（等同全部范围）' : '按范围限制' }}</span>
            <a-button size="mini" :disabled="form.unrestricted" @click="clearScopes">清空＝不限制</a-button>
          </a-space>
        </a-form-item>
        <a-alert v-if="form.unrestricted" class="tips" type="warning">
          当前为「未限制（等同全部范围）」：保存后服务端不校验 scope，等同授予全部权限，用于兼容旧应用；
          建议按需收窄。
        </a-alert>
        <a-form-item label="预置组合">
          <a-space wrap>
            <a-button v-for="preset in SCOPE_PRESETS" :key="preset.key" size="small" @click="applyPreset(preset)">
              {{ preset.label }}
            </a-button>
          </a-space>
        </a-form-item>
        <a-form-item label="授予的 scope">
          <a-checkbox-group :model-value="form.scopes" @change="onScopesChange">
            <a-checkbox
              v-for="scope in API_SCOPES"
              :key="scope.value"
              :value="scope.value"
              :disabled="scope.fixed"
              class="scope-option"
            >
              <a-tooltip :content="scope.description">
                <span>{{ scope.label }}（{{ scope.value }}）{{ scope.fixed ? ' · 始终保留' : '' }}</span>
              </a-tooltip>
            </a-checkbox>
          </a-checkbox-group>
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="keyVisible" title="API Key 已生成" :footer="false">
      <a-alert type="warning" class="tips">完整 Key 只显示这一次，关闭后系统只保存哈希，请立即复制保存。</a-alert>
      <a-typography-paragraph copyable code class="key">{{ apiKey }}</a-typography-paragraph>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    apiDocsUrl,
    createApiApp,
    deleteApiApp,
    fetchApiApps,
    resetApiAppKey,
    updateApiApp,
  } from '@/api/admin';
  import {
    ALL_SCOPES,
    API_SCOPES,
    FIXED_SCOPE,
    SCOPE_PRESETS,
    isUnrestricted,
    normalizeScopes,
    scopesTooltip,
    summarizeScopes,
    type ScopePreset,
  } from '@/lib/api-scopes';

  const apps = ref<any[]>([]);
  const loading = ref(false);
  const editorVisible = ref(false);
  const keyVisible = ref(false);
  const apiKey = ref('');
  const form = reactive({
    id: '',
    name: '',
    rate_limit: 0,
    note: '',
    enabled: true,
    // unrestricted=true 时保存 scopes: []（＝未限制）；scopes 始终包含 me:read，用于展示勾选状态。
    unrestricted: false,
    scopes: [...ALL_SCOPES] as string[],
  });

  function localTime(value: string) {
    return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
  }
  function openDocs() {
    window.open(apiDocsUrl(), '_blank');
  }
  function scopesAttr(scopes: unknown) {
    return JSON.stringify(Array.isArray(scopes) ? scopes : []);
  }

  async function load() {
    loading.value = true;
    try {
      apps.value = (await fetchApiApps()).apps || [];
    } finally {
      loading.value = false;
    }
  }

  function openEditor(record?: any) {
    // 旧应用没有 scopes 字段 / 空数组一律视为「未限制」。
    const restricted = record ? !isUnrestricted(record.scopes) : true;
    Object.assign(form, {
      id: record?.id || '',
      name: record?.name || '',
      rate_limit: record?.rate_limit || 0,
      note: record?.note || '',
      enabled: record?.enabled ?? true,
      unrestricted: record ? !restricted : false,
      // 新建默认「全功能」；编辑未限制应用时仅展示固定勾选的 me:read。
      scopes: record ? (restricted ? normalizeScopes(record.scopes) : [FIXED_SCOPE]) : [...ALL_SCOPES],
    });
    editorVisible.value = true;
  }

  /** me:read 始终在选中集合里；用户勾选任一 scope 即切换为「按范围限制」。 */
  function onScopesChange(value: (string | number | boolean)[]) {
    const scopes = normalizeScopes(value);
    form.scopes = scopes.length ? scopes : [FIXED_SCOPE];
    form.unrestricted = false;
  }

  function applyPreset(preset: ScopePreset) {
    form.unrestricted = false;
    form.scopes = normalizeScopes(preset.scopes);
  }

  function clearScopes() {
    form.unrestricted = true;
    form.scopes = [FIXED_SCOPE];
  }

  /** 保存成功后按服务端返回回填列表与表单。 */
  function applyServerApp(app: any) {
    if (!app) return;
    const index = apps.value.findIndex((item) => item.id === app.id);
    if (index >= 0) apps.value.splice(index, 1, { ...apps.value[index], ...app });
    if (app.id && app.id === form.id) {
      Object.assign(form, {
        name: app.name ?? form.name,
        rate_limit: app.rate_limit ?? form.rate_limit,
        note: app.note ?? form.note,
        enabled: app.enabled ?? form.enabled,
        unrestricted: isUnrestricted(app.scopes),
        scopes: isUnrestricted(app.scopes) ? [FIXED_SCOPE] : normalizeScopes(app.scopes),
      });
    }
  }

  // Arco 的 onBeforeOk：返回 false 阻止关闭。HTTP 错误已由 api/interceptor.ts 弹出后端 message，
  // 这里只需保证不把异常抛出去（否则会产生未处理的 Promise 报错）。
  async function save(): Promise<boolean> {
    if (!form.name.trim()) {
      Message.warning('请填写平台名称');
      return false;
    }
    const scopes = form.unrestricted ? [] : normalizeScopes(form.scopes);
    const payload = {
      name: form.name,
      rate_limit: Number(form.rate_limit) || 0,
      note: form.note,
      enabled: form.enabled,
      scopes,
    };
    try {
      if (form.id) {
        const result = await updateApiApp(form.id, payload);
        applyServerApp(result?.app);
      } else {
        const created = await createApiApp(payload);
        applyServerApp(created?.app);
        // 兼容两种一次性明文 Key 字段名：既有实现的 api_key 与冻结契约的 key。
        const plaintextKey = created?.api_key || created?.key;
        if (plaintextKey) {
          apiKey.value = plaintextKey;
          keyVisible.value = true;
        }
      }
      editorVisible.value = false;
      await load();
      Message.success('已保存');
      return true;
    } catch (error: any) {
      if (!error?.response && !error?.isAxiosError) {
        Message.error(error?.message || '保存失败');
      }
      return false;
    }
  }

  function resetKey(record: any) {
    Modal.confirm({
      title: '重置 Key',
      content: `重置后 ${record.name} 的旧 Key 立即失效。`,
      onOk: async () => {
        const payload = await resetApiAppKey(record.id);
        apiKey.value = payload.api_key || payload.key || '';
        keyVisible.value = true;
        await load();
      },
    });
  }

  async function toggle(record: any) {
    await updateApiApp(record.id, { enabled: !record.enabled });
    await load();
  }

  function remove(record: any) {
    Modal.confirm({
      title: '删除授权',
      content: `删除后 ${record.name} 将无法再调用开放接口。`,
      onOk: async () => {
        await deleteApiApp(record.id);
        Message.success('已删除');
        await load();
      },
    });
  }

  onMounted(load);
</script>

<style scoped>
  .tips {
    margin-bottom: 14px;
  }
  .key {
    margin-top: 8px;
    word-break: break-all;
  }
  .scope-help {
    margin-bottom: 14px;
    padding: 10px 14px;
    border: 1px solid var(--color-border-2);
    border-radius: 4px;
    background: var(--color-fill-1);
  }
  .scope-help-title {
    margin-bottom: 6px;
    font-weight: 500;
    color: var(--color-text-1);
  }
  .scope-help-list {
    margin: 0;
    padding-left: 18px;
    color: var(--color-text-2);
    line-height: 1.9;
  }
  .scope-help-list code {
    margin-right: 6px;
  }
  .scope-help-label {
    margin-right: 6px;
    color: var(--color-text-1);
  }
  .scope-summary {
    cursor: help;
  }
  .scope-summary--open {
    color: rgb(var(--orange-6));
  }
  .mode-text {
    color: var(--color-text-1);
  }
  .scope-option {
    display: flex;
    margin-bottom: 6px;
  }
</style>
