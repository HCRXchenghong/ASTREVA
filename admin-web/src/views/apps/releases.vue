<template>
  <a-card :bordered="false">
    <template #title>App 版本</template>
    <template #extra>
      <a-space>
        <a-button :loading="loading" @click="load">刷新</a-button>
        <a-button type="primary" @click="openUpload">上传新版本</a-button>
      </a-space>
    </template>

    <a-table :data="releases" :loading="loading" :pagination="false" row-key="id">
      <template #columns>
        <a-table-column title="版本" data-index="version_name" />
        <a-table-column title="平台" data-index="platform" />
        <a-table-column title="大小">
          <template #cell="{ record }">{{ bytes(record.file_size) }}</template>
        </a-table-column>
        <a-table-column title="更新类型">
          <template #cell="{ record }">
            <a-tag :color="record.force_update ? 'red' : 'arcoblue'">{{ record.force_update ? '强制更新' : '可选更新' }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="状态">
          <template #cell="{ record }">
            <a-tag :color="record.enabled ? 'green' : 'gray'">{{ record.enabled ? '启用' : '停用' }}</a-tag>
          </template>
        </a-table-column>
        <a-table-column title="操作" :width="220">
          <template #cell="{ record }">
            <a-space wrap>
              <a-link @click="toggleEnabled(record)">{{ record.enabled ? '停用' : '启用' }}</a-link>
              <a-link @click="toggleForce(record)">{{ record.force_update ? '改为可选' : '设为强制' }}</a-link>
              <a-link status="danger" @click="remove(record)">删除</a-link>
            </a-space>
          </template>
        </a-table-column>
      </template>
      <template #empty>
        <a-empty description="暂无已上传的版本" />
      </template>
    </a-table>

    <a-modal
      v-model:visible="uploadVisible"
      title="上传新版本"
      :width="640"
      ok-text="上传并发布"
      cancel-text="取消"
      :ok-loading="uploading"
      :on-before-ok="submit"
      unmount-on-close
      @cancel="resetForm"
    >
      <a-alert class="tips" type="info">
        安装包为 .apk / .ipa；版本代码必须是递增的整数，App 端按它判断升级。
      </a-alert>
      <a-form :model="form" layout="vertical">
        <a-form-item label="平台">
          <a-select v-model="form.platform" :options="platformOptions" />
        </a-form-item>
        <a-form-item label="版本号（如 1.2.0）" required>
          <a-input v-model="form.version_name" placeholder="1.2.0" />
        </a-form-item>
        <a-form-item label="版本代码（整数，递增）" required>
          <a-input-number v-model="form.version_code" :min="1" style="width: 200px" />
        </a-form-item>
        <a-form-item label="安装包文件" required>
          <a-upload
            v-model:file-list="fileList"
            :auto-upload="false"
            :limit="1"
            accept=".apk,.ipa"
            @change="onFileChange"
          />
        </a-form-item>
        <a-form-item label="更新说明">
          <a-textarea v-model="form.release_notes" :auto-size="{ minRows: 3 }" />
        </a-form-item>
        <a-form-item>
          <a-checkbox v-model="form.force_update">强制更新（低于此版本必须升级）</a-checkbox>
        </a-form-item>
      </a-form>
    </a-modal>
  </a-card>
</template>

<script lang="ts" setup>
  import { onMounted, reactive, ref } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { deleteRelease, fetchReleases, updateRelease, uploadRelease } from '@/api/admin';

  const releases = ref<any[]>([]);
  const loading = ref(false);
  const uploading = ref(false);
  const uploadVisible = ref(false);
  const file = ref<File | null>(null);
  const fileList = ref<any[]>([]);
  const form = reactive({
    platform: 'android',
    version_name: '',
    version_code: 1,
    release_notes: '',
    force_update: false,
  });
  const platformOptions = [
    { label: 'Android', value: 'android' },
    { label: 'iOS', value: 'ios' },
  ];

  function bytes(value: number) {
    const size = Number(value || 0);
    if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
    if (size >= 1024) return `${(size / 1024).toFixed(1)} KB`;
    return `${size} B`;
  }

  // Arco Upload 的 change 事件里拿原始文件（auto-upload 关闭时）。
  function onFileChange(_fileList: any, currentFile: any) {
    file.value = currentFile?.file?.originFile || currentFile?.file || null;
  }

  function resetForm() {
    form.version_name = '';
    form.release_notes = '';
    form.force_update = false;
    form.version_code = 1;
    form.platform = 'android';
    file.value = null;
    fileList.value = [];
  }

  function openUpload() {
    resetForm();
    uploadVisible.value = true;
  }

  async function load() {
    loading.value = true;
    try {
      releases.value = (await fetchReleases()).releases || [];
    } finally {
      loading.value = false;
    }
  }

  // 通过 a-modal 的 on-before-ok 提交：校验失败返回 false，弹窗不关闭。
  async function submit(): Promise<boolean> {
    if (!form.version_name.trim() || !form.version_code) {
      Message.warning('请填写版本号与版本代码');
      return false;
    }
    if (!file.value) {
      Message.warning('请选择安装包文件');
      return false;
    }
    const data = new FormData();
    data.append('platform', form.platform);
    data.append('version_name', form.version_name.trim());
    data.append('version_code', String(form.version_code));
    data.append('release_notes', form.release_notes);
    data.append('force_update', form.force_update ? 'true' : 'false');
    data.append('file', file.value);
    uploading.value = true;
    try {
      await uploadRelease(data);
      Message.success('新版本已上传');
      await load();
      return true;
    } catch (reason: any) {
      Message.error(
        reason?.response?.data?.error?.message ||
          reason?.message ||
          '上传失败，请稍后重试。'
      );
      return false;
    } finally {
      uploading.value = false;
    }
  }

  async function toggleEnabled(record: any) {
    await updateRelease(record.id, { enabled: !record.enabled });
    await load();
  }
  async function toggleForce(record: any) {
    await updateRelease(record.id, { force_update: !record.force_update });
    await load();
  }
  function remove(record: any) {
    Modal.confirm({
      title: '删除版本',
      content: `删除 ${record.version_name} 会同时移除安装包文件。`,
      onOk: async () => {
        await deleteRelease(record.id);
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
</style>
