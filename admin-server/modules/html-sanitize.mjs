/**
 * 极简 HTML 白名单消毒：用于 CMS 富文本字段（写入前清洗，避免存储型 XSS）。
 * 只保留展示型标签与安全属性，丢弃脚本/事件处理器/未知协议。
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

const ALLOWED_ATTRS = new Set([
  'href', 'src', 'srcset', 'sizes', 'alt', 'title', 'target', 'rel', 'width', 'height',
  'class', 'id', 'colspan', 'rowspan', 'scope', 'type', 'loading', 'decoding', 'media'
]);

const SAFE_URL = /^(?:https?:\/\/|\/|#|mailto:|tel:)/i;

function sanitizeAttributes(rawTag, tagName) {
  const attrs = [];
  const attrPattern = /([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/g;
  let match;
  while ((match = attrPattern.exec(rawTag))) {
    const name = match[1].toLowerCase();
    const value = (match[2] ?? match[3] ?? match[4] ?? '').trim();
    if (name.startsWith('on') || name === 'style') continue;
    if (!ALLOWED_ATTRS.has(name)) continue;
    if ((name === 'href' || name === 'src') && !SAFE_URL.test(value)) continue;
    if (name === 'srcset') {
      const cleaned = value
        .split(',')
        .map((item) => item.trim())
        .filter((item) => SAFE_URL.test(item.split(/\s+/)[0] || ''))
        .join(', ');
      if (!cleaned) continue;
      attrs.push(`${name}="${cleaned.replace(/"/g, '&quot;')}"`);
      continue;
    }
    if (name === 'target') {
      attrs.push('target="_blank"');
      attrs.push('rel="noopener noreferrer"');
      continue;
    }
    attrs.push(`${name}="${value.replace(/"/g, '&quot;')}"`);
  }
  if (tagName === 'a' && !attrs.some((item) => item.startsWith('rel='))) {
    attrs.push('rel="noopener noreferrer"');
  }
  return attrs.length ? ` ${attrs.join(' ')}` : '';
}

export function sanitizeHtml(value) {
  const input = String(value ?? '');
  if (!input) return '';
  if (!/<[a-z!/]/i.test(input)) return input; // 纯文本原样返回
  let output = '';
  let index = 0;
  while (index < input.length) {
    const lt = input.indexOf('<', index);
    if (lt === -1) {
      output += input.slice(index);
      break;
    }
    output += input.slice(index, lt);
    const gt = input.indexOf('>', lt);
    if (gt === -1) {
      output += input.slice(lt);
      break;
    }
    const raw = input.slice(lt + 1, gt);
    const closing = raw.startsWith('/');
    const body = closing ? raw.slice(1) : raw;
    const nameMatch = body.match(/^\s*([a-zA-Z0-9]+)/);
    const tagName = (nameMatch?.[1] || '').toLowerCase();
    if (!tagName || !ALLOWED_TAGS.has(tagName)) {
      // 丢弃不允许的标签本身，保留其文字内容
      index = gt + 1;
      continue;
    }
    if (closing) {
      output += `</${tagName}>`;
    } else if (/\/$/.test(body)) {
      output += `<${tagName}${sanitizeAttributes(body, tagName)} />`;
    } else {
      output += `<${tagName}${sanitizeAttributes(body, tagName)}>`;
    }
    index = gt + 1;
  }
  return output;
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
