#!/usr/bin/env node
/**
 * verify-ai-persistence.mjs —— 验证「AI 供应商与能力绑定真正持久化在 PostgreSQL」。
 *
 * 用法（两阶段，中间必须真的重启 Go 引擎）：
 *
 *   node scripts/verify-ai-persistence.mjs before   # 建供应商 + 配候选链，并把指纹写到 /tmp
 *   # ... 重启引擎（STORE_DRIVER=postgres）...
 *   node scripts/verify-ai-persistence.mjs after    # 校验重启后数据仍在、Key 仍可解密
 *
 * 为什么这样设计：只在一个进程里"写进去又读出来"证明不了持久化——内存驱动同样能通过。
 * 唯一有判别力的证据是「进程重启后仍在」。所以 before/after 之间必须由外部重启引擎。
 *
 * after 阶段的关键断言：
 *   1. 重启后 GET /api/admin/ai-providers 仍能列出之前创建的供应商（内存驱动这里必为空）；
 *   2. 故障转移候选链仍按 priority 读回，顺序与启停状态不变；
 *   3. 供应商 API Key 仍是掩码形态且末四位一致（说明库内密文在重启后能用同一
 *      DATA_ENCRYPTION_KEY 解密——换过密钥的话这里会变成不可用，属于真故障）；
 *   4. POST /api/admin/ai-providers/{id}/test 能连上真实上游（证明解密出的 Key 可直接用）。
 *
 * 环境：从 .env 读取（gitignored，只打印掩码），可覆盖 ASTREVA_E2E_* 变量。
 */

import fs from 'node:fs';
import path from 'node:path';

const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..');
const ENGINE = (process.env.SERVICEBRIDGE_API_URL || 'http://127.0.0.1:8080').replace(/\/+$/, '');
const ACCOUNT = process.env.SERVICEBRIDGE_ADMIN_ACCOUNT || 'superadmin';
const PASSWORD = process.env.SERVICEBRIDGE_ADMIN_PASSWORD || process.env.ADMIN_BOOTSTRAP_PASSWORD || '';
if (!PASSWORD) {
  console.error('缺少 SERVICEBRIDGE_ADMIN_PASSWORD / ADMIN_BOOTSTRAP_PASSWORD：验收脚本不再使用默认 123456。');
  process.exit(1);
}
const FINGERPRINT_FILE = process.env.ASTREVA_PERSIST_FINGERPRINT || '/tmp/astreva-ai-persistence.json';
const PROVIDER_NAME = 'E2E MiMo (persistence)';

const results = [];
function check(name, ok, evidence = '') {
  results.push({ name, ok });
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}`);
  if (evidence) console.log(`      evidence: ${evidence}`);
}

function readEnvFile() {
  const file = path.join(ROOT, '.env');
  if (!fs.existsSync(file)) return {};
  const out = {};
  for (const raw of fs.readFileSync(file, 'utf8').split('\n')) {
    const line = raw.trim();
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq < 0) continue;
    out[line.slice(0, eq).trim()] = line.slice(eq + 1).trim().replace(/^["']|["']$/g, '');
  }
  return out;
}

const envFile = readEnvFile();
const pick = (...keys) => {
  for (const key of keys) {
    if (process.env[key]) return process.env[key];
    if (envFile[key]) return envFile[key];
  }
  return '';
};

const mimoBaseUrl = (pick('ASTREVA_E2E_MIMO_BASE_URL', 'MIMO_BASE_URL') || 'https://api.xiaomimimo.com/v1').replace(/\/+$/, '');
const mimoModel = pick('ASTREVA_E2E_MIMO_MODEL', 'MIMO_MODEL') || 'mimo-v2.6-flash';
const mimoKey = pick('ASTREVA_E2E_MIMO_API_KEY', 'MIMO_API_KEY');

async function request(method, urlPath, { token, body, timeoutMs = 30_000 } = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${ENGINE}${urlPath}`, {
      method,
      headers: {
        'content-type': 'application/json',
        ...(token ? { authorization: `Bearer ${token}` } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
    });
    const text = await res.text();
    let json;
    try { json = text ? JSON.parse(text) : undefined; } catch { json = undefined; }
    return { status: res.status, json, text };
  } finally {
    clearTimeout(timer);
  }
}

function ok(res, status, label) {
  if (res.status !== status) {
    throw new Error(`${label}: HTTP ${res.status} ${res.text?.slice(0, 300)}`);
  }
  return res;
}

async function login() {
  const res = ok(await request('POST', '/api/admin/login', { body: { account: ACCOUNT, password: PASSWORD } }), 200, '登录');
  const token = res.json?.token;
  if (!token) throw new Error('登录响应缺少 token');
  return token;
}

const listProviders = async (token) => (ok(await request('GET', '/api/admin/ai-providers', { token }), 200, 'GET ai-providers').json?.providers ?? []);
const listTargets = async (token) => (ok(await request('GET', '/api/admin/ai-binding-targets', { token }), 200, 'GET ai-binding-targets').json?.targets ?? []);

function maskOf(provider) {
  return provider.api_key_masked || provider.api_key || '';
}

