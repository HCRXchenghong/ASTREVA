/**
 * CMS 富文本白名单消毒。
 *
 * 早期版本用正则逐段拼接，遇到“没有闭合 `>` 的标签”会把剩余内容原样输出，
 * 与 Astro `set:html` 组合可形成存储型 XSS。这里改成逐字符扫描 + 严格转义：
 * - 任何不完整的 `<...` 都按文本转义，绝不原样输出；
 * - 只保留白名单标签/属性，事件属性、style、未知协议全部丢弃；
 * - 属性值统一转义，避免引号逃逸；
 * - 同时提供 CMS 链接字段的协议校验，防止 `javascript:` 进入 href/src。
 */

const ALLOWED_TAGS = new Set([
  'p', 'br', 'hr', 'strong', 'b', 'em', 'i', 'u', 's', 'sub', 'sup', 'small', 'mark',
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'ul', 'ol', 'li', 'dl', 'dt', 'dd',
  'blockquote', 'pre', 'code',
  'a', 'img', 'figure', 'figcaption', 'picture', 'source',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'caption', 'colgroup', 'col',
  'div', 'span', 'section', 'article', 'header', 'footer', 'nav', 'aside', 'main'
]);

const VOID_TAGS = new Set(['br', 'hr', 'img', 'source', 'col']);

const ALLOWED_ATTRS = new Set([
  'href', 'src', 'srcset', 'sizes', 'alt', 'title', 'target', 'rel', 'width', 'height',
  'class', 'id', 'colspan', 'rowspan', 'scope', 'type', 'loading', 'decoding', 'media'
]);

