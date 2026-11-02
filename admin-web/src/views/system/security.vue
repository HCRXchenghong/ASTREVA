<template>
  <div class="security-settings">
    <a-alert type="warning" class="tips">
      这里的开关直接决定线上行为。开放 API 默认关闭；只有明确需要第三方接入时才打开。
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <a-card :bordered="false" class="section">
        <template #title>开放 API</template>
        <a-form :model="form" layout="vertical">
          <a-form-item label="总开关">
            <a-switch v-model="form.openApiEnabled" />
            <span class="hint">
              {{ form.openApiEnabled ? '已开启：持有 ast_live_ Key 的第三方可调用 /open/v1/*' : '已关闭：/open/v1/* 返回 503，不泄露任何数据' }}
            </span>
          </a-form-item>
          <a-form-item label="客服只读围观范围">
            <a-select v-model="form.watchScope" style="width: 360px">
              <a-option value="own_group">仅本人负责 / 本组队列 / 未分配会话（推荐）</a-option>
              <a-option value="all">任意客服可围观任意未结束会话</a-option>
            </a-select>
          </a-form-item>
        </a-form>
      </a-card>

      <a-card :bordered="false" class="section">
        <template #title>草稿预览</template>
        <a-form :model="form" layout="vertical">
          <a-form-item label="登录预览 Cookie 有效期">
            <a-select v-model="form.previewTtlMinutes" style="width: 240px">
              <a-option :value="5">5 分钟</a-option>
              <a-option :value="10">10 分钟</a-option>
              <a-option :value="15">15 分钟</a-option>
              <a-option :value="30">30 分钟</a-option>
              <a-option :value="60">60 分钟</a-option>
              <a-option :value="120">120 分钟</a-option>
            </a-select>
            <span class="hint">预览仍然只允许已登录后台、且点击过“预览草稿”的浏览器查看。</span>
          </a-form-item>
        </a-form>
      </a-card>

      <div class="actions">
        <a-button type="primary" :loading="saving" @click="save">保存安全策略</a-button>
        <a-button @click="load">恢复已保存值</a-button>
      </div>
    </a-spin>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { fetchServicePolicy, updateServicePolicy } from '@/api/admin';
  import { fetchSecuritySettings, updateSecuritySettings } from '@/api/security';

  const loading = ref(false);
  const saving = ref(false);
  const form = reactive({
    openApiEnabled: false,
    watchScope: 'own_group',
    previewTtlMinutes: 10
  });

  function backendMessage(reason: any, fallback: string) {
    return reason?.response?.data?.error?.message || reason?.response?.data?.message || reason?.message || fallback;
  }

  async function load() {
    loading.value = true;
    try {
      const [policyResult, security] = await Promise.all([fetchServicePolicy(), fetchSecuritySettings()]);
      const policy = policyResult?.policy ?? policyResult ?? {};
      form.openApiEnabled = policy.open_api_enabled === true;
      form.watchScope = policy.agent_watch_scope === 'all' ? 'all' : 'own_group';
      form.previewTtlMinutes = Number(security?.preview_ttl_minutes ?? security?.previewTtlMinutes ?? 10);
    } catch (reason: any) {
      Message.error(backendMessage(reason, '安全策略加载失败。'));
    } finally {
      loading.value = false;
    }
  }

  async function save() {
    saving.value = true;
    try {
      await updateServicePolicy({
        open_api_enabled: form.openApiEnabled,
        agent_watch_scope: form.watchScope
      });
      await updateSecuritySettings({ preview_ttl_minutes: form.previewTtlMinutes });
      Message.success('安全策略已保存');
    } catch (reason: any) {
      Message.error(backendMessage(reason, '安全策略保存失败。'));
    } finally {
      saving.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped lang="less">
  .security-settings {
    .tips {
      margin-bottom: 14px;
    }
    .section {
      margin-bottom: 14px;
    }
    .hint {
      margin-left: 12px;
      color: var(--color-text-3);
      font-size: 12px;
    }
    .actions {
      display: flex;
      gap: 10px;
    }
  }
</style>