async function phaseBefore() {
  const token = await login();
  let provider = (await listProviders(token)).find((item) => item.name === PROVIDER_NAME);
  if (!provider) {
    if (!mimoKey) {
      throw new Error('需要 ASTREVA_E2E_MIMO_API_KEY（写在 gitignored 的 .env 里）才能创建供应商');
    }
    provider = ok(await request('POST', '/api/admin/ai-providers', {
      token,
      body: {
        name: PROVIDER_NAME,
        kind: 'text',
        api_type: 'chat_completions',
        base_url: mimoBaseUrl,
        api_key: mimoKey,
        models: [mimoModel],
        default_model: mimoModel,
        timeout_ms: 60_000,
        allowed_hosts: '',
        enabled: true,
        sort_order: 0,
      },
    }), 201, 'POST ai-providers').json?.provider;
    console.log(`[persistence] 已创建供应商 id=${provider.id} key=${maskOf(provider)}`);
  } else {
    console.log(`[persistence] 复用已存在供应商 id=${provider.id} key=${maskOf(provider)}`);
  }

  // 候选链：把该供应商放进 reception 的 priority 0，其余能力保持不动。
  const others = (await listTargets(token)).filter((item) => item.capability !== 'reception');
  const targets = [
    ...others,
    { capability: 'reception', provider_id: provider.id, model: mimoModel, priority: 0, enabled: true },
  ];
  const saved = ok(await request('PUT', '/api/admin/ai-binding-targets', { token, body: { targets } }), 200, 'PUT ai-binding-targets').json?.targets ?? [];

  const fingerprint = {
    providerId: provider.id,
    providerName: provider.name,
    baseUrl: mimoBaseUrl,
    defaultModel: provider.default_model,
    keyMask: maskOf(provider),
    receptionChain: saved
      .filter((item) => item.capability === 'reception')
      .sort((a, b) => a.priority - b.priority)
      .map((item) => ({ providerId: item.provider_id, model: item.model, priority: item.priority, enabled: item.enabled })),
    createdAt: new Date().toISOString(),
  };
  fs.writeFileSync(FINGERPRINT_FILE, JSON.stringify(fingerprint, null, 2));
  console.log(`[persistence] 指纹已写入 ${FINGERPRINT_FILE}（重启引擎后执行 after 阶段）`);

  const test = ok(await request('POST', `/api/admin/ai-providers/${encodeURIComponent(provider.id)}/test`, { token, timeoutMs: 90_000 }), 200, 'test connection');
  check('before: 供应商已入库且真实上游可连通（Key 可加密并解密回来）', test.json?.ok === true, `ok=${test.json?.ok} latency_ms=${test.json?.latency_ms} ${test.json?.message ?? ''}`);
  check('before: 候选链已保存（reception priority 0）', fingerprint.receptionChain.length === 1 && fingerprint.receptionChain[0].providerId === provider.id, JSON.stringify(fingerprint.receptionChain));
  return 0;
}

async function phaseAfter() {
  if (!fs.existsSync(FINGERPRINT_FILE)) {
    throw new Error(`缺少指纹文件 ${FINGERPRINT_FILE}；请先执行 before 阶段`);
  }
  const before = JSON.parse(fs.readFileSync(FINGERPRINT_FILE, 'utf8'));
  const token = await login();

  const providers = await listProviders(token);
  const restored = providers.find((item) => item.id === before.providerId);
  check('after: 重启后供应商仍在库里（内存驱动此处必为空）', Boolean(restored), restored ? `id=${restored.id} name=${restored.name} key=${maskOf(restored)}` : `providers=${providers.length}`);

  if (restored) {
    check('after: 重启后供应商字段一致', restored.name === before.providerName && restored.default_model === before.defaultModel, `name=${restored.name} model=${restored.default_model}`);
    check('after: 重启后 Key 掩码一致（密文能用同一 DATA_ENCRYPTION_KEY 解密）', maskOf(restored) === before.keyMask, `before=${before.keyMask} after=${maskOf(restored)}`);
  }

  const chain = (await listTargets(token))
    .filter((item) => item.capability === 'reception')
    .sort((a, b) => a.priority - b.priority)
    .map((item) => ({ providerId: item.provider_id, model: item.model, priority: item.priority, enabled: item.enabled }));
  check('after: 重启后故障转移候选链完整（顺序与启停不变）', JSON.stringify(chain) === JSON.stringify(before.receptionChain), JSON.stringify(chain));

  if (restored && before.keyMask) {
    const test = ok(await request('POST', `/api/admin/ai-providers/${encodeURIComponent(restored.id)}/test`, { token, timeoutMs: 90_000 }), 200, 'test connection');
    check('after: 重启后真实上游仍可连通（解密出的 Key 可直接使用）', test.json?.ok === true, `ok=${test.json?.ok} latency_ms=${test.json?.latency_ms} ${test.json?.message ?? ''}`);
  }

  const failed = results.filter((item) => !item.ok).length;
  console.log(`\n${results.length - failed}/${results.length} assertions passed`);
  if (failed) {
    console.log('RESULT: FAIL — AI 供应商/绑定没有真正持久化，重启后数据丢失或不可用。');
    return 1;
  }
  console.log('RESULT: PASS — AI 供应商、候选链与加密 Key 在引擎重启后仍完整可用。');
  return 0;
}

const phase = (process.argv[2] || '').toLowerCase();
let exitCode = 1;
try {
  if (phase === 'before') exitCode = await phaseBefore();
  else if (phase === 'after') exitCode = await phaseAfter();
  else {
    console.error('用法: node scripts/verify-ai-persistence.mjs <before|after>');
    exitCode = 2;
  }
} catch (error) {
  console.error(`\n[persistence] ${phase} 阶段失败：${error.message}`);
  exitCode = 1;
}
if (phase === 'before' && exitCode === 0) {
  const failed = results.filter((item) => !item.ok).length;
  console.log(`\n${results.length - failed}/${results.length} assertions passed`);
  console.log('RESULT: PASS — before 阶段就绪；现在重启 Go 引擎（STORE_DRIVER=postgres），然后跑 after。');
}
process.exit(exitCode);
