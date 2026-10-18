import crypto from 'node:crypto';
import { existsSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import pg from 'pg';

const { Pool } = pg;
const moduleDir = path.dirname(fileURLToPath(import.meta.url));
const migrationFile = path.join(moduleDir, '..', 'migrations', '001_cms.sql');

async function readJson(file, fallback) {
  try {
    return JSON.parse(await fs.readFile(file, 'utf8'));
  } catch {
    return fallback;
  }
}

async function writeJson(file, value) {
  await fs.mkdir(path.dirname(file), { recursive: true });
  await fs.writeFile(file, `${JSON.stringify(value, null, 2)}\n`);
}

function toIso(value) {
  return value ? new Date(value).toISOString() : new Date().toISOString();
}

function stableValue(value) {
  if (Array.isArray(value)) return value.map(stableValue);
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, stableValue(value[key])]));
  }
  return value;
}

function stableHash(value) {
  return crypto.createHash('sha256').update(JSON.stringify(stableValue(value))).digest('hex');
}

function createCipher(secret = '') {
  if (!secret) return { encrypt: (value) => value, decrypt: (value) => value, enabled: false };
  const key = crypto.createHash('sha256').update(secret).digest();
  return {
    enabled: true,
    encrypt(value) {
      const iv = crypto.randomBytes(12);
      const cipher = crypto.createCipheriv('aes-256-gcm', key, iv);
      const ciphertext = Buffer.concat([cipher.update(String(value), 'utf8'), cipher.final()]);
      return `v1:${iv.toString('base64url')}:${cipher.getAuthTag().toString('base64url')}:${ciphertext.toString('base64url')}`;
    },
    decrypt(value) {
      if (!String(value).startsWith('v1:')) return value;
      const [, ivValue, tagValue, ciphertextValue] = String(value).split(':');
      const decipher = crypto.createDecipheriv('aes-256-gcm', key, Buffer.from(ivValue, 'base64url'));
      decipher.setAuthTag(Buffer.from(tagValue, 'base64url'));
      return Buffer.concat([decipher.update(Buffer.from(ciphertextValue, 'base64url')), decipher.final()]).toString('utf8');
    }
  };
}

/**
 * CMS 持久化层。生产使用 PostgreSQL；json 模式仅保留给本地开发和首次导入。
 */
