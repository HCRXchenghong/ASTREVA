<template>
  <div class="settings-panel">
    <a-alert type="info" class="settings-tip">
      本页只读展示当前运行配置、存储健康与 CORS 提示；写入设置仍由旧系统设置页负责。
      <a-link href="/site-admin/?view=settings" target="_blank">前往旧系统设置页</a-link>
    </a-alert>

    <a-alert v-if="error" type="error" class="settings-tip">
      <template #title>运行配置加载失败</template>
      {{ error }}
    </a-alert>

    <a-spin :loading="loading" style="width: 100%">
      <div class="settings-grid">
        <a-card :bordered="false" class="setting-card">
          <template #title>存储配置</template>
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
            <a-descriptions-item label="公开域名">
              {{ storage.publicBaseUrl || '未设置' }}
            </a-descriptions-item>
            <a-descriptions-item label="对象前缀">{{ storage.prefix || '—' }}</a-descriptions-item>
            <a-descriptions-item v-if="health.message" label="连通信息">
              <span class="danger-text">{{ health.message }}</span>
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>图片处理</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="生成尺寸">
              {{ image.variantWidths || '—' }} px
            </a-descriptions-item>
            <a-descriptions-item label="输出格式">WebP</a-descriptions-item>
            <a-descriptions-item label="压缩质量">
              {{ image.webpQuality ? `${image.webpQuality}%` : '—' }}
            </a-descriptions-item>
            <a-descriptions-item label="保留原图">
              {{ image.keepOriginal === false ? '否' : '是' }}
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>上传限制</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="单文件上限">
              {{ formatBytes(image.maxUploadBytes) }}
            </a-descriptions-item>
            <a-descriptions-item label="允许类型">{{ mimeCount }} 种</a-descriptions-item>
            <a-descriptions-item label="直传校验">大小 / Content-Type / 魔数</a-descriptions-item>
            <a-descriptions-item label="SVG">
              <a-tag color="red" size="small">禁止入库</a-tag>
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>AI 生图</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="接口地址">{{ ai.baseUrl || '—' }}</a-descriptions-item>
            <a-descriptions-item label="默认模型">{{ ai.defaultModel || '—' }}</a-descriptions-item>
            <a-descriptions-item label="API Key">
              <a-tag :color="ai.apiKeyConfigured ? 'green' : 'red'" size="small">
                {{ ai.apiKeyConfigured ? `已配置 ${ai.apiKeyMasked || ''}` : '未配置' }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="请求超时">
              {{ Math.round(Number(ai.requestTimeoutMs || 0) / 1000) }} 秒
            </a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>安全</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="管理员登录">统一 2FA Cookie</a-descriptions-item>
            <a-descriptions-item label="第二套账号 / JWT">未启用</a-descriptions-item>
            <a-descriptions-item label="密钥持久化">PostgreSQL 加密字段</a-descriptions-item>
            <a-descriptions-item label="密钥加密">AES-256-GCM</a-descriptions-item>
            <a-descriptions-item label="敏感操作">写入审计日志</a-descriptions-item>
          </a-descriptions>
        </a-card>

        <a-card :bordered="false" class="setting-card">
          <template #title>运行环境</template>
          <a-descriptions :column="1" size="small">
            <a-descriptions-item label="数据库">
              {{ runtime.database?.driver || 'PostgreSQL' }}
            </a-descriptions-item>
            <a-descriptions-item label="图片处理">
              {{ runtime.processing?.engine || 'sharp / libvips' }}
            </a-descriptions-item>
            <a-descriptions-item label="Node.js">
              {{ runtime.service?.node || '—' }}
            </a-descriptions-item>
            <a-descriptions-item label="服务健康">
              <a-tag :color="runtime.service?.ok ? 'green' : 'gray'" size="small">
                {{ runtime.service?.ok ? '正常' : '检查中' }}
              </a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="素材总量">{{ stats.total || 0 }}</a-descriptions-item>
            <a-descriptions-item label="存储占用">
              {{ formatBytes(stats.storedBytes ?? stats.totalBytes) }}
            </a-descriptions-item>
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
  import { computed } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import type { MediaRuntime, MediaSettings, MediaStats } from '@/api/media';
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

  const storage = computed(() => ({
    ...(props.settings?.config?.storage || {}),
    ...(props.settings?.storageHealth || {}),
  }));
  const image = computed(() => props.settings?.config?.image || {});
  const ai = computed(() => ({
    ...(props.settings?.config?.aiImage || {}),
    ...(props.settings?.aiImage || {}),
  }));
  const health = computed(() => props.settings?.storageHealth || props.runtime?.storage || {});
  const mimeCount = computed(
    () => String(image.value.allowedMimeTypes || '').split(',').filter(Boolean).length
  );

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
