#!/usr/bin/env node
/**
 * scripts/verify-ai-failover.mjs
 *
 * Live, repeatable end-to-end verification of the AI provider failover chain
 * (multi-provider + circuit breaker + automatic switch to the next candidate)
 * against the REAL running stack.
 *
 * What it proves
 * --------------
 *   1. It configures the `reception` capability chain through the exact same
 *      admin endpoint the console uses: PUT /api/admin/ai-binding-targets,
 *      with candidates ordered [broken @ priority 0, mimo @ priority 1].
 *   2. It drives a real visitor message down the public Open API
 *      (/open/v1/conversations + POST .../messages), which triggers the real
 *      auto-reply path (store.generateAutoRepliesLocked -> NeedAI ->
 *      app.generateAIReply -> runFailoverChain).
 *   3. It asserts a real AI answer came back even though priority 0 was a dead
 *      provider, that the answer is NOT a local fallback string, and that the
 *      health endpoint reports the broken provider with a failure signal
 *      (consecutive_failures >= 1 / state open) while MiMo stays healthy.
 *
 * Runner (plain Node, no new dependencies)
 * ----------------------------------------
 *     node scripts/verify-ai-failover.mjs
 *
 * Optional environment / .env overrides (read from the repo .env when present):
 *     ASTREVA_E2E_ENGINE_URL        Go engine base URL      (default http://127.0.0.1:8080)
 *     ASTREVA_E2E_ADMIN_ACCOUNT     admin account           (default superadmin)
 *     ASTREVA_E2E_ADMIN_PASSWORD    admin password          (default 123456)
 *     ASTREVA_E2E_MIMO_API_KEY      real MiMo API key       (only needed when the provider is missing)
 *     ASTREVA_E2E_MIMO_BASE_URL     default https://api.xiaomimimo.com/v1
 *     ASTREVA_E2E_MIMO_MODEL        default mimo-v2.6-flash
 *
 * Secrets are never printed: API keys are masked as `sk-****`.
 *
 * Idempotency / safety
 * --------------------
 *   - The MiMo provider is reused when present and created when missing; it is
 *     never deleted.
 *   - The broken provider is a throwaway fixture (dead local port). Any stale
 *     fixture from a previous run is removed first so the circuit breaker state
 *     is fresh, guaranteeing priority 0 is actually attempted.
 *   - Previous binding targets and the previous `ai_keyword_priority` policy are
 *     restored verbatim in a finally block, even on failure.
 *   - A throwaway API app is created for the visitor flow and deleted afterwards.
 */

import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

/** Dead local port: connection refused immediately, so the candidate fails fast. */
const BROKEN_BASE_URL = 'http://127.0.0.1:9/v1';
const BROKEN_MODEL = 'broken-model';
/** Placeholder key for the fixture provider; not a real secret. */
const BROKEN_API_KEY = 'sk-e2e-broken-placeholder';
const BROKEN_PROVIDER_NAME = 'E2E Broken Failover (auto)';

const MIMO_PROVIDER_NAME = 'E2E MiMo (auto)';
const APP_NAME = 'E2E Failover Visitor (auto)';

const TEST_MESSAGE =
  'Hello! Could you briefly introduce what your company does and what you can help me with?';

const POLL_INTERVAL_MS = 1500;
const POLL_TIMEOUT_MS = 90_000;

// Static local fallbacks that prove a reply did NOT come from a real provider.
// These mirror the Go constants; when the Go sources are present the script
// extracts them at runtime so the comparison stays tied to the real code.
const FALLBACK_REPLY_ZH_FALLBACK =
  '我已收到您的问题。您可以继续补充细节，人工客服会尽快为您跟进。'; // internal/ai/client.go fallbackReply (default/zh)
const AI_BUSY_TEXT_ZH_FALLBACK =
  'AI 客服暂时繁忙，已为您记录问题，人工客服会尽快为您跟进。'; // internal/app/service.go aiBusyText (default/zh)

