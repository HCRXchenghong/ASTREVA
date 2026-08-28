import assert from 'node:assert/strict';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { createPlatformStore } from './platform-store.mjs';

function files(root) {
  return {
    content: path.join(root, 'site-content.json'),
    users: path.join(root, 'admin-users.json'),
    leads: path.join(root, 'leads.json'),
    fallbackVersions: path.join(root, 'fallback-versions.json'),
    audit: path.join(root, 'site-audit-events.json'),
    publishJobs: path.join(root, 'site-publish-jobs.json'),
    uploadDir: path.join(root, 'uploads')
  };
}

test('切换前 JSON 数据可被完整读取，供一次性 PostgreSQL 导入核验', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'astreva-platform-store-'));
  const store = createPlatformStore({ driver: 'json', files: files(root) });
  const content = { site: { brandName: '测试站点' }, staticPages: [] };
  const version = { id: 'fallback_test', name: '测试降级版本', content, createdAt: '2026-01-01T00:00:00.000Z', updatedAt: '2026-01-01T00:00:00.000Z' };

  await store.saveContent(content);
  await store.saveUsers({ users: [{ id: 'admin-1', email: 'admin@example.com', passwordHash: 'hash', totpSecret: 'secret' }] });
  await store.saveLeads({ leads: [{ id: 'lead-1', submittedAt: '2026-01-01T00:00:00.000Z' }] });
  await store.saveFallbackStore({ activeId: version.id, versions: [version] });
  await store.appendAudit({ action: 'content.saved', resource: 'home' });
  const jobId = await store.enqueuePublish('admin-1');
  await store.markPublishRunning(jobId);
  await store.completePublish(jobId, 'succeeded');

  assert.deepEqual(await store.readContent({}), content);
  assert.equal((await store.readUsers()).users[0].email, 'admin@example.com');
  assert.equal((await store.readLeads()).leads[0].id, 'lead-1');
  assert.equal((await store.readFallbackStore({})).activeId, version.id);
  assert.equal((await store.listAudit())[0].action, 'content.saved');
  assert.equal((await store.listPublishJobs())[0].status, 'succeeded');
});
