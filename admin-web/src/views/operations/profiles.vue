<template>
  <div class="profiles-page" data-testid="profiles-page">
    <a-card :bordered="false">
      <template #title>访客画像</template>
      <template #extra>
        <a-space>
          <a-button
            :loading="loading"
            data-testid="profiles-reload"
            @click="load"
          >
            刷新
          </a-button>
          <a-button
            type="primary"
            data-testid="profiles-settings-open"
            @click="settingsVisible = true"
          >
            画像设置
          </a-button>
        </a-space>
      </template>

      <a-alert class="tips" type="info">
        画像聚合访客的地理位置、语言占比与 AI 摘要；国籍是按依据加权得出的<strong>推测</strong>结果，不是事实。
        地理三级降级顺序：CDN 头 → 本地 GeoLite2 → 第三方 IP 库。
        {{ showFullIp === null ? '' : showFullIp ? '当前显示完整 IP。' : '当前已关闭完整 IP，IP 列为脱敏值。' }}
      </a-alert>

      <div class="filters">
        <a-form layout="inline">
          <a-form-item label="关键词">
            <div data-testid="profiles-q">
              <a-input-search
                v-model="filters.q"
                placeholder="访客 ID / IP / 摘要"
                allow-clear
                style="width: 220px"
                @search="applyFilters"
                @press-enter="applyFilters"
              />
            </div>
          </a-form-item>
          <a-form-item label="国家码">
            <div data-testid="profiles-country">
              <a-input
                v-model="filters.country"
                placeholder="如 CN / US"
                allow-clear
                style="width: 140px"
                @press-enter="applyFilters"
              />
            </div>
          </a-form-item>
          <a-form-item label="标签">
            <div data-testid="profiles-tag">
              <a-input
                v-model="filters.tag"
                placeholder="如 高意向"
                allow-clear
                style="width: 160px"
                @press-enter="applyFilters"
              />
            </div>
          </a-form-item>
          <a-form-item>
            <a-space>
              <a-button
                type="primary"
                data-testid="profiles-filter-apply"
                @click="applyFilters"
              >
                查询
              </a-button>
              <a-button data-testid="profiles-filter-reset" @click="resetFilters">
                重置
              </a-button>
            </a-space>
          </a-form-item>
        </a-form>
      </div>

      <a-alert v-if="loadError" class="banner" type="error" show-icon>
        {{ loadError }}
        <template #action>
          <a-button size="mini" data-testid="profiles-retry" @click="load">
            重试
          </a-button>
        </template>
      </a-alert>

      <a-table
        :data="profiles"
        :loading="loading"
        :pagination="false"
        :scroll="{ x: 1680 }"
        row-key="visitor_id"
        data-testid="profiles-table"
        @row-click="openDetail"
      >
        <template #columns>
          <a-table-column title="访客 ID" :width="190">
            <template #cell="{ record }">
              <a-space size="mini">
                <a-typography-text
                  class="mono"
                  :title="record.visitor_id"
                  :data-testid="`profiles-id-${record.visitor_id}`"
                >
                  {{ truncateId(record.visitor_id) }}
                </a-typography-text>
                <a-link
                  :data-testid="`profiles-copy-${record.visitor_id}`"
                  @click.stop="copyId(record.visitor_id)"
                >
                  复制
                </a-link>
              </a-space>
            </template>
          </a-table-column>
          <a-table-column title="国家 / 地区 / 城市" :width="190">
            <template #cell="{ record }">{{ regionText(record) }}</template>
          </a-table-column>
          <a-table-column title="时区" :width="130">
            <template #cell="{ record }">{{ record.timezone || '-' }}</template>
          </a-table-column>
          <a-table-column title="语言" :width="170">
            <template #cell="{ record }">
              <a-tooltip :content="languageTooltip(record.languages)">
                <span data-testid="profiles-languages">
                  {{ languageSummary(record.languages) }}
                </span>
              </a-tooltip>
            </template>
          </a-table-column>
          <a-table-column title="方言" :width="110">
            <template #cell="{ record }">{{ record.dialect || '-' }}</template>
          </a-table-column>
          <a-table-column title="推测国籍" :width="190">
            <template #cell="{ record }">
              <span v-if="record.nationality_guess" class="guess">
                <a-tag size="small" color="arcoblue">推测</a-tag>
                <span class="guess-code">{{ record.nationality_guess }}</span>
                <a-typography-text type="secondary" class="guess-confidence">
                  {{ confidencePercent(record.nationality_confidence) }}%
                </a-typography-text>
              </span>
              <span v-else>-</span>
            </template>
          </a-table-column>
          <a-table-column title="标签" :width="180">
            <template #cell="{ record }">
              <a-space v-if="asArray(record.tags).length" size="mini" wrap>
                <a-tag
                  v-for="tag in asArray(record.tags)"
                  :key="String(tag)"
                  size="small"
                  color="arcoblue"
                >
                  {{ tag }}
                </a-tag>
              </a-space>
              <span v-else>-</span>
            </template>
          </a-table-column>
          <a-table-column title="摘要" :width="240" ellipsis tooltip>
            <template #cell="{ record }">
              <span :data-testid="`profiles-summary-${record.visitor_id}`">
                {{ record.summary || '-' }}
              </span>
            </template>
          </a-table-column>
          <a-table-column title="访问次数" :width="100">
            <template #cell="{ record }">{{ Number(record.visits || 0) }}</template>
          </a-table-column>
          <a-table-column title="最近访问时间" :width="180">
            <template #cell="{ record }">
              {{ localTime(record.last_seen_at) }}
            </template>
          </a-table-column>
          <a-table-column title="IP" :width="150">
            <template #cell="{ record }">
              <!-- 受 show_full_ip 影响：后端返回什么就原样显示，前端不再二次脱敏。 -->
              <span :data-testid="`profiles-ip-${record.visitor_id}`">
                {{ record.ip_last || '-' }}
              </span>
            </template>
          </a-table-column>
          <a-table-column title="操作" :width="80">
            <template #cell="{ record }">
              <a-link
                :data-testid="`profiles-detail-${record.visitor_id}`"
                @click.stop="openDetail(record)"
              >
                详情
              </a-link>
            </template>
          </a-table-column>
        </template>
        <template #empty>
          <a-empty description="没有匹配的访客画像" />
        </template>
      </a-table>

      <div class="pagination">
        <a-pagination
          :current="page"
          :page-size="pageSize"
          :total="total"
          show-total
          @change="onPageChange"
        />
      </div>
    </a-card>

    <!-- T1 详情抽屉：完整画像 + 依据解释 + 证据消息 + 相关会话 -->
    <a-drawer
      v-model:visible="detailVisible"
      title="访客画像详情"
      :width="760"
      :footer="false"
      data-testid="profile-detail-drawer"
      @cancel="detail = null"
    >
      <a-spin :loading="detailLoading" style="width: 100%">
        <a-alert v-if="detailError" class="banner" type="error" show-icon>
          {{ detailError }}
          <template #action>
            <a-button size="mini" data-testid="profile-detail-retry" @click="reloadDetail">
              重试
            </a-button>
          </template>
        </a-alert>

        <template v-if="detail">
          <div class="detail-head">
            <div class="detail-id">
              <strong class="mono" :title="detail.visitor_id">{{ detail.visitor_id }}</strong>
              <a-link data-testid="profile-copy" @click="copyId(detail.visitor_id)">
                复制
              </a-link>
            </div>
            <a-button
              type="primary"
              size="small"
              :loading="refreshing"
              data-testid="profile-refresh"
              @click="refreshProfile"
            >
              重新分析
            </a-button>
          </div>

          <a-descriptions :column="2" size="small" bordered class="detail-desc">
            <a-descriptions-item label="国家 / 地区 / 城市">
              {{ regionText(detail) }}
            </a-descriptions-item>
            <a-descriptions-item label="时区">
              {{ detail.timezone || '-' }}
            </a-descriptions-item>
            <a-descriptions-item label="推测国籍">
              <span v-if="detail.nationality_guess" data-testid="profile-nationality">
                <a-tag size="small" color="arcoblue">推测</a-tag>
                <span class="guess-code">{{ detail.nationality_guess }}</span>
                <a-typography-text type="secondary">
                  置信度 {{ confidencePercent(detail.nationality_confidence) }}%
                </a-typography-text>
              </span>
              <span v-else>-</span>
            </a-descriptions-item>
            <a-descriptions-item label="访问次数">
              {{ Number(detail.visits || 0) }}
            </a-descriptions-item>
            <a-descriptions-item label="最近访问时间">
              {{ localTime(detail.last_seen_at) }}
            </a-descriptions-item>
            <a-descriptions-item label="方言">
              {{ detail.dialect || '-' }}
            </a-descriptions-item>
            <a-descriptions-item label="语码切换">
              {{ detail.code_switching ? '存在多语切换' : '未发现' }}
            </a-descriptions-item>
            <a-descriptions-item label="最近路径">
              {{ detail.last_path || '-' }}
            </a-descriptions-item>
            <a-descriptions-item label="IP" :span="2">
              <span data-testid="profile-ip">{{ detail.ip_last || '-' }}</span>
              <a-typography-text type="secondary" class="ip-mode">
                （{{ ipModeLabel }}，由「显示完整 IP」开关决定）
              </a-typography-text>
            </a-descriptions-item>
          </a-descriptions>

          <a-divider orientation="left">国籍推测依据</a-divider>
          <div data-testid="profile-basis">
            <a-space v-if="asArray(detail.nationality_basis).length" size="mini" wrap>
              <a-tag
                v-for="basis in asArray(detail.nationality_basis)"
                :key="String(basis)"
                color="orange"
              >
                {{ basisLabel(basis) }}
              </a-tag>
            </a-space>
            <a-typography-text v-else type="secondary">暂无依据记录</a-typography-text>
            <a-typography-text type="secondary" class="hint">
              依据优先级：CDN 头 &gt; 本地 GeoLite2 &gt; 第三方 IP 库 &gt; 语言 &gt; 时区；
              推测国籍不是事实，仅供运营参考。
            </a-typography-text>
          </div>

          <a-divider orientation="left">语言占比</a-divider>
          <div data-testid="profile-languages">
            <template v-if="languageRows(detail.languages).length">
              <div
                v-for="item in languageRows(detail.languages)"
                :key="item.code"
                class="lang-row"
              >
                <span class="lang-name">{{ item.name }}</span>
                <a-progress
                  :percent="item.percent"
                  :show-text="false"
                  class="lang-bar"
                />
                <span class="lang-count">{{ item.count }} 条 · {{ item.percent }}%</span>
              </div>
            </template>
            <a-typography-text v-else type="secondary">暂无语言数据</a-typography-text>
          </div>

          <a-divider orientation="left">标签与兴趣</a-divider>
          <div data-testid="profile-tags">
            <a-space
              v-if="asArray(detail.tags).length || asArray(detail.interests).length"
              size="mini"
              wrap
            >
              <a-tag
                v-for="tag in asArray(detail.tags)"
                :key="`tag-${tag}`"
                color="arcoblue"
              >
                {{ tag }}
              </a-tag>
              <a-tag
                v-for="interest in asArray(detail.interests)"
                :key="`interest-${interest}`"
                color="green"
              >
                {{ interest }}
              </a-tag>
            </a-space>
            <a-typography-text v-else type="secondary">暂无标签</a-typography-text>
          </div>

          <a-divider orientation="left">画像摘要</a-divider>
          <a-typography-paragraph class="summary" data-testid="profile-summary">
            {{ detail.summary || '暂无摘要' }}
          </a-typography-paragraph>

          <a-divider orientation="left">证据消息</a-divider>
          <div data-testid="profile-evidence">
            <template v-if="asArray(detail.evidence).length">
              <div
                v-for="item in asArray(detail.evidence)"
                :key="String(item.message_id)"
                class="evidence-row"
                :data-testid="`profile-evidence-${item.message_id}`"
              >
                <div class="evidence-text">{{ item.text || '（空消息）' }}</div>
                <small class="muted">{{ localTime(item.created_at) }}</small>
              </div>
            </template>
            <a-typography-text v-else type="secondary">暂无证据消息</a-typography-text>
          </div>

          <a-divider orientation="left">相关会话</a-divider>
          <div data-testid="profile-conversations">
            <template v-if="asArray(detail.conversations).length">
              <div
                v-for="item in asArray(detail.conversations)"
                :key="String(item.conversation_id)"
                class="conversation-row"
              >
                <router-link
                  class="conversation-link"
                  :data-testid="`profile-conversation-${item.conversation_id}`"
                  :to="{
                    path: '/operations/inquiries',
                    query: { conversation: item.conversation_id },
                  }"
                >
                  {{ item.conversation_id }}
                </router-link>
                <a-tag size="small" :color="statusColor(item.status)">
                  {{ statusLabel(item.status) }}
                </a-tag>
                <small class="muted">
                  最近消息 {{ localTime(item.last_message_at) }}
                </small>
              </div>
            </template>
            <a-typography-text v-else type="secondary">暂无相关会话</a-typography-text>
          </div>
        </template>
        <a-empty v-else-if="!detailLoading && !detailError" description="选择一条画像查看详情" />
      </a-spin>
    </a-drawer>

    <!-- T2 设置抽屉：字段与三级降级说明，放在画像页顶部入口 -->
    <a-drawer
      v-model:visible="settingsVisible"
      title="访客画像设置"
      :width="620"
      :footer="false"
      unmount-on-close
      data-testid="profiles-settings-drawer"
    >
      <VisitorProfileSettings
        v-if="settingsVisible"
        @saved="onSettingsSaved"
      />
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchVisitorProfile,
    fetchVisitorProfileSettings,
    fetchVisitorProfiles,
    refreshVisitorProfile,
  } from '@/api/admin';
  import VisitorProfileSettings from './components/VisitorProfileSettings.vue';

  // 语种基础码 → 中文名；未知码原样大写展示（与询盘页保持一致的映射）。
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

  // 国籍推测依据的中文解释；未知依据原样展示，避免后端新增依据时前端丢信息。
  const BASIS_LABELS: Record<string, string> = {
    ip_cdn: 'CDN 头（CF-IPCountry 等）',
    geoip: '本地 GeoLite2 库',
    third_party: '第三方 IP 库',
    language: '语言线索',
    timezone: '时区线索',
  };

  const STATUS_LABELS: Record<string, string> = {
    online: '在线',
    busy: '忙碌',
    offline: '离线',
    waiting: '待接入',
    ai_serving: 'AI 接待',
    human_requested: '等待人工',
    assigned: '人工服务',
    closed: '已结束',
  };

  const profiles = ref<any[]>([]);
  const loading = ref(false);
  const loadError = ref('');
  const total = ref(0);
  const page = ref(1);
  const pageSize = 20;
  // null = 尚未取到设置，避免在未知状态下误报「当前显示完整 IP」。
  const showFullIp = ref<boolean | null>(null);

  const filters = reactive({ q: '', country: '', tag: '' });

  const detailVisible = ref(false);
  const detail = ref<any>(null);
  const detailId = ref('');
  const detailLoading = ref(false);
  const detailError = ref('');
  const refreshing = ref(false);

  const settingsVisible = ref(false);

  const ipModeLabel = computed(() => {
    if (showFullIp.value === true) return '完整 IP';
    if (showFullIp.value === false) return '脱敏 IP';
    return 'IP 显示方式未知';
  });

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function asArray(value: any): any[] {
    if (Array.isArray(value)) return value;
    if (typeof value === 'string' && value.trim()) {
      try {
        const parsed = JSON.parse(value);
        return Array.isArray(parsed) ? parsed : [];
      } catch {
        return [];
      }
    }
    return [];
  }

  function languageName(code?: string) {
    const raw = String(code || '').trim().toLowerCase();
    if (!raw) return '';
    const base = raw.split(/[-_]/)[0];
    return LANGUAGE_NAMES[base] || base.toUpperCase();
  }

  function languageRows(languages: any) {
    let source: any = languages;
    if (typeof source === 'string') {
      try {
        source = JSON.parse(source);
      } catch {
        source = {};
      }
    }
    if (!source || typeof source !== 'object' || Array.isArray(source)) return [];
    const entries = Object.entries(source)
      .map(([code, count]) => ({ code, count: Number(count) || 0 }))
      .filter((item) => item.count > 0)
      .sort((a, b) => b.count - a.count);
    const sum = entries.reduce((acc, item) => acc + item.count, 0) || 1;
    return entries.map((item) => ({
      code: item.code,
      name: languageName(item.code),
      count: item.count,
      percent: Math.min(100, Math.round((item.count / sum) * 100)),
    }));
  }

  function languageSummary(languages: any) {
    const rows = languageRows(languages);
    if (!rows.length) return '-';
    const top = rows
      .slice(0, 2)
      .map((row) => row.name)
      .join('、');
    return rows.length > 2 ? `${top} 等 ${rows.length} 种` : top;
  }

  function languageTooltip(languages: any) {
    const rows = languageRows(languages);
    if (!rows.length) return '暂无语言数据';
    return rows.map((row) => `${row.name} ${row.count} 条（${row.percent}%）`).join('，');
  }

  function confidencePercent(value: any) {
    const parsed = Number(value);
    if (!Number.isFinite(parsed) || parsed <= 0) return 0;
    // 后端契约是 0~1 的比例（NUMERIC(3,2)）；若上游改成百分数也能平滑显示。
    return Math.max(0, Math.min(100, Math.round(parsed > 1 ? parsed : parsed * 100)));
  }

  function basisLabel(basis: any) {
    const key = String(basis || '');
    return BASIS_LABELS[key] || key || '未知依据';
  }

  function statusLabel(value: string) {
    return STATUS_LABELS[value] || value || '-';
  }

  function statusColor(value: string) {
    return (
      {
        waiting: 'orange',
        human_requested: 'orangered',
        assigned: 'green',
        ai_serving: 'arcoblue',
        closed: 'gray',
      }[value] || 'gray'
    );
  }

  function truncateId(value: unknown) {
    const raw = String(value ?? '');
    if (raw.length <= 16) return raw || '-';
    return `${raw.slice(0, 8)}…${raw.slice(-6)}`;
  }

  function regionText(record: any) {
    const parts = [record?.country, record?.region, record?.city]
      .map((item) => String(item ?? '').trim())
      .filter(Boolean);
    return parts.length ? parts.join(' / ') : '-';
  }

  function localTime(value: unknown) {
    const raw = String(value ?? '');
    if (!raw) return '-';
    const time = Date.parse(raw);
    if (!Number.isFinite(time)) return raw;
    return new Date(time).toLocaleString('zh-CN', { hour12: false });
  }

  function buildParams() {
    const params: Record<string, unknown> = {
      limit: pageSize,
      offset: (page.value - 1) * pageSize,
    };
    const q = filters.q.trim();
    const country = filters.country.trim();
    const tag = filters.tag.trim();
    if (q) params.q = q;
    if (country) params.country = country;
    if (tag) params.tag = tag;
    return params;
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const payload = await fetchVisitorProfiles(buildParams());
      profiles.value = asArray(payload?.profiles);
      total.value = Number(payload?.total ?? profiles.value.length) || 0;
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '访客画像加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  async function loadSettings() {
    try {
      const payload = await fetchVisitorProfileSettings();
      const settings = payload?.settings ?? payload ?? {};
      if (settings && settings.show_full_ip !== undefined) {
        showFullIp.value = Boolean(settings.show_full_ip);
      }
    } catch {
      // 设置读取失败不影响画像列表；顶部提示按默认「完整 IP」展示。
    }
  }

  function applyFilters() {
    page.value = 1;
    load();
  }

  function resetFilters() {
    filters.q = '';
    filters.country = '';
    filters.tag = '';
    page.value = 1;
    load();
  }

  function onPageChange(current: number) {
    page.value = current;
    load();
  }

  async function openDetail(record: any) {
    const id = String(record?.visitor_id ?? '');
    if (!id) return;
    detailVisible.value = true;
    detailId.value = id;
    detail.value = null;
    detailError.value = '';
    await loadDetail(id);
  }

  async function loadDetail(id: string) {
    detailLoading.value = true;
    detailError.value = '';
    try {
      const payload = await fetchVisitorProfile(id);
      detail.value = payload?.profile ?? payload ?? null;
    } catch (reason: any) {
      detailError.value = backendMessage(reason, '画像详情加载失败，请稍后重试。');
    } finally {
      detailLoading.value = false;
    }
  }

  function reloadDetail() {
    if (detailId.value) loadDetail(detailId.value);
  }

  async function refreshProfile() {
    if (!detailId.value) return;
    refreshing.value = true;
    try {
      const payload = await refreshVisitorProfile(detailId.value);
      const next = payload?.profile ?? payload ?? null;
      if (next) detail.value = next;
      Message.success('已重新分析访客画像');
      await load();
    } catch (reason: any) {
      // 拦截器已弹出后端 message；这里补一条抽屉内可见的错误，避免只有 toast。
      detailError.value = backendMessage(reason, '重新分析失败，请稍后重试。');
    } finally {
      refreshing.value = false;
    }
  }

  async function copyId(value: unknown) {
    const text = String(value ?? '');
    if (!text) return;
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const area = document.createElement('textarea');
        area.value = text;
        area.style.position = 'fixed';
        area.style.opacity = '0';
        document.body.appendChild(area);
        area.select();
        document.execCommand('copy');
        document.body.removeChild(area);
      }
      Message.success('已复制访客 ID');
    } catch {
      Message.warning('复制失败，请手动选择文本');
    }
  }

  function onSettingsSaved(settings: Record<string, any>) {
    if (settings && settings.show_full_ip !== undefined) {
      showFullIp.value = Boolean(settings.show_full_ip);
    }
    load();
  }

  onMounted(() => {
    load();
    loadSettings();
  });