// ---------------------------------------------------------------------------
// Tiny .env parser (no dependencies) + secret masking
// ---------------------------------------------------------------------------

function parseEnvFile(path) {
  const out = {};
  if (!existsSync(path)) return out;
  const text = readFileSync(path, 'utf8');
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const match = /^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line);
    if (!match) continue;
    let value = match[2].trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    out[match[1]] = value;
  }
  return out;
}

const dotenv = parseEnvFile(resolve(ROOT, '.env'));

function envValue(...names) {
  for (const name of names) {
    const fromProcess = process.env[name];
    if (fromProcess !== undefined && String(fromProcess).trim() !== '') return String(fromProcess).trim();
    const fromFile = dotenv[name];
    if (fromFile !== undefined && String(fromFile).trim() !== '') return String(fromFile).trim();
  }
  return '';
}

/** Mask any API key so full secrets never reach stdout. */
function maskKey(key) {
  if (!key) return '(none)';
  return key.startsWith('sk-') ? 'sk-****' : '****';
}

/** Masked label for a provider that already exposes only an api_key_masked field. */
function maskProviderKey(provider) {
  return provider && provider.api_key_masked ? 'sk-****' : '(none)';
}

const SECRETS = [];
function registerSecret(value) {
  const trimmed = String(value || '').trim();
  if (trimmed.length >= 8) SECRETS.push(trimmed);
}
function redact(text) {
  let out = String(text ?? '');
  for (const secret of SECRETS) out = out.split(secret).join('****');
  return out;
}
function snippet(value, max = 600) {
  let text;
  if (typeof value === 'string') text = value;
  else {
    try {
      text = JSON.stringify(value);
    } catch {
      text = String(value);
    }
  }
  text = redact(text ?? '');
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

// ---------------------------------------------------------------------------
// Config resolution
// ---------------------------------------------------------------------------

const cfg = {
  engineUrl: (envValue('ASTREVA_E2E_ENGINE_URL', 'SERVICEBRIDGE_ENGINE_URL') || 'http://127.0.0.1:8080').replace(/\/+$/, ''),
  adminAccount: envValue('ASTREVA_E2E_ADMIN_ACCOUNT', 'SERVICEBRIDGE_ADMIN_ACCOUNT') || 'superadmin',
  adminPassword: envValue('ASTREVA_E2E_ADMIN_PASSWORD', 'SERVICEBRIDGE_ADMIN_PASSWORD') || '123456',
  mimoBaseUrl: (envValue('ASTREVA_E2E_MIMO_BASE_URL', 'MIMO_BASE_URL') || 'https://api.xiaomimimo.com/v1').replace(/\/+$/, ''),
  mimoModel: envValue('ASTREVA_E2E_MIMO_MODEL', 'MIMO_MODEL') || 'mimo-v2.6-flash',
  mimoKey: envValue('ASTREVA_E2E_MIMO_API_KEY', 'MIMO_API_KEY'),
};

// ---------------------------------------------------------------------------
// Logging / assertions
// ---------------------------------------------------------------------------

const results = [];
function log(message) {
  console.log(`[verify-ai-failover] ${redact(message)}`);
}
function check(name, ok, evidence = '') {
  const line = ok ? `PASS  ${name}` : `FAIL  ${name}`;
  console.log(line + (evidence ? `\n      evidence: ${snippet(evidence, 700)}` : ''));
  results.push({ name, ok });
  return ok;
}
function fatal(message) {
  throw new Error(message);
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// ---------------------------------------------------------------------------
// HTTP helper
// ---------------------------------------------------------------------------

async function request(method, path, { token, apiKey, body, timeoutMs = 30_000 } = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (token) headers.Authorization = `Bearer ${token}`;
  else if (apiKey) headers.Authorization = `Bearer ${apiKey}`;
  try {
    const res = await fetch(cfg.engineUrl + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: controller.signal,
    });
    const text = await res.text();
    let json = null;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = null;
    }
    return { status: res.status, ok: res.ok, json, text };
  } finally {
    clearTimeout(timer);
  }
}

