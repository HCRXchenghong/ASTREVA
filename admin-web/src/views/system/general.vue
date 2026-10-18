<template>
  <a-row :gutter="16">
    <a-col :span="12">
      <a-card :bordered="false" title="营业时间">
        <a-alert class="tips" type="info">
          关闭表示不限制时间。非营业时间仍由 AI 接待，但不会自动分配人工，并给访客发送下面的留言提示。
        </a-alert>
        <a-form :model="business" layout="vertical">
          <a-form-item label="时区">
            <a-select
              v-model="business.timezone"
              :options="timezoneOptions"
              allow-search
              allow-clear
              placeholder="Asia/Shanghai"
            />
          </a-form-item>
          <a-form-item label="开始时间">
            <a-input v-model="business.start" placeholder="09:00" />
          </a-form-item>
          <a-form-item label="结束时间">
            <a-input v-model="business.end" placeholder="18:00" />
          </a-form-item>
          <a-form-item label="非营业时间留言提示">
            <a-textarea v-model="business.offline_reply" :auto-size="{ minRows: 2 }" placeholder="留空使用默认文案" />
          </a-form-item>
          <a-form-item>
            <a-checkbox v-model="business.enabled">启用营业时间限制</a-checkbox>
          </a-form-item>
          <a-button type="primary" :loading="savingBusiness" @click="saveBusiness">保存营业时间</a-button>
        </a-form>
      </a-card>
    </a-col>
    <a-col :span="12">
      <a-card :bordered="false" title="联系方式与欢迎语">
        <a-alert class="tips" type="warning">全部留空即不在前台显示该联系方式；这里的内容会同步到官网与访客窗口。</a-alert>
        <a-form :model="contact" layout="vertical">
          <a-form-item label="客服电话">
            <a-input v-model="contact.telephone" allow-clear />
          </a-form-item>
          <a-form-item label="WhatsApp">
            <a-input v-model="contact.whatsapp" allow-clear />
          </a-form-item>
          <a-form-item label="Telegram">
            <a-input v-model="contact.telegram" allow-clear />
          </a-form-item>
          <a-form-item label="欢迎语">
            <a-textarea v-model="contact.entry_reply" :auto-size="{ minRows: 3 }" />
          </a-form-item>
          <a-button type="primary" :loading="savingContact" @click="saveContact">保存联系方式</a-button>
        </a-form>
      </a-card>
      <a-card :bordered="false" title="运行状态" class="status-card">
        <a-descriptions :column="1" bordered>
          <a-descriptions-item label="数据库">{{ status?.database?.ok ? '正常' : status?.database?.message || '未知' }}</a-descriptions-item>
          <a-descriptions-item label="AI">{{ status?.ai?.configured ? `${status.ai.model}（${status.ai.mode}）` : '未配置' }}</a-descriptions-item>
          <a-descriptions-item label="WebSocket 连接">
            访客 {{ status?.websocket?.Visitors ?? 0 }} / 客服 {{ status?.websocket?.Agents ?? 0 }} / 管理员 {{ status?.websocket?.Admins ?? 0 }}
          </a-descriptions-item>
        </a-descriptions>
      </a-card>
    </a-col>
  </a-row>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import {
    fetchBusinessHours,
    fetchContactSettings,
    fetchSystemStatus,
    updateBusinessHours,
    updateContactSettings,
  } from '@/api/admin';

  // 时区下拉：常见业务时区优先，历史值不在列表时保留为额外选项，避免回显丢失。
  const TIMEZONES = [
    { label: '中国（Asia/Shanghai）', value: 'Asia/Shanghai' },
    { label: '香港（Asia/Hong_Kong）', value: 'Asia/Hong_Kong' },
    { label: '台湾（Asia/Taipei）', value: 'Asia/Taipei' },
    { label: '马来西亚（Asia/Kuala_Lumpur）', value: 'Asia/Kuala_Lumpur' },
    { label: '新加坡（Asia/Singapore）', value: 'Asia/Singapore' },
    { label: '阿富汗（Asia/Kabul）', value: 'Asia/Kabul' },
    { label: '迪拜（Asia/Dubai）', value: 'Asia/Dubai' },
  ];

  const business = reactive({ timezone: 'Asia/Shanghai', start: '09:00', end: '18:00', enabled: true, offline_reply: '' });
  const contact = reactive({ telephone: '', whatsapp: '', telegram: '', entry_reply: '' });
  const status = ref<any>(null);
  const savingBusiness = ref(false);
  const savingContact = ref(false);

  const timezoneOptions = computed(() => {
    const current = String(business.timezone || '').trim();
    if (current && !TIMEZONES.some((item) => item.value === current)) {
      return [{ label: `${current}（当前值）`, value: current }, ...TIMEZONES];
    }
    return TIMEZONES;
  });

  async function load() {
    const [hours, contacts, systemStatus] = await Promise.allSettled([
      fetchBusinessHours(),
      fetchContactSettings(),
      fetchSystemStatus(),
    ]);
    if (hours.status === 'fulfilled') Object.assign(business, hours.value || {});
    if (contacts.status === 'fulfilled') {
      Object.assign(contact, {
        telephone: contacts.value?.telephone || '',
        whatsapp: contacts.value?.whatsapp || '',
        telegram: contacts.value?.telegram || '',
        entry_reply: contacts.value?.entry_reply || '',
      });
    }
    if (systemStatus.status === 'fulfilled') status.value = systemStatus.value;
  }

  async function saveBusiness() {
    savingBusiness.value = true;
    try {
      await updateBusinessHours({ ...business });
      Message.success('营业时间已保存');
    } finally {
      savingBusiness.value = false;
    }
  }

  async function saveContact() {
    savingContact.value = true;
    try {
      await updateContactSettings({ ...contact });
      Message.success('联系方式已保存');
    } finally {
      savingContact.value = false;
    }
  }

  onMounted(load);
</script>

<style scoped>
  .tips {
    margin-bottom: 14px;
  }
  .status-card {
    margin-top: 16px;
  }
</style>
