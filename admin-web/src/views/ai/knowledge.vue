<template>
  <div class="knowledge-page">
    <a-card :bordered="false">
      <template #title>知识库</template>
      <template #extra>
        <a-space wrap>
          <a-button @click="openSettings()">知识库设置</a-button>
          <a-button type="primary" @click="openBaseEditor()">新增知识库</a-button>
        </a-space>
      </template>
      <a-alert class="tips" type="info">
        知识库为 AI 与客服提供可检索的问答素材：条目按关键词 / 内容命中后才会被引用。
        命中分数低于「知识库设置」里的阈值时不会用于回答（当前阈值
        {{ settings.score_threshold }}，最多引用 {{ settings.max_entries }} 条）。
      </a-alert>

      <a-row :gutter="16">
        <a-col :xs="24" :lg="8" :xl="7">
          <a-card class="pane" title="知识库列表" size="small">
            <a-spin :loading="basesLoading" class="pane-body">
              <a-empty v-if="!bases.length" description="还没有知识库" />
              <ul v-else class="base-list">
                <li
                  v-for="base in bases"
                  :key="base.id"
                  class="base-item"
                  :class="{ active: base.id === activeBaseId }"
                  @click="selectBase(base.id)"
                >
                  <div class="base-head">
                    <strong class="base-name" :title="base.name">{{ base.name }}</strong>
                    <a-tag size="small" :color="base.enabled ? 'green' : 'gray'">
                      {{ base.enabled ? '启用' : '停用' }}
                    </a-tag>
                  </div>
                  <div class="base-desc">{{ base.description || '（无说明）' }}</div>
                  <div class="base-meta">
                    <span>{{ Number(base.entry_count || 0) }} 条条目</span>
                    <span>排序 {{ base.sort_order ?? 0 }}</span>
                    <span class="base-actions">
                      <a-link @click.stop="openBaseEditor(base)">编辑</a-link>
                      <a-link status="danger" @click.stop="removeBase(base)">删除</a-link>
                    </span>
                  </div>
                </li>
              </ul>
            </a-spin>
          </a-card>
        </a-col>

        <a-col :xs="24" :lg="16" :xl="17">
          <a-card class="pane" size="small">
            <template #title>
              <span v-if="activeBase">条目：{{ activeBase.name }}</span>
              <span v-else>条目</span>
            </template>
            <template #extra>
              <a-space wrap>
                <a-input-search
                  v-model="query"
                  class="entry-search"
                  placeholder="搜索标题 / 内容 / 关键词"
                  allow-clear
                  @search="onSearch"
                  @clear="onSearch"
                  @press-enter="onSearch"
                />
                <a-button type="primary" :disabled="!activeBaseId" @click="openEntryEditor()">
                  新增条目
                </a-button>
                <a-button :disabled="!activeBaseId" @click="openImport()">批量导入</a-button>
              </a-space>
            </template>

            <a-empty v-if="!activeBaseId" description="请先在左侧选择一个知识库" />
            <template v-else>
              <a-table
                :data="entries"
                :loading="entriesLoading"
                :pagination="pagination"
                row-key="id"
                @page-change="onPageChange"
                @page-size-change="onPageSizeChange"
              >
                <template #columns>
                  <a-table-column title="标题">
                    <template #cell="{ record }">
                      <div><strong>{{ record.title }}</strong></div>
                      <a-typography-text type="secondary" class="entry-preview">
                        {{ preview(record.content) }}
                      </a-typography-text>
                    </template>
                  </a-table-column>
                  <a-table-column title="分类" :width="110">
                    <template #cell="{ record }">{{ record.category || '-' }}</template>
                  </a-table-column>
                  <a-table-column title="关键词" :width="220">
                    <template #cell="{ record }">
                      <a-space wrap>
                        <a-tag
                          v-for="word in record.keywords || []"
                          :key="word"
                          size="small"
                        >
                          {{ word }}
                        </a-tag>
                        <span v-if="!(record.keywords || []).length">-</span>
                      </a-space>
                    </template>
                  </a-table-column>
                  <a-table-column title="优先级" :width="80">
                    <template #cell="{ record }">{{ record.priority ?? 0 }}</template>
                  </a-table-column>
                  <a-table-column title="启用" :width="80">
                    <template #cell="{ record }">
                      <a-tag :color="record.enabled ? 'green' : 'gray'">
                        {{ record.enabled ? '启用' : '停用' }}
                      </a-tag>
                    </template>
                  </a-table-column>
                  <a-table-column title="更新时间" :width="170">
                    <template #cell="{ record }">{{ localTime(record.updated_at) }}</template>
                  </a-table-column>
                  <a-table-column title="操作" :width="120">
                    <template #cell="{ record }">
                      <a-space>
                        <a-link @click="openEntryEditor(record)">编辑</a-link>
                        <a-link status="danger" @click="removeEntry(record)">删除</a-link>
                      </a-space>
                    </template>
                  </a-table-column>
                </template>
              </a-table>
            </template>
          </a-card>
        </a-col>
      </a-row>

      <a-card class="pane search-pane" title="检索预览" size="small">
        <a-alert class="tips" type="info">
          低于阈值的不会用于回答：只有分数 ≥ {{ settings.score_threshold }} 的命中才会被 AI 引用，
          最多引用 {{ settings.max_entries }} 条。这里可以先用真实的客户提问验证知识能否被检索到。
        </a-alert>
        <a-space wrap class="search-bar">
          <a-input
            v-model="searchForm.query"
            class="search-input"
            placeholder="输入一句客户问题，例如：你们支持哪些付款方式？"
            allow-clear
            @press-enter="runSearch"
          />
          <a-input-number
            v-model="searchForm.limit"
            :min="1"
            :max="20"
            class="search-limit"
          />
          <a-checkbox v-model="searchForm.onlyActiveBase">
            仅检索当前知识库
          </a-checkbox>
          <a-button type="primary" :loading="searchLoading" @click="runSearch">检索</a-button>
        </a-space>
        <a-empty v-if="searched && !hits.length" description="没有命中任何条目" />
        <div v-else-if="hits.length" class="hit-list">
          <div v-for="hit in hits" :key="hit.entry_id || hit.title" class="hit-item">
            <div class="hit-head">
              <strong>{{ hit.title }}</strong>
              <a-tag color="arcoblue">分数 {{ scoreText(hit.score) }}</a-tag>
              <a-tag v-if="belowThreshold(hit.score)" color="orange">低于阈值</a-tag>
              <a-typography-text type="secondary" class="hit-base">
                {{ baseName(hit.base_id) }}
              </a-typography-text>
            </div>
            <div class="hit-matched">
              命中词：
              <a-tag
                v-for="word in hit.matched || []"
                :key="word"
                size="small"
                color="green"
              >
                {{ word }}
              </a-tag>
              <span v-if="!(hit.matched || []).length">-</span>
            </div>
          </div>
        </div>
      </a-card>
    </a-card>

    <a-modal
      v-model:visible="baseEditorVisible"
      :title="baseForm.id ? '编辑知识库' : '新增知识库'"
      :ok-loading="baseSaving"
      @ok="saveBase"
    >
      <a-form :model="baseForm" layout="vertical">
        <a-form-item label="名称" required>
          <a-input v-model="baseForm.name" placeholder="例如：支付与物流常见问题" />
        </a-form-item>
        <a-form-item label="说明">
          <a-textarea
            v-model="baseForm.description"
            :auto-size="{ minRows: 2, maxRows: 4 }"
            placeholder="简要说明这个知识库收录什么内容"
          />
        </a-form-item>
        <a-grid :cols="2" :col-gap="12">
          <a-grid-item>
            <a-form-item label="排序">
              <a-input-number v-model="baseForm.sort_order" :min="-10000" :max="10000" />
            </a-form-item>
          </a-grid-item>
          <a-grid-item>
            <a-form-item label="启用">
              <a-switch v-model="baseForm.enabled" />
            </a-form-item>
          </a-grid-item>
        </a-grid>
      </a-form>
    </a-modal>

    <a-drawer
      v-model:visible="entryEditorVisible"
      :width="620"
      :title="entryForm.id ? '编辑条目' : '新增条目'"
      :ok-loading="entrySaving"
      ok-text="保存"
      cancel-text="取消"
      unmount-on-close
      @ok="saveEntry"
    >
      <a-form :model="entryForm" layout="vertical">
        <a-form-item label="所属知识库">
          <a-select
            v-model="entryForm.base_id"
            :options="baseOptions"
            :disabled="Boolean(entryForm.id)"
          />
        </a-form-item>
        <a-form-item label="标题" required>
          <a-input v-model="entryForm.title" placeholder="一句话概括这条知识" />
        </a-form-item>
        <a-form-item label="内容" required>
          <a-textarea
            v-model="entryForm.content"
            :auto-size="{ minRows: 5, maxRows: 12 }"
            placeholder="回答正文，可包含多条要点"
          />
        </a-form-item>
        <a-form-item label="关键词">
          <a-input-tag
            v-model="entryForm.keywords"
            placeholder="输入后回车添加，例如：付款方式、PayPal"
            allow-clear
          />
          <a-typography-text type="secondary" class="field-hint">
            关键词命中会提升该条目的检索分数；也可以只用内容匹配。
          </a-typography-text>
        </a-form-item>
        <a-form-item label="分类">
          <a-input v-model="entryForm.category" placeholder="例如：售后 / 支付" />
        </a-form-item>
        <a-grid :cols="2" :col-gap="12">
          <a-grid-item>
            <a-form-item label="优先级">
              <a-input-number v-model="entryForm.priority" :min="-1000" :max="1000" />
            </a-form-item>
          </a-grid-item>
          <a-grid-item>
            <a-form-item label="启用">
              <a-switch v-model="entryForm.enabled" />
            </a-form-item>
          </a-grid-item>
        </a-grid>
        <a-typography-text type="secondary" class="field-hint">
          优先级数值越大，在分数接近时越优先被引用。
        </a-typography-text>
      </a-form>
    </a-drawer>

    <a-modal
      v-model:visible="importVisible"
      title="批量导入条目"
      :ok-loading="importSubmitting"
      ok-text="导入"
      @ok="submitImport"
    >
      <a-alert class="tips" type="info">
        粘贴 JSON 数组，或上传 .json 文件。字段：title、content、keywords（数组或逗号分隔）、
        category、priority。导入前会先本地校验并预览条数。
      </a-alert>
      <a-space class="import-bar" wrap>
        <a-upload
          :auto-upload="false"
          :show-file-list="false"
          accept=".json,application/json"
          @change="onImportFile"
        >
          <a-button>选择 .json 文件</a-button>
        </a-upload>
        <a-button size="small" @click="fillImportSample">填入示例</a-button>
      </a-space>
      <a-textarea
        v-model="importText"
        class="import-text"
        :auto-size="{ minRows: 8, maxRows: 16 }"
        placeholder='[{"title":"付款方式","content":"支持信用卡、PayPal 与银行转账。","keywords":["付款方式","PayPal"],"category":"支付","priority":10}]'
      />
      <a-alert v-if="importPreview.error" type="error" class="import-result">
        {{ importPreview.error }}
      </a-alert>
      <a-alert v-else type="success" class="import-result">
        已解析 {{ importPreview.count }} 条，可导入到「{{ activeBase?.name || '-' }}」。
      </a-alert>
    </a-modal>

    <a-drawer
      v-model:visible="settingsVisible"
      :width="560"
      title="知识库设置"
      :ok-loading="settingsSaving"
      ok-text="保存"
      cancel-text="取消"
      unmount-on-close
      @ok="saveSettings"
    >
      <a-form :model="settingsForm" layout="vertical">
        <a-form-item label="知识库总开关">
          <a-switch v-model="settingsForm.enabled" />
          <a-typography-text type="secondary" class="field-hint">
            关闭后 AI 不再检索知识库，行为等同于没有任何知识条目。
          </a-typography-text>
        </a-form-item>
        <a-form-item label="库外行为">
          <a-radio-group v-model="settingsForm.unknown_action" type="button">
            <a-radio value="refuse">拒答</a-radio>
            <a-radio value="handoff">转人工</a-radio>
          </a-radio-group>
          <a-typography-text type="secondary" class="field-hint">
            知识库里没有可用命中时：拒答=按下面的文案回复；转人工=直接转接人工客服。
          </a-typography-text>
        </a-form-item>
        <a-form-item label="拒答文案">
          <a-textarea
            v-model="settingsForm.refuse_reply"
            :auto-size="{ minRows: 3, maxRows: 6 }"
            placeholder="抱歉，我暂时没有查到相关信息，可以为您转接人工客服。"
          />
        </a-form-item>
        <a-form-item label="命中阈值（0–1，步进 0.05）">
          <a-space>
            <a-slider
              v-model="settingsForm.score_threshold"
              :min="0"
              :max="1"
              :step="0.05"
              class="threshold-slider"
            />
            <a-input-number
              v-model="settingsForm.score_threshold"
              :min="0"
              :max="1"
              :step="0.05"
            />
          </a-space>
          <a-typography-text type="secondary" class="field-hint">
            低于该分数的命中不会用于回答；调高更严格，调低更宽松。
          </a-typography-text>
        </a-form-item>
        <a-form-item label="最多引用条数（1–10）">
          <a-input-number v-model="settingsForm.max_entries" :min="1" :max="10" />
        </a-form-item>
      </a-form>
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import {
    createKnowledgeBase,
    createKnowledgeEntry,
    deleteKnowledgeBase,
    deleteKnowledgeEntry,
    fetchKnowledgeBases,
    fetchKnowledgeEntries,
    fetchKnowledgeSettings,
    importKnowledgeEntries,
    searchKnowledge,
    updateKnowledgeBase,
    updateKnowledgeEntry,
    updateKnowledgeSettings,
  } from '@/api/admin';

  // 与服务端默认值保持一致的设置默认项（后端未落盘时页面仍可用）。
  // 阈值 / 条数与 servicebridge domain.DefaultKnowledgeScoreThreshold(0.35)、
  // DefaultKnowledgeMaxEntries(3) 对齐，库外行为默认 handoff。
  const DEFAULT_SETTINGS = {
    unknown_action: 'handoff',
    refuse_reply: '',
    score_threshold: 0.35,
    max_entries: 3,
    enabled: true,
  };

  const bases = ref<any[]>([]);
  const basesLoading = ref(false);
  const activeBaseId = ref('');
  const query = ref('');
  const entries = ref<any[]>([]);
  const entriesLoading = ref(false);
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
    pageSizeOptions: [10, 20, 50],
  });

  const baseEditorVisible = ref(false);
  const baseSaving = ref(false);
  const baseForm = reactive({
    id: '',
    name: '',
    description: '',
    enabled: true,
    sort_order: 0,
  });

  const entryEditorVisible = ref(false);
  const entrySaving = ref(false);
  const entryForm = reactive<any>({
    id: '',
    base_id: '',
    title: '',
    content: '',
    keywords: [] as string[],
    category: '',
    priority: 0,
    enabled: true,
  });

  const importVisible = ref(false);
  const importSubmitting = ref(false);
  const importText = ref('');
  const importPreview = reactive({ count: 0, error: '', entries: [] as any[] });

  const searchForm = reactive({ query: '', limit: 5, onlyActiveBase: true });
  const searchLoading = ref(false);
  const searched = ref(false);
  const hits = ref<any[]>([]);

  const settingsVisible = ref(false);
  const settingsSaving = ref(false);
  const settings = reactive({ ...DEFAULT_SETTINGS });
  const settingsForm = reactive({ ...DEFAULT_SETTINGS });

  const activeBase = computed(
    () => bases.value.find((item) => item.id === activeBaseId.value) || null
  );
  const baseOptions = computed(() =>
    bases.value.map((item) => ({ label: item.name, value: item.id }))
  );

  function localTime(value: string) {
    return value
      ? new Date(value).toLocaleString('zh-CN', { hour12: false })
      : '-';
  }
  function preview(value: string) {
    const text = String(value || '').replace(/\s+/g, ' ').trim();
    return text.length > 60 ? `${text.slice(0, 60)}…` : text || '-';
  }
  function scoreText(value: number) {
    const score = Number(value);
    return Number.isFinite(score) ? score.toFixed(3) : '-';
  }
  function belowThreshold(value: number) {
    const score = Number(value);
    return Number.isFinite(score) && score < Number(settings.score_threshold);
  }
  function baseName(id: string) {
    return bases.value.find((item) => item.id === id)?.name || id || '-';
  }
  function normalizeKeywords(value: any): string[] {
    if (Array.isArray(value)) {
      return value.map((item) => String(item).trim()).filter(Boolean);
    }
    if (typeof value === 'string') {
      return value
        .split(/[,，\n]/)
        .map((item) => item.trim())
        .filter(Boolean);
    }
    return [];
  }

  // ---------- 知识库 ----------
  async function loadBases(keepSelection = true) {
    basesLoading.value = true;
    try {
      const payload = await fetchKnowledgeBases();
      bases.value = payload?.bases || [];
      const exists = bases.value.some((item) => item.id === activeBaseId.value);
      if (!keepSelection || !exists) {
        activeBaseId.value = bases.value[0]?.id || '';
        query.value = '';
        pagination.current = 1;
      }
    } catch {
      // 失败提示由 axios 拦截器统一给出，这里保留空列表避免页面崩掉。
    } finally {
      basesLoading.value = false;
    }
  }

  function selectBase(id: string) {
    if (activeBaseId.value === id) return;
    activeBaseId.value = id;
    query.value = '';
    pagination.current = 1;
    searched.value = false;
    hits.value = [];
    loadEntries();
  }

  function openBaseEditor(record?: any) {
    Object.assign(baseForm, {
      id: record?.id || '',
      name: record?.name || '',
      description: record?.description || '',
      enabled: record?.enabled ?? true,
      sort_order: Number(record?.sort_order || 0),
    });
    baseEditorVisible.value = true;
  }

  async function saveBase() {
    if (!baseForm.name.trim()) {
      Message.warning('请填写知识库名称');
      return;
    }
    const payload = {
      name: baseForm.name.trim(),
      description: baseForm.description || '',
      enabled: baseForm.enabled,
      sort_order: Number(baseForm.sort_order || 0),
    };
    baseSaving.value = true;
    try {
      if (baseForm.id) {
        await updateKnowledgeBase(baseForm.id, payload);
        Message.success('知识库已保存');
      } else {
        const created = await createKnowledgeBase(payload);
        const newId = created?.base?.id || created?.id;
        if (newId) activeBaseId.value = newId;
        Message.success('知识库已创建');
      }
      baseEditorVisible.value = false;
      await loadBases();
      await loadEntries();
    } catch {
      // 失败时保留弹窗内容，方便修正后重试。
    } finally {
      baseSaving.value = false;
    }
  }

  function removeBase(record: any) {
    Modal.confirm({
      title: '删除知识库',
      content: `确定删除「${record.name}」？该库下的 ${
        Number(record.entry_count || 0)
      } 条条目会一并删除，且不可恢复。`,
      okText: '删除',
      okButtonProps: { status: 'danger' },
      onOk: async () => {
        try {
          await deleteKnowledgeBase(record.id);
          if (activeBaseId.value === record.id) {
            activeBaseId.value = '';
          }
          Message.success('知识库已删除');
          await loadBases();
          await loadEntries();
        } catch {
          // 拦截器已提示。
        }
      },
    });
  }

  // ---------- 条目 ----------
  async function loadEntries() {
    if (!activeBaseId.value) {
      entries.value = [];
      pagination.total = 0;
      return;
    }
    entriesLoading.value = true;
    try {
      const payload = await fetchKnowledgeEntries({
        base_id: activeBaseId.value,
        q: query.value.trim() || undefined,
        limit: pagination.pageSize,
        offset: (pagination.current - 1) * pagination.pageSize,
      });
      entries.value = payload?.entries || [];
      pagination.total = Number(payload?.total || 0);
    } catch {
      entries.value = [];
      pagination.total = 0;
    } finally {
      entriesLoading.value = false;
    }
  }

  function onSearch() {
    pagination.current = 1;
    loadEntries();
  }
  function onPageChange(page: number) {
    pagination.current = page;
    loadEntries();
  }
  function onPageSizeChange(size: number) {
    pagination.pageSize = size;
    pagination.current = 1;
    loadEntries();
  }

  function openEntryEditor(record?: any) {
    Object.assign(entryForm, {
      id: record?.id || '',
      base_id: record?.base_id || activeBaseId.value,
      title: record?.title || '',
      content: record?.content || '',
      keywords: normalizeKeywords(record?.keywords),
      category: record?.category || '',
      priority: Number(record?.priority || 0),
      enabled: record?.enabled ?? true,
    });
    entryEditorVisible.value = true;
  }

  async function saveEntry() {
    if (!entryForm.title.trim()) {
      Message.warning('请填写条目标题');
      return;
    }
    if (!entryForm.content.trim()) {
      Message.warning('请填写条目内容');
      return;
    }
    const payload = {
      base_id: entryForm.base_id || activeBaseId.value,
      title: entryForm.title.trim(),
      content: entryForm.content,
      keywords: normalizeKeywords(entryForm.keywords),
      category: entryForm.category || '',
      priority: Number(entryForm.priority || 0),
      enabled: entryForm.enabled,
    };
    entrySaving.value = true;
    try {
      if (entryForm.id) {
        await updateKnowledgeEntry(entryForm.id, payload);
        Message.success('条目已保存');
      } else {
        await createKnowledgeEntry(payload);
        Message.success('条目已创建');
      }
      entryEditorVisible.value = false;
      await loadEntries();
      await loadBases();
    } catch {
      // 失败时保留抽屉内容。
    } finally {
      entrySaving.value = false;
    }
  }

  function removeEntry(record: any) {
    Modal.confirm({
      title: '删除条目',
      content: `确定删除「${record.title}」？删除后无法恢复。`,
      okText: '删除',
      okButtonProps: { status: 'danger' },
      onOk: async () => {
        try {
          await deleteKnowledgeEntry(record.id);
          Message.success('条目已删除');
          await loadEntries();
          await loadBases();
        } catch {
          // 拦截器已提示。
        }
      },
    });
  }

  // ---------- 批量导入 ----------
  function openImport() {
    importText.value = '';
    importVisible.value = true;
  }

  function fillImportSample() {
    importText.value = JSON.stringify(
      [
        {
          title: '付款方式',
          content: '支持信用卡、PayPal 与银行转账。',
          keywords: ['付款方式', 'PayPal'],
          category: '支付',
          priority: 10,
        },
        {
          title: '发货时效',
          content: '现货商品在付款后 48 小时内发出。',
          keywords: ['发货', '时效'],
          category: '物流',
          priority: 5,
        },
      ],
      null,
      2
    );
  }

  // 本地校验：JSON 合法 + 顶层数组 + 每项含 title / content，通过后给出预览条数。
  function parseImportText(text: string) {
    const raw = String(text || '').trim();
    if (!raw) return { entries: [] as any[], error: '请先粘贴 JSON 或上传 .json 文件' };
    let data: any;
    try {
      data = JSON.parse(raw);
    } catch (error) {
      return { entries: [] as any[], error: `JSON 解析失败：${(error as Error).message}` };
    }
    if (!Array.isArray(data)) {
      return { entries: [] as any[], error: '顶层必须是数组，例如 [{ "title": "…" }]' };
    }
    if (!data.length) return { entries: [] as any[], error: '数组为空，没有可导入的条目' };
    const list: any[] = [];
    for (let index = 0; index < data.length; index += 1) {
      const item = data[index];
      if (!item || typeof item !== 'object' || Array.isArray(item)) {
        return { entries: [] as any[], error: `第 ${index + 1} 项不是对象` };
      }
      const title = String(item.title ?? '').trim();
      const content = String(item.content ?? '').trim();
      if (!title) return { entries: [] as any[], error: `第 ${index + 1} 项缺少 title` };
      if (!content) return { entries: [] as any[], error: `第 ${index + 1} 项缺少 content` };
      list.push({
        title,
        content,
        keywords: normalizeKeywords(item.keywords),
        category: String(item.category ?? '').trim(),
        priority: Number.isFinite(Number(item.priority)) ? Number(item.priority) : 0,
      });
    }
    return { entries: list, error: '' };
  }

  function refreshImportPreview() {
    const result = parseImportText(importText.value);
    importPreview.error = result.error;
    importPreview.entries = result.entries;
    importPreview.count = result.entries.length;
  }

  function onImportFile(fileList: any[]) {
    const file = fileList?.[fileList.length - 1]?.file;
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      importText.value = String(reader.result || '');
      refreshImportPreview();
    };
    reader.onerror = () => {
      Message.error('文件读取失败，请重试或直接粘贴 JSON');
    };
    reader.readAsText(file);
  }

  async function submitImport() {
    if (!activeBaseId.value) {
      Message.warning('请先选择要导入的知识库');
      return;
    }
    refreshImportPreview();
    if (importPreview.error) {
      Message.warning(importPreview.error);
      return;
    }
    importSubmitting.value = true;
    try {
      const payload = await importKnowledgeEntries(
        activeBaseId.value,
        importPreview.entries
      );
      const count = Number(payload?.imported ?? importPreview.entries.length);
      Message.success(`已导入 ${count} 条条目`);
      importVisible.value = false;
      await loadEntries();
      await loadBases();
    } catch {
      // 拦截器已提示。
    } finally {
      importSubmitting.value = false;
    }
  }

  watch(importText, refreshImportPreview);

  // ---------- 检索预览 ----------
  async function runSearch() {
    const text = searchForm.query.trim();
    if (!text) {
      Message.warning('请输入要检索的客户问题');
      return;
    }
    const body: { query: string; base_ids?: string[]; limit: number } = {
      query: text,
      limit: Number(searchForm.limit || 5),
    };
    if (searchForm.onlyActiveBase && activeBaseId.value) {
      body.base_ids = [activeBaseId.value];
    }
    searchLoading.value = true;
    try {
      const payload = await searchKnowledge(body);
      hits.value = payload?.hits || [];
      searched.value = true;
    } catch {
      hits.value = [];
      searched.value = true;
    } finally {
      searchLoading.value = false;
    }
  }

  // ---------- 知识库设置 ----------
  function applySettings(value: any) {
    Object.assign(settingsForm, {
      unknown_action: value?.unknown_action === 'refuse' ? 'refuse' : 'handoff',
      refuse_reply: String(value?.refuse_reply ?? ''),
      score_threshold: Number.isFinite(Number(value?.score_threshold))
        ? Number(value.score_threshold)
        : DEFAULT_SETTINGS.score_threshold,
      max_entries: Number.isFinite(Number(value?.max_entries))
        ? Number(value.max_entries)
        : DEFAULT_SETTINGS.max_entries,
      enabled: value?.enabled ?? true,
    });
    Object.assign(settings, settingsForm);
  }

  async function loadSettings() {
    try {
      const payload = await fetchKnowledgeSettings();
      applySettings(payload?.settings);
    } catch {
      applySettings(DEFAULT_SETTINGS);
    }
  }

  function openSettings() {
    Object.assign(settingsForm, settings);
    settingsVisible.value = true;
  }

  function clamp(value: number, min: number, max: number, fallback: number) {
    const number = Number(value);
    if (!Number.isFinite(number)) return fallback;
    return Math.min(max, Math.max(min, number));
  }

  async function saveSettings() {
    const payload = {
      unknown_action: settingsForm.unknown_action === 'refuse' ? 'refuse' : 'handoff',
      refuse_reply: settingsForm.refuse_reply || '',
      // 步进 0.05 会产生浮点尾差，统一保留两位小数并夹在 0–1。
      score_threshold: Number(
        clamp(settingsForm.score_threshold, 0, 1, 0.35).toFixed(2)
      ),
      max_entries: Math.round(clamp(settingsForm.max_entries, 1, 10, 3)),
      enabled: Boolean(settingsForm.enabled),
    };
    settingsSaving.value = true;
    try {
      const result = await updateKnowledgeSettings(payload);
      applySettings(result?.settings || payload);
      Message.success('知识库设置已保存');
      settingsVisible.value = false;
    } catch {
      // 失败时保留抽屉内容。
    } finally {
      settingsSaving.value = false;
    }
  }

  onMounted(async () => {
    await loadBases(false);
    await Promise.all([loadEntries(), loadSettings()]);
  });