// 只允许绝对 http(s)、根相对路径（但不能是 //evil.com）、锚点、mailto、tel。
const SAFE_URL = /^(?:https?:\/\/|\/(?!\/)|#|mailto:|tel:)/i;
const UNSAFE_SCHEME = /^(?:javascript|vbscript|data|file|blob):/i;
const MAX_HTML_LENGTH = 200_000;

function escapeText(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function escapeAttr(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

function isSafeUrl(value) {
  const candidate = String(value || '').trim();
  if (!candidate) return false;
  if (UNSAFE_SCHEME.test(candidate)) return false;
  return SAFE_URL.test(candidate);
}

function sanitizeSrcset(value) {
  return String(value)
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item && isSafeUrl(item.split(/\s+/)[0] || ''))
    .map((item) => escapeAttr(item))
    .join(', ');
}

function sanitizeAttributes(rawTag, tagName) {
  const attrs = [];
  const seen = new Set();
  const attrPattern = /([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/g;
  let match;
  while ((match = attrPattern.exec(rawTag))) {
    const name = match[1].toLowerCase();
    const value = (match[2] ?? match[3] ?? match[4] ?? '').trim();
    if (seen.has(name)) continue;
    if (name.startsWith('on') || name === 'style') continue;
    if (!ALLOWED_ATTRS.has(name)) continue;
    seen.add(name);
    if ((name === 'href' || name === 'src') && !isSafeUrl(value)) continue;
    if (name === 'srcset') {
      const cleaned = sanitizeSrcset(value);
      if (!cleaned) continue;
      attrs.push(`${name}="${cleaned}"`);
      continue;
    }
    if (name === 'target') {
      attrs.push('target="_blank"');
      attrs.push('rel="noopener noreferrer"');
      continue;
    }
    if (name === 'rel') continue;
    attrs.push(`${name}="${escapeAttr(value)}"`);
  }
  if (tagName === 'a' && !attrs.some((item) => item.startsWith('rel='))) {
    attrs.push('rel="noopener noreferrer"');
  }
  return attrs.length ? ` ${attrs.join(' ')}` : '';
}

export function sanitizeHtml(value) {
  const input = String(value ?? '').slice(0, MAX_HTML_LENGTH);
  if (!input) return '';
  // 没有明显标签时仍做一次文本转义检查，避免孤立 `<` 原样进入 HTML。
  if (!input.includes('<')) return input;

  let output = '';
  let index = 0;
  while (index < input.length) {
    const lt = input.indexOf('<', index);
    if (lt === -1) {
      output += input.slice(index);
      break;
    }
    output += input.slice(index, lt);
    const rest = input.slice(lt);

    // 注释/doctype/处理指令整段丢弃。
    if (rest.startsWith('<!--')) {
      const end = input.indexOf('-->', lt + 4);
      if (end === -1) break;
      index = end + 3;
      continue;
    }
    if (rest.startsWith('<!') || rest.startsWith('<?')) {
      const end = input.indexOf('>', lt + 2);
      if (end === -1) break;
      index = end + 1;
      continue;
    }

    const gt = input.indexOf('>', lt);
    if (gt === -1) {
      // 关键修复：不完整的标签不能原样输出，否则浏览器会在后续模板补的 `>` 处闭合它。
      output += escapeText(rest);
      break;
    }

    const raw = input.slice(lt + 1, gt);
    const closing = raw.startsWith('/');
    const body = closing ? raw.slice(1) : raw;
    const nameMatch = body.match(/^([a-zA-Z0-9]+)/);
    const tagName = (nameMatch?.[1] || '').toLowerCase();
    if (!tagName || !ALLOWED_TAGS.has(tagName)) {
      // 不是合法白名单标签：把 `<` 当文本转义，保留其余内容，避免误吞后续标签。
      output += '&lt;';
      index = lt + 1;
      continue;
    }
    if (closing) {
      output += `</${tagName}>`;
    } else if (VOID_TAGS.has(tagName) || /\/\s*$/.test(body)) {
      output += `<${tagName}${sanitizeAttributes(body, tagName)} />`;
    } else {
      output += `<${tagName}${sanitizeAttributes(body, tagName)}>`;
    }
    index = gt + 1;
  }
  return output;
}

const URL_KEY = /^(?:href|url|link|src|videoUrl|audioUrl|coverImage|backgroundImage|poster|image|logoUrl|serviceBridgeUrl)$/i;

/** 校验 CMS 中常见的链接字段；不安全时返回空字符串。 */
export function safeUrl(value) {
  const candidate = String(value ?? '').trim();
  if (!candidate) return '';
  if (UNSAFE_SCHEME.test(candidate)) return '';
  if (candidate.startsWith('/')) return candidate.startsWith('//') ? '' : candidate;
  if (SAFE_URL.test(candidate)) return candidate;
  return '';
}

/** 递归清洗 CMS 内容里的链接字段（href/url/src/图片/视频等）。 */
export function sanitizeContentLinks(value, key = '') {
  if (Array.isArray(value)) return value.map((item) => sanitizeContentLinks(item, key));
  if (value && typeof value === 'object') {
    const output = {};
    for (const [childKey, child] of Object.entries(value)) {
      output[childKey] = sanitizeContentLinks(child, childKey);
    }
    return output;
  }
  if (typeof value !== 'string') return value;
  if (URL_KEY.test(key) || UNSAFE_SCHEME.test(value.trim())) {
    return safeUrl(value);
  }
  return value;
}

/** 递归清洗内容里的富文本字段（键名 body / html / content 且值是字符串）。 */
export function sanitizeContentRichText(value, key = '') {
  if (Array.isArray(value)) return value.map((item) => sanitizeContentRichText(item, key));
  if (value && typeof value === 'object') {
    const output = {};
    for (const [childKey, child] of Object.entries(value)) {
      output[childKey] = sanitizeContentRichText(child, childKey);
    }
    return output;
  }
  if (typeof value === 'string' && /^(body|html|content|description)$/i.test(key)) {
    return sanitizeHtml(value);
  }
  return value;
}

/** 保存/翻译/预览统一入口：先清洗富文本，再校验链接协议。 */
export function sanitizeCmsContent(value) {
  return sanitizeContentLinks(sanitizeContentRichText(value));
}

export { isSafeUrl };
