<template>
  <div class="ai-bindings">
    <a-alert class="tips" type="info" title="故障转移链说明">
      <ul class="tips-list">
        <li>
          调用时按<strong>优先级从小到大</strong>依次尝试该能力的候选供应商；优先级数字越小越先尝试。
        </li>
        <li>
          供应商处于熔断打开（<code>open</code>）时<strong>完全跳过</strong>，不参与本次调用；
          某候选失败且可重试时自动切到下一个候选。
        </li>
        <li>
          全部候选都不可用后，才回退到「默认绑定（兜底）」；未绑定默认绑定且无候选时，
          回退「系统设置」里的全局上游。
        </li>
        <li>
          候选保存是<strong>整体替换</strong>：每次提交全部能力的完整候选列表，
          清空某能力的候选即表示该能力不参与故障转移链（直接走兜底）。
        </li>
      </ul>
    </a-alert>

    <!-- ===== 故障转移链（多供应商 + 优先级）===== -->
    <a-card :bordered="false" class="chain-card">
      <template #title>故障转移链</template>
      <template #extra>
        <a-button
          type="primary"
          :loading="savingAll"
          :disabled="!targetsLoaded"
          data-test="save-all"
          @click="saveAllTargets"
        >
          保存全部
        </a-button>
      </template>

      <a-alert
        v-if="targetsError"
        class="save-error"
        type="warning"
        title="候选列表加载失败"
        data-test="targets-error"
      >
        {{ targetsError }}
      </a-alert>
      <a-alert
        v-if="saveError"
        class="save-error"
        type="error"
        title="保存失败"
        data-test="save-error"
      >
        {{ saveError }}
      </a-alert>

      <a-spin :loading="loading" class="chain-body">
        <a-empty v-if="!loading && !targets.length" description="暂无候选供应商" />
        <div
          v-for="group in groups"
          :key="group.capability"
          class="cap-card"
          :data-test="`chain-${group.capability}`"
        >
          <div class="cap-head">
            <div class="cap-title">
              <strong>{{ group.label }}</strong>
              <a-tag size="small" :color="group.kind === 'image' ? 'purple' : 'arcoblue'">
                {{ group.kind === 'image' ? '生图' : '文本' }}
              </a-tag>
              <a-typography-text type="secondary" class="cap-count">
                {{ group.targets.length }} 个候选 · 按优先级从小到大尝试
              </a-typography-text>
            </div>
            <a-space>
              <a-button size="small" data-test="toggle-add" @click="toggleAdd(group)">
                添加候选
              </a-button>
              <a-button
                size="small"
                type="primary"
                :loading="savingCapability === group.capability"
                :disabled="!targetsLoaded"
                :data-test="`save-${group.capability}`"
                @click="saveCapability(group)"
              >
                保存本能力
              </a-button>
            </a-space>
          </div>

          <div v-if="!group.targets.length" class="empty-hint">
            该能力暂无候选：不参与故障转移链，调用时直接走「默认绑定（兜底）」。
          </div>
          <div v-else class="candidate-list">
            <div
              v-for="(target, index) in group.targets"
              :key="target.provider_id"
              class="candidate-row"
              data-test="candidate-row"
              :data-provider-id="target.provider_id"
              :data-priority="target.priority"
            >
              <a-tag class="priority-tag" data-test="priority">
                优先级 {{ target.priority }}
              </a-tag>
              <div class="candidate-provider" data-test="provider">
                <span class="provider-name">{{ providerName(target.provider_id) }}</span>
                <a-tooltip :content="healthTooltip(target.provider_id)">
                  <a-tag
                    size="small"
                    :color="healthMeta(target.provider_id).color"
                    data-test="health-tag"
                  >
                    {{ healthLabel(target.provider_id) }}
                  </a-tag>
                </a-tooltip>
                <a-tag
                  v-if="!providerOf(target.provider_id)"
                  size="small"
                  color="red"
                >
                  供应商缺失
                </a-tag>
                <a-tag
                  v-else-if="!providerOf(target.provider_id).enabled"
                  size="small"
                  color="gray"
                >
                  已禁用
                </a-tag>
              </div>
              <a-select
                v-model="target.model"
                class="candidate-model"
                :options="modelOptions(target)"
                allow-create
                allow-search
                placeholder="跟随供应商默认模型"
                data-test="candidate-model"
              />
              <a-switch
                v-model="target.enabled"
                class="candidate-switch"
                data-test="candidate-enabled"
              />
              <a-space class="candidate-actions">
                <a-link
                  :disabled="index === 0"
                  data-test="move-up"
                  @click="moveTarget(group, index, -1)"
                >
                  上移
                </a-link>
                <a-link
                  :disabled="index === group.targets.length - 1"
                  data-test="move-down"
                  @click="moveTarget(group, index, 1)"
                >
                  下移
                </a-link>
                <a-link
                  status="danger"
                  data-test="remove"
                  @click="removeTarget(group, index)"
                >
                  删除
                </a-link>
              </a-space>
            </div>
          </div>

          <div v-if="group.adding" class="add-row">
            <a-select
              v-model="group.draft.provider_id"
              class="add-provider"
              :options="providerOptions(group)"
              allow-search
              placeholder="选择供应商（仅启用且类型匹配）"
              data-test="add-provider"
              @change="onDraftProviderChange(group)"
            />
            <a-select
              v-model="group.draft.model"
              class="add-model"
              :options="draftModelOptions(group)"
              :disabled="!group.draft.provider_id"
              allow-create
              allow-search
              placeholder="选择模型（可手填）"
              data-test="add-model"
            />
            <a-space>
              <a-button
                size="small"
                type="primary"
                :disabled="!group.draft.provider_id"
                data-test="add-confirm"
                @click="confirmAdd(group)"
              >
                追加为优先级 {{ nextPriority(group) }}
              </a-button>
              <a-button size="small" @click="toggleAdd(group)">取消</a-button>
            </a-space>
          </div>
        </div>
      </a-spin>

      <a-typography-text type="secondary" class="footer-hint">
        模型下拉允许手填：上游未返回全部模型时可直接输入模型名；留空表示跟随该供应商的默认模型。
      </a-typography-text>
    </a-card>

    <!-- ===== 供应商熔断健康 ===== -->
    <a-card :bordered="false" class="health-card">
      <template #title>供应商熔断健康</template>
      <template #extra>
        <a-space>
          <a-typography-text type="secondary" class="health-hint">
            每 10 秒自动刷新（页面不可见时暂停）
          </a-typography-text>
          <a-button
            size="small"
            :loading="healthLoading"
            data-test="health-refresh"
            @click="refreshHealth"
          >
            刷新
          </a-button>
        </a-space>
      </template>
      <a-alert
        v-if="healthError"
        class="health-error"
        type="warning"
        data-test="health-error"
      >
        {{ healthError }}
      </a-alert>
      <a-table
        :data="healthRows"
        :loading="healthLoading"
        :pagination="false"
        row-key="id"
        data-test="health-table"
      >
        <template #columns>
          <a-table-column title="供应商" :width="200">
            <template #cell="{ record }">
              <div><strong>{{ record.name || record.id }}</strong></div>
              <a-typography-text type="secondary">{{ record.id }}</a-typography-text>
            </template>
          </a-table-column>
          <a-table-column title="熔断状态" :width="190">
            <template #cell="{ record }">
              <a-tag
                :color="healthMeta(record.id).color"
                data-test="health-state"
              >
                {{ healthLabel(record.id) }}
              </a-tag>
            </template>
          </a-table-column>
          <a-table-column title="连续失败" :width="100">
            <template #cell="{ record }">
              <span data-test="health-failures">
                {{ Number(record.consecutive_failures || 0) }}
              </span>
            </template>
          </a-table-column>
          <a-table-column title="最后错误">
            <template #cell="{ record }">
              <a-tooltip v-if="record.last_error" :content="record.last_error">
                <span class="last-error">{{ record.last_error }}</span>
              </a-tooltip>
              <a-typography-text v-else type="secondary">-</a-typography-text>
            </template>
          </a-table-column>
        </template>
      </a-table>
    </a-card>

    <!-- ===== 默认绑定（兜底）===== -->
    <a-card :bordered="false" class="fallback-card">
      <template #title>默认绑定（兜底）</template>
      <template #extra>
        <a-button
          type="primary"
          :loading="savingBindings"
          data-test="save-all-bindings"
          @click="saveAllBindings"
        >
          保存全部
        </a-button>
      </template>
      <a-alert class="tips" type="info">
        故障转移链上的候选全部不可用时才使用默认绑定；未绑定默认绑定且无候选时，
        回退到「系统设置」里的全局上游。接待 AI 与翻译 / 语言检测由客服引擎使用；
        AI SEO 与 AI 生图当前仍由官网后台使用独立配置，下一轮接入。
      </a-alert>
      <a-table :data="rows" :loading="loading" :pagination="false" row-key="capability">
        <template #columns>
          <a-table-column title="能力" :width="200">
            <template #cell="{ record }">
              <div><strong>{{ record.label }}</strong></div>
              <a-typography-text type="secondary">
                {{ record.provider_id ? requirementHint(record) : UNBOUND_HINT }}
              </a-typography-text>
            </template>
          </a-table-column>
          <a-table-column title="供应商">
            <template #cell="{ record }">
              <a-select
                v-model="record.provider_id"
                :options="providerOptions(record)"
                allow-search
                placeholder="未绑定"
                @change="onProviderChange(record)"
              />
            </template>
          </a-table-column>
          <a-table-column title="模型">
            <template #cell="{ record }">
              <a-select
                v-model="record.model"
                :options="modelOptions(record)"
                :disabled="!record.provider_id"
                allow-create
                allow-search
                placeholder="跟随供应商默认模型"
              />
            </template>
          </a-table-column>
          <a-table-column title="操作" :width="90">
            <template #cell="{ record }">
              <a-link :loading="savingCapability === record.capability" @click="saveRow(record)">
                保存
              </a-link>
            </template>
          </a-table-column>
        </template>
      </a-table>
      <a-typography-text type="secondary" class="footer-hint">
        模型下拉允许手填：上游未返回全部模型时可直接输入模型名。
      </a-typography-text>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, onUnmounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchAiBindingTargets,
    fetchAiBindings,
    fetchAiProviderHealth,
    fetchAiProviders,
    updateAiBindingTargets,
    updateAiBindings,
  } from '@/api/admin';

  const UNBOUND_HINT = '未绑定（使用系统设置里的默认上游）';
  // 能力顺序固定，与服务端 domain.AICapabilities() 一致。
  const CAPABILITIES = [
    { capability: 'reception', label: '接待 AI', kind: 'text' },
    { capability: 'translation', label: '翻译', kind: 'text' },
    { capability: 'detection', label: '语言检测', kind: 'text' },
    { capability: 'seo', label: 'AI SEO', kind: 'text' },
    { capability: 'image', label: 'AI 生图', kind: 'image' },
  ];
  // 健康快照轮询间隔：10 秒；document.hidden 时跳过本轮。
  const HEALTH_INTERVAL_MS = 10000;
  const HEALTH_STATE_META: Record<string, { label: string; color: string }> = {
    closed: { label: '正常', color: 'green' },
    open: { label: '熔断中', color: 'red' },
    half_open: { label: '半开探测', color: 'orange' },
  };

  const providers = ref<any[]>([]);
  const rows = ref<any[]>([]);
  const groups = ref<any[]>([]);
  const healthList = ref<any[]>([]);
  const loading = ref(false);
  const savingAll = ref(false);
  const savingBindings = ref(false);
  const savingCapability = ref('');
  const saveError = ref('');
  // 候选列表是否已成功加载：加载失败时禁用保存，避免用空数组误清空后端候选。
  const targetsLoaded = ref(false);
  const targetsError = ref('');
  // 秒级倒计时用：每秒推进一次，熔断剩余秒数随之更新。
  const nowMs = ref(Date.now());
  const healthLoading = ref(false);
  const healthError = ref('');

  let healthTimer: number | undefined;
  let tickTimer: number | undefined;

  const targets = computed(() =>
    groups.value.flatMap((group) => group.targets || [])
  );
  const healthMap = computed<Record<string, any>>(() =>
    Object.fromEntries(
      healthList.value.filter((item) => item?.id).map((item) => [item.id, item])
    )
  );
  // 健康表以健康接口为准；接口没返回时用供应商列表兜底为 closed。
  const healthRows = computed(() => {
    if (healthList.value.length) return healthList.value;
    return providers.value.map((provider) => ({
      id: provider.id,
      name: provider.name,
      enabled: provider.enabled,
      state: 'closed',
      consecutive_failures: 0,
    }));
  });

  function errorMessage(error: any) {
    return (
      error?.response?.data?.error?.message ||
      error?.response?.data?.message ||
      error?.message ||
      '请求失败'
    );
  }

  // ---------- 供应商 / 模型解析 ----------
  function providerOf(id: string) {
    return providers.value.find((provider) => provider.id === id) || null;
  }

  function providerName(id: string) {
    return providerOf(id)?.name || id || '（未选择）';
  }

  // 只列启用且类型匹配的供应商：生图能力只认 image，其余能力只认 text。
  function providerOptions(record: any) {
    return providers.value
      .filter((provider) => provider.enabled && provider.kind === record.kind)
      .map((provider) => ({ label: provider.name, value: provider.id }));
  }

  function modelOptions(record: any) {
    const provider = providerOf(record.provider_id);
    const values = [
      ...new Set([record.model, ...(provider?.models || [])].filter(Boolean)),
    ];
    return values.map((value) => ({ label: value, value }));
  }

  // ---------- 故障转移链数据 ----------
  function normalizeTargets(value: any, capability: string) {
    return (Array.isArray(value) ? value : [])
      .filter((item) => item && item.capability === capability)
      .map((item) => ({
        capability,
        provider_id: item.provider_id || '',
        model: item.model || '',
        priority: Number(item.priority) || 0,
        enabled: item.enabled !== false,
      }))
      .sort(
        (a, b) =>
          a.priority - b.priority ||
          String(a.provider_id).localeCompare(String(b.provider_id))
      );
  }

  function buildGroups(list: any[]) {
    return CAPABILITIES.map((meta) => ({
      ...meta,
      targets: normalizeTargets(list, meta.capability),
      draft: { provider_id: '', model: '' },
      adding: false,
    }));
  }

  // 保存成功后用响应回填候选，同时保留未提交的「添加候选」草稿与展开状态。
  function applyTargets(list: any[]) {
    const fresh = buildGroups(list);
    const previous = new Map(
      groups.value.map((group) => [group.capability, group])
    );
    groups.value = fresh.map((group) => {
      const old = previous.get(group.capability);
      return old
        ? { ...group, draft: old.draft, adding: old.adding }
        : group;
    });
  }

  function collectTargets() {
    return groups.value.flatMap((group) =>
      group.targets.map((target: any) => ({
        capability: group.capability,
        provider_id: target.provider_id,
        model: target.model || '',
        priority: Number(target.priority) || 0,
        enabled: target.enabled !== false,
      }))
    );
  }

  // 前端先挡住重复 / 缺失 / 类型不匹配，避免把必然 400 的请求发出去。
  function validateTargets() {
    for (const group of groups.value) {
      const seen = new Set<string>();
      for (const target of group.targets) {
        if (!target.provider_id) {
          return `${group.label}：存在未选择供应商的候选`;
        }
        if (seen.has(target.provider_id)) {
          return `${group.label}：供应商「${providerName(target.provider_id)}」重复，请先删除重复候选`;
        }
        seen.add(target.provider_id);
        const provider = providerOf(target.provider_id);
        if (!provider) {
          return `${group.label}：供应商 ${target.provider_id} 不存在，请先删除该候选`;
        }
        if (!provider.enabled) {
          return `${group.label}：供应商「${provider.name}」已禁用，请先启用或删除该候选`;
        }
        if (provider.kind !== group.kind) {
          return `${group.label}：供应商「${provider.name}」类型与该能力不匹配`;
        }
      }
    }
    return '';
  }

  function nextPriority(group: any) {
    return group.targets.reduce(
      (max: number, target: any) => Math.max(max, Number(target.priority) || 0),
      -1
    ) + 1;
  }

  function toggleAdd(group: any) {
    group.adding = !group.adding;
    if (!group.adding) group.draft = { provider_id: '', model: '' };
  }

  function onDraftProviderChange(group: any) {
    // 换供应商后原模型大概率不属于新上游，清空让管理员重新选择。
    group.draft.model = '';
  }

  function draftModelOptions(group: any) {
    const provider = providerOf(group.draft.provider_id);
    return (provider?.models || []).map((model: string) => ({
      label: model,
      value: model,
    }));
  }

  function confirmAdd(group: any) {
    const providerId = group.draft.provider_id;
    if (!providerId) {
      Message.warning('请先选择供应商');
      return;
    }
    const provider = providerOf(providerId);
    if (!provider) {
      Message.warning(`供应商 ${providerId} 不存在`);
      return;
    }
    if (provider.kind !== group.kind) {
      Message.warning(`供应商「${provider.name}」类型与该能力不匹配`);
      return;
    }
    if (!provider.enabled) {
      Message.warning(`供应商「${provider.name}」已禁用`);
      return;
    }
    if (group.targets.some((target: any) => target.provider_id === providerId)) {
      Message.warning(`供应商「${provider.name}」已在该能力的候选链中，不能重复添加`);
      return;
    }
    group.targets.push({
      capability: group.capability,
      provider_id: providerId,
      model: group.draft.model || '',
      priority: nextPriority(group),
      enabled: true,
    });
    group.draft = { provider_id: '', model: '' };
    group.adding = false;
  }

  // 上移 / 下移后按行序重排优先级为 0..n-1，保证与页面展示顺序一致。
  function reindex(group: any) {
    group.targets = group.targets.map((target: any, index: number) => ({
      ...target,
      priority: index,
    }));
  }

  function moveTarget(group: any, index: number, delta: number) {
    const next = index + delta;
    if (next < 0 || next >= group.targets.length) return;
    const list = [...group.targets];
    [list[index], list[next]] = [list[next], list[index]];
    group.targets = list;
    reindex(group);
  }

  function removeTarget(group: any, index: number) {
    group.targets.splice(index, 1);
    reindex(group);
  }

  // ---------- 供应商熔断健康 ----------
  function healthEntry(id: string) {
    return healthMap.value[id] || null;
  }

  function healthMeta(id: string) {
    const state = healthEntry(id)?.state || 'closed';
    return HEALTH_STATE_META[state] || HEALTH_STATE_META.closed;
  }

  function healthSeconds(id: string): number | null {
    const until = healthEntry(id)?.opened_until;
    if (!until) return null;
    const diff = Math.ceil((new Date(until).getTime() - nowMs.value) / 1000);
    return diff > 0 ? diff : 0;
  }

  function healthLabel(id: string) {
    const meta = healthMeta(id);
    const seconds = healthSeconds(id);
    return seconds === null ? meta.label : `${meta.label}（剩余 ${seconds}s）`;
  }

  function healthTooltip(id: string) {
    const entry = healthEntry(id);
    const parts = [`状态：${healthMeta(id).label}`];
    if (entry?.enabled === false) parts.push('供应商已禁用');
    parts.push(`连续失败：${Number(entry?.consecutive_failures || 0)}`);
    if (entry?.last_error) parts.push(`最后错误：${entry.last_error}`);
    return parts.join('；');
  }

  // 轮询失败静默降级到上一次数据，只在页面内提示，不弹全局错误。
  async function loadHealth() {
    healthLoading.value = true;
    try {
      const payload = await fetchAiProviderHealth({ skipErrorMessage: true });
      healthList.value = payload?.providers || [];
      healthError.value = '';
    } catch (error: any) {
      healthError.value = `供应商熔断健康获取失败：${errorMessage(error)}`;
    } finally {
      healthLoading.value = false;
    }
  }

  async function refreshHealth() {
    await loadHealth();
    if (healthError.value) Message.error(healthError.value);
    else Message.success('供应商健康状态已刷新');
  }

  function onVisibilityChange() {
    if (!document.hidden) loadHealth();
  }

  // ---------- 加载 / 保存 ----------
  // 兼容两种响应形状：契约里的 {capability:{...}} 映射，以及客服引擎实际返回的 [{capability,...}] 数组。
  function normalizeBindings(value: any): Record<string, any> {
    if (Array.isArray(value)) {
      return Object.fromEntries(
        value
          .filter((item) => item && item.capability)
          .map((item) => [item.capability, item])
      );
    }
    return value && typeof value === 'object' ? value : {};
  }

  function requirementHint(record: any) {
    return record.kind === 'image' ? '需要生图类型供应商' : '需要文本类型供应商';
  }

  function onProviderChange(record: any) {
    // 换供应商后原模型大概率不属于新上游，清空让管理员重新选择。
    record.model = '';
  }

  function applyBindings(bindings: any) {
    const map = normalizeBindings(bindings);
    rows.value = rows.value.map((row) => {
      const binding = map[row.capability];
      if (!binding) return row;
      return {
        ...row,
        provider_id: binding.provider_id || '',
        model: binding.model || '',
        params: binding.params || {},
      };
    });
  }

  async function load() {
    loading.value = true;
    targetsError.value = '';
    try {
      const [providerPayload, bindingPayload] = await Promise.all([
        fetchAiProviders(),
        fetchAiBindings(),
      ]);
      providers.value = providerPayload.providers || [];
      const map = normalizeBindings(bindingPayload.bindings);
      rows.value = CAPABILITIES.map((meta) => {
        const binding = map[meta.capability] || {};
        return {
          ...meta,
          provider_id: binding.provider_id || '',
          model: binding.model || '',
          params: binding.params || {},
        };
      });
      try {
        const targetPayload = await fetchAiBindingTargets();
        applyTargets(targetPayload?.targets || []);
        targetsLoaded.value = true;
      } catch (error: any) {
        // 加载失败时保留空链但锁住保存：整体替换语义下用空数组提交会清空后端候选。
        targetsLoaded.value = false;
        targetsError.value = `候选列表加载失败：${errorMessage(error)}。为避免误清空，保存已禁用。`;
      }
    } finally {
      loading.value = false;
    }
  }

  function toPayload(record: any) {
    return {
      capability: record.capability,
      provider_id: record.provider_id || '',
      model: record.model || '',
      params: record.params || {},
    };
  }

  async function persistTargets(label: string) {
    if (!targetsLoaded.value) {
      saveError.value = '候选列表未成功加载，为避免误清空，暂不能保存。';
      Message.warning(saveError.value);
      return;
    }
    const invalid = validateTargets();
    if (invalid) {
      saveError.value = invalid;
      Message.warning(invalid);
      return;
    }
    saveError.value = '';
    try {
      const result = await updateAiBindingTargets(collectTargets(), {
        skipErrorMessage: true,
      });
      // 后端是权威结果（整体替换），成功后用响应回填。
      applyTargets(result?.targets || []);
      Message.success(label);
    } catch (error: any) {
      saveError.value = errorMessage(error);
      Message.error(saveError.value);
    }
  }

  async function saveCapability(group: any) {
    savingCapability.value = group.capability;
    try {
      await persistTargets(`${group.label} 候选已保存`);
    } finally {
      savingCapability.value = '';
    }
  }

  async function saveAllTargets() {
    savingAll.value = true;
    try {
      await persistTargets('全部能力候选已保存');
    } finally {
      savingAll.value = false;
    }
  }

  async function saveRow(record: any) {
    savingCapability.value = record.capability;
    try {
      const result = await updateAiBindings([toPayload(record)]);
      if (result?.bindings) applyBindings(result.bindings);
      Message.success(`${record.label} 绑定已保存`);
    } finally {
      savingCapability.value = '';
    }
  }

  async function saveAllBindings() {
    savingBindings.value = true;
    try {
      const result = await updateAiBindings(rows.value.map(toPayload));
      if (result?.bindings) applyBindings(result.bindings);
      Message.success('全部能力绑定已保存');
    } finally {
      savingBindings.value = false;
    }
  }

  onMounted(async () => {
    await load();
    await loadHealth();
    tickTimer = window.setInterval(() => {
      nowMs.value = Date.now();
    }, 1000);
    healthTimer = window.setInterval(() => {
      if (document.hidden) return; // 页面不可见时暂停轮询
      loadHealth();
    }, HEALTH_INTERVAL_MS);
    document.addEventListener('visibilitychange', onVisibilityChange);
  });

  onUnmounted(() => {
    if (tickTimer) window.clearInterval(tickTimer);
    if (healthTimer) window.clearInterval(healthTimer);
    document.removeEventListener('visibilitychange', onVisibilityChange);
  });