</script>

<style scoped>
  .knowledge-page :deep(.arco-card) {
    border-radius: 6px;
  }
  .tips {
    margin-bottom: 14px;
  }
  .pane {
    margin-bottom: 16px;
  }
  .pane-body {
    display: block;
    min-height: 120px;
  }
  .base-list {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: 520px;
    overflow: auto;
  }
  .base-item {
    padding: 10px 12px;
    border-radius: 6px;
    border: 1px solid transparent;
    cursor: pointer;
    transition: background-color 0.15s ease;
  }
  .base-item + .base-item {
    margin-top: 4px;
  }
  .base-item:hover {
    background: var(--color-fill-1);
  }
  .base-item.active {
    background: var(--color-fill-2);
    border-color: rgb(var(--arcoblue-6));
  }
  .base-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .base-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .base-desc {
    margin-top: 4px;
    color: var(--color-text-3);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .base-meta {
    margin-top: 6px;
    display: flex;
    align-items: center;
    gap: 12px;
    color: var(--color-text-3);
    font-size: 12px;
  }
  .base-actions {
    margin-left: auto;
  }
  .entry-search {
    width: 240px;
  }
  .entry-preview {
    display: block;
    font-size: 12px;
    max-width: 420px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .search-pane {
    margin-top: 0;
  }
  .search-bar {
    margin-bottom: 12px;
  }
  .search-input {
    width: 420px;
  }
  .search-limit {
    width: 110px;
  }
  .hit-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .hit-item {
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
  }
  .hit-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .hit-base {
    font-size: 12px;
  }
  .hit-matched {
    margin-top: 6px;
    font-size: 12px;
    color: var(--color-text-3);
  }
  .field-hint {
    display: block;
    font-size: 12px;
    margin-top: 4px;
  }
  .import-bar {
    margin-bottom: 10px;
  }
  .import-text {
    font-family: Menlo, Consolas, monospace;
  }
  .import-result {
    margin-top: 10px;
  }
  .threshold-slider {
    width: 260px;
  }
</style>
