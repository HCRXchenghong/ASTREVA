import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

/**
 * 官网目录的单一事实来源。
 *
 * 新旧前端并行期间，构建、静态服务、契约校验、静态资源托管都要知道"当前生效的是哪一个目录"，
 * 否则切换和回滚就得满仓库改路径。默认仍然是 `frontend`（现有生产前端），
 * 通过 `ACTIVE_FRONTEND` / `FRONTEND_DIR` 切到 `frontend-next` 即可，回滚就是把变量去掉。
 */
export const DEFAULT_FRONTEND = 'frontend';

export function resolveFrontendName(explicit) {
  const name = String(explicit ?? process.env.FRONTEND_DIR ?? process.env.ACTIVE_FRONTEND ?? DEFAULT_FRONTEND).trim()
    || DEFAULT_FRONTEND;
  if (!/^[a-z0-9][a-z0-9._-]*$/i.test(name)) {
    throw new Error(`非法的前端目录名：${name}`);
  }
  return name;
}

export function resolveFrontend(explicit) {
  const name = resolveFrontendName(explicit);
  const dir = path.join(ROOT, name);
  return {
    name,
    dir,
    src: path.join(dir, 'src'),
    dist: path.join(dir, 'dist'),
    public: path.join(dir, 'public')
  };
}
