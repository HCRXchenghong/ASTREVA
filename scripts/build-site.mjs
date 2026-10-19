import { spawn } from 'node:child_process';
import { resolveFrontend } from './frontend-target.mjs';

/**
 * Builds whichever frontend ACTIVE_FRONTEND / FRONTEND_DIR points at
 * (default: `frontend`), running the CMS content contract first.
 *
 * This is the single entry point used by:
 *   - `npm run build:frontend`      -> the active target
 *   - `npm run build:frontend-next` -> the new frontend, explicitly
 *   - scripts/build-frontend.mjs    -> the publish pipeline (adds CONTENT_API_URL)
 */
function run(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: 'inherit', env: process.env, ...options });
    child.once('error', reject);
    child.once('exit', (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`${command} ${args.join(' ')} 失败（退出码 ${code ?? 'null'}，信号 ${signal || '无'}）`));
    });
  });
}

export async function buildSite() {
  const frontend = resolveFrontend();
  console.log(`[build] 目标前端：${frontend.name}（${frontend.dir}）`);
  await run('node', ['scripts/check-cms-content-contract.mjs'], {
    env: { ...process.env, FRONTEND_DIR: frontend.name }
  });
  await run('npm', ['run', 'build', '--workspace', frontend.name]);
}

if (process.argv[1] && process.argv[1].endsWith('build-site.mjs')) {
  buildSite().catch((error) => {
    console.error(`[build] 官网构建失败：${error.message}`);
    process.exitCode = 1;
  });
}
