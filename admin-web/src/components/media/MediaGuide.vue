<template>
  <div class="guide">
    <a-card :bordered="false" class="guide-hero">
      <div class="hero-body">
        <div>
          <h2>Cloudflare R2 接入指南</h2>
          <p>
            从创建 Bucket、生成 API Token，到填写后台配置和绑定自定义域名，一步完成真实接入。
          </p>
          <a-space>
            <a-link href="/site-admin/?view=settings" target="_blank">去填写存储配置</a-link>
            <a-link href="https://dash.cloudflare.com/" target="_blank" rel="noopener noreferrer">
              打开 Cloudflare 控制台 ↗
            </a-link>
          </a-space>
        </div>
      </div>
    </a-card>

    <a-card :bordered="false" title="接入路径" class="guide-block">
      <div class="guide-flow">
        <span>Cloudflare 控制台</span><b>→</b>
        <span>R2 Bucket 与 API Token</span><b>→</b>
        <span>本后台存储配置</span><b>→</b>
        <span>自定义域名公开素材</span>
      </div>
    </a-card>

    <a-card :bordered="false" title="四步完成配置" class="guide-block">
      <div class="guide-steps">
        <article v-for="step in STEPS" :key="step.index">
          <b>{{ step.index }}</b>
          <div>
            <h3>{{ step.title }}</h3>
            <code>{{ step.path }}</code>
            <p>{{ step.detail }}</p>
          </div>
        </article>
      </div>
    </a-card>

    <a-card :bordered="false" title="后台字段怎么填写" class="guide-block">
      <a-table :data="FIELDS" :pagination="false" row-key="field" size="small">
        <template #columns>
          <a-table-column title="后台字段" data-index="field" />
          <a-table-column title="去哪里找" data-index="where" />
          <a-table-column title="示例" data-index="example" />
          <a-table-column title="注意事项" data-index="note" />
        </template>
      </a-table>
    </a-card>

    <div class="guide-two">
      <a-card :bordered="false" title="预签名直传 CORS">
        <p class="cors-tip">把后台真实域名填入 AllowedOrigins，不要使用通配符。</p>
        <pre class="cors-code">{{ CORS_EXAMPLE }}</pre>
        <a-button class="cors-copy" @click="copyCors">复制 CORS JSON</a-button>
      </a-card>

      <a-card :bordered="false" title="配置后验证清单">
        <ol class="checklist">
          <li>保存时显示 Bucket 真实连通</li>
          <li>智能上传能生成 WebP 主图与变体</li>
          <li>R2 直传能完成 PUT 与服务端魔数确认</li>
          <li>公开 URL 在官网域名下能直接访问</li>
          <li>后台重启后配置与素材仍然存在</li>
        </ol>
        <a-alert type="info">
          Access Key、Secret Key 和 AI Key 均由 AES-256-GCM 加密保存在 PostgreSQL，页面不返回明文。
        </a-alert>
      </a-card>
    </div>
  </div>
</template>

<script lang="ts" setup>
  import { Message } from '@arco-design/web-vue';
  import { CORS_EXAMPLE, copyText } from '@/lib/media';

  const STEPS = [
    {
      index: 1,
      title: '创建 R2 Bucket',
      path: 'Cloudflare 控制台 → R2 对象存储 → 创建 Bucket',
      detail: '记录 Bucket 名称；生产与测试环境建议使用不同 Bucket。',
    },
    {
      index: 2,
      title: '创建 API Token',
      path: 'R2 → 管理 R2 API Token',
      detail: '授予目标 Bucket 的“对象读和写”权限，保存 Access Key ID 与 Secret Access Key。',
    },
    {
      index: 3,
      title: '填写后台配置',
      path: '素材中心 → 系统设置 → 存储配置',
      detail: 'Endpoint 通常为 https://账户ID.r2.cloudflarestorage.com，区域填写 auto。',
    },
    {
      index: 4,
      title: '绑定公开域名并验证',
      path: 'Bucket → 设置 → 自定义域',
      detail: '填写 Public Base URL，保存时后台会真实执行 HeadBucket 验证。',
    },
  ];

  const FIELDS = [
    {
      field: 'Endpoint',
      where: 'R2 API Token 页面 / S3 API',
      example: 'https://账户ID.r2.cloudflarestorage.com',
      note: '不要填写 Bucket 名称和查询参数',
    },
    { field: 'Region', where: 'Cloudflare R2 固定值', example: 'auto', note: 'MinIO 可用 us-east-1' },
    { field: 'Bucket', where: 'R2 Bucket 列表', example: 'site-images', note: '必须与 Token 权限一致' },
    {
      field: 'Access / Secret Key',
      where: '创建 R2 API Token 后显示',
      example: '只填写后台，不会回显明文',
      note: 'Secret 只显示一次，请安全保存',
    },
    {
      field: 'Public Base URL',
      where: 'Bucket 自定义域',
      example: 'https://img.example.com',
      note: '不要以 / 结尾',
    },
    {
      field: '对象前缀',
      where: '由管理员自定义',
      example: 'site-admin',
      note: '已有素材后不可直接更换',
    },
  ];

  async function copyCors() {
    const ok = await copyText(CORS_EXAMPLE);
    if (ok) Message.success('CORS JSON 已复制到剪贴板。');
    else Message.error('浏览器拒绝访问剪贴板，请手动复制。');
  }
</script>

<style scoped>
  .guide-block {
    margin-top: 12px;
  }

  .guide-hero h2 {
    margin: 0 0 6px;
  }

  .guide-hero p {
    margin: 0 0 10px;
    color: var(--color-text-3);
  }

  .guide-flow {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }

  .guide-flow span {
    padding: 6px 12px;
    border-radius: 999px;
    background: var(--color-fill-2);
    font-size: 13px;
  }

  .guide-flow b {
    color: var(--color-text-3);
  }

  .guide-steps {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 16px;
  }

  .guide-steps article {
    display: flex;
    gap: 12px;
  }

  .guide-steps article > b {
    flex: 0 0 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: rgb(var(--primary-6));
    color: #fff;
  }

  .guide-steps h3 {
    margin: 0 0 6px;
    font-size: 14px;
  }

  .guide-steps code {
    display: block;
    margin-bottom: 6px;
    font-size: 12px;
  }

  .guide-steps p {
    margin: 0;
    color: var(--color-text-3);
    font-size: 12px;
  }

  .guide-two {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: 12px;
    margin-top: 12px;
  }

  .cors-tip {
    color: var(--color-text-3);
    font-size: 12px;
  }

  .cors-code {
    margin: 0 0 12px;
    padding: 12px;
    border-radius: 6px;
    background: var(--color-fill-1);
    font-size: 12px;
    overflow: auto;
  }

  .checklist {
    margin: 0 0 12px;
    padding-left: 18px;
    color: var(--color-text-2);
  }
</style>
