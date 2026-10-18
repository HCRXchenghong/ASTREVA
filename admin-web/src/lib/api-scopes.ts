// 开放 API 的权限范围（scope）定义与展示工具。
// 契约冻结（servicebridge）：
//   GET /api/admin/api-apps -> {apps:[{..., scopes:[...]}]}
//   POST/PATCH /api/admin/api-apps 接受 scopes?: string[]
// 兼容语义：scopes 为空数组 = 未限制（等同全部范围），用于兼容旧应用。
// 注意：API 定义顺序即提交时的规范顺序，新增 scope 只能追加在末尾，避免历史数组顺序漂移。

export interface ScopeDefinition {
  /** 契约里的 scope 取值，原样提交给服务端。 */
  value: string;
  /** 界面展示的中文名。 */
  label: string;
  /** 一句话用途说明（tooltip / 说明列表共用）。 */
  description: string;
  /** 固定勾选且不可取消（me:read 始终保留）。 */
  fixed?: boolean;
}

export const API_SCOPES: ScopeDefinition[] = [
  {
    value: 'me:read',
    label: '读取应用信息',
    description: '读取当前 API 应用自身的信息（GET /open/v1/me）。始终保留，不可取消。',
    fixed: true,
  },
  {
    value: 'conversations:read',
    label: '读取会话列表/详情',
    description: '列出并查看访客会话。',
  },
  {
    value: 'conversations:create',
    label: '创建会话',
    description: '为访客新建会话。',
  },
  {
    value: 'conversations:close',
    label: '结束会话',
    description: '主动结束指定访客会话。',
  },
  {
    value: 'messages:read',
    label: '读取消息',
    description: '拉取会话内的历史消息。',
  },
  {
    value: 'messages:write',
    label: '发送消息',
    description: '以访客身份向会话发送消息。',
  },
  {
    value: 'events:subscribe',
    label: '订阅实时事件',
    description: '通过 WebSocket 订阅会话实时事件。',
  },
];

/** 始终保留、不可取消的 scope。 */
export const FIXED_SCOPE = 'me:read';

/** 全部 scope，按契约顺序排列（新建应用的默认值＝全功能）。 */
export const ALL_SCOPES: string[] = API_SCOPES.map((scope) => scope.value);

export interface ScopePreset {
  key: string;
  label: string;
  hint: string;
  /** 预置组合已包含 me:read，套用即按范围限制。 */
  scopes: string[];
}

// 预置组合与产品文档一致；每个组合都显式包含 me:read（始终保留）。
export const SCOPE_PRESETS: ScopePreset[] = [
  {
    key: 'full',
    label: '全功能',
    hint: '全部 7 项能力',
    scopes: [...ALL_SCOPES],
  },
  {
    key: 'readonly-subscribe',
    label: '只读订阅',
    hint: '查看会话与消息、订阅实时事件',
    scopes: ['me:read', 'conversations:read', 'messages:read', 'events:subscribe'],
  },
  {
    key: 'create-and-send',
    label: '仅创建与发消息',
    hint: '新建访客会话并发送消息',
    scopes: ['me:read', 'conversations:create', 'messages:write'],
  },
  {
    key: 'service-bot',
    label: '客服机器人',
    hint: '读取会话与消息、代客回复并结束会话',
    scopes: [
      'me:read',
      'conversations:read',
      'messages:read',
      'messages:write',
      'conversations:close',
    ],
  },
];

const SCOPE_LABELS: Record<string, string> = API_SCOPES.reduce(
  (result, scope) => ({ ...result, [scope.value]: scope.label }),
  {} as Record<string, string>
);

/** 未知 scope 原样回显，避免服务端新增取值时界面显示空白。 */
export function scopeLabel(value: string): string {
  return SCOPE_LABELS[value] || value;
}

/** scopes 缺失或为空数组＝未限制（等同全部范围）。 */
export function isUnrestricted(scopes: unknown): boolean {
  return !Array.isArray(scopes) || scopes.length === 0;
}

/**
 * 归一化提交用的 scope 数组：
 * - 空数组保持为空（＝未限制），不擅自补 me:read；
 * - 非空时按契约顺序去重排序，并保证包含 me:read；
 * - 服务端返回的未知 scope 保留在末尾，避免保存时静默丢权限。
 */
export function normalizeScopes(value: unknown): string[] {
  const list = Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string' && item.trim() !== '')
    : [];
  if (list.length === 0) return [];
  const unique = Array.from(new Set(list));
  const known = ALL_SCOPES.filter((scope) => unique.includes(scope));
  const unknown = unique.filter((scope) => !ALL_SCOPES.includes(scope));
  const ordered = [...known, ...unknown];
  if (!ordered.includes(FIXED_SCOPE)) ordered.unshift(FIXED_SCOPE);
  return ordered;
}

function sameSet(left: string[], right: string[]): boolean {
  if (left.length !== right.length) return false;
  const values = new Set(left);
  return right.every((item) => values.has(item));
}

/** 完全命中某个预置组合时返回该组合，否则返回 null。 */
export function findPreset(scopes: unknown): ScopePreset | null {
  if (isUnrestricted(scopes)) return null;
  const list = (scopes as string[]).filter((item) => typeof item === 'string');
  return SCOPE_PRESETS.find((preset) => sameSet(preset.scopes, list)) || null;
}

/**
 * 列表「权限范围」列摘要：
 * - 空数组 -> 未限制
 * - 命中预置 -> 「N 项 · 预置名」
 * - 否则 -> 前两个 scope 中文名 + 「等 N 项」
 */
export function summarizeScopes(scopes: unknown): string {
  if (isUnrestricted(scopes)) return '未限制';
  const list = (scopes as string[]).filter((item) => typeof item === 'string');
  const preset = findPreset(list);
  if (preset) return `${list.length} 项 · ${preset.label}`;
  const names = list.map(scopeLabel);
  const head = names.slice(0, 2).join('、');
  return list.length > 2 ? `${head} 等 ${list.length} 项` : `${head}（${list.length} 项）`;
}

/** 悬浮提示：展开全部 scope 的中文名与取值。 */
export function scopesTooltip(scopes: unknown): string {
  if (isUnrestricted(scopes)) {
    return '未限制（等同全部范围）：服务端不校验 scope，建议按需收窄。';
  }
  return (scopes as string[])
    .map((scope) => `${scopeLabel(scope)}（${scope}）`)
    .join('；');
}
