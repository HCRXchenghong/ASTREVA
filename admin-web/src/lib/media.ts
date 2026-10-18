// R2 素材库通用工具：格式化、复制、MIME 推断与直传辅助。
// 迁移自 admin-server/public/media-admin.js 中的同名工具函数，行为保持一致。
import { h } from 'vue';
import { Message } from '@arco-design/web-vue';

/** 让 Modal 内容保留换行（Arco 默认折叠空白）。 */
export function preformatted(text: string) {
  return () =>
    h('div', { style: { whiteSpace: 'pre-line', lineHeight: '1.7' } }, String(text ?? ''));
}

/**
 * 统一错误提示：HTTP 错误已由 api/interceptor.ts 弹出，这里只补充非 HTTP 错误
 * （例如浏览器直传 R2 失败），避免同一错误重复弹窗。
 */
export function reportError(error: any, fallback = '操作失败。'): void {
  if (error?.response || error?.isAxiosError || error?.code === 'ERR_CANCELED') return;
  Message.error(error?.message || fallback);
}

/**
 * 包装 Modal.confirm 的 onOk：Arco 不会处理 onOk 的 rejection，
 * 必须在这里吞掉异常，否则浏览器控制台会出现未处理的 Promise 报错。
 */
export function runModalTask(task: () => Promise<unknown>) {
  return () => {
    task().catch((error) => reportError(error));
  };
}

/** 人类可读的文件体积。 */
export function formatBytes(bytes: unknown): string {
  const value = Number(bytes || 0);
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  return `${(value / 1024 ** 3).toFixed(2)} GB`;
}

/** 本地化时间（与旧页面一致的 zh-CN / 24 小时制）。 */
export function formatDateTime(value?: string | number | null): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString('zh-CN', { hour12: false });
}

/** 运行时长（天 / 小时 / 分钟）。 */
export function formatDuration(seconds: unknown): string {
  const value = Math.max(0, Number(seconds || 0));
  const days = Math.floor(value / 86400);
  const hours = Math.floor((value % 86400) / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  if (days) return `${days} 天 ${hours} 小时`;
  if (hours) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分钟`;
}

/** 百分比（0–100，用于进度条）。 */
export function ratioPercent(used: unknown, total: unknown): number {
  const max = Number(total || 0);
  if (!(max > 0)) return 0;
  return Math.max(0, Math.min(100, (Number(used || 0) / max) * 100));
}

/** 媒体类型中文名。 */
export function typeLabel(type?: string): string {
  return { image: '图片', video: '视频', file: '文件' }[String(type || '')] || String(type || '文件');
}

/** 素材来源中文名。 */
export function sourceLabel(sourceType?: string): string {
  return (
    {
      upload: '后台上传',
      replacement: '稳定替换',
      direct: 'R2 直传',
      ai: 'AI 生图',
      migrate: '历史迁移',
    }[String(sourceType || 'upload')] || String(sourceType || 'upload')
  );
}

/** 扩展名 → MIME 推断（浏览器未给出 file.type 时使用）。 */
export const EXTENSION_MIME: Record<string, string> = {
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  png: 'image/png',
  webp: 'image/webp',
  gif: 'image/gif',
  avif: 'image/avif',
  mp4: 'video/mp4',
  webm: 'video/webm',
  mov: 'video/quicktime',
  pdf: 'application/pdf',
  zip: 'application/zip',
  dwg: 'application/acad',
  dxf: 'application/dxf',
};

/** 根据文件名推断声明用 MIME。 */
export function declaredMime(file: File): string {
  if (file.type) return file.type;
  const extension = String(file.name || '').toLowerCase().split('.').pop() || '';
  return EXTENSION_MIME[extension] || '';
}

/** 是否为位图（智能模式只允许位图）。 */
export function isRasterImage(file: File): boolean {
  return declaredMime(file).startsWith('image/');
}

/** 触发浏览器文件选择。 */
export function pickFile(accept: string, multiple = false): Promise<File[]> {
  return new Promise((resolve) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = accept;
    input.multiple = multiple;
    input.style.display = 'none';
    document.body.appendChild(input);
    input.addEventListener(
      'change',
      () => {
        const files = [...(input.files || [])];
        input.remove();
        resolve(files);
      },
      { once: true }
    );
    input.click();
  });
}

/** 复制文本到剪贴板；返回是否成功，失败不抛出。 */
export async function copyText(text: string): Promise<boolean> {
  const value = String(text ?? '');
  if (!value) return false;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return true;
    }
  } catch {
    // 剪贴板权限被拒绝时退回 execCommand。
  }
  try {
    const area = document.createElement('textarea');
    area.value = value;
    area.setAttribute('readonly', 'readonly');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    area.remove();
    return ok;
  } catch {
    return false;
  }
}

/** 素材用于 <img srcset> 的候选串。 */
export function srcsetOf(item: any): string {
  const variants = Array.isArray(item?.variants?.webp) ? item.variants.webp : [];
  return variants.map((entry: any) => `${entry.url} ${entry.width}w`).join(', ');
}

/** 素材允许选择的替换文件类型。 */
export function replaceAccept(item: any): string {
  return item?.type === 'image'
    ? 'image/jpeg,image/png,image/webp,image/gif,image/avif'
    : '*/*';
}

/** R2 预签名直传的 CORS 参考配置（含后台真实域名占位）。 */
export const CORS_EXAMPLE = JSON.stringify(
  [
    {
      AllowedOrigins: [typeof window === 'undefined' ? 'https://admin.example.com' : window.location.origin],
      AllowedMethods: ['PUT', 'GET', 'HEAD'],
      AllowedHeaders: ['Content-Type'],
      ExposeHeaders: ['ETag'],
      MaxAgeSeconds: 3600,
    },
  ],
  null,
  2
);