function expectStatus(res, expected, label) {
  if (res.status !== expected) {
    fatal(`${label}: expected HTTP ${expected}, got ${res.status}: ${snippet(res.text, 400)}`);
  }
  return res;
}

function normalizeUrl(value) {
  return String(value || '').trim().replace(/\/+$/, '');
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

async function listProviders(token) {
  const res = expectStatus(await request('GET', '/api/admin/ai-providers', { token }), 200, 'GET ai-providers');
  return res.json?.providers ?? [];
}

async function listTargets(token) {
  const res = expectStatus(await request('GET', '/api/admin/ai-binding-targets', { token }), 200, 'GET ai-binding-targets');
  return res.json?.targets ?? [];
}

async function getHealth(token) {
  const res = expectStatus(await request('GET', '/api/admin/ai-providers/health', { token }), 200, 'GET ai-providers/health');
  return res.json?.providers ?? [];
}

/**
 * Wait out a leftover circuit-breaker cooldown so the candidate is really
 * attempted again. A previous failed run can leave a provider `open` for the
 * configured cooldown; without this, the re-run would skip it and produce a
 * misleading result. Bounded by maxWaitMs.
 */
async function waitForCircuitClosed(token, providerId, label, maxWaitMs = 70_000) {
  const deadline = Date.now() + maxWaitMs;
  for (;;) {
    const health = await getHealth(token);
    const entry = health.find((h) => h.id === providerId);
    if (!entry || entry.state !== 'open' || !entry.opened_until) return entry;
    const until = Date.parse(entry.opened_until);
    const waitMs = Number.isFinite(until) ? until - Date.now() + 300 : 0;
    if (waitMs <= 0) return entry;
    if (Date.now() + waitMs > deadline) {
      log(`warn: ${label} circuit still open until ${entry.opened_until}; proceeding anyway`);
      return entry;
    }
    log(`waiting ~${Math.ceil(waitMs / 1000)}s for ${label} circuit cooldown (state=open, until ${entry.opened_until})`);
    await sleep(Math.min(waitMs, 5000));
  }
}

async function ensureMimoProvider(token, providers) {
  const found =
    providers.find((p) => normalizeUrl(p.base_url) === normalizeUrl(cfg.mimoBaseUrl)) ||
    providers.find((p) => p.name === MIMO_PROVIDER_NAME);

  if (found) {
    // Normalize the fields that matter for this test. When a known-good key is
    // available it is always (re)applied so a stale/broken stored key cannot
    // masquerade as a failover failure.
    const needsPatch =
      normalizeUrl(found.base_url) !== normalizeUrl(cfg.mimoBaseUrl) ||
      found.default_model !== cfg.mimoModel ||
      found.enabled !== true ||
      (cfg.mimoKey ? true : !found.api_key_masked);
    if (!needsPatch) {
      log(`reusing real MiMo provider id=${found.id} name="${found.name}" base_url=${found.base_url} model=${found.default_model} key=${maskProviderKey(found)}`);
      return found;
    }
    const patch = {
      base_url: cfg.mimoBaseUrl,
      default_model: cfg.mimoModel,
      models: [cfg.mimoModel],
      timeout_ms: 30_000,
      enabled: true,
    };
    if (cfg.mimoKey) patch.api_key = cfg.mimoKey;
    const res = expectStatus(
      await request('PATCH', `/api/admin/ai-providers/${encodeURIComponent(found.id)}`, { token, body: patch }),
      200,
      'PATCH MiMo provider',
    );
    const provider = res.json.provider;
    log(`normalized real MiMo provider id=${provider.id} name="${provider.name}" base_url=${provider.base_url} model=${provider.default_model} key=${maskProviderKey(provider)}`);
    return provider;
  }

  if (!cfg.mimoKey) {
    fatal(
      'real MiMo provider is missing and no API key is available. ' +
        'Set ASTREVA_E2E_MIMO_API_KEY in the environment or in the gitignored .env file.',
    );
  }
  const res = expectStatus(
    await request('POST', '/api/admin/ai-providers', {
      token,
      body: {
        name: MIMO_PROVIDER_NAME,
        kind: 'text',
        api_type: 'chat_completions',
        base_url: cfg.mimoBaseUrl,
        api_key: cfg.mimoKey,
        models: [cfg.mimoModel],
        default_model: cfg.mimoModel,
        timeout_ms: 30_000,
        allowed_hosts: '',
        enabled: true,
        sort_order: 0,
      },
    }),
    201,
    'POST MiMo provider',
  );
  const provider = res.json.provider;
  log(`created real MiMo provider id=${provider.id} name="${provider.name}" base_url=${provider.base_url} key=${maskProviderKey(provider)}`);
  return provider;
}

/**
 * Ensure the broken fixture exists with a FRESH circuit-breaker entry.
 * The breaker is keyed by provider id, so deleting any stale fixture (and
 * recreating it) guarantees priority 0 is really attempted this run.
 * A stale fixture referenced by the previous binding targets is reused instead
 * of deleted, so the restore step can never reference a missing provider.
 */
async function ensureBrokenProvider(token, previousTargets) {
  const providers = await listProviders(token);
  const stale = providers.filter((p) => normalizeUrl(p.base_url) === normalizeUrl(BROKEN_BASE_URL));
  const referenced = new Set(previousTargets.map((t) => t.provider_id));

  const reusable = stale.find((p) => referenced.has(p.id));
  for (const provider of stale) {
    if (reusable && provider.id === reusable.id) continue;
    const res = await request('DELETE', `/api/admin/ai-providers/${encodeURIComponent(provider.id)}`, { token });
    if (res.status !== 200) {
      log(`warn: could not delete stale broken provider ${provider.id} (HTTP ${res.status})`);
    } else {
      log(`deleted stale broken fixture id=${provider.id}`);
    }
  }

  if (reusable) {
    log(`reusing pre-existing broken fixture id=${reusable.id} (referenced by previous targets)`);
    return { provider: reusable, created: false };
  }

  const res = expectStatus(
    await request('POST', '/api/admin/ai-providers', {
      token,
      body: {
        name: BROKEN_PROVIDER_NAME,
        kind: 'text',
        api_type: 'chat_completions',
        base_url: BROKEN_BASE_URL,
        api_key: BROKEN_API_KEY,
        models: [BROKEN_MODEL],
        default_model: BROKEN_MODEL,
        timeout_ms: 3000,
        allowed_hosts: '',
        enabled: true,
        sort_order: 0,
      },
    }),
    201,
    'POST broken provider',
  );
  const provider = res.json.provider;
  log(`created broken fixture id=${provider.id} base_url=${provider.base_url} timeout_ms=${provider.timeout_ms} key=${maskProviderKey(provider)}`);
  return { provider, created: true };
}

async function deleteStaleApps(token) {
  const res = await request('GET', '/api/admin/api-apps', { token });
  if (res.status !== 200) return;
  for (const app of res.json?.apps ?? []) {
    if (app.name !== APP_NAME) continue;
    const del = await request('DELETE', `/api/admin/api-apps/${encodeURIComponent(app.id)}`, { token });
    log(`deleted stale E2E api app id=${app.id} (HTTP ${del.status})`);
  }
}

// ---------------------------------------------------------------------------
// Fallback constants (extracted from the real Go source when available)
// ---------------------------------------------------------------------------

/** Extract every `return "..."` string literal inside a named Go function. */
function extractGoFuncReturns(relPath, funcName) {
  try {
    const src = readFileSync(resolve(ROOT, relPath), 'utf8');
    const start = src.indexOf(`func ${funcName}(`);
    if (start < 0) return [];
    // The first column-0 closing brace after the function header ends the body
    // (nested switch/if blocks are indented).
    const end = src.indexOf('\n}', start);
    const block = src.slice(start, end < 0 ? src.length : end);
    const out = [];
    for (const m of block.matchAll(/return\s+"((?:[^"\\]|\\.)*)"/g)) {
      try {
        out.push(JSON.parse(`"${m[1]}"`));
      } catch {
        out.push(m[1]);
      }
    }
    return out;
  } catch {
    return [];
  }
}

