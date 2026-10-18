<template>
  <a-card :bordered="false" class="alerts-page">
    <template #title>提醒设置</template>
    <template #extra>
      <a-space>
        <a-button :loading="loading" @click="load">刷新</a-button>
        <a-button
          type="primary"
          :loading="saving"
          data-testid="alert-smtp-save"
          @click="save"
        >
          保存
        </a-button>
      </a-space>
    </template>

    <a-alert class="tips" type="info">
      邮箱提醒依赖此配置，未配置时邮件渠道测试会失败。密码只加密保存在服务端，页面只回显掩码：<strong data-testid="alert-smtp-masked">{{ passwordState }}</strong>。留空表示不修改密码，勾选「清除密码」并保存可删除已保存的密码。
    </a-alert>

    <a-alert v-if="loadError" class="banner" type="error" show-icon>
      {{ loadError }}
      <template #action>
        <a-button size="mini" @click="load">重试</a-button>
      </template>
    </a-alert>

    <a-alert v-if="saveError" class="banner" type="error" show-icon>
      {{ saveError }}
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-form :model="form" layout="vertical" class="smtp-form">
        <a-form-item label="SMTP 服务器">
          <a-input
            v-model="form.host"
            placeholder="例如：smtp.example.com"
            :input-attrs="{ 'data-testid': 'alert-smtp-host' }"
          />
          <template #extra>
            用于发送邮箱提醒的 SMTP 服务地址；Gmail 可填 smtp.gmail.com。
          </template>
        </a-form-item>
        <a-form-item label="端口">
          <a-input-number
            v-model="form.port"
            :min="1"
            :max="65535"
            :precision="0"
            :input-attrs="{ 'data-testid': 'alert-smtp-port' }"
            style="width: 180px"
          />
          <template #extra>
            常用 587（STARTTLS）或 465（SSL）。
          </template>
        </a-form-item>
        <a-form-item label="用户名">
          <a-input
            v-model="form.username"
            placeholder="通常为完整邮箱地址"
            :input-attrs="{ 'data-testid': 'alert-smtp-username' }"
          />
        </a-form-item>
        <a-form-item label="密码 / 授权码">
          <a-input-password
            v-model="form.password"
            placeholder="留空不修改"
            :input-attrs="{ 'data-testid': 'alert-smtp-password' }"
          />
          <template #extra>
            留空不修改；要删除已保存的密码请勾选下面的「清除已保存的密码」。
          </template>
        </a-form-item>
        <a-form-item label="发件人地址">
          <a-input
            v-model="form.from_address"
            placeholder="例如：alerts@example.com"
            :input-attrs="{ 'data-testid': 'alert-smtp-from' }"
          />
        </a-form-item>
        <a-form-item label="启用 TLS">
          <a-switch
            v-model="form.use_tls"
            data-testid="alert-smtp-tls"
          />
          <template #extra>
            587 端口一般需要开启 TLS；465 端口由 SSL 直接加密，可按服务商说明选择。
          </template>
        </a-form-item>
        <a-form-item>
          <a-checkbox
            v-model="form.clearPassword"
            data-testid="alert-smtp-clear"
          >
            清除已保存的密码
          </a-checkbox>
        </a-form-item>
        <a-form-item>
          <a-space>
            <a-button
              type="primary"
              :loading="saving"
              data-testid="alert-smtp-save-bottom"
              @click="save"
            >
              保存
            </a-button>
            <a-typography-text type="secondary">
              保存后新配置立即用于邮箱提醒，无需重启。
            </a-typography-text>
          </a-space>
        </a-form-item>
      </a-form>
    </a-spin>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchAlertSmtp, updateAlertSmtp } from '@/api/admin';

  const settings = ref<any>({});
  const loading = ref(false);
  const saving = ref(false);
  const loadError = ref('');
  const saveError = ref('');
  const form = reactive({
    host: '',
    port: 587,
    username: '',
    password: '',
    from_address: '',
    use_tls: true,
    clearPassword: false,
  });

  const passwordState = computed(() => {
    const masked = String(settings.value?.password_masked ?? '').trim();
    return masked ? `已配置（${masked}）` : '未配置';
  });

  function backendMessage(reason: any, fallback: string) {
    return (
      reason?.response?.data?.error?.message ||
      reason?.response?.data?.message ||
      reason?.message ||
      fallback
    );
  }

  function applySettings(value: any) {
    const source = value && typeof value === 'object' ? value : {};
    settings.value = source;
    form.host = source.host ? String(source.host) : '';
    const port = Number(source.port);
    form.port = Number.isFinite(port) && port > 0 ? Math.trunc(port) : 587;
    form.username = source.username ? String(source.username) : '';
    form.from_address = source.from_address ? String(source.from_address) : '';
    form.use_tls = source.use_tls !== false;
    // 密码永不回显：每次加载都清空输入框与清除标记。
    form.password = '';
    form.clearPassword = false;
  }

  // password 不传=不改；传 ""=清除。
  function buildPayload() {
    const payload: Record<string, unknown> = {
      host: String(form.host ?? '').trim(),
      port: Number(form.port) || 587,
      username: String(form.username ?? '').trim(),
      from_address: String(form.from_address ?? '').trim(),
      use_tls: Boolean(form.use_tls),
    };
    if (form.clearPassword) {
      payload.password = '';
    } else if (String(form.password ?? '').length) {
      payload.password = String(form.password);
    }
    return payload;
  }

  async function load() {
    loading.value = true;
    loadError.value = '';
    try {
      const result = await fetchAlertSmtp();
      applySettings(result?.settings ?? result);
    } catch (reason: any) {
      loadError.value = backendMessage(reason, '提醒设置加载失败，请稍后重试。');
    } finally {
      loading.value = false;
    }
  }

  async function save() {
    saving.value = true;
    saveError.value = '';
    try {
      const result = await updateAlertSmtp(buildPayload());
      const next = result?.settings ?? result;
      if (next && typeof next === 'object') {
        applySettings(next);
      } else {
        form.password = '';
        form.clearPassword = false;
      }
      Message.success('提醒设置已保存');
    } catch (reason: any) {
      saveError.value = backendMessage(reason, '提醒设置保存失败，请稍后重试。');
    } finally {
      saving.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .tips {
    margin-bottom: 14px;
  }
  .banner {
    margin-bottom: 14px;
  }
  .smtp-form {
    max-width: 640px;
  }
</style>
