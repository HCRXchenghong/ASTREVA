<template>
  <div class="settings-panel">
    <a-alert v-if="error" type="error" class="settings-tip">
      <template #title>运行配置加载失败</template>
      {{ error }}
    </a-alert>

    <a-card :bordered="false" class="edit-card">
      <template #title>编辑媒体与存储设置</template>
      <a-alert type="info" class="settings-tip">
        AI 生图的配置与生成已归入「AI 管理 → AI 生图」。
        <a-link href="/site-admin/ai/image">前往 AI 生图</a-link>
      </a-alert>
      <a-tabs default-active-key="storage">
        <a-tab-pane key="storage" title="R2 / S3 存储">
          <a-form :model="storageForm" layout="vertical">
            <a-grid :cols="{ xs: 1, md: 2 }" :col-gap="12">
              <a-grid-item>
                <a-form-item label="Endpoint" required>
                  <a-input v-model="storageForm.endpoint" placeholder="https://账户ID.r2.cloudflarestorage.com" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Region" required>
                  <a-input v-model="storageForm.region" placeholder="auto / us-east-1" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Bucket" required>
                  <a-input v-model="storageForm.bucket" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Public Base URL" required>
                  <a-input v-model="storageForm.publicBaseUrl" placeholder="https://img.example.com" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="对象前缀">
                  <a-input v-model="storageForm.prefix" placeholder="site-admin" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="寻址方式">
                  <a-select v-model="storageForm.forcePathStyle" :options="pathStyleOptions" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Access Key ID">
                  <a-input-password
                    v-model="storageForm.accessKeyId"
                    :placeholder="storage.accessKeyMasked ? `留空保留 ${storage.accessKeyMasked}` : '未配置'"
                    autocomplete="off"
                  />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Secret Access Key">
                  <a-input-password
                    v-model="storageForm.secretAccessKey"
                    :placeholder="storage.secretAccessKeyConfigured ? '已配置；留空不修改' : '未配置'"
                    autocomplete="new-password"
                  />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="Session Token（可选）">
                  <a-input-password v-model="storageForm.sessionToken" autocomplete="new-password" placeholder="留空不修改" />
                </a-form-item>
              </a-grid-item>
              <a-grid-item>
                <a-form-item label="服务端加密标识">
                  <a-input v-model="storageForm.serverSideEncryption" placeholder="例如 AES256（R2 通常留空）" />
                </a-form-item>
              </a-grid-item>
            </a-grid>
            <a-form-item>
              <a-checkbox v-model="storageForm.clearSessionToken">清除已保存的 Session Token</a-checkbox>
            </a-form-item>
            <a-alert type="warning" class="form-tip">
              已有素材时，Endpoint / Bucket / 对象前缀 / Public URL 变更会被后端拒绝（需先做对象迁移）。
              保存时会真实执行 HeadBucket 连通验证。
            </a-alert>
            <a-button type="primary" :loading="saving.storage" @click="saveStorage">保存并验证存储</a-button>
          </a-form>
        </a-tab-pane>

        <a-tab-pane key="image" title="图片处理与上传限制">
          <a-form :model="imageForm" layout="vertical">
            <a-form-item label="生成宽度（px，逗号分隔）" required>
              <a-input v-model="imageForm.variantWidths" placeholder="400,800,1200,1600" />
            </a-form-item>
            <a-form-item label="WebP 压缩质量（40-100）" required>
              <a-input-number v-model="imageForm.webpQuality" :min="40" :max="100" />
            </a-form-item>
            <a-form-item>
              <a-checkbox v-model="imageForm.keepOriginal">智能上传默认保留原图</a-checkbox>
            </a-form-item>
            <a-form-item label="单文件上限（MB）" required>
              <a-input-number v-model="imageForm.maxUploadMb" :min="1" :max="2048" />
            </a-form-item>
            <a-form-item label="允许入库的 Content-Type">
              <a-checkbox-group v-model="imageForm.allowedMimeTypes" :options="mimeOptions" />
            </a-form-item>
            <a-alert type="info" class="form-tip">
              SVG 始终禁止入库；预签名直传还会校验对象大小、Content-Type 与文件魔数。新设置只影响后续上传与替换。
            </a-alert>
            <a-button type="primary" :loading="saving.image" @click="saveImage">保存图片与上传设置</a-button>
          </a-form>
        </a-tab-pane>

      </a-tabs>
    </a-card>

    <a-spin :loading="loading" style="width: 100%">
      <div class="settings-grid">
        <a-card :bordered="false" class="setting-card">
          <template #title>存储配置（当前生效）</template>
          <template #extra>
            <a-tag :color="health.ok ? 'green' : 'red'">
              {{ health.ok ? '已连通' : '异常' }}
            </a-tag>
          </template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="存储驱动">
              {{ storage.driver ? String(storage.driver).toUpperCase() : '—' }}
            </a-descriptions-item>
            <a-descriptions-item label="Bucket">{{ storage.bucket || '—' }}</a-descriptions-item>
            <a-descriptions-item label="Endpoint">{{ storage.endpoint || '—' }}</a-descriptions-item>
            <a-descriptions-item label="公开域名">{{ storage.publicBaseUrl || '未设置' }}</a-descriptions-item>
            <a-descriptions-item label="对象前缀">{{ storage.prefix || '—' }}</a-descriptions-item>
            <a-descriptions-item v-if="health.message" label="连通信息">
              <span class="danger-text">{{ health.message }}</span>
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>图片处理（当前生效）</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="生成尺寸">{{ image.variantWidths || '—' }} px</a-descriptions-item>
            <a-descriptions-item label="输出格式">WebP</a-descriptions-item>
            <a-descriptions-item label="压缩质量">{{ image.webpQuality ? `${image.webpQuality}%` : '—' }}</a-descriptions-item>
            <a-descriptions-item label="保留原图">{{ image.keepOriginal === false ? '否' : '是' }}</a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>上传限制（当前生效）</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="单文件上限">{{ formatBytes(image.maxUploadBytes) }}</a-descriptions-item>
            <a-descriptions-item label="允许类型">{{ mimeCount }} 种</a-descriptions-item>
            <a-descriptions-item label="直传校验">大小 / Content-Type / 魔数</a-descriptions-item>
            <a-descriptions-item label="SVG"><a-tag color="red" size="small">禁止入库</a-tag></a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>运行环境</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="数据库">{{ runtime.database?.driver || 'PostgreSQL' }}</a-descriptions-item>
            <a-descriptions-item label="图片处理">{{ runtime.processing?.engine || 'sharp / libvips' }}</a-descriptions-item>
            <a-descriptions-item label="Node.js">{{ runtime.service?.node || '—' }}</a-descriptions-item>
            <a-descriptions-item label="服务健康">
              <a-tag :color="runtime.service?.ok ? 'green' : 'gray'" size="small">
                {{ runtime.service?.ok ? '正常' : '检查中' }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="素材总量">{{ stats.total || 0 }}</a-descriptions-item>
            <a-descriptions-item label="存储占用">{{ formatBytes(stats.storedBytes ?? stats.totalBytes) }}</a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>安全</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="管理员登录">统一 2FA Cookie</a-descriptions-item>
            <a-descriptions-item label="密钥持久化">PostgreSQL 加密字段</a-descriptions-item>
            <a-descriptions-item label="密钥加密">AES-256-GCM</a-descriptions-item>
            <a-descriptions-item label="页面回显">只显示末四位掩码</a-descriptions-item>
            <a-descriptions-item label="敏感操作">写入审计日志</a-descriptions-item>
          </a-descriptions>
        </a-card>
      </div>

      <a-card :bordered="false" class="cors-card">
        <template #title>预签名直传 CORS</template>
        <template #extra>
          <a-button size="small" @click="copyCors">复制 CORS JSON</a-button>
        </template>
        <p class="cors-tip">
          大文件直传由浏览器直接 PUT 到 R2。请把后台真实域名填入 AllowedOrigins（不要使用通配符），
          并确保 AllowedMethods 包含 PUT、AllowedHeaders 包含 Content-Type，否则会出现跨域失败。
        </p>
        <pre class="cors-code">{{ CORS_EXAMPLE }}</pre>
      </a-card>
    </a-spin>
  </div>
</template>

<script lang="ts" setup>
  import { computed, reactive, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import type { MediaRuntime, MediaSettings, MediaStats } from '@/api/media';
  import { updateMediaImage, updateMediaStorage } from '@/api/media';
  import { CORS_EXAMPLE, copyText, formatBytes } from '@/lib/media';

  const props = withDefaults(
    defineProps<{
      settings?: MediaSettings | null;
      runtime?: MediaRuntime;
      stats?: MediaStats;
      loading?: boolean;
      error?: string;
    }>(),
    { settings: null, runtime: () => ({}), stats: () => ({}), loading: false, error: '' }
  );
  const emit = defineEmits<{ (event: 'saved'): void }>();

  const storage = computed(() => ({
    ...(props.settings?.config?.storage || {}),
    ...(props.settings?.storageHealth || {}),
  }));
  const image = computed(() => props.settings?.config?.image || {});
  const health = computed(() => props.settings?.storageHealth || props.runtime?.storage || {});
  const mimeCount = computed(() => String(image.value.allowedMimeTypes || '').split(',').filter(Boolean).length);

  const MIME_OPTIONS = [
    'image/jpeg', 'image/png', 'image/webp', 'image/gif', 'image/avif',
    'video/mp4', 'video/webm', 'video/quicktime',
    'application/pdf', 'application/zip', 'application/acad', 'application/dxf', 'image/vnd.dwg',
  ];
  const mimeOptions = computed(() => {
    const current = String(image.value.allowedMimeTypes || '').split(',').filter(Boolean);
    const values = [...new Set([...MIME_OPTIONS, ...current])];
    return values.map((value) => ({ label: value, value }));
  });
  const pathStyleOptions = [
    { label: '虚拟主机（R2 推荐）', value: 'false' },
    { label: 'Path Style（MinIO）', value: 'true' },
  ];
  const saving = reactive({ storage: false, image: false });
  const storageForm = reactive({
    endpoint: '',
    region: 'auto',
    bucket: '',
    publicBaseUrl: '',
    prefix: '',
    forcePathStyle: 'false',
    accessKeyId: '',
    secretAccessKey: '',
    sessionToken: '',
    serverSideEncryption: '',
    clearSessionToken: false,
  });
  const imageForm = reactive({
    variantWidths: '400,800,1200,1600',
    webpQuality: 82,
    keepOriginal: true,
    maxUploadMb: 200,
    allowedMimeTypes: [] as string[],
  });

  function syncForms() {
    Object.assign(storageForm, {
      endpoint: storage.value.endpoint || '',
      region: storage.value.region || 'auto',
      bucket: storage.value.bucket || '',
      publicBaseUrl: storage.value.publicBaseUrl || '',
      prefix: storage.value.prefix || '',
      forcePathStyle: storage.value.forcePathStyle ? 'true' : 'false',
      accessKeyId: '',
      secretAccessKey: '',
      sessionToken: '',
      serverSideEncryption: storage.value.serverSideEncryption || '',
      clearSessionToken: false,
    });
    Object.assign(imageForm, {
      variantWidths: image.value.variantWidths || '400,800,1200,1600',
      webpQuality: Number(image.value.webpQuality || 82),
      keepOriginal: image.value.keepOriginal !== false,
      maxUploadMb: Math.max(1, Math.round(Number(image.value.maxUploadBytes || 0) / 1024 / 1024) || 200),
      allowedMimeTypes: String(image.value.allowedMimeTypes || '').split(',').filter(Boolean),
    });
  }

  watch(() => props.settings, syncForms, { immediate: true, deep: true });

  async function saveStorage() {
    saving.storage = true;
    try {
      const payload: Record<string, unknown> = {
        endpoint: storageForm.endpoint.trim(),
        region: storageForm.region.trim(),
        bucket: storageForm.bucket.trim(),
        publicBaseUrl: storageForm.publicBaseUrl.trim(),
        prefix: storageForm.prefix.trim(),
        serverSideEncryption: storageForm.serverSideEncryption.trim(),
        forcePathStyle: storageForm.forcePathStyle === 'true',
        clearSessionToken: storageForm.clearSessionToken,
      };
      for (const key of ['accessKeyId', 'secretAccessKey', 'sessionToken']) {
        if (String(payload[key] || '').trim()) payload[key] = String(payload[key]).trim();
        else delete payload[key];
      }
      await updateMediaStorage(payload);
      Message.success('存储配置已保存并通过连通验证');
      emit('saved');
    } finally {
      saving.storage = false;
    }
  }

  async function saveImage() {
    if (!imageForm.allowedMimeTypes.length) {
      Message.warning('请至少保留一种允许入库的 Content-Type');
      return;
    }
    saving.image = true;
    try {
      await updateMediaImage({
        variantWidths: imageForm.variantWidths,
        webpQuality: Number(imageForm.webpQuality),
        keepOriginal: imageForm.keepOriginal,
        maxUploadMb: Number(imageForm.maxUploadMb),
        allowedMimeTypes: imageForm.allowedMimeTypes.join(','),
      });
      Message.success('图片处理与上传限制已保存并立即生效');
      emit('saved');
    } finally {
      saving.image = false;
    }
  }

  async function copyCors() {
    const ok = await copyText(CORS_EXAMPLE);
    if (ok) Message.success('CORS JSON 已复制到剪贴板。');
    else Message.error('浏览器拒绝访问剪贴板，请手动复制。');
  }
</script>

<style scoped>
  .settings-tip {
    margin-bottom: 12px;
  }

  .edit-card {
    margin-bottom: 12px;
  }

  .form-tip {
    margin-bottom: 12px;
  }

  .settings-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 12px;
  }

  .cors-card {
    margin-top: 12px;
  }

  .cors-tip {
    color: var(--color-text-3);
    font-size: 12px;
  }

  .cors-code {
    margin: 0;
    padding: 12px;
    border-radius: 6px;
    background: var(--color-fill-1);
    font-size: 12px;
    overflow: auto;
  }

  .danger-text {
    color: rgb(var(--danger-6));
  }
</style>
