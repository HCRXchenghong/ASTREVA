#!/usr/bin/env bash
# ASTREVA 一键验证（macOS / Linux 通用）
#
# 覆盖：Node 入口语法、CMS 内容契约、后台单测、后台构建、官网构建（真实 admin-server 内容 API）、
#       Go 后端测试与 vet、Shell 语法、Compose 配置、Caddyfile 校验、Flutter 静态分析。
#
# 约定：
#   * 任一步骤失败立即以非零退出，并打印清晰原因；最后汇总 PASS/FAIL/SKIP 清单。
#   * 环境不具备的步骤（无 docker 守护进程、无 flutter、沙箱/网络受限）记为 SKIP，不算失败。
#   * 不使用 `timeout`（macOS 默认没有），需要限时的地方用内置的 run_with_timeout。
#
# 用法：
#   bash scripts/verify-all.sh
#
# 环境变量：
#   GOCACHE                  Go 构建缓存目录，默认 /tmp/astreva-go-cache
#   VERIFY_SKIP_STEP5=1      强制跳过第 5 步官网构建（即使 dist 缺失）
#   PUB_CACHE                默认指向仓库内 .pub-cache，避免污染系统缓存
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_EXAMPLE="deployments/ubuntu/.env.ubuntu.example"
COMPOSE_FILE="deployments/ubuntu/docker-compose.ubuntu.yml"
export GOCACHE="${GOCACHE:-/tmp/astreva-go-cache}"

SUMMARY_FILE="$(mktemp "${TMPDIR:-/tmp}/astreva-verify-summary.XXXXXX")"
ADMIN_SMOKE_PID=""
ADMIN_SMOKE_DIR=""

cleanup() {
  if [[ -n "$ADMIN_SMOKE_PID" ]] && kill -0 "$ADMIN_SMOKE_PID" 2>/dev/null; then
    kill "$ADMIN_SMOKE_PID" 2>/dev/null || true
    wait "$ADMIN_SMOKE_PID" 2>/dev/null || true
  fi
  if [[ -n "$ADMIN_SMOKE_DIR" ]]; then
    rm -rf "$ADMIN_SMOKE_DIR"
  fi
  rm -f "$SUMMARY_FILE"
}
trap cleanup EXIT

# ---------------------------------------------------------------- 结果记录
CURRENT_STEP=""

record() { printf '%s\t%s\n' "$1" "$2" >>"$SUMMARY_FILE"; }

begin() {
  CURRENT_STEP="$1"
  printf '\n=== %s ===\n' "$1"
}

mark_pass() {
  record PASS "$CURRENT_STEP"
  printf '[PASS] %s\n' "$CURRENT_STEP"
}

mark_skip() {
  local reason="${1:-环境限制}"
  record SKIP "${CURRENT_STEP}（${reason}）"
  printf '[SKIP] %s（%s）\n' "$CURRENT_STEP" "$reason"
}

mark_fail() {
  local reason="${1:-未知原因}"
  record FAIL "${CURRENT_STEP}（${reason}）"
  printf '[FAIL] %s（%s）\n' "$CURRENT_STEP" "$reason" >&2
}

print_summary() {
  local pass fail skip total
  pass=0
  fail=0
  skip=0
  if [[ -s "$SUMMARY_FILE" ]]; then
    read -r pass fail skip < <(awk -F'\t' '{c[$1]++} END {printf "%d %d %d\n", c["PASS"]+0, c["FAIL"]+0, c["SKIP"]+0}' "$SUMMARY_FILE")
  fi
  total=$((pass + fail + skip))
  printf '\n================ 验证汇总 ================\n'
  printf '总计 %d 项：PASS %d / FAIL %d / SKIP %d\n' "$total" "$pass" "$fail" "$skip"
  if [[ -s "$SUMMARY_FILE" ]]; then
    awk -F'\t' '{printf "  [%-4s] %s\n", $1, $2}' "$SUMMARY_FILE"
  fi
  printf '=========================================\n'
}