</script>

<style scoped>
  .tips {
    margin-bottom: 14px;
  }
  .tips-list {
    margin: 0;
    padding-left: 18px;
    line-height: 1.9;
  }
  .chain-card,
  .health-card {
    margin-bottom: 14px;
  }
  .save-error {
    margin-bottom: 12px;
    white-space: pre-wrap;
  }
  .chain-body {
    display: block;
  }
  .cap-card {
    border: 1px solid var(--color-neutral-3);
    border-radius: 6px;
    padding: 12px 14px;
    margin-bottom: 12px;
    background: var(--color-bg-2);
  }
  .cap-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .cap-title {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .cap-count {
    font-size: 12px;
  }
  .empty-hint {
    margin-top: 10px;
    color: var(--color-text-3);
    font-size: 12px;
  }
  .candidate-list {
    margin-top: 10px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .candidate-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 8px 10px;
    border-radius: 4px;
    background: var(--color-fill-1);
  }
  .priority-tag {
    min-width: 84px;
    text-align: center;
  }
  .candidate-provider {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 200px;
  }
  .provider-name {
    font-weight: 500;
  }
  /* Arco 的 Select 根节点不吃 scoped 属性（inheritAttrs:false），只能用 :deep。
     select 默认 width:100% 且作为 flex 项会被撑满，这里固定宽度让候选信息单行排列。 */
  .candidate-row :deep(.candidate-model) {
    flex: 0 0 220px;
    width: 220px;
  }
  .candidate-switch {
    flex: 0 0 auto;
  }
  .candidate-actions {
    margin-left: auto;
    flex: 0 0 auto;
  }
  .add-row {
    margin-top: 10px;
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .add-row :deep(.add-provider) {
    flex: 0 0 240px;
    width: 240px;
  }
  .add-row :deep(.add-model) {
    flex: 0 0 220px;
    width: 220px;
  }
  .health-hint {
    font-size: 12px;
  }
  .health-error {
    margin-bottom: 12px;
  }
  .last-error {
    display: inline-block;
    max-width: 420px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: bottom;
    color: var(--color-text-3);
  }
  .footer-hint {
    display: block;
    margin-top: 12px;
    font-size: 12px;
  }
</style>
