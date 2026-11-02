import { collectSeoTargets } from './website-seo-service.mjs';
import { collectTranslatableEntries } from './website-translation-service.mjs';

const SETTING_KEY = 'website-auto-seo';

// SEO-only and media-only edits must not start a new AI job. FAQ and other visible
// copy still count, even if they are not directly included in an SEO target.
export function needsAutomaticSeo(before, after) {
  const signature = (content) => JSON.stringify({
    pages: collectSeoTargets(content).map(({ id, name, source }) => ({ id, name, source })),
    copy: collectTranslatableEntries(content),
    paths: {
      categories: content.categories?.map(({ id, slug }) => [id, slug]),
      products: content.products?.map(({ id, categorySlug, slug }) => [id, categorySlug, slug]),
      staticPages: content.staticPages?.map(({ slug }) => slug)
    }
  });
  return signature(before) !== signature(after);
}

export function createWebsiteAutoSeo({ platformStore, readContentRecord, seoService, translationService, logger = console }) {
  let running = false;
  let attemptedRevision = 0;

  async function status() {
    return (await platformStore.readRuntimeSetting(SETTING_KEY))?.config || null;
  }

  async function writeStatus(config) {
    await platformStore.saveRuntimeSetting(SETTING_KEY, { config });
  }

  async function supersedeIfCurrent(job) {
    const current = await status();
    if (current?.status === 'pending' && current.sourceRevision === job.sourceRevision) {
      await writeStatus({ ...current, status: 'superseded', finishedAt: new Date().toISOString() });
    }
  }

  async function finishIfCurrent(job, result) {
    const current = await status();
    if (current?.status !== 'pending' || current.sourceRevision !== job.sourceRevision) return false;
    await writeStatus({ sourceRevision: job.sourceRevision, ...result });
    return true;
  }

  function kick() {
    queueMicrotask(() => { void drain().catch((error) => logger.error(`[AI SEO] 自动任务失败：${error.message}`)); });
  }

  async function enqueue(sourceRevision) {
    await writeStatus({ sourceRevision, status: 'pending', error: '', queuedAt: new Date().toISOString() });
    kick();
  }

  async function drain() {
    if (running) return;
    running = true;
    try {
      while (true) {
        const job = await status();
        if (job?.status !== 'pending' || job.sourceRevision === attemptedRevision) break;
        attemptedRevision = job.sourceRevision;
        const source = await readContentRecord();
        if (source.revision !== job.sourceRevision) {
          await supersedeIfCurrent(job);
          continue;
        }
        try {
          const generated = await seoService.generate(source.content);
          const latest = await readContentRecord();
          if (latest.revision !== job.sourceRevision) {
            await supersedeIfCurrent(job);
            continue;
          }
          // Direct CAS write: this is the SEO result of an earlier content save,
          // not another user edit. Going through saveContent would enqueue itself.
          const saved = await platformStore.saveContent(generated.content, { expectedRevision: job.sourceRevision });
          await finishIfCurrent(job, { resultRevision: saved.revision, status: 'ready', generatedCount: generated.generatedCount, generatedAt: generated.generatedAt, error: '' });
          try {
            await translationService.schedule(generated.content, saved.revision);
          } catch (error) {
            logger.error(`[AI SEO] 译文排队失败，重启时将恢复：${error.message}`);
          }
        } catch (error) {
          if (error?.code === 'CONTENT_REVISION_CONFLICT') {
            await supersedeIfCurrent(job);
            continue;
          }
          const latest = await readContentRecord();
          if (latest.revision !== job.sourceRevision) {
            await supersedeIfCurrent(job);
            continue;
          }
          logger.error(`[AI SEO] 第 ${job.sourceRevision} 版中文 SEO 生成失败：${error.message}`);
          await finishIfCurrent(job, { status: 'failed', error: error.message, finishedAt: new Date().toISOString() });
          // Content remains editable and localized pages can still be translated.
          try {
            await translationService.schedule(source.content, source.revision);
          } catch (scheduleError) {
            logger.error(`[AI SEO] 译文排队失败，重启时将恢复：${scheduleError.message}`);
          }
        }
      }
    } finally {
      running = false;
      const pending = await status();
      if (pending?.status === 'pending' && pending.sourceRevision !== attemptedRevision) kick();
    }
  }

  async function resume() {
    const pending = await status();
    if (pending?.status === 'pending') kick();
  }

  return { enqueue, resume, status };
}