fatal() {
  mark_fail "${1:-命令失败}"
  printf '\n验证中断：%s\n' "${1:-命令失败}" >&2
  print_summary
  exit 1
}

finish() {
  print_summary
  local fail_count
  fail_count="$(awk -F'\t' '$1 == "FAIL" {n++} END {print n+0}' "$SUMMARY_FILE")"
  if [[ "$fail_count" -gt 0 ]]; then
    exit 1
  fi
  exit 0
}

# ---------------------------------------------------------------- 工具函数
# 可移植的超时执行（不依赖 coreutils 的 timeout，macOS 也能用）。
run_with_timeout() {
  local seconds="$1"
  shift
  "$@" &
  local cmd_pid=$!
  (
    sleep "$seconds"
    if kill -0 "$cmd_pid" 2>/dev/null; then
      kill -TERM "$cmd_pid" 2>/dev/null || true
    fi
  ) &
  local watcher_pid=$!
  local rc=0
  wait "$cmd_pid" || rc=$?
  kill "$watcher_pid" 2>/dev/null || true
  wait "$watcher_pid" 2>/dev/null || true
  return "$rc"
}

consider_monitoring_config() {
  docker compose --env-file "$ENV_EXAMPLE" --profile monitoring -f "$COMPOSE_FILE" config >/dev/null
}

printf 'ASTREVA 一键验证（仓库：%s）\n' "$ROOT_DIR"
printf 'Go 缓存：%s\n' "$GOCACHE"

