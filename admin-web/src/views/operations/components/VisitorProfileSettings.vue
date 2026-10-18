<template>
  <div class="visitor-profile-settings" data-testid="visitor-profile-settings">
    <a-alert class="tips" type="info">
      <template #title>地理信息三级降级</template>
      按顺序尝试：<strong>① CDN 头</strong>（CF-IPCountry / X-Vercel-IP-Country /
      X-Country-Code）→ <strong>② 本地 GeoLite2 库</strong> → <strong>③ 第三方 IP 库</strong>。
      三级都没有结果时只保留 Accept-Language 与时区线索；第三方超时或失败不影响访客会话。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" data-testid="vps-retry" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-form :model="form" layout="vertical">
        <a-form-item label="总开关">
          <div data-testid="vps-enabled">
            <a-switch v-model="form.enabled" />
            <span class="switch-text">{{ form.enabled ? '已启用画像采集' : '已关闭画像采集' }}</span>
          </div>
          <a-typography-text type="secondary" class="hint">
            关闭后不再采集地理/语言数据，列表与详情接口也不再返回画像（已有数据保留，随访客数据保留策略清理）。
          </a-typography-text>
        </a-form-item>

        <a-form-item label="GeoLite2 数据库路径">
          <div data-testid="vps-geoip-path">
            <a-input
              v-model="form.geoip_db_path"
              placeholder="/data/GeoLite2-City.mmdb"
              allow-clear
            />
          </div>
          <a-typography-text type="secondary" class="hint">
            容器内 mmdb 文件路径（如 /data/GeoLite2-City.mmdb）；留空则跳过本地库，直接走 CDN 头或第三方库。文件缺失时后端自动降级。
          </a-typography-text>
        </a-form-item>

        <a-form-item label="第三方 IP 库 URL 模板">
          <div data-testid="vps-third-url">
            <a-input
              v-model="form.third_party_url"
              placeholder="https://ipapi.co/{ip}/json/"
              allow-clear
            />
          </div>
          <a-typography-text type="secondary" class="hint">
            必须包含 <code>{ip}</code> 占位符，请求时替换为访客 IP；留空表示不启用第三方库。
          </a-typography-text>
        </a-form-item>

        <a-form-item label="第三方 Key">
          <div data-testid="vps-third-key">
            <a-input-password
              v-model="thirdPartyKey"
              :placeholder="keyPlaceholder"
              allow-clear
              :disabled="clearThirdPartyKey"
            />
          </div>
          <div class="key-row">
            <a-checkbox v-model="clearThirdPartyKey" data-testid="vps-clear-key">
              清除已保存的第三方 Key
            </a-checkbox>
            <a-typography-text type="secondary" class="hint inline">
              留空表示不修改，只有填了新 Key 或勾选清除才会提交该字段。
            </a-typography-text>
          </div>
        </a-form-item>

        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item label="第三方查询超时（毫秒）">
              <div data-testid="vps-timeout">
                <a-input-number
                  v-model="form.third_party_timeout_ms"
                  :min="200"
                  :max="30000"
                  :step="100"
                  style="width: 100%"
                />
              </div>
              <a-typography-text type="secondary" class="hint">
                默认 2000ms；超时即降级，不阻塞会话。
              </a-typography-text>
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item label="结果缓存（小时）">
              <div data-testid="vps-cache-hours">
                <a-input-number
                  v-model="form.cache_ttl_hours"
                  :min="1"
                  :max="720"
                  style="width: 100%"
                />
              </div>
              <a-typography-text type="secondary" class="hint">
                同一个 IP 的解析结果缓存时长，默认 24 小时。
              </a-typography-text>
            </a-form-item>
          </a-col>
        </a-row>

        <a-divider orientation="left">AI 摘要</a-divider>

        <a-form-item label="启用 AI 画像摘要">
          <div data-testid="vps-ai-enabled">
            <a-switch v-model="form.ai_summary_enabled" />
            <span class="switch-text">{{ form.ai_summary_enabled ? '已启用' : '已关闭' }}</span>
          </div>
          <a-typography-text type="secondary" class="hint">
            开启后按下方频率异步生成 summary / tags / interests；解析失败只记日志，不覆盖旧摘要。
          </a-typography-text>
        </a-form-item>

        <a-form-item label="每累计多少条访客消息触发一次摘要">
          <div data-testid="vps-ai-every">
            <a-input-number
              v-model="form.ai_summary_every_messages"
              :min="1"
              :max="200"
              style="width: 160px"
            />
          </div>
        </a-form-item>

        <a-divider orientation="left">隐私</a-divider>

        <a-form-item label="显示完整 IP">
          <div data-testid="vps-show-full-ip">
            <a-switch v-model="form.show_full_ip" />
            <span class="switch-text">{{ form.show_full_ip ? '客服端可见完整 IP' : '客服端只看脱敏 IP' }}</span>
          </div>
          <a-typography-text type="secondary" class="hint">
            关闭后，客服 App 画像卡与运营中心画像列表只显示脱敏 IP（如 1.2.3.*）。
          </a-typography-text>
        </a-form-item>

        <a-alert class="tips" type="warning">
          推测国籍由 IP 国别、语言、时区等证据加权得出，<strong>不是事实</strong>，请勿据此做合规或风控判定。
        </a-alert>

        <a-space>
          <a-button
            type="primary"
            data-testid="vps-save"
            :loading="saving"
            :disabled="loading"
            @click="save"
          >
            保存画像设置
          </a-button>
          <a-button data-testid="vps-reload" :disabled="loading || saving" @click="load">
            重新加载
          </a-button>
        </a-space>
      </a-form>
    </a-spin>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchVisitorProfileSettings,
    updateVisitorProfileSettings,
  } from '@/api/admin';

  const emit = defineEmits<{ (e: 'saved', settings: Record<string, any>): void }>();

  const settings = ref<Record<string, any>>({});
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref('');
  const thirdPartyKey = ref('');
  const clearThirdPartyKey = ref(false);

  const form = reactive({
    enabled: true,
    geoip_db_path: '',
    third_party_url: '',
    third_party_timeout_ms: 2000,
    cache_ttl_hours: 24,
    ai_summary_enabled: true,
    ai_summary_every_messages: 10,
    show_full_ip: true,
  });

  const keyPlaceholder = computed(() => {
    if (clearThirdPartyKey.value) return '提交后将清除已保存的 Key';
    return settings.value.third_party_key_masked
      ? `已配置（${settings.value.third_party_key_masked}），留空不改`
      : '未配置，填入后保存';
  });

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function boolOr(value: unknown, fallback: boolean) {
    return value === undefined || value === null ? fallback : Boolean(value);
  }

  function intOr(value: unknown, fallback: number) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  function applySettings(raw: Record<string, any>) {
    const next = raw && typeof raw === 'object' ? raw : {};
    settings.value = next;
    Object.assign(form, {
      enabled: boolOr(next.enabled, true),
      geoip_db_path: String(next.geoip_db_path ?? ''),
      third_party_url: String(next.third_party_url ?? ''),
      third_party_timeout_ms: intOr(next.third_party_timeout_ms, 2000),
      cache_ttl_hours: intOr(next.cache_ttl_hours, 24),
      ai_summary_enabled: boolOr(next.ai_summary_enabled, true),
      ai_summary_every_messages: intOr(next.ai_summary_every_messages, 10),
      show_full_ip: boolOr(next.show_full_ip, true),
    });
    thirdPartyKey.value = '';
    clearThirdPartyKey.value = false;
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const payload = await fetchVisitorProfileSettings();
      applySettings(payload?.settings ?? payload ?? {});
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '画像设置加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  // 局部更新：普通字段全量提交；第三方 Key 只在有输入或勾选清除时出现。
  function buildPayload() {
    const payload: Record<string, unknown> = {
      enabled: Boolean(form.enabled),
      geoip_db_path: String(form.geoip_db_path ?? '').trim(),
      third_party_url: String(form.third_party_url ?? '').trim(),
      third_party_timeout_ms: Number(form.third_party_timeout_ms ?? 2000),
      cache_ttl_hours: Number(form.cache_ttl_hours ?? 24),
      ai_summary_enabled: Boolean(form.ai_summary_enabled),
      ai_summary_every_messages: Number(form.ai_summary_every_messages ?? 10),
      show_full_ip: Boolean(form.show_full_ip),
    };
    if (clearThirdPartyKey.value) payload.third_party_key = '';
    else if (String(thirdPartyKey.value ?? '') !== '') {
      payload.third_party_key = thirdPartyKey.value;
    }
    return payload;
  }

  async function save() {
    saving.value = true;
    try {
      const payload = await updateVisitorProfileSettings(buildPayload());
      const next = payload?.settings ?? payload;
      if (next && typeof next === 'object' && Object.keys(next).length) {
        applySettings(next);
      } else {
        // 后端未回传设置体（例如 204）时保留表单现值，只清掉 Key 输入与清除勾选。
        thirdPartyKey.value = '';
        clearThirdPartyKey.value = false;
      }
      Message.success('画像设置已保存');
      emit('saved', settings.value);
    } catch (reason: any) {
      // 拦截器已按后端 message 弹错；这里保证失败后不静默、不抛出未捕获异常。
      loadError.value = '';
      Message.error(backendMessage(reason, '画像设置保存失败'));
    } finally {
      saving.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .visitor-profile-settings {
    .tips {
      margin-bottom: 14px;
    }
    .banner {
      margin-bottom: 14px;
    }
    .hint {
      display: block;
      margin-top: 4px;
      font-size: 12px;
      line-height: 18px;
    }
    .hint.inline {
      margin-top: 0;
    }
    .switch-text {
      margin-left: 8px;
      color: var(--color-text-3);
      font-size: 12px;
    }
    .key-row {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px;
      margin-top: 4px;
    }
  }
</style>
