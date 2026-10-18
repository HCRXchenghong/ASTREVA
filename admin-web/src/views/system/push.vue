<template>
  <a-row :gutter="16">
    <a-col :span="14">
      <a-card :bordered="false" title="推送设置">
        <a-alert class="tips" type="info">
          这里配置的 Firebase 参数会通过 <code>GET /api/app-config</code> 下发给客服 App；
          服务端密钥只加密保存在数据库里，不会回显明文。改完立即生效，无需重启或重新构建 App。
        </a-alert>
        <a-form :model="form" layout="vertical">
          <a-divider orientation="left">Firebase 客户端参数（App 可见）</a-divider>
          <a-form-item label="API Key">
            <a-input v-model="form.firebase_api_key" placeholder="AIza..." />
          </a-form-item>
          <a-form-item label="App ID">
            <a-input v-model="form.firebase_app_id" placeholder="1:123456:android:abc" />
          </a-form-item>
          <a-form-item label="Messaging Sender ID">
            <a-input v-model="form.firebase_messaging_sender_id" placeholder="123456789" />
          </a-form-item>
          <a-form-item label="Project ID">
            <a-input v-model="form.firebase_project_id" placeholder="your-firebase-project" />
          </a-form-item>
          <a-form-item label="Storage Bucket（可选）">
            <a-input v-model="form.firebase_storage_bucket" />
          </a-form-item>
          <a-form-item label="Web Push VAPID Key（可选）">
            <a-input v-model="form.firebase_vapid_key" />
          </a-form-item>

          <a-divider orientation="left">服务端密钥（仅加密保存）</a-divider>
          <a-form-item>
            <a-typography-text type="secondary">
              FCM：{{ settings.fcm_configured ? `已配置 ${settings.fcm_secret_masked}` : '未配置' }}
              ｜APNs：{{ settings.apns_configured ? `已配置 ${settings.apns_secret_masked}` : '未配置' }}
            </a-typography-text>
          </a-form-item>
          <a-form-item label="Firebase 服务账号 JSON（推荐，用于自动换取 OAuth2 令牌）">
            <a-textarea v-model="form.fcm_service_account_json" :auto-size="{ minRows: 4 }" placeholder="留空表示不修改；粘贴服务账号 JSON 全文" />
          </a-form-item>
          <a-form-item label="或 FCM 静态 access token（不推荐，会过期）">
            <a-input-password v-model="form.fcm_access_token" placeholder="留空表示不修改" />
          </a-form-item>
          <a-form-item label="APNs .p8 私钥">
            <a-textarea v-model="form.apns_key_p8" :auto-size="{ minRows: 4 }" placeholder="留空表示不修改；粘贴 .p8 全文" />
          </a-form-item>
          <a-row :gutter="12">
            <a-col :span="8">
              <a-form-item label="APNs Key ID">
                <a-input v-model="form.apns_key_id" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="Apple Team ID">
                <a-input v-model="form.apns_team_id" />
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item label="环境">
                <a-select v-model="form.apns_environment" :options="envOptions" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item label="APNs Topic（Bundle ID）">
            <a-input v-model="form.apns_topic" placeholder="com.astreva.online_customer_service" />
          </a-form-item>
          <a-space>
            <a-button type="primary" :loading="saving" @click="save">保存推送设置</a-button>
            <a-button :loading="testing" @click="test">验证凭证</a-button>
            <a-button status="danger" @click="clearFcm">清除 FCM 密钥</a-button>
            <a-button status="danger" @click="clearApns">清除 APNs 密钥</a-button>
          </a-space>
        </a-form>
      </a-card>
    </a-col>
    <a-col :span="10">
      <a-card :bordered="false" title="验证结果">
        <a-empty v-if="!diagnostics" description="点击“验证凭证”查看结果" />
        <a-descriptions v-else :column="1" bordered>
          <a-descriptions-item v-for="(value, key) in diagnostics" :key="key" :label="String(key).toUpperCase()">
            {{ value }}
          </a-descriptions-item>
        </a-descriptions>
      </a-card>
      <a-card :bordered="false" title="接入步骤" class="steps">
        <a-typography-paragraph>
          1. Firebase 控制台创建项目并开启 Cloud Messaging；<br />
          2. 把客户端参数与服务账号 JSON 填到本页并保存；<br />
          3. Android 直接生效；iOS 还需在 Firebase 上传 APNs 密钥或在 App 侧开启 Push Notifications capability；<br />
          4. 点击“验证凭证”，看到通过后即可在真机验证通知。
        </a-typography-paragraph>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { fetchPushSettings, testPushSettings, updatePushSettings } from '@/api/admin';

  const settings = ref<any>({});
  const diagnostics = ref<Record<string, string> | null>(null);
  const saving = ref(false);
  const testing = ref(false);
  const envOptions = [
    { label: 'production', value: 'production' },
    { label: 'sandbox', value: 'sandbox' },
  ];
  const form = reactive<Record<string, string>>({
    firebase_api_key: '',
    firebase_app_id: '',
    firebase_messaging_sender_id: '',
    firebase_project_id: '',
    firebase_storage_bucket: '',
    firebase_vapid_key: '',
    fcm_service_account_json: '',
    fcm_access_token: '',
    apns_key_p8: '',
    apns_key_id: '',
    apns_team_id: '',
    apns_topic: '',
    apns_environment: 'production',
  });

  async function load() {
    settings.value = await fetchPushSettings();
    Object.assign(form, {
      firebase_api_key: settings.value.firebase_api_key || '',
      firebase_app_id: settings.value.firebase_app_id || '',
      firebase_messaging_sender_id: settings.value.firebase_messaging_sender_id || '',
      firebase_project_id: settings.value.firebase_project_id || '',
      firebase_storage_bucket: settings.value.firebase_storage_bucket || '',
      firebase_vapid_key: settings.value.firebase_vapid_key || '',
      apns_key_id: settings.value.apns_key_id || '',
      apns_team_id: settings.value.apns_team_id || '',
      apns_topic: settings.value.apns_topic || '',
      apns_environment: settings.value.apns_environment || 'production',
      fcm_service_account_json: '',
      fcm_access_token: '',
      apns_key_p8: '',
    });
  }

  function payloadWithSecrets() {
    const payload: Record<string, unknown> = { ...form };
    for (const key of ['fcm_service_account_json', 'fcm_access_token', 'apns_key_p8']) {
      if (!String(payload[key] || '').trim()) delete payload[key];
    }
    return payload;
  }

  async function save() {
    saving.value = true;
    try {
      settings.value = await updatePushSettings(payloadWithSecrets());
      Message.success('推送设置已保存');
      await load();
    } finally {
      saving.value = false;
    }
  }

  async function test() {
    testing.value = true;
    try {
      const result = await testPushSettings();
      diagnostics.value = result.diagnostics || {};
    } finally {
      testing.value = false;
    }
  }

  function clearFcm() {
    Modal.confirm({
      title: '清除 FCM 密钥',
      content: '清除后 Android 推送将停止，直到重新配置。',
      onOk: async () => {
        settings.value = await updatePushSettings({ clear_fcm: true });
        Message.success('已清除');
        await load();
      },
    });
  }

  function clearApns() {
    Modal.confirm({
      title: '清除 APNs 密钥',
      content: '清除后 iOS 推送将停止，直到重新配置。',
      onOk: async () => {
        settings.value = await updatePushSettings({ clear_apns: true });
        Message.success('已清除');
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
  .steps {
    margin-top: 16px;
  }
</style>