/** Extract a `const NAME = "..."` string literal from a Go file. */
function extractGoConst(relPath, constName) {
  try {
    const src = readFileSync(resolve(ROOT, relPath), 'utf8');
    const m = new RegExp(`${constName}\\s*=\\s*"((?:[^"\\\\]|\\\\.)*)"`).exec(src);
    return m ? m[1] : '';
  } catch {
    return '';
  }
}

/**
 * Every local string the AI reception path can persist instead of a real
 * provider answer. Extracted from the Go sources so the assertion stays tied to
 * the code (fallbacks are language-specific, so we must cover all variants).
 * Hardcoded mirrors keep the check working if the sources move.
 */
function collectFallbackStrings() {
  const set = new Set();
  for (const s of extractGoFuncReturns('servicebridge/backend/internal/ai/client.go', 'fallbackReply')) set.add(s);
  for (const s of extractGoFuncReturns('servicebridge/backend/internal/app/service.go', 'aiBusyText')) set.add(s);
  const refuse = extractGoConst('servicebridge/backend/internal/domain/models.go', 'DefaultKnowledgeRefuseReply');
  if (refuse) set.add(refuse);
  const limit = extractGoConst('servicebridge/backend/internal/domain/ai_limits.go', 'DefaultAILimitExceedMessage');
  if (limit) set.add(limit);
  set.add(FALLBACK_REPLY_ZH_FALLBACK);
  set.add(AI_BUSY_TEXT_ZH_FALLBACK);
  set.add(
    'Our AI assistant is temporarily busy. Your question has been recorded and our support team will follow up shortly.',
  );
  return [...set].map((s) => String(s).trim()).filter((s) => s.length > 0);
}