export function createPlatformStore({ driver = 'json', databaseUrl = '', encryptionKey = '', files, logger = console }) {
  const usePostgres = driver === 'postgres';
  const pool = usePostgres ? new Pool({ connectionString: databaseUrl, max: Number(process.env.CMS_DB_POOL_MAX || 8) }) : null;
  const cipher = createCipher(encryptionKey);
  const testRuntimeSettings = new Map();
  const testTranslations = new Map();
  let testContentRevision = 1;

  async function query(sql, params = []) {
    if (!pool) throw new Error('当前 CMS 未启用 PostgreSQL 存储。');
    return pool.query(sql, params);
  }

  async function withTransaction(callback) {
    const client = await pool.connect();
    try {
      await client.query('BEGIN');
      const result = await callback(client);
      await client.query('COMMIT');
      return result;
    } catch (error) {
      await client.query('ROLLBACK').catch(() => {});
      throw error;
    } finally {
      client.release();
    }
  }

  async function init() {
    if (!usePostgres) return;
    if (!databaseUrl) throw new Error('CMS_STORE_DRIVER=postgres 时必须配置 DATABASE_URL。');
    if (!cipher.enabled) throw new Error('CMS_STORE_DRIVER=postgres 时必须配置 CMS_DATA_ENCRYPTION_KEY 或 DATA_ENCRYPTION_KEY。');
    await query(await fs.readFile(migrationFile, 'utf8'));
    await query('SELECT 1');
    logger.info('[cms-store] PostgreSQL 已就绪。旧 JSON 只能通过 cms:cutover 一次性导入。');
  }

  async function readContentRecord(defaultContent) {
    if (!usePostgres) {
      return { content: await readJson(files.content, defaultContent), revision: testContentRevision, updatedAt: new Date().toISOString() };
    }
    const result = await query('SELECT content, revision, updated_at FROM site_content WHERE id = 1');
    if (result.rows[0]) {
      return { content: result.rows[0].content, revision: Number(result.rows[0].revision), updatedAt: toIso(result.rows[0].updated_at) };
    }
    const inserted = await query(
      'INSERT INTO site_content (id, content, revision) VALUES (1, $1::jsonb, 1) RETURNING content, revision, updated_at',
      [JSON.stringify(defaultContent)]
    );
    await auditSystem('content.initialized', { source: 'immutable-container-seed' });
    return { content: inserted.rows[0].content, revision: Number(inserted.rows[0].revision), updatedAt: toIso(inserted.rows[0].updated_at) };
  }

  async function readContent(defaultContent) {
    return (await readContentRecord(defaultContent)).content;
  }

  async function saveContent(content, options = {}) {
    const expectedRevision = options.expectedRevision == null ? null : Number(options.expectedRevision);
    if (!usePostgres) {
      if (expectedRevision != null && expectedRevision !== testContentRevision) {
        const error = new Error('内容已被其他页面更新，请刷新后再保存。');
        error.code = 'CONTENT_REVISION_CONFLICT';
        throw error;
      }
      await writeJson(files.content, content);
      testContentRevision += 1;
      return { revision: testContentRevision, updatedAt: new Date().toISOString() };
    }
    let result;
    if (expectedRevision == null) {
      result = await query(
        `INSERT INTO site_content (id, content, revision, updated_at) VALUES (1, $1::jsonb, 1, now())
         ON CONFLICT (id) DO UPDATE SET content = EXCLUDED.content, revision = site_content.revision + 1, updated_at = now()
         RETURNING revision, updated_at`,
        [JSON.stringify(content)]
      );
    } else {
      result = await query(
        'UPDATE site_content SET content = $1::jsonb, revision = revision + 1, updated_at = now() WHERE id = 1 AND revision = $2 RETURNING revision, updated_at',
        [JSON.stringify(content), expectedRevision]
      );
      if (!result.rows[0]) {
        const error = new Error('内容已被其他页面更新，请刷新后再保存。');
        error.code = 'CONTENT_REVISION_CONFLICT';
        throw error;
      }
    }
    return { revision: Number(result.rows[0].revision), updatedAt: toIso(result.rows[0].updated_at) };
  }

  async function listContentTranslations() {
    if (!usePostgres) return [...testTranslations.values()].map((item) => structuredClone(item));
    const result = await query(
      'SELECT locale, content, source_revision, status, last_error, started_at, finished_at, updated_at FROM site_content_translations ORDER BY locale'
    );
    return result.rows.map((row) => ({
      locale: row.locale,
      content: row.content,
      sourceRevision: Number(row.source_revision),
      status: row.status,
      lastError: row.last_error || '',
      startedAt: row.started_at ? toIso(row.started_at) : null,
      finishedAt: row.finished_at ? toIso(row.finished_at) : null,
      updatedAt: toIso(row.updated_at)
    }));
  }

  async function readContentTranslation(locale) {
    return (await listContentTranslations()).find((item) => item.locale === locale) || null;
  }

  async function saveContentTranslation(locale, value) {
    if (!['en', 'ar'].includes(locale)) throw new Error('仅支持英语和阿拉伯语译文。');
    const record = {
      locale,
      content: value.content ?? null,
      sourceRevision: Number(value.sourceRevision || 0),
      status: value.status || 'pending',
      lastError: String(value.lastError || '').slice(0, 4000),
      startedAt: value.startedAt || null,
      finishedAt: value.finishedAt || null,
      updatedAt: new Date().toISOString()
    };
    if (!usePostgres) {
      testTranslations.set(locale, structuredClone(record));
      return record;
    }
    const result = await query(
      `INSERT INTO site_content_translations (locale, content, source_revision, status, last_error, started_at, finished_at, updated_at)
       VALUES ($1, $2::jsonb, $3, $4, $5, $6, $7, now())
       ON CONFLICT (locale) DO UPDATE SET content = EXCLUDED.content, source_revision = EXCLUDED.source_revision,
         status = EXCLUDED.status, last_error = EXCLUDED.last_error, started_at = EXCLUDED.started_at,
         finished_at = EXCLUDED.finished_at, updated_at = now()
       RETURNING updated_at`,
      [locale, record.content == null ? null : JSON.stringify(record.content), record.sourceRevision, record.status, record.lastError, record.startedAt, record.finishedAt]
    );
    record.updatedAt = toIso(result.rows[0].updated_at);
    return record;
  }

  async function markContentTranslationsPending(sourceRevision) {
    const current = new Map((await listContentTranslations()).map((item) => [item.locale, item]));
    return Promise.all(['en', 'ar'].map((locale) => saveContentTranslation(locale, {
      content: current.get(locale)?.content || null,
      sourceRevision,
      status: 'pending',
      lastError: '',
      startedAt: null,
      finishedAt: null
    })));
  }

  async function readUsers() {
    if (!usePostgres) return readJson(files.users, { users: [] });
    const result = await query('SELECT id, email, password_hash, totp_secret, created_at FROM site_admin_users ORDER BY created_at ASC');
    if (result.rows.length) {
      return {
        users: result.rows.map((row) => ({
          id: row.id,
          email: row.email,
          passwordHash: row.password_hash,
          totpSecret: cipher.decrypt(row.totp_secret),
          createdAt: toIso(row.created_at)
        }))
      };
    }
    return { users: [] };
  }

  async function saveUsers(store) {
    if (!usePostgres) return writeJson(files.users, store);
    await withTransaction(async (client) => {
      await client.query('DELETE FROM site_admin_users');
      for (const user of store.users || []) {
        await client.query(
          'INSERT INTO site_admin_users (id, email, password_hash, totp_secret, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, now())',
          [user.id, user.email, user.passwordHash, cipher.encrypt(user.totpSecret), user.createdAt || new Date().toISOString()]
        );
      }
    });
  }

  async function readLeads() {
    if (!usePostgres) return readJson(files.leads, { leads: [] });
    const result = await query('SELECT data FROM site_leads ORDER BY submitted_at DESC');
    if (result.rows.length) return { leads: result.rows.map((row) => row.data) };
    return { leads: [] };
  }

  async function saveLeads(store) {
    if (!usePostgres) return writeJson(files.leads, store);
    await withTransaction(async (client) => {
      await client.query('DELETE FROM site_leads');
      for (const lead of store.leads || []) {
        await client.query(
          'INSERT INTO site_leads (id, submitted_at, updated_at, data) VALUES ($1, $2, $3, $4::jsonb)',
          [lead.id, lead.submittedAt || new Date().toISOString(), lead.updatedAt || lead.submittedAt || new Date().toISOString(), JSON.stringify(lead)]
        );
      }
    });
  }

  async function readFallbackStore(defaultStore) {
    if (!usePostgres) return readJson(files.fallbackVersions, defaultStore);
    const [versionsResult, stateResult] = await Promise.all([
      query('SELECT id, name, content, created_at, updated_at FROM site_fallback_versions ORDER BY created_at DESC'),
      query('SELECT active_version_id FROM site_fallback_state WHERE id = 1')
    ]);
    if (!versionsResult.rows.length) {
      return defaultStore;
    }
    return {
      activeId: stateResult.rows[0]?.active_version_id || versionsResult.rows[0].id,
      versions: versionsResult.rows.map((row) => ({
        id: row.id,
        name: row.name,
        content: row.content,
        createdAt: toIso(row.created_at),
        updatedAt: toIso(row.updated_at)
      }))
    };
  }

  async function saveFallbackStore(store) {
    if (!usePostgres) return writeJson(files.fallbackVersions, store);
    await withTransaction(async (client) => {
      await client.query('DELETE FROM site_fallback_state WHERE id = 1');
      await client.query('DELETE FROM site_fallback_versions');
      for (const version of store.versions || []) {
        await client.query(
          'INSERT INTO site_fallback_versions (id, name, content, created_at, updated_at) VALUES ($1, $2, $3::jsonb, $4, $5)',
          [version.id, version.name, JSON.stringify(version.content), version.createdAt, version.updatedAt]
        );
      }
      if (store.activeId) {
        await client.query('INSERT INTO site_fallback_state (id, active_version_id, updated_at) VALUES (1, $1, now())', [store.activeId]);
      }
    });
  }

  const mediaColumns = `id, name, object_key, url, mime_type, media_type, size_bytes, storage_driver,
    category, source_type, original_key, width, height, checksum_sha256, variants, metadata, created_at, updated_at`;

  function mediaFromRow(row) {
    if (!row) return null;
    return {
      id: row.id,
      name: row.name,
      key: row.object_key,
      url: row.url,
      mime: row.mime_type,
      type: row.media_type,
      size: Number(row.size_bytes),
      storageDriver: row.storage_driver,
      category: row.category || '未分类',
      sourceType: row.source_type || 'upload',
      originalKey: row.original_key || '',
      width: row.width == null ? null : Number(row.width),
      height: row.height == null ? null : Number(row.height),
      checksumSha256: row.checksum_sha256 || '',
      variants: row.variants || {},
      metadata: row.metadata || {},
      createdAt: toIso(row.created_at),
      updatedAt: toIso(row.updated_at)
    };
  }

  async function listMedia() {
    if (!usePostgres) return [];
    const result = await query(`SELECT ${mediaColumns} FROM site_media ORDER BY updated_at DESC`);
    return result.rows.map(mediaFromRow);
  }

  async function searchMedia(options = {}) {
    if (!usePostgres) return { items: [], total: 0, page: 1, pageSize: 24, pages: 0 };
    const q = String(options.q || '').trim().slice(0, 200);
    const category = String(options.category || '').trim().slice(0, 80);
    const type = ['image', 'video', 'file'].includes(options.type) ? options.type : '';
    const page = Math.max(1, Number.parseInt(options.page, 10) || 1);
    const pageSize = Math.max(1, Math.min(Number.parseInt(options.pageSize, 10) || 24, 100));
    const where = [];
    const params = [];
    if (q) {
      params.push(`%${q}%`);
      where.push(`(name ILIKE $${params.length} OR object_key ILIKE $${params.length} OR metadata::text ILIKE $${params.length})`);
    }
    if (category) {
      params.push(category);
      where.push(`category = $${params.length}`);
    }
    if (type) {
      params.push(type);
      where.push(`media_type = $${params.length}`);
    }
    const clause = where.length ? `WHERE ${where.join(' AND ')}` : '';
    const countResult = await query(`SELECT count(*)::int AS total FROM site_media ${clause}`, params);
    const total = countResult.rows[0]?.total || 0;
    params.push(pageSize, (page - 1) * pageSize);
    const result = await query(
      `SELECT ${mediaColumns} FROM site_media ${clause} ORDER BY updated_at DESC LIMIT $${params.length - 1} OFFSET $${params.length}`,
      params
    );
    return { items: result.rows.map(mediaFromRow), total, page, pageSize, pages: total ? Math.ceil(total / pageSize) : 0 };
  }

  async function getMedia(id) {
    if (!usePostgres) return null;
    const result = await query(`SELECT ${mediaColumns} FROM site_media WHERE id = $1`, [id]);
    return mediaFromRow(result.rows[0]);
  }

  async function saveMedia(media) {
    if (!usePostgres) return;
    const category = String(media.category || '未分类').trim().slice(0, 80) || '未分类';
    await withTransaction(async (client) => {
      await client.query('INSERT INTO site_media_categories (name) VALUES ($1) ON CONFLICT (name) DO NOTHING', [category]);
      await client.query(
        `INSERT INTO site_media
          (id, name, object_key, url, mime_type, media_type, size_bytes, storage_driver, category, source_type,
           original_key, width, height, checksum_sha256, variants, metadata, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb, now())
         ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, object_key = EXCLUDED.object_key, url = EXCLUDED.url,
           mime_type = EXCLUDED.mime_type, media_type = EXCLUDED.media_type, size_bytes = EXCLUDED.size_bytes,
           storage_driver = EXCLUDED.storage_driver, category = EXCLUDED.category, source_type = EXCLUDED.source_type,
           original_key = EXCLUDED.original_key, width = EXCLUDED.width, height = EXCLUDED.height,
           checksum_sha256 = EXCLUDED.checksum_sha256, variants = EXCLUDED.variants, metadata = EXCLUDED.metadata, updated_at = now()`,
        [
          media.id || crypto.randomUUID(), media.name, media.key, media.url, media.mime || '', media.type, media.size || 0,
          media.storageDriver, category, media.sourceType || 'upload', media.originalKey || '', media.width ?? null,
          media.height ?? null, media.checksumSha256 || '', JSON.stringify(media.variants || {}), JSON.stringify(media.metadata || {})
        ]
      );
    });
  }

  async function deleteMedia(id) {
    if (!usePostgres) return false;
    const result = await query('DELETE FROM site_media WHERE id = $1 RETURNING id', [id]);
    return result.rowCount > 0;
  }

  async function listMediaCategories() {
    if (!usePostgres) return [{ name: '未分类', count: 0 }];
    const result = await query(
      `SELECT c.name, count(m.id)::int AS count
       FROM site_media_categories c LEFT JOIN site_media m ON m.category = c.name
       GROUP BY c.name, c.created_at ORDER BY CASE WHEN c.name = '未分类' THEN 0 ELSE 1 END, c.created_at ASC, c.name ASC`
    );
    return result.rows.map((row) => ({ name: row.name, count: row.count }));
  }

  async function createMediaCategory(name) {
    if (!usePostgres) return { name, count: 0 };
    const normalized = String(name || '').trim().replace(/\s+/g, ' ').slice(0, 80);
    if (!normalized) throw new Error('分类名称不能为空。');
    const existing = await query('SELECT name FROM site_media_categories WHERE lower(name) = lower($1)', [normalized]);
    if (existing.rows[0]) return { name: existing.rows[0].name, count: 0 };
    await query('INSERT INTO site_media_categories (name) VALUES ($1)', [normalized]);
    return { name: normalized, count: 0 };
  }

  async function deleteMediaCategory(name) {
    if (!usePostgres) return { name, reassigned: 0 };
    if (name === '未分类') throw new Error('默认的“未分类”不能删除。');
    return withTransaction(async (client) => {
      const moved = await client.query("UPDATE site_media SET category = '未分类', updated_at = now() WHERE category = $1", [name]);
      const deleted = await client.query('DELETE FROM site_media_categories WHERE name = $1 RETURNING name', [name]);
      if (!deleted.rows[0]) throw new Error('分类不存在或已被删除。');
      return { name, reassigned: moved.rowCount };
    });
  }

  async function mediaStats() {
    if (!usePostgres) return { total: 0, totalBytes: 0, storedBytes: 0, images: 0, videos: 0, files: 0, categories: 1 };
    const [mediaResult, categoryResult] = await Promise.all([
      query(`SELECT count(*)::int AS total, COALESCE(sum(size_bytes), 0)::bigint AS total_bytes,
        COALESCE(sum(size_bytes
          + COALESCE((SELECT sum((variant->>'size')::bigint) FROM jsonb_array_elements(COALESCE(variants->'webp', '[]'::jsonb)) AS variant), 0)
          + COALESCE((variants->'original'->>'size')::bigint, 0)), 0)::bigint AS stored_bytes,
        count(*) FILTER (WHERE media_type = 'image')::int AS images,
        count(*) FILTER (WHERE media_type = 'video')::int AS videos,
        count(*) FILTER (WHERE media_type = 'file')::int AS files FROM site_media`),
      query('SELECT count(*)::int AS categories FROM site_media_categories')
    ]);
    const row = mediaResult.rows[0];
    return {
      total: row.total,
      totalBytes: Number(row.total_bytes),
      storedBytes: Number(row.stored_bytes),
      images: row.images,
      videos: row.videos,
      files: row.files,
      categories: categoryResult.rows[0]?.categories || 0
    };
  }

  async function appendAudit(event) {
    if (!usePostgres) {
      const store = await readJson(files.audit, { events: [] });
      store.events.unshift({ id: crypto.randomUUID(), ...event, createdAt: new Date().toISOString() });
      store.events = store.events.slice(0, 5000);
      await writeJson(files.audit, store);
      return;
    }
    await query(
      'INSERT INTO site_audit_events (id, actor_id, actor_email, action, resource, resource_id, ip_address, user_agent, detail) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)',
      [crypto.randomUUID(), event.actorId || '', event.actorEmail || '', event.action, event.resource || '', event.resourceId || '', event.ip || '', event.userAgent || '', JSON.stringify(event.detail || {})]
    );
  }

  async function auditSystem(action, detail) {
    return appendAudit({ action, resource: 'system', detail });
  }

  async function readRuntimeSetting(settingKey) {
    const key = String(settingKey || '').trim();
    if (!key) throw new Error('运行时配置键不能为空。');
    if (!usePostgres) return testRuntimeSettings.get(key) || null;
    const result = await query('SELECT config, encrypted_secrets, updated_at FROM site_runtime_settings WHERE setting_key = $1', [key]);
    const row = result.rows[0];
    if (!row) return null;
    let secrets = {};
    if (row.encrypted_secrets) {
      try { secrets = JSON.parse(cipher.decrypt(row.encrypted_secrets)); }
      catch { throw new Error(`运行时配置 ${key} 的加密数据无法解密。`); }
    }
    return { config: row.config || {}, secrets, updatedAt: toIso(row.updated_at) };
  }

  async function saveRuntimeSetting(settingKey, value) {
    const key = String(settingKey || '').trim();
    if (!key) throw new Error('运行时配置键不能为空。');
    const record = {
      config: structuredClone(value?.config || {}),
      secrets: structuredClone(value?.secrets || {}),
      updatedAt: new Date().toISOString()
    };
    if (!usePostgres) {
      testRuntimeSettings.set(key, record);
      return record;
    }
    await query(
      `INSERT INTO site_runtime_settings (setting_key, config, encrypted_secrets, updated_at)
       VALUES ($1, $2::jsonb, $3, now())
       ON CONFLICT (setting_key) DO UPDATE SET config = EXCLUDED.config, encrypted_secrets = EXCLUDED.encrypted_secrets, updated_at = now()`,
      [key, JSON.stringify(record.config), cipher.encrypt(JSON.stringify(record.secrets))]
    );
    return record;
  }

  async function listAudit(limit = 100) {
    const safeLimit = Math.max(1, Math.min(Number(limit) || 100, 500));
    if (!usePostgres) {
      const store = await readJson(files.audit, { events: [] });
      return (store.events || []).slice(0, safeLimit);
    }
    const result = await query(
      'SELECT id, actor_id, actor_email, action, resource, resource_id, ip_address, user_agent, detail, created_at FROM site_audit_events ORDER BY created_at DESC LIMIT $1',
      [safeLimit]
    );
    return result.rows.map((row) => ({
      id: row.id,
      actorId: row.actor_id,
      actorEmail: row.actor_email,
      action: row.action,
      resource: row.resource,
      resourceId: row.resource_id,
      ip: row.ip_address,
      userAgent: row.user_agent,
      detail: row.detail,
      createdAt: toIso(row.created_at)
    }));
  }

  async function enqueuePublish(requestedBy = '') {
    const id = crypto.randomUUID();
    if (!usePostgres) {
      const store = await readJson(files.publishJobs, { jobs: [] });
      store.jobs.unshift({ id, status: 'queued', requestedBy, requestedAt: new Date().toISOString(), errorMessage: '', metadata: {} });
      store.jobs = store.jobs.slice(0, 200);
      await writeJson(files.publishJobs, store);
      return id;
    }
    await query('INSERT INTO site_publish_jobs (id, status, requested_by) VALUES ($1, $2, $3)', [id, 'queued', requestedBy]);
    return id;
  }

  async function markPublishRunning(id, metadata = {}) {
    if (!id) return;
    if (!usePostgres) {
      const store = await readJson(files.publishJobs, { jobs: [] });
      const job = store.jobs.find((item) => item.id === id);
      if (job) Object.assign(job, { status: 'running', startedAt: job.startedAt || new Date().toISOString(), metadata });
      await writeJson(files.publishJobs, store);
      return;
    }
    await query('UPDATE site_publish_jobs SET status = $2, started_at = COALESCE(started_at, now()), metadata = $3::jsonb WHERE id = $1', [id, 'running', JSON.stringify(metadata)]);
  }

  async function completePublish(id, status, errorMessage = '', metadata = {}) {
    if (!id) return;
    if (!usePostgres) {
      const store = await readJson(files.publishJobs, { jobs: [] });
      const job = store.jobs.find((item) => item.id === id);
      if (job) Object.assign(job, { status, finishedAt: new Date().toISOString(), errorMessage, metadata });
      await writeJson(files.publishJobs, store);
      return;
    }
    await query(
      'UPDATE site_publish_jobs SET status = $2, started_at = COALESCE(started_at, now()), finished_at = now(), error_message = $3, metadata = $4::jsonb WHERE id = $1',
      [id, status, errorMessage, JSON.stringify(metadata)]
    );
  }

  async function settleLatestPublish(status, metadata = {}) {
    if (!['succeeded', 'failed'].includes(status)) return;
    if (!usePostgres) {
      const store = await readJson(files.publishJobs, { jobs: [] });
      const job = store.jobs.find((item) => ['queued', 'running'].includes(item.status));
      if (job) Object.assign(job, { status, finishedAt: new Date().toISOString(), errorMessage: metadata.errorMessage || '', metadata });
      await writeJson(files.publishJobs, store);
      return;
    }
    await query(
      `UPDATE site_publish_jobs SET status = $1, finished_at = now(), error_message = $2, metadata = $3::jsonb
       WHERE id = (SELECT id FROM site_publish_jobs WHERE status IN ('queued', 'running') ORDER BY requested_at DESC LIMIT 1)`,
      [status, metadata.errorMessage || '', JSON.stringify(metadata)]
    );
  }

  async function listPublishJobs(limit = 30) {
    const safeLimit = Math.max(1, Math.min(Number(limit) || 30, 200));
    if (!usePostgres) return (await readJson(files.publishJobs, { jobs: [] })).jobs.slice(0, safeLimit);
    const result = await query('SELECT id, status, requested_by, requested_at, started_at, finished_at, error_message, metadata FROM site_publish_jobs ORDER BY requested_at DESC LIMIT $1', [safeLimit]);
    return result.rows.map((row) => ({
      id: row.id,
      status: row.status,
      requestedBy: row.requested_by,
      requestedAt: toIso(row.requested_at),
      startedAt: row.started_at ? toIso(row.started_at) : null,
      finishedAt: row.finished_at ? toIso(row.finished_at) : null,
      errorMessage: row.error_message,
      metadata: row.metadata
    }));
  }

  /**
   * 一次性导入旧 JSON 运行数据。该方法只在 PostgreSQL 目标库为空或切换期使用；
   * 调用方必须在比对报告通过后，才删除源文件。
   */
  async function migrateLegacyJson() {
    if (!usePostgres) throw new Error('旧数据切换只能导入 PostgreSQL。');
    const discarded = [];
    async function legacy(file, fallback) {
      if (!existsSync(file)) return { exists: false, value: fallback };
      try {
        return { exists: true, value: JSON.parse(await fs.readFile(file, 'utf8')) };
      } catch (error) {
        discarded.push({ file, reason: `JSON 无法解析：${error.message}` });
        return { exists: true, value: fallback };
      }
    }
    const [content, users, leads, fallbackVersions, audit, publishJobs] = await Promise.all([
      legacy(files.content, null),
      legacy(files.users, { users: [] }),
      legacy(files.leads, { leads: [] }),
      legacy(files.fallbackVersions, { activeId: '', versions: [] }),
      legacy(files.audit, { events: [] }),
      legacy(files.publishJobs, { jobs: [] })
    ]);

    if (content.exists && content.value && typeof content.value === 'object') await saveContent(content.value);
    else if (content.exists) discarded.push({ file: files.content, reason: '不是对象，未导入内容。' });
    if (users.exists && Array.isArray(users.value?.users)) await saveUsers(users.value);
    else if (users.exists) discarded.push({ file: files.users, reason: 'users 不是数组，未导入管理员。' });
    if (leads.exists && Array.isArray(leads.value?.leads)) await saveLeads(leads.value);
    else if (leads.exists) discarded.push({ file: files.leads, reason: 'leads 不是数组，未导入咨询单。' });
    if (fallbackVersions.exists && Array.isArray(fallbackVersions.value?.versions)) await saveFallbackStore(fallbackVersions.value);
    else if (fallbackVersions.exists) discarded.push({ file: files.fallbackVersions, reason: 'versions 不是数组，未导入降级版本。' });

    const auditEvents = Array.isArray(audit.value?.events) ? audit.value.events.filter((event) => event?.id && event?.action) : [];
    if (audit.exists && !Array.isArray(audit.value?.events)) discarded.push({ file: files.audit, reason: 'events 不是数组，未导入审计日志。' });
    const publishRows = Array.isArray(publishJobs.value?.jobs)
      ? publishJobs.value.jobs.filter((job) => job?.id && ['queued', 'running', 'succeeded', 'failed'].includes(job.status))
      : [];
    if (publishJobs.exists && !Array.isArray(publishJobs.value?.jobs)) discarded.push({ file: files.publishJobs, reason: 'jobs 不是数组，未导入发布任务。' });

    await withTransaction(async (client) => {
      for (const event of auditEvents) {
        await client.query(
          'INSERT INTO site_audit_events (id, actor_id, actor_email, action, resource, resource_id, ip_address, user_agent, detail, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10) ON CONFLICT (id) DO NOTHING',
          [event.id, event.actorId || '', event.actorEmail || '', event.action, event.resource || '', event.resourceId || '', event.ip || '', event.userAgent || '', JSON.stringify(event.detail || {}), event.createdAt || new Date().toISOString()]
        );
      }
      for (const job of publishRows) {
        await client.query(
          'INSERT INTO site_publish_jobs (id, status, requested_by, requested_at, started_at, finished_at, error_message, metadata) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb) ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, requested_by = EXCLUDED.requested_by, requested_at = EXCLUDED.requested_at, started_at = EXCLUDED.started_at, finished_at = EXCLUDED.finished_at, error_message = EXCLUDED.error_message, metadata = EXCLUDED.metadata',
          [job.id, job.status, job.requestedBy || '', job.requestedAt || new Date().toISOString(), job.startedAt || null, job.finishedAt || null, job.errorMessage || '', JSON.stringify(job.metadata || {})]
        );
      }
    });

    const source = {
      content: content.exists && content.value && typeof content.value === 'object' ? stableHash(content.value) : null,
      users: Array.isArray(users.value?.users) ? users.value.users.length : 0,
      leads: Array.isArray(leads.value?.leads) ? leads.value.leads.length : 0,
      fallbackVersions: Array.isArray(fallbackVersions.value?.versions) ? fallbackVersions.value.versions.length : 0,
      fallbackActiveId: fallbackVersions.value?.activeId || '',
      auditEvents: auditEvents.length,
      publishJobs: publishRows.length
    };
    const [storedContent, storedUsers, storedLeads, storedFallback, storedAudit, storedJobs] = await Promise.all([
      query('SELECT content FROM site_content WHERE id = 1'),
      query('SELECT count(*)::int AS count FROM site_admin_users'),
      query('SELECT count(*)::int AS count FROM site_leads'),
      query('SELECT count(*)::int AS count FROM site_fallback_versions'),
      query('SELECT count(*)::int AS count FROM site_audit_events WHERE id = ANY($1::text[])', [auditEvents.map((event) => event.id)]),
      query('SELECT count(*)::int AS count FROM site_publish_jobs WHERE id = ANY($1::text[])', [publishRows.map((job) => job.id)])
    ]);
    const activeResult = await query('SELECT active_version_id FROM site_fallback_state WHERE id = 1');
    const target = {
      content: storedContent.rows[0] ? stableHash(storedContent.rows[0].content) : null,
      users: storedUsers.rows[0].count,
      leads: storedLeads.rows[0].count,
      fallbackVersions: storedFallback.rows[0].count,
      fallbackActiveId: activeResult.rows[0]?.active_version_id || '',
      auditEvents: storedAudit.rows[0].count,
      publishJobs: storedJobs.rows[0].count
    };
    const verified =
      source.content === target.content &&
      source.users === target.users &&
      source.leads === target.leads &&
      source.fallbackVersions === target.fallbackVersions &&
      source.fallbackActiveId === target.fallbackActiveId &&
      source.auditEvents === target.auditEvents &&
      source.publishJobs === target.publishJobs;
    return { verified, source, target, discarded };
  }

  return {
    driver,
    init,
    close: () => pool?.end(),
    readContentRecord,
    readContent,
    saveContent,
    listContentTranslations,
    readContentTranslation,
    saveContentTranslation,
    markContentTranslationsPending,
    readUsers,
    saveUsers,
    readLeads,
    saveLeads,
    readFallbackStore,
    saveFallbackStore,
    listMedia,
    searchMedia,
    getMedia,
    saveMedia,
    deleteMedia,
    listMediaCategories,
    createMediaCategory,
    deleteMediaCategory,
    mediaStats,
    appendAudit,
    listAudit,
    readRuntimeSetting,
    saveRuntimeSetting,
    enqueuePublish,
    markPublishRunning,
    completePublish,
    settleLatestPublish,
    listPublishJobs,
    migrateLegacyJson,
    legacyMediaDirectoryExists: () => existsSync(files.uploadDir)
  };
}