# ---------------------------------------------------------------- 1. Node 入口语法
begin "1/11 node --check（admin-server 入口）"
shopt -s nullglob
node_entries=(admin-server/*.mjs)
shopt -u nullglob
if [[ ${#node_entries[@]} -eq 0 ]]; then
  fatal "未找到 admin-server/*.mjs 入口文件"
fi
for entry in "${node_entries[@]}"; do
  if ! node --check "$entry"; then
    fatal "node --check $entry 失败"
  fi
done
mark_pass

# ---------------------------------------------------------------- 2. CMS 内容契约
begin "2/11 npm run cms:contract"
if ! npm run cms:contract; then
  fatal "npm run cms:contract 失败（后台内容与官网契约不一致）"
fi
mark_pass

# ---------------------------------------------------------------- 3. 后台单测
begin "3/11 npm run test:admin"
if ! npm run test:admin; then
  fatal "npm run test:admin 失败（后台单元测试未通过）"
fi
mark_pass

# ---------------------------------------------------------------- 4. admin-web 构建
begin "4/11 npm --workspace admin-web run build"
if [[ -f admin-web/package.json ]]; then
  if ! npm --workspace admin-web run build; then
    fatal "admin-web 构建失败"
  fi
  mark_pass
else
  mark_skip "admin-web/package.json 不存在"
fi

# ---------------------------------------------------------------- 5. 官网构建
begin "5/11 官网构建（真实 admin-server 内容 API）"
dist_present=0
if [[ -d frontend/dist ]] && [[ -n "$(ls -A frontend/dist 2>/dev/null)" ]]; then
  dist_present=1
fi
if [[ "${VERIFY_SKIP_STEP5:-0}" == "1" ]]; then
  mark_skip "VERIFY_SKIP_STEP5=1"
elif [[ "$dist_present" -eq 1 ]]; then
  mark_skip "frontend/dist 已存在且非空"
else
  ADMIN_SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/astreva-real-content.XXXXXX")"
  ADMIN_SMOKE_PORT="$(
    node -e 'const net=require("net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close();});'
  )" || fatal "无法分配真实 admin-server 测试端口"
  ADMIN_LOG="$ADMIN_SMOKE_DIR/admin.log"
  ADMIN_DATA_DIR="$ADMIN_SMOKE_DIR/data" \
  ADMIN_UPLOAD_DIR="$ADMIN_SMOKE_DIR/uploads" \
  PORT="$ADMIN_SMOKE_PORT" HOST=127.0.0.1 \
  ALLOW_LEGACY_STORAGE_FOR_TESTS=true CMS_STORE_DRIVER=json ADMIN_MEDIA_DRIVER=local \
  ADMIN_2FA_SESSION_SECRET=verify-real-content-secret-0123456789 \
  ADMIN_SETUP_TOKEN=verify-real-content-setup-token \
  CMS_DATA_ENCRYPTION_KEY=verify-real-content-encryption-key-0123456789 \
  SITE_CONTENT_SEED_FILE="$ROOT_DIR/admin-server/seed/site-content.json" \
  PUBLIC_ADMIN_URL="http://127.0.0.1:$ADMIN_SMOKE_PORT" \
  node admin-server/server.mjs >"$ADMIN_LOG" 2>&1 &
  ADMIN_SMOKE_PID=$!
  admin_ready=0
  for _ in $(seq 1 80); do
    if node -e 'fetch(process.argv[1]).then((r)=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))'       "http://127.0.0.1:$ADMIN_SMOKE_PORT/healthz" >/dev/null 2>&1; then
      admin_ready=1
      break
    fi
    if ! kill -0 "$ADMIN_SMOKE_PID" 2>/dev/null; then
      cat "$ADMIN_LOG" >&2
      fatal "真实 admin-server 启动失败"
    fi
    sleep 0.1
  done
  if [[ "$admin_ready" -ne 1 ]]; then
    cat "$ADMIN_LOG" >&2
    fatal "真实 admin-server 在 8 秒内未就绪"
  fi
  printf '真实内容 API：http://127.0.0.1:%s（admin-server + seed）\n' "$ADMIN_SMOKE_PORT"
  BUILD_LOG="$(mktemp "${TMPDIR:-/tmp}/astreva-frontend-build.XXXXXX")"
  build_ok=0
  if PUBLIC_ADMIN_URL="http://127.0.0.1:$ADMIN_SMOKE_PORT" \
    CONTENT_API_URL="http://127.0.0.1:$ADMIN_SMOKE_PORT" \
    PUBLIC_SERVICEBRIDGE_URL="http://127.0.0.1:5173" \
    PUBLIC_SITE_URL="http://127.0.0.1:4321" \
    npm run build:frontend 2>&1 | tee "$BUILD_LOG"; then
    build_ok=1
  fi
  kill "$ADMIN_SMOKE_PID" 2>/dev/null || true
  wait "$ADMIN_SMOKE_PID" 2>/dev/null || true
  ADMIN_SMOKE_PID=""
  rm -rf "$ADMIN_SMOKE_DIR"
  ADMIN_SMOKE_DIR=""
  if [[ "$build_ok" -ne 1 ]]; then
    if grep -q "Cannot find native binding" "$BUILD_LOG" 2>/dev/null; then
      printf '\n提示：node_modules 缺少可选原生依赖（npm optional dependencies bug）。\n' >&2
      printf '请在有网络的环境执行 `npm ci` 恢复依赖后重跑：npm ci && bash scripts/verify-all.sh\n' >&2
    fi
    rm -f "$BUILD_LOG"
    fatal "使用真实 admin-server 内容 API 构建官网失败"
  fi
  rm -f "$BUILD_LOG"
  if [[ ! -d frontend/dist ]] || [[ -z "$(ls -A frontend/dist 2>/dev/null)" ]]; then
    fatal "构建命令成功但 frontend/dist 仍为空"
  fi
  mark_pass
fi

# ---------------------------------------------------------------- 6. Go 后端
begin "6/11 Go 后端测试与 vet"
if ! (cd servicebridge/backend && go test ./...); then
  fatal "servicebridge/backend: go test ./... 失败"
fi
if ! (cd servicebridge/backend && go vet ./...); then
  fatal "servicebridge/backend: go vet ./... 失败"
fi
mark_pass

# ---------------------------------------------------------------- 7. Shell 语法
begin "7/11 Shell 语法检查（bash -n）"
shell_failures=""
while IFS= read -r script; do
  [[ -n "$script" ]] || continue
  if ! bash -n "$script"; then
    shell_failures="$shell_failures $script"
  fi
done < <(
  {
    find scripts servicebridge/scripts -type f -name '*.sh' 2>/dev/null
    find deployments/ubuntu -maxdepth 1 -type f -name '*.sh' 2>/dev/null
  } | LC_ALL=C sort
)
if [[ -n "$shell_failures" ]]; then
  fatal "bash -n 失败：$shell_failures"
fi
mark_pass

# ---------------------------------------------------------------- 8. Compose 配置
begin "8/11 docker compose config"
if ! docker compose --env-file "$ENV_EXAMPLE" -f "$COMPOSE_FILE" config >/dev/null; then
  fatal "docker compose config（默认 profile）失败"
fi
if ! consider_monitoring_config; then
  fatal "docker compose --profile monitoring config 失败"
fi
mark_pass

# ---------------------------------------------------------------- 9. Caddyfile
begin "9/11 Caddyfile 校验（caddy:2.8-alpine）"
caddy_skip=""
if ! command -v docker >/dev/null 2>&1; then
  caddy_skip="本机没有 docker"
elif ! docker info >/dev/null 2>&1; then
  caddy_skip="Docker 守护进程不可用"
elif ! docker image inspect caddy:2.8-alpine >/dev/null 2>&1 \
  && ! docker pull caddy:2.8-alpine >/dev/null 2>&1; then
  caddy_skip="无法获取 caddy:2.8-alpine 镜像"
fi
if [[ -n "$caddy_skip" ]]; then
  mark_skip "$caddy_skip"
else
  if ! docker run --rm \
    -e SITE_DOMAIN=x.example \
    -e ADMIN_DOMAIN=y.example \
    -e SERVICE_DOMAIN=z.example \
    -e ACME_EMAIL=a@example.com \
    -v "$ROOT_DIR/deployments/ubuntu/Caddyfile:/etc/caddy/Caddyfile:ro" \
    caddy:2.8-alpine caddy validate --config /etc/caddy/Caddyfile; then
    fatal "caddy validate 失败（Caddyfile 配置无效）"
  fi
  mark_pass
fi

# ---------------------------------------------------------------- 10. Flutter
begin "10/11 Flutter 静态分析（agent-mobile）"
MOBILE_DIR="servicebridge/apps/agent-mobile"
if [[ ! -f "$MOBILE_DIR/pubspec.yaml" ]]; then
  mark_skip "servicebridge/apps/agent-mobile/pubspec.yaml 不存在"
elif ! command -v flutter >/dev/null 2>&1 || ! command -v dart >/dev/null 2>&1; then
  mark_skip "本机没有 flutter/dart"
else
  export PUB_CACHE="${PUB_CACHE:-$ROOT_DIR/.pub-cache}"
  mkdir -p "$PUB_CACHE"
  if (cd "$MOBILE_DIR" && run_with_timeout 600 flutter pub get)     && (cd "$MOBILE_DIR" && run_with_timeout 600 dart analyze); then
    mark_pass
  else
    # 沙箱/无网络时 flutter 包装脚本会因写 SDK 缓存而失败；若依赖已就绪，
    # 直接使用 Flutter 内置的 Dart SDK 做静态分析，避免把可验证项降级为 SKIP。
    BUNDLED_DART="$(command -v flutter >/dev/null 2>&1 && dirname "$(command -v flutter)")/cache/dart-sdk/bin/dart"
    if [[ ! -x "$BUNDLED_DART" ]]; then
      BUNDLED_DART="/Users/seron-cheng/development/flutter/bin/cache/dart-sdk/bin/dart"
    fi
    if [[ -x "$BUNDLED_DART" && -f "$MOBILE_DIR/.dart_tool/package_config.json" ]]; then
      if (cd "$MOBILE_DIR" && run_with_timeout 600 "$BUNDLED_DART" analyze lib); then
        printf '[INFO] flutter 包装脚本不可用，已改用内置 Dart SDK 完成静态分析。\n'
        mark_pass
      else
        fatal "dart analyze 失败（agent-mobile 存在静态分析问题）"
      fi
    else
      mark_skip "环境限制：flutter pub get 未能完成，且没有可用的内置 Dart SDK 或依赖缓存"
    fi
  fi
fi

# ---------------------------------------------------------------- 11. PostgreSQL 集成测试
begin "11/11 真实 PostgreSQL 集成测试（Go 存储升级与分页）"
pg_skip=""
if ! command -v docker >/dev/null 2>&1; then
  pg_skip="本机没有 docker"
elif ! docker info >/dev/null 2>&1; then
  pg_skip="Docker 守护进程不可用"
elif ! docker image inspect postgres:16-alpine >/dev/null 2>&1 \
  && ! docker pull postgres:16-alpine >/dev/null 2>&1; then
  pg_skip="无法获取 postgres:16-alpine 镜像"
fi
if [[ -n "$pg_skip" ]]; then
  mark_skip "$pg_skip"
else
  PG_PORT="${VERIFY_PG_PORT:-55432}"
  export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/astreva-go-cache}"
  mkdir -p "$GOCACHE"
  docker rm -f astreva-verify-pg >/dev/null 2>&1 || true
  if ! docker run --rm -d --name astreva-verify-pg \
    -e POSTGRES_PASSWORD=verify -e POSTGRES_DB=astreva_verify \
    -p "127.0.0.1:${PG_PORT}:5432" postgres:16-alpine >/dev/null 2>&1; then
    mark_skip "无法启动 postgres:16-alpine 容器（端口 ${PG_PORT} 可能被占用，可用 VERIFY_PG_PORT 指定）"
  else
    pg_ready=0
    for _ in $(seq 1 30); do
      if docker exec astreva-verify-pg pg_isready -U postgres >/dev/null 2>&1; then
        pg_ready=1
        break
      fi
      sleep 1
    done
    if [[ "$pg_ready" != "1" ]]; then
      docker rm -f astreva-verify-pg >/dev/null 2>&1 || true
      mark_skip "PostgreSQL 容器未在 30 秒内就绪"
    else
      pg_ok=0
      if (cd servicebridge/backend && TEST_DATABASE_URL="postgres://postgres:verify@127.0.0.1:${PG_PORT}/astreva_verify?sslmode=disable" \
        run_with_timeout 300 go test ./internal/store/ -run TestPostgres -count=1); then
        pg_ok=1
      fi
      # 后台控制台的 Postgres 分支也要在真实库上过一遍：数据看板的「访问趋势」是手写 SQL，
      # 只用 JSON 分支跑测试的话，参数编号写错（$1 未被引用）这种问题会漏到线上变成 500。
      if [[ "$pg_ok" == "1" ]]; then
        if ! TEST_DATABASE_URL="postgres://postgres:verify@127.0.0.1:${PG_PORT}/astreva_verify?sslmode=disable" \
          run_with_timeout 300 node --test admin-server/modules/analytics-postgres.integration.test.mjs; then
          pg_ok=0
        fi
      fi
      docker rm -f astreva-verify-pg >/dev/null 2>&1 || true
      if [[ "$pg_ok" == "1" ]]; then
        mark_pass
      else
        fatal "PostgreSQL 集成测试失败（真实库建表/升级/读写、分页查询或控制台看板 SQL 有问题）"
      fi
    fi
  fi
fi

finish