// ---------------------------------------------------------------------------
// Visitor flow
// ---------------------------------------------------------------------------

async function pollForAiReply(appKey, conversationId) {
  const deadline = Date.now() + POLL_TIMEOUT_MS;
  let lastPage = null;
  while (Date.now() < deadline) {
    const res = await request('GET', `/open/v1/conversations/${encodeURIComponent(conversationId)}/messages?limit=50`, {
      apiKey: appKey,
      timeoutMs: 15_000,
    });
    if (res.status === 200) {
      lastPage = res.json;
      const messages = res.json?.messages ?? [];
      const aiMessages = messages.filter((m) => m.sender_type === 'ai' && String(m.content || '').trim() !== '');
      if (aiMessages.length) {
        return { reply: aiMessages[aiMessages.length - 1], page: res.json };
      }
    }
    await sleep(POLL_INTERVAL_MS);
  }
  return { reply: null, page: lastPage };
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main() {
  registerSecret(cfg.mimoKey);
  registerSecret(BROKEN_API_KEY);

  const fallbackStrings = collectFallbackStrings();
  log(`engine=${cfg.engineUrl} admin=${cfg.adminAccount} mimo_base_url=${cfg.mimoBaseUrl} mimo_model=${cfg.mimoModel} mimo_key=${maskKey(cfg.mimoKey)}`);
  log(`loaded ${fallbackStrings.length} local fallback strings from the Go sources (fallbackReply/aiBusyText/refuse/limit)`);

  const state = {
    token: '',
    previousTargets: null,
    previousPriority: null,
    policyChanged: false,
    mimoId: '',
    brokenId: '',
    brokenCreated: false,
    appId: '',
    appKey: '',
    conversationId: '',
  };

  try {
    // 1. Authenticate as the seeded superadmin.
    const login = expectStatus(
      await request('POST', '/api/admin/login', {
        body: { account: cfg.adminAccount, password: cfg.adminPassword },
      }),
      200,
      'POST /api/admin/login',
    );
    state.token = login.json.token;
    log(`admin login ok account=${login.json.admin?.account} token=${maskKey(state.token)}`);

    // 2. Snapshot current state for verbatim restore.
    state.previousTargets = await listTargets(state.token);
    const policyRes = expectStatus(
      await request('GET', '/api/admin/service-policy', { token: state.token }),
      200,
      'GET service-policy',
    );
    state.previousPriority = policyRes.json.policy.ai_keyword_priority;
    log(`previous binding targets (${state.previousTargets.length}): ${snippet(state.previousTargets, 400)}`);
    log(`previous ai_keyword_priority=${state.previousPriority}`);

    // 3. Ensure the real MiMo provider + verify with the test endpoint.
    const providers = await listProviders(state.token);
    const mimo = await ensureMimoProvider(state.token, providers);
    state.mimoId = mimo.id;
    const mimoTest = expectStatus(
      await request('POST', `/api/admin/ai-providers/${encodeURIComponent(mimo.id)}/test`, { token: state.token, timeoutMs: 60_000 }),
      200,
      'POST MiMo test',
    );
    check(
      'real MiMo provider test-connection succeeds',
      mimoTest.json?.ok === true,
      `ok=${mimoTest.json?.ok} latency_ms=${mimoTest.json?.latency_ms} message=${mimoTest.json?.message}`,
    );

    // 4. Ensure a fresh broken fixture.
    const broken = await ensureBrokenProvider(state.token, state.previousTargets);
    state.brokenId = broken.provider.id;
    state.brokenCreated = broken.created;

    // 5. Configure the reception failover chain via the console endpoint.
    const otherTargets = state.previousTargets
      .filter((t) => String(t.capability || '').toLowerCase() !== 'reception')
      .map((t) => ({
        capability: t.capability,
        provider_id: t.provider_id,
        model: t.model || '',
        priority: t.priority ?? 0,
        enabled: t.enabled !== false,
      }));
    const desiredTargets = [
      ...otherTargets,
      { capability: 'reception', provider_id: state.brokenId, model: BROKEN_MODEL, priority: 0, enabled: true },
      { capability: 'reception', provider_id: state.mimoId, model: cfg.mimoModel, priority: 1, enabled: true },
    ];
    const putTargets = expectStatus(
      await request('PUT', '/api/admin/ai-binding-targets', { token: state.token, body: { targets: desiredTargets } }),
      200,
      'PUT ai-binding-targets',
    );
    const receptionChain = (putTargets.json?.targets ?? [])
      .filter((t) => t.capability === 'reception')
      .sort((a, b) => a.priority - b.priority);
    check(
      'reception chain saved as [broken@0, mimo@1] via PUT /api/admin/ai-binding-targets',
      receptionChain.length === 2 &&
        receptionChain[0].provider_id === state.brokenId &&
        receptionChain[0].priority === 0 &&
        receptionChain[0].enabled === true &&
        receptionChain[1].provider_id === state.mimoId &&
        receptionChain[1].priority === 1 &&
        receptionChain[1].enabled === true,
      receptionChain,
    );

    // 6. Make sure a visitor message actually reaches the AI reception path.
    if (state.previousPriority !== 'ai_only') {
      const patch = expectStatus(
        await request('PATCH', '/api/admin/service-policy', { token: state.token, body: { ai_keyword_priority: 'ai_only' } }),
        200,
        'PATCH service-policy',
      );
      state.policyChanged = true;
      log(`service policy ai_keyword_priority -> ai_only (will restore to ${state.previousPriority}); now=${patch.json.policy.ai_keyword_priority}`);
    } else {
      log('service policy ai_keyword_priority already ai_only; leaving untouched');
    }

    // 7. Throwaway API app + public visitor conversation.
    await deleteStaleApps(state.token);
    const appRes = expectStatus(
      await request('POST', '/api/admin/api-apps', {
        token: state.token,
        body: { name: APP_NAME, scopes: [], note: 'created by scripts/verify-ai-failover.mjs' },
      }),
      201,
      'POST api-apps',
    );
    state.appId = appRes.json.app.id;
    state.appKey = appRes.json.api_key;
    registerSecret(state.appKey);
    log(`created throwaway api app id=${state.appId} key=${maskKey(state.appKey)}`);

    const convRes = expectStatus(
      await request('POST', '/open/v1/conversations', { apiKey: state.appKey, body: { lang: 'zh' } }),
      201,
      'POST /open/v1/conversations',
    );
    state.conversationId = convRes.json.conversation.id;
    log(`created visitor conversation id=${state.conversationId} status=${convRes.json.conversation.status}`);

    // A previous failed run may have left a provider in circuit cooldown; wait it
    // out so both candidates are genuinely attempted in this run.
    await waitForCircuitClosed(state.token, state.mimoId, 'MiMo');
    await waitForCircuitClosed(state.token, state.brokenId, 'broken fixture');

    // Health snapshot before the message, to prove the broken candidate is attempted now.
    const healthBefore = await getHealth(state.token);
    const brokenBefore = healthBefore.find((h) => h.id === state.brokenId);
    const brokenFailuresBefore = brokenBefore?.consecutive_failures ?? 0;

    // 8. Post a real visitor message -> triggers the auto-reply / AI path.
    const msgRes = expectStatus(
      await request('POST', `/open/v1/conversations/${encodeURIComponent(state.conversationId)}/messages`, {
        apiKey: state.appKey,
        body: { content: TEST_MESSAGE, message_type: 'text', client_msg_id: `e2e-${Date.now()}` },
      }),
      201,
      'POST visitor message',
    );
    log(`posted visitor message server_msg_id=${msgRes.json.message.server_msg_id} generated=${(msgRes.json.generated || []).length}`);
    const conversationAfterPost = msgRes.json.conversation;
    check(
      'visitor message entered the AI reception path (conversation status ai_serving)',
      conversationAfterPost?.status === 'ai_serving',
      `status=${conversationAfterPost?.status}`,
    );

    // 9. Poll for the AI reply (failover: broken fails, MiMo answers).
    const { reply, page } = await pollForAiReply(state.appKey, state.conversationId);
    const replyText = String(reply?.content || '').trim();
    check(
      'AI reply was produced despite priority-0 provider being broken',
      replyText.length > 0,
      reply ? { sender_type: reply.sender_type, message_type: reply.message_type, content: replyText } : `messages=${snippet(page, 500)}`,
    );

    if (replyText) {
      log(`AI reply (${replyText.length} chars): ${replyText}`);
    }
    const matchedFallback = fallbackStrings.find((s) => s === replyText);
    check(
      'AI reply is NOT a local fallback string (proves a real provider answered)',
      replyText.length > 0 && !matchedFallback,
      matchedFallback
        ? `matched local fallback: ${matchedFallback}`
        : `reply differs from all ${fallbackStrings.length} known local fallbacks (len=${replyText.length})`,
    );

    // 10. Health assertions.
    const health = await getHealth(state.token);
    const brokenHealth = health.find((h) => h.id === state.brokenId);
    const mimoHealth = health.find((h) => h.id === state.mimoId);
    log(`health snapshot: ${snippet(health, 900)}`);

    check(
      'broken provider health shows a failure signal (consecutive_failures >= 1 or state open)',
      Boolean(brokenHealth) &&
        ((brokenHealth.consecutive_failures ?? 0) >= 1 || brokenHealth.state === 'open'),
      brokenHealth,
    );
    check(
      'broken provider was actually attempted this run (failures increased from before the message)',
      Boolean(brokenHealth) && (brokenHealth.consecutive_failures ?? 0) > brokenFailuresBefore,
      `before=${brokenFailuresBefore} after=${brokenHealth?.consecutive_failures}`,
    );
    check(
      'broken provider last_error names the dead upstream (priority 0 was attempted first)',
      Boolean(brokenHealth) &&
        String(brokenHealth.last_error || '').trim() !== '' &&
        /127\.0\.0\.1:9|connection refused|connect/i.test(String(brokenHealth.last_error || '')),
      brokenHealth?.last_error,
    );
    check(
      'real MiMo provider stays healthy (no consecutive failures, circuit closed)',
      Boolean(mimoHealth) &&
        (mimoHealth.consecutive_failures ?? 0) === 0 &&
        mimoHealth.state === 'closed',
      mimoHealth ?? 'no health entry (provider never failed)',
    );
  } finally {
    // 11. Cleanup: restore previous state verbatim, remove throwaway fixtures.
    if (state.token) {
      if (state.previousTargets !== null) {
        const restore = state.previousTargets.map((t) => ({
          capability: t.capability,
          provider_id: t.provider_id,
          model: t.model || '',
          priority: t.priority ?? 0,
          enabled: t.enabled !== false,
        }));
        try {
          const res = await request('PUT', '/api/admin/ai-binding-targets', { token: state.token, body: { targets: restore } });
          log(`restored previous binding targets (HTTP ${res.status}, ${restore.length} targets)`);
        } catch (err) {
          log(`warn: failed to restore binding targets: ${err.message}`);
        }
      }
      if (state.policyChanged && state.previousPriority) {
        try {
          const res = await request('PATCH', '/api/admin/service-policy', {
            token: state.token,
            body: { ai_keyword_priority: state.previousPriority },
          });
          log(`restored ai_keyword_priority=${state.previousPriority} (HTTP ${res.status})`);
        } catch (err) {
          log(`warn: failed to restore service policy: ${err.message}`);
        }
      }
      if (state.conversationId) {
        try {
          const closeRes = await request('POST', `/open/v1/conversations/${encodeURIComponent(state.conversationId)}/close`, {
            apiKey: state.appKey,
          });
          const delRes = await request('DELETE', `/api/conversations/${encodeURIComponent(state.conversationId)}`, {
            token: state.token,
          });
          log(`removed test conversation id=${state.conversationId} (close HTTP ${closeRes.status}, delete HTTP ${delRes.status})`);
        } catch (err) {
          log(`warn: failed to remove test conversation: ${err.message}`);
        }
      }
      if (state.appId) {
        try {
          const res = await request('DELETE', `/api/admin/api-apps/${encodeURIComponent(state.appId)}`, { token: state.token });
          log(`deleted throwaway api app id=${state.appId} (HTTP ${res.status})`);
        } catch (err) {
          log(`warn: failed to delete api app: ${err.message}`);
        }
      }
      if (state.brokenCreated && state.brokenId) {
        try {
          const res = await request('DELETE', `/api/admin/ai-providers/${encodeURIComponent(state.brokenId)}`, { token: state.token });
          log(`deleted throwaway broken fixture id=${state.brokenId} (HTTP ${res.status})`);
        } catch (err) {
          log(`warn: failed to delete broken fixture: ${err.message}`);
        }
      } else if (state.brokenId) {
        log(`kept pre-existing broken fixture id=${state.brokenId}`);
      }
      log(`kept real MiMo provider id=${state.mimoId || '(none)'}`);
    }
  }

  // 12. Summary.
  const failed = results.filter((r) => !r.ok);
  console.log('');
  console.log('==================== verify-ai-failover summary ====================');
  for (const r of results) console.log(`${r.ok ? 'PASS' : 'FAIL'}  ${r.name}`);
  console.log(`-------------------------------------------------------------------`);
  console.log(`${results.length - failed.length}/${results.length} assertions passed`);
  if (failed.length) {
    console.log(`RESULT: FAIL (${failed.length} failed)`);
    return 1;
  }
  console.log('RESULT: PASS — AI provider failover delivered a real reply via MiMo after the priority-0 provider failed.');
  return 0;
}

main()
  .then((code) => {
    process.exitCode = code;
  })
  .catch((err) => {
    console.error(`[verify-ai-failover] FATAL: ${redact(err?.stack || err?.message || String(err))}`);
    console.error('RESULT: FAIL (fatal error)');
    process.exitCode = 1;
  });
