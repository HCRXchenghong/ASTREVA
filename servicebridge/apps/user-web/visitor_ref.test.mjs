// 极简 node:test 校验：聊天挂件的 visitor_ref（画像聚合键）构造逻辑。
//
// 运行：node --test servicebridge/apps/user-web/visitor_ref.test.mjs
// 无第三方依赖，只读取同目录 index.html、抽出内联脚本做 node --check 与函数级断言。
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(here, 'index.html'), 'utf8');

// 抽出所有无 src 的内联 <script> 内容。
function inlineScripts(source) {
    const out = [];
    const re = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi;
    let match;
    while ((match = re.exec(source)) !== null) out.push(match[1]);
    return out;
}

// 取出 readVisitorRef 函数源码，注入依赖后在沙箱里执行。
function loadReadVisitorRef() {
    const match = html.match(/function readVisitorRef\(storage\) \{[\s\S]*?\n        \}/);
    assert.ok(match, 'index.html 必须定义 readVisitorRef(storage)');
    const prefix = "const VISITOR_REF_KEY = 'astreva_visitor_id';\n";
    return new Function(`${prefix}${match[0]}; return readVisitorRef;`)();
}

function fakeStorage(value) {
    return { getItem: (key) => (key === 'astreva_visitor_id' ? value : null) };
}

test('内联脚本全部通过 node --check 语法校验', () => {
    const dir = mkdtempSync(join(tmpdir(), 'astreva-widget-'));
    try {
        const scripts = inlineScripts(html);
        assert.ok(scripts.length >= 3, `应至少有 3 段内联脚本，实际 ${scripts.length}`);
        scripts.forEach((code, index) => {
            const file = join(dir, `inline-${index}.js`);
            writeFileSync(file, code, 'utf8');
            execFileSync(process.execPath, ['--check', file], { stdio: 'pipe' });
        });
    } finally {
        rmSync(dir, { recursive: true, force: true });
    }
});

test('readVisitorRef 只接受合法访客 ID', () => {
    const readVisitorRef = loadReadVisitorRef();
    assert.equal(readVisitorRef(fakeStorage('av_abc-123.X')), 'av_abc-123.X');
    assert.equal(readVisitorRef(fakeStorage('  av_trim_me  ')), 'av_trim_me');
});

test('readVisitorRef 对缺失/非法/超长值返回空串', () => {
    const readVisitorRef = loadReadVisitorRef();
    assert.equal(readVisitorRef(fakeStorage(null)), '');
    assert.equal(readVisitorRef(fakeStorage('')), '');
    assert.equal(readVisitorRef(fakeStorage('   ')), '');
    assert.equal(readVisitorRef(fakeStorage('bad id')), '', '含空格应被忽略');
    assert.equal(readVisitorRef(fakeStorage('bad/id')), '', '含斜杠应被忽略');
    assert.equal(readVisitorRef(fakeStorage('a'.repeat(65))), '', '超过 64 字符应被忽略');
    assert.equal(readVisitorRef({ getItem() { throw new Error('denied'); } }), '', '读取失败应静默降级');
});

test('建会话 payload 仅在取到 visitor_ref 时携带该字段', () => {
    // 静态断言：payload 先构造，visitor_ref 由 readVisitorRef 结果条件性补入。
    assert.match(html, /const payload = \{\s*source:[^}]*lang:[^}]*\};/);
    assert.match(html, /const visitorRef = readVisitorRef\(\);/);
    assert.match(html, /if \(visitorRef\) payload\.visitor_ref = visitorRef;/);
    assert.match(html, /body: JSON\.stringify\(payload\)/);
    assert.doesNotMatch(html, /visitor_ref: window\.localStorage/);
});
