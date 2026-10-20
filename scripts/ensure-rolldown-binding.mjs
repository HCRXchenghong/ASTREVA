import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

/**
 * npm has a long-standing bug where a plain `npm install` can delete the
 * platform-specific optional dependency of rolldown (the bundler behind Vite 8 /
 * Astro 7): https://github.com/npm/cli/issues/4828
 *
 * Without it every build dies with "Cannot find native binding". This postinstall
 * checks whether the binding for the current platform is resolvable and, if not,
 * installs the exact version rolldown asks for. It is best-effort: a failure only
 * prints instructions, because a blocked network must not break `npm install`.
 */

const SKIP_ENV = 'ASTREVA_SKIP_BINDING_FIX';

function platformPackage() {
  const platform = process.platform;
  const arch = process.arch;
  if (platform === 'darwin') return `@rolldown/binding-darwin-${arch}`;
  if (platform === 'win32') return `@rolldown/binding-win32-${arch}-msvc`;
  if (platform === 'linux') {
    // glibc vs musl: assume glibc unless the musl loader is present.
    const musl = fs.existsSync('/lib/ld-musl-x86_64.so.1') || fs.existsSync('/lib/ld-musl-aarch64.so.1');
    return `@rolldown/binding-linux-${arch}-${musl ? 'musl' : 'gnu'}`;
  }
  return null;
}

function rolldownVersion() {
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join('node_modules', 'rolldown', 'package.json'), 'utf8'));
    return pkg.version;
  } catch {
    return null;
  }
}

function resolvable(name) {
  try {
    const require = createRequire(import.meta.url);
    require.resolve(`${name}/package.json`);
    return true;
  } catch {
    return false;
  }
}

function main() {
  if (process.env[SKIP_ENV] === '1') return;
  const name = platformPackage();
  const version = rolldownVersion();
  if (!name || !version) return;
  if (resolvable(name)) return;

  console.warn(`[postinstall] ${name} 缺失，尝试安装 ${version}…`);
  const result = spawnSync(
    'npm',
    // 用仓库内的缓存：CI/开发机的 ~/.npm 可能是 root 拥有的，直接写会 EPERM。
    ['install', '--no-save', '--no-audit', '--no-fund', '--cache', '.npm-cache', `${name}@${version}`],
    { stdio: 'inherit', env: { ...process.env, [SKIP_ENV]: '1' } }
  );

  if (result.status !== 0 || !resolvable(name)) {
    console.warn(
      `[postinstall] 自动安装失败。若构建报 "Cannot find native binding"，请手动执行：\n` +
        `  npm install --no-save ${name}@${version}`
    );
    return;
  }
  console.log(`[postinstall] 已恢复 ${name}@${version}`);
}

main();