</script>

<style scoped lang="less">
  .profiles-page {
    .tips {
      margin-bottom: 14px;
    }
    .filters {
      margin-bottom: 12px;
    }
    .banner {
      margin-bottom: 14px;
    }
    .pagination {
      display: flex;
      justify-content: flex-end;
      margin-top: 14px;
    }
    :deep(.arco-table-tr) {
      cursor: pointer;
    }
  }

  // 详情抽屉由 Arco teleport 到 body，不能依赖 .profiles-page 祖先选择器，
  // 因此这些样式必须放在顶层（scoped 属性仍在元素上）。
  .mono {
    font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
    font-size: 12px;
  }
  .guess {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .guess-code {
    font-weight: 600;
  }
  .guess-confidence {
    font-size: 12px;
  }
  .detail-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .detail-id {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .detail-desc {
    margin-bottom: 6px;
  }
  .ip-mode {
    margin-left: 6px;
    font-size: 12px;
  }
  .hint {
    display: block;
    margin-top: 8px;
    font-size: 12px;
    line-height: 18px;
  }
  .lang-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 8px;
  }
  .lang-name {
    width: 76px;
    flex: 0 0 auto;
    font-size: 13px;
  }
  .lang-bar {
    flex: 1 1 auto;
    min-width: 0;
  }
  .lang-count {
    width: 112px;
    flex: 0 0 auto;
    color: var(--color-text-3);
    font-size: 12px;
    text-align: right;
  }
  .summary {
    margin-bottom: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .evidence-row,
  .conversation-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 10px;
    margin-bottom: 8px;
    background: var(--color-fill-1);
    border-radius: 8px;
  }
  .conversation-row {
    flex-direction: row;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .evidence-text {
    font-size: 13px;
    line-height: 20px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .conversation-link {
    font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
    font-size: 12px;
  }
  .muted {
    color: var(--color-text-3);
    font-size: 12px;
  }
</style>
