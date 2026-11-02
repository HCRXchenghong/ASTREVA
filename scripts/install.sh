#!/usr/bin/env bash
# ASTREVA 商业化一键部署脚本
#
# 支持两种运行方式：
#   1) 从 GitHub 直接复制粘贴（推荐先用 ASTREVA_REPO_URL 指定仓库）：
#      curl -fsSL https://raw.githubusercontent.com/HCRXchenghong/ASTREVA/main/scripts/install.sh \
#        | sudo bash -s -- --repo https://github.com/HCRXchenghong/ASTREVA.git
#   2) 在已 clone 的仓库里执行：
#      sudo bash scripts/install.sh
#
# 功能：
#   - 检查并安装/升级 Docker、Compose、Git、curl、openssl 等运行环境
#   - 对 GitHub、Docker 安装脚本、Docker 镜像源做延迟探测，优先使用可用且较快的渠道
#   - 交互式询问部署域名和 ACME 邮箱（也可用 ASTREVA_DOMAIN / ASTREVA_EMAIL 非交互传入）
#   - 生成 .env.ubuntu（保留已有密钥），Caddy 自动签发并续期证书
#   - 已有旧版本时先备份 PostgreSQL，再原地更新，不执行 down -v，不动数据库卷
#   - 部署完成后检查官网、后台、客服 API、容器健康和证书，全部正常才退出 0
#
# 常用环境变量：
#   ASTREVA_REPO_URL        GitHub 仓库地址（从 curl | bash 运行时必填）
#   ASTREVA_REF             分支/标签/commit，默认 main
#   ASTREVA_COMMIT          可选；指定后强制校验 HEAD 与该 commit 一致
#   ASTREVA_INSTALL_DIR     安装目录，默认 /opt/astreva
#   ASTREVA_DOMAIN          主域名，例如 www.example.com
#   ASTREVA_ADMIN_DOMAIN    后台域名，默认 admin.<主域>
#   ASTREVA_SERVICE_DOMAIN  客服域名，默认 service.<主域>
#   ASTREVA_EMAIL           ACME 邮箱
#   ASTREVA_NONINTERACTIVE  1=不询问，缺少参数直接失败
#   ASTREVA_USE_MIRRORS     1=启用镜像测速（默认），0=只用直连
#   ASTREVA_STACK_NAME      Compose 项目名，默认沿用已有或 astreva-ubuntu
#   ASTREVA_DOCKER_VERSION  可选；强制安装指定 Docker 版本（可用于降级）
#   ASTREVA_FORCE_UPDATE    1=源码有本地改动也强制更新（会先备份 patch）
#
# 安全说明：第三方镜像可能带来供应链风险。脚本会尽量用 GitHub 官方 ls-remote 校验
# 代码 commit；对镜像源只做网络可用性/延迟探测，生产环境可设置 ASTREVA_USE_MIRRORS=0。

set -Eeuo pipefail
export GIT_TERMINAL_PROMPT=0
IFS=$'\n\t'

MIN_DOCKER_VERSION="24.0.0"
MIN_COMPOSE_VERSION="2.20.0"

REPO_URL="${ASTREVA_REPO_URL:-}"
REF="${ASTREVA_REF:-main}"
REF_EXPLICIT=0
[[ -n "${ASTREVA_REF:-}" ]] && REF_EXPLICIT=1
INSTALL_DIR="${ASTREVA_INSTALL_DIR:-/opt/astreva}"
INSTALL_DIR_EXPLICIT=0
[[ -n "${ASTREVA_INSTALL_DIR:-}" ]] && INSTALL_DIR_EXPLICIT=1
DOMAIN="${ASTREVA_DOMAIN:-}"
DOMAIN_EXPLICIT=0
[[ -n "${ASTREVA_DOMAIN:-}" ]] && DOMAIN_EXPLICIT=1
ADMIN_DOMAIN="${ASTREVA_ADMIN_DOMAIN:-}"
SERVICE_DOMAIN="${ASTREVA_SERVICE_DOMAIN:-}"
EMAIL="${ASTREVA_EMAIL:-}"
NONINTERACTIVE="${ASTREVA_NONINTERACTIVE:-0}"
USE_MIRRORS="${ASTREVA_USE_MIRRORS:-1}"
STACK_NAME="${ASTREVA_STACK_NAME:-}"
DOCKER_VERSION="${ASTREVA_DOCKER_VERSION:-}"
FORCE_UPDATE="${ASTREVA_FORCE_UPDATE:-0}"
SETUP_TOKEN=""

SCRIPT_DIR=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi

log()  { printf '\033[1;34m[astreva]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[ ok ]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,40p' "$0" 2>/dev/null || cat <<'USAGE'
ASTREVA 一键部署
用法: sudo bash scripts/install.sh [选项]
  --domain DOMAIN        主域名
  --admin-domain DOMAIN  后台域名
  --service-domain DOMAIN 客服域名
  --email EMAIL          ACME 邮箱
  --repo URL             GitHub 仓库
  --dir PATH             安装目录
  --ref REF              分支/标签/commit
  --non-interactive      不询问
  --no-mirrors           只用直连
  --force                源码有改动也强制更新
  -h, --help             帮助
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain) DOMAIN="${2:-}"; DOMAIN_EXPLICIT=1; shift 2 ;;
    --domain=*) DOMAIN="${1#*=}"; DOMAIN_EXPLICIT=1; shift ;;
    --admin-domain) ADMIN_DOMAIN="${2:-}"; shift 2 ;;
    --admin-domain=*) ADMIN_DOMAIN="${1#*=}"; shift ;;
    --service-domain) SERVICE_DOMAIN="${2:-}"; shift 2 ;;
    --service-domain=*) SERVICE_DOMAIN="${1#*=}"; shift ;;
    --email) EMAIL="${2:-}"; shift 2 ;;
    --email=*) EMAIL="${1#*=}"; shift ;;
    --repo) REPO_URL="${2:-}"; shift 2 ;;
    --repo=*) REPO_URL="${1#*=}"; shift ;;
    --dir) INSTALL_DIR="${2:-}"; INSTALL_DIR_EXPLICIT=1; shift 2 ;;
    --dir=*) INSTALL_DIR="${1#*=}"; INSTALL_DIR_EXPLICIT=1; shift ;;
    --ref) REF="${2:-}"; REF_EXPLICIT=1; shift 2 ;;
    --ref=*) REF="${1#*=}"; REF_EXPLICIT=1; shift ;;
    --non-interactive) NONINTERACTIVE=1; shift ;;
    --no-mirrors) USE_MIRRORS=0; shift ;;
    --force) FORCE_UPDATE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数: $1（用 --help 查看用法）" ;;
  esac
done

# 帮助不需要 root
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then usage; exit 0; fi

if [[ $EUID -ne 0 ]]; then
  if [[ -f "$0" && -t 0 ]] && command -v sudo >/dev/null 2>&1; then
    exec sudo -E bash "$0" "$@"
  fi
  die "需要 root 权限。请使用：curl -fsSL <安装脚本URL> | sudo bash"
fi

# 日志：不记录密码；只记录流程和 URL（敏感 URL 会掩码）
LOG_FILE="${ASTREVA_INSTALL_LOG:-/var/log/astreva-install.log}"
mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || LOG_FILE="/tmp/astreva-install.log"
: >"$LOG_FILE" 2>/dev/null || true
exec > >(tee -a "$LOG_FILE") 2>&1

CURRENT_STEP="初始化"
on_error() {
  local code=$?
  printf '\n\033[1;31m[失败]\033[0m 步骤：%s（退出码 %s）\n' "$CURRENT_STEP" "$code" >&2
  printf '排查：查看 %s，以及 docker compose -f deployments/ubuntu/docker-compose.ubuntu.yml logs --tail=120\n' "$LOG_FILE" >&2
  exit "$code"
}
trap on_error ERR

mask_url() {
  local url="$1"
  if [[ "$url" == *"@"* ]]; then
    printf '%s\n' "$url" | sed -E 's#(https?://)[^/@]+@#\1***@#'
  else
    printf '%s\n' "$url"
  fi
}

prompt_tty() {
  local var_name="$1" message="$2" default="${3:-}" answer=""
  if [[ "$NONINTERACTIVE" == "1" ]]; then
    die "非交互模式缺少 $var_name，请通过环境变量或参数传入"
  fi
  if [[ -r /dev/tty ]]; then
    read -r -p "$message${default:+ [$default]}: " answer </dev/tty || true
  elif [[ -t 0 ]]; then
    read -r -p "$message${default:+ [$default]}: " answer || true
  else
    die "当前没有终端可交互，请设置 $var_name 或使用 --non-interactive"
  fi
  answer="${answer:-$default}"
  [[ -n "$answer" ]] || die "$var_name 不能为空"
  printf -v "$var_name" '%s' "$answer"
}

version_ge() {
  [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" == "$2" ]]
}

normalize_version() { printf '%s' "${1#v}"; }

detect_os() {
  [[ -f /etc/os-release ]] || die "无法识别系统：缺少 /etc/os-release"
  # shellcheck disable=SC1091
  . /etc/os-release
  local id_like="${ID_LIKE:-}"
  case "${ID:-} ${id_like}" in
    *ubuntu*|*debian*) PKG_MANAGER="apt" ;;
    *rhel*|*centos*|*fedora*|*rocky*|*almalinux*) PKG_MANAGER="dnf" ;;
    *) die "暂不支持的系统：${PRETTY_NAME:-${ID:-unknown}}（支持 Ubuntu/Debian/RHEL 系）" ;;
  esac
  command -v systemctl >/dev/null 2>&1 || die "需要 systemd（当前环境可能是 LXC/容器，请在真实 VPS 上运行）"
}

install_base_packages() {
  CURRENT_STEP="安装基础依赖"
  log "使用 $PKG_MANAGER 安装基础依赖..."
  local packages=(curl ca-certificates git openssl tar gzip sed grep awk coreutils python3)
  if [[ "$PKG_MANAGER" == "apt" ]]; then
    packages+=(gnupg iproute2)
    export DEBIAN_FRONTEND=noninteractive
    local attempt
    for attempt in 1 2 3; do
      if apt-get update -y && apt-get install -y --no-install-recommends "${packages[@]}"; then return 0; fi
      warn "apt 安装失败，第 ${attempt}/3 次重试..."
      sleep 2
    done
    die "基础依赖安装失败"
  else
    packages+=(gnupg2 iproute)
    dnf makecache -y || true
    dnf install -y "${packages[@]}" || die "基础依赖安装失败"
  fi
}

probe_http() {
  local url="$1" result code seconds
  result="$(curl -LfsS --max-time 4 -o /dev/null -w '%{http_code} %{time_total}' "$url" 2>/dev/null || true)"
  code="${result%% *}"; seconds="${result##* }"
  if [[ "$code" =~ ^(200|204|301|302|307|308|401|403)$ && "$seconds" =~ ^[0-9.]+$ ]]; then
    printf '%s\n' "$seconds"
  fi
}

pick_fastest() {
  local best="" best_time="" url time
  for url in "$@"; do
    [[ -n "$url" ]] || continue
    time="$(probe_http "$url")"
    [[ -n "$time" ]] || continue
    if [[ -z "$best" ]] || awk -v a="$time" -v b="$best_time" 'BEGIN{exit !(a<b)}'; then
      best="$url"; best_time="$time"
    fi
  done
  [[ -n "$best" ]] && printf '%s\n' "$best"
}

docker_installed() { command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; }

install_docker() {
  CURRENT_STEP="安装/升级 Docker"
  local script_url="https://get.docker.com" selected
  if [[ "$USE_MIRRORS" == "1" ]]; then
    selected="$(pick_fastest "$script_url" "https://ghproxy.com/$script_url" "https://mirror.ghproxy.com/$script_url" || true)"
  fi
  selected="${selected:-$script_url}"
  log "下载 Docker 安装脚本：$(mask_url "$selected")"
  local tmp_script
  tmp_script="$(mktemp /tmp/astreva-get-docker.XXXXXX.sh)"
  curl -LfsS --max-time 60 "$selected" -o "$tmp_script" || die "Docker 安装脚本下载失败"
  [[ -s "$tmp_script" ]] || die "Docker 安装脚本为空"
  local install_args=()
  [[ -n "$DOCKER_VERSION" ]] && install_args=(--version "$DOCKER_VERSION")
  if ! sh "$tmp_script" ${install_args[@]+"${install_args[@]}"}; then
    warn "镜像渠道安装失败，回退官方直连..."
    curl -LfsS --max-time 90 "https://get.docker.com" -o "$tmp_script" || die "Docker 安装脚本下载失败"
    sh "$tmp_script" ${install_args[@]+"${install_args[@]}"} || die "Docker 安装失败"
  fi
  rm -f "$tmp_script"
  systemctl enable --now docker >/dev/null 2>&1 || true
  docker info >/dev/null 2>&1 || die "Docker 已安装但守护进程不可用"
}

check_environment() {
  CURRENT_STEP="检查运行环境"
  log "检查 Docker / Compose..."
  local docker_version="" compose_version=""

  if ! command -v docker >/dev/null 2>&1; then
    install_docker
  else
    docker_version="$(normalize_version "$(docker version --format '{{.Server.Version}}' 2>/dev/null || true)")"
    if [[ -z "$docker_version" ]] || ! version_ge "$docker_version" "$MIN_DOCKER_VERSION"; then
      warn "Docker 版本 ${docker_version:-未知} 低于最低要求 $MIN_DOCKER_VERSION，开始升级..."
      install_docker
    fi
    if [[ -n "$DOCKER_VERSION" && "$docker_version" != "$DOCKER_VERSION" ]]; then
      warn "当前 Docker $docker_version，指定版本 $DOCKER_VERSION，开始安装指定版本..."
      install_docker
    fi
  fi

  if ! docker compose version >/dev/null 2>&1; then
    warn "缺少 Docker Compose v2，尝试安装 docker-compose-plugin..."
    if [[ "$PKG_MANAGER" == "apt" ]]; then
      apt-get install -y docker-compose-plugin || install_docker
    else
      dnf install -y docker-compose-plugin || install_docker
    fi
  fi
  compose_version="$(normalize_version "$(docker compose version --short 2>/dev/null || true)")"
  if [[ -z "$compose_version" ]] || ! version_ge "$compose_version" "$MIN_COMPOSE_VERSION"; then
    warn "Compose 版本 ${compose_version:-未知} 低于 $MIN_COMPOSE_VERSION，重装 Docker 以获取新版 Compose..."
    install_docker
  fi
  ok "Docker $(docker version --format '{{.Server.Version}}') / Compose ${compose_version:-$(docker compose version --short)}"
}

configure_docker_mirrors() {
  [[ "$USE_MIRRORS" == "1" ]] || return 0
  command -v python3 >/dev/null 2>&1 || return 0
  CURRENT_STEP="配置 Docker 镜像加速"
  local current
  current="$(docker info --format '{{json .RegistryConfig.Mirrors}}' 2>/dev/null || echo '[]')"
  if [[ "$current" != "[]" && "$current" != "null" ]]; then
    log "Docker 已配置 registry mirror，跳过"
    return 0
  fi
  local mirrors=("https://docker.m.daocloud.io" "https://dockerproxy.com" "https://docker.mirrors.ustc.edu.cn" "https://hub-mirror.c.163.com")
  local picked=() mirror time
  for mirror in "${mirrors[@]}"; do
    time="$(probe_http "$mirror/v2/")"
    [[ -n "$time" ]] && picked+=("$mirror")
  done
  [[ ${#picked[@]} -gt 0 ]] || { warn "没有可用的 Docker 镜像加速源，继续直连 Docker Hub"; return 0; }
  warn "将使用第三方 Docker 镜像加速（可能带来供应链风险）：${picked[*]}"
  mkdir -p /etc/docker
  [[ -f /etc/docker/daemon.json ]] && cp /etc/docker/daemon.json "/etc/docker/daemon.json.bak.$(date +%s)"
  python3 - "${picked[@]}" <<'PY'
import json, os, sys
path = "/etc/docker/daemon.json"
try:
    with open(path, "r", encoding="utf-8") as fh:
        data = json.load(fh)
except Exception:
    data = {}
if not isinstance(data.get("registry-mirrors"), list):
    data["registry-mirrors"] = []
for item in sys.argv[1:]:
    if item not in data["registry-mirrors"]:
        data["registry-mirrors"].append(item)
with open(path, "w", encoding="utf-8") as fh:
    json.dump(data, fh, ensure_ascii=False, indent=2)
PY
  systemctl restart docker || { warn "Docker 重启失败，尝试继续"; return 0; }
  sleep 1
  docker info >/dev/null 2>&1 || die "Docker 重启后不可用"
  ok "Docker registry mirror 已配置"
}

github_candidates() {
  local direct="$1"
  printf '%s\n' "$direct"
  [[ "$USE_MIRRORS" == "1" ]] || return 0
  [[ "$direct" == *"@"* ]] && return 0
  printf 'https://ghproxy.com/%s\n' "$direct"
  printf 'https://mirror.ghproxy.com/%s\n' "$direct"
  printf 'https://ghfast.top/%s\n' "$direct"
  printf 'https://github.moeyy.xyz/%s\n' "$direct"
}

pick_git_url() {
  local direct="$1" best="" best_time="" url start end elapsed
  while IFS= read -r url; do
    [[ -n "$url" ]] || continue
    start="$(date +%s%N)"
    if timeout 10 git ls-remote "$url" HEAD >/dev/null 2>&1; then
      end="$(date +%s%N)"
      elapsed="$(( (end - start) / 1000000 ))"
      if [[ -z "$best" || "$elapsed" -lt "$best_time" ]]; then
        best="$url"; best_time="$elapsed"
      fi
    fi
  done < <(github_candidates "$direct")
  [[ -n "$best" ]] && printf '%s\n' "$best"
}

resolve_repo() {
  CURRENT_STEP="拉取代码"
  local repo_root=""
  if [[ -n "$SCRIPT_DIR" ]]; then
    repo_root="$(cd "$SCRIPT_DIR/.." && pwd)"
  fi

  if [[ -z "$REPO_URL" && -n "$repo_root" ]] && git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    REPO_URL="$(git -C "$repo_root" remote get-url origin 2>/dev/null || true)"
  fi
  if [[ -n "$repo_root" && ( "$INSTALL_DIR_EXPLICIT" -eq 0 || "$INSTALL_DIR" == "$repo_root" ) && -f "$repo_root/deployments/ubuntu/docker-compose.ubuntu.yml" ]]; then
    INSTALL_DIR="$repo_root"
    if [[ "$REF_EXPLICIT" -eq 0 ]]; then
      local current_branch
      current_branch="$(git -C "$repo_root" branch --show-current 2>/dev/null || true)"
      [[ -n "$current_branch" ]] && REF="$current_branch"
    fi
    log "使用当前仓库：$INSTALL_DIR（ref=$REF）"
    # 没有 remote 的本地仓库无法自动更新；有 remote 时继续走 fetch/更新逻辑。
    if [[ -z "$REPO_URL" ]]; then
      log "当前仓库没有配置 git remote，跳过源码更新"
      return 0
    fi
  fi
  [[ -n "$REPO_URL" ]] || die "缺少 GitHub 仓库地址。请用 ASTREVA_REPO_URL=https://github.com/HCRXchenghong/ASTREVA.git 重新运行"

  if [[ -d "$INSTALL_DIR/.git" ]]; then
    log "检测到已有安装目录，准备更新：$INSTALL_DIR"
    if [[ -n "$(git -C "$INSTALL_DIR" status --porcelain 2>/dev/null || true)" ]]; then
      local patch
      patch="/var/backups/astreva-local-$(date +%Y%m%dT%H%M%SZ).patch"
      mkdir -p /var/backups
      git -C "$INSTALL_DIR" diff >"$patch" || true
      if [[ "$FORCE_UPDATE" != "1" ]]; then
        die "安装目录有未提交改动，已备份到 $patch；确认无误后用 ASTREVA_FORCE_UPDATE=1 重跑"
      fi
      warn "检测到本地改动，已备份 patch：$patch；按 --force 覆盖源码"
      git -C "$INSTALL_DIR" reset --hard HEAD
      git -C "$INSTALL_DIR" clean -fd -e '.env*' -e 'backups' -e 'uploads' -e 'data'
    fi
    local selected
    selected="$(pick_git_url "$REPO_URL" || true)"
    selected="${selected:-$REPO_URL}"
    log "更新代码：$(mask_url "$selected") ($REF)"
    if [[ -n "$repo_root" && "$INSTALL_DIR" == "$repo_root" ]]; then
      timeout 180 git -C "$INSTALL_DIR" fetch origin "$REF" || die "git fetch 失败"
    else
      timeout 120 git -C "$INSTALL_DIR" fetch --depth 1 origin "$REF" || die "git fetch 失败"
    fi
    if [[ -n "$repo_root" && "$INSTALL_DIR" == "$repo_root" && "$FORCE_UPDATE" != "1" ]]; then
      if ! git -C "$INSTALL_DIR" merge-base --is-ancestor HEAD FETCH_HEAD 2>/dev/null; then
        die "当前仓库存在未推送到远端的本地提交；为避免覆盖，请先 push 或用 ASTREVA_FORCE_UPDATE=1 明确强制更新"
      fi
    fi
    git -C "$INSTALL_DIR" checkout -B "$REF" FETCH_HEAD >/dev/null 2>&1 || git -C "$INSTALL_DIR" checkout --detach FETCH_HEAD
    git -C "$INSTALL_DIR" reset --hard FETCH_HEAD
    git -C "$INSTALL_DIR" remote set-url origin "$REPO_URL"
  else
    [[ -e "$INSTALL_DIR" && -n "$(ls -A "$INSTALL_DIR" 2>/dev/null || true)" ]] && die "安装目录非空且不是 git 仓库：$INSTALL_DIR"
    local selected
    selected="$(pick_git_url "$REPO_URL" || true)"
    selected="${selected:-$REPO_URL}"
    log "克隆代码：$(mask_url "$selected") ($REF)"
    mkdir -p "$(dirname "$INSTALL_DIR")"
    if ! timeout 180 git clone --depth 1 --branch "$REF" "$selected" "$INSTALL_DIR"; then
      rm -rf "$INSTALL_DIR"
      timeout 180 git clone --depth 1 "$selected" "$INSTALL_DIR" || die "git clone 失败"
      git -C "$INSTALL_DIR" fetch --depth 1 origin "$REF" >/dev/null 2>&1 || true
      git -C "$INSTALL_DIR" checkout "$REF" >/dev/null 2>&1 \
        || git -C "$INSTALL_DIR" checkout FETCH_HEAD >/dev/null 2>&1 \
        || die "无法切到版本 $REF"
    fi
    git -C "$INSTALL_DIR" remote set-url origin "$REPO_URL"
  fi

  local head expected canonical
  head="$(git -C "$INSTALL_DIR" rev-parse HEAD)"
  if [[ -n "${ASTREVA_COMMIT:-}" && "$head" != "$ASTREVA_COMMIT" ]]; then
    die "HEAD $head 与 ASTREVA_COMMIT=$ASTREVA_COMMIT 不一致，拒绝部署"
  fi
  expected="$head"
  canonical="$(timeout 15 git ls-remote "$REPO_URL" "$REF" 2>/dev/null | awk 'NR==1{print $1}' || true)"
  if [[ -z "$canonical" && "$USE_MIRRORS" == "1" ]]; then
    warn "无法通过 GitHub 官方校验 commit（可能是私库/网络受限）；本次仅能校验本地 HEAD"
  fi
  if [[ -n "$canonical" && "$canonical" != "$expected" ]]; then
    die "代码校验失败：本地 HEAD=$expected，GitHub 官方 $REF=$canonical（可能用了过期/被篡改的镜像）。请设置 ASTREVA_USE_MIRRORS=0 重试"
  fi
  ok "代码版本：$(git -C "$INSTALL_DIR" rev-parse --short HEAD) ($REF)"
}

select_stack_name() {
  if [[ -n "$STACK_NAME" ]]; then return 0; fi
  STACK_NAME="astreva-ubuntu"
  local project workdir line
  while IFS= read -r line; do
    project="${line%%|*}"; workdir="${line#*|}"
    [[ -n "$project" ]] || continue
    if [[ "$workdir" == "$INSTALL_DIR" || "$workdir" == "$INSTALL_DIR/deployments/ubuntu" ]]; then
      STACK_NAME="$project"
      log "检测到已有 Compose 项目：$STACK_NAME（$workdir）"
      return 0
    fi
  done < <(docker ps --format '{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.project.working_dir"}}' 2>/dev/null || true)
  for project in astreva-ubuntu ubuntu astreva; do
    if docker ps --filter "label=com.docker.compose.project=$project" --format '{{.Names}}' 2>/dev/null | grep -q .; then
      STACK_NAME="$project"
      log "检测到已有 Compose 项目：$STACK_NAME"
      return 0
    fi
    if docker volume inspect "${project}_postgres-data" >/dev/null 2>&1; then
      STACK_NAME="$project"
      log "检测到已有停止的 Compose 项目：$STACK_NAME（保留数据库卷）"
      return 0
    fi
  done
  # 自定义项目名：同时存在 postgres-data 与 admin-data/caddy-data 才认为属于本系统，避免误认。
  local volume candidate
  while IFS= read -r volume; do
    [[ "$volume" == *_postgres-data ]] || continue
    candidate="${volume%_postgres-data}"
    if docker volume inspect "${candidate}_admin-data" >/dev/null 2>&1 \
      || docker volume inspect "${candidate}_caddy-data" >/dev/null 2>&1; then
      STACK_NAME="$candidate"
      log "检测到已有停止的 Compose 项目：$STACK_NAME（按数据卷匹配）"
      return 0
    fi
  done < <(docker volume ls --format '{{.Name}}' 2>/dev/null || true)
}

validate_domain() {
  local d="$1"
  [[ "$d" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$ ]] || return 1
  [[ "$d" != "example.com" && "$d" != *.example.com && "$d" != "localhost" ]] || return 1
  [[ ! "$d" =~ ^[0-9.]+$ ]] || return 1
  return 0
}

collect_domains() {
  CURRENT_STEP="确认部署域名"
  local env_file="$INSTALL_DIR/deployments/ubuntu/.env.ubuntu"
  local existing_site="" existing_admin="" existing_service="" existing_email=""
  if [[ -f "$env_file" ]]; then
    existing_site="$(sed -n 's/^SITE_DOMAIN=//p' "$env_file" | tail -n1)"
    existing_admin="$(sed -n 's/^ADMIN_DOMAIN=//p' "$env_file" | tail -n1)"
    existing_service="$(sed -n 's/^SERVICE_DOMAIN=//p' "$env_file" | tail -n1)"
    existing_email="$(sed -n 's/^ACME_EMAIL=//p' "$env_file" | tail -n1)"
  fi

  if [[ -z "$DOMAIN" ]]; then DOMAIN="$existing_site"; fi
  if [[ -z "$DOMAIN" ]]; then prompt_tty DOMAIN "请输入部署主域名（例如 www.example.com）"; fi
  validate_domain "$DOMAIN" || die "域名格式不正确：$DOMAIN"
  DOMAIN="${DOMAIN,,}"

  local base="${DOMAIN#www.}"
  if [[ -z "$ADMIN_DOMAIN" ]]; then
    if [[ "$DOMAIN_EXPLICIT" == "1" ]]; then ADMIN_DOMAIN="admin.$base"; else ADMIN_DOMAIN="${existing_admin:-admin.$base}"; fi
  fi
  if [[ -z "$SERVICE_DOMAIN" ]]; then
    if [[ "$DOMAIN_EXPLICIT" == "1" ]]; then SERVICE_DOMAIN="service.$base"; else SERVICE_DOMAIN="${existing_service:-service.$base}"; fi
  fi
  validate_domain "$ADMIN_DOMAIN" || die "后台域名格式不正确：$ADMIN_DOMAIN"
  validate_domain "$SERVICE_DOMAIN" || die "客服域名格式不正确：$SERVICE_DOMAIN"

  if [[ -z "$EMAIL" ]]; then EMAIL="$existing_email"; fi
  if [[ -z "$EMAIL" ]]; then prompt_tty EMAIL "请输入 ACME 证书邮箱"; fi
  [[ "$EMAIL" =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || die "邮箱格式不正确：$EMAIL"
  DOMAIN="${DOMAIN,,}"; ADMIN_DOMAIN="${ADMIN_DOMAIN,,}"; SERVICE_DOMAIN="${SERVICE_DOMAIN,,}"

  log "主站：https://$DOMAIN"
  log "后台：https://$ADMIN_DOMAIN/site-admin/"
  log "客服：https://$SERVICE_DOMAIN/"
  if [[ "$NONINTERACTIVE" != "1" && -r /dev/tty ]]; then
    local confirm
    read -r -p "确认使用以上域名？[Y/n]: " confirm </dev/tty || true
    case "${confirm:-Y}" in y|Y|yes|YES) ;; *) die "已取消";; esac
  fi
}

check_dns_and_ports() {
  CURRENT_STEP="检查 DNS 和端口"
  local domain
  for domain in "$DOMAIN" "$ADMIN_DOMAIN" "$SERVICE_DOMAIN"; do
    if ! getent ahosts "$domain" 2>/dev/null | awk '{print $1}' | grep -q .; then
      die "域名 $domain 没有 A/AAAA 记录。请先把 DNS 指向本机公网 IP，再运行脚本"
    fi
  done
  local public_ip=""
  public_ip="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
  if [[ -n "$public_ip" ]]; then
    local resolved
    resolved="$(getent ahosts "$DOMAIN" 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ')"
    if [[ "$resolved" != *"$public_ip"* ]]; then
      warn "域名 $DOMAIN 解析到 [$resolved]，本机公网 IP 是 $public_ip；如使用 Cloudflare 代理可忽略"
    fi
  fi

  if command -v ss >/dev/null 2>&1; then
    local caddy_running="" port listeners
    caddy_running="$(docker ps --filter "label=com.docker.compose.project=$STACK_NAME" --filter "label=com.docker.compose.service=caddy" --format '{{.Names}}' 2>/dev/null || true)"
    for port in 80 443; do
      listeners="$(ss -ltn "sport = :$port" 2>/dev/null | tail -n +2 || true)"
      if [[ -n "$listeners" && -z "$caddy_running" ]]; then
        die "端口 $port 已被占用，Caddy 需要独占 80/443；请先停止宿主机 nginx/apache 等进程"
      fi
    done
  fi

  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow 80/tcp >/dev/null 2>&1 || true
    ufw allow 443/tcp >/dev/null 2>&1 || true
    log "已放通 UFW 80/443"
  fi
  if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
    firewall-cmd --permanent --add-service=http >/dev/null 2>&1 || true
    firewall-cmd --permanent --add-service=https >/dev/null 2>&1 || true
    firewall-cmd --reload >/dev/null 2>&1 || true
    log "已放通 firewalld http/https"
  fi
}

rand_hex() { openssl rand -hex "${1:-24}"; }

env_set() {
  local file="$1" key="$2" value="$3"
  python3 - "$file" "$key" "$value" <<'PY'
import sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
lines = []
found = False
try:
    with open(path, "r", encoding="utf-8") as fh:
        lines = fh.readlines()
except FileNotFoundError:
    lines = []
for i, line in enumerate(lines):
    if line.startswith(key + "="):
        lines[i] = f"{key}={value}\n"
        found = True
        break
if not found:
    if lines and not lines[-1].endswith("\n"):
        lines[-1] += "\n"
    lines.append(f"{key}={value}\n")
with open(path, "w", encoding="utf-8") as fh:
    fh.writelines(lines)
PY
}

env_get() {
  local file="$1" key="$2"
  sed -n "s/^${key}=//p" "$file" | tail -n1
}

write_env() {
  CURRENT_STEP="生成生产环境配置"
  local dir="$INSTALL_DIR/deployments/ubuntu"
  local env_file="$dir/.env.ubuntu"
  local existed=0
  [[ -f "$env_file" ]] && existed=1
  if [[ "$existed" -eq 1 ]]; then
    mkdir -p "$INSTALL_DIR/backups/env"
    cp "$env_file" "$INSTALL_DIR/backups/env/.env.ubuntu.$(date +%Y%m%dT%H%M%SZ)"
    chmod 600 "$INSTALL_DIR/backups/env/.env.ubuntu."*
  fi
  if [[ "$existed" -eq 0 ]]; then
    cp "$dir/.env.ubuntu.example" "$env_file"
    chmod 600 "$env_file"
  fi

  # 已有部署：绝不轮换 DATA_ENCRYPTION_KEY / POSTGRES_PASSWORD / 管理员口令，否则会解不开旧数据。
  local data_key postgres_password admin_password agent_password session_secret rebuild_secret metrics_token internal_token minio_password setup_token
  data_key="$(env_get "$env_file" DATA_ENCRYPTION_KEY)"
  postgres_password="$(env_get "$env_file" POSTGRES_PASSWORD)"
  admin_password="$(env_get "$env_file" ADMIN_BOOTSTRAP_PASSWORD)"
  agent_password="$(env_get "$env_file" AGENT_BOOTSTRAP_PASSWORD)"
  session_secret="$(env_get "$env_file" ADMIN_2FA_SESSION_SECRET)"
  rebuild_secret="$(env_get "$env_file" REBUILD_WEBHOOK_SECRET)"
  metrics_token="$(env_get "$env_file" METRICS_BEARER_TOKEN)"
  internal_token="$(env_get "$env_file" INTERNAL_API_TOKEN)"
  minio_password="$(env_get "$env_file" MINIO_ROOT_PASSWORD)"
  setup_token="$(env_get "$env_file" ADMIN_SETUP_TOKEN)"

  if [[ -z "$data_key" || "$data_key" == change-me* ]]; then
    [[ "$existed" -eq 1 ]] && die "已有环境文件缺少有效的 DATA_ENCRYPTION_KEY，拒绝自动轮换（会解不开旧数据）；请手工恢复"
    data_key="$(rand_hex 32)"
  fi
  [[ -n "$postgres_password" ]] || postgres_password="$(rand_hex 24)"
  [[ -n "$admin_password" ]] || admin_password="$(rand_hex 18)"
  [[ -n "$agent_password" ]] || agent_password="$(rand_hex 18)"
  [[ -n "$session_secret" ]] || session_secret="$(rand_hex 32)"
  [[ -n "$rebuild_secret" ]] || rebuild_secret="$(rand_hex 32)"
  [[ -n "$metrics_token" ]] || metrics_token="$(rand_hex 24)"
  [[ -n "$internal_token" ]] || internal_token="$(rand_hex 32)"
  [[ -n "$minio_password" ]] || minio_password="$(rand_hex 24)"
  [[ -n "$setup_token" ]] || setup_token="$(rand_hex 24)"
  if [[ "$existed" -eq 1 && "$postgres_password" == change-me* ]]; then
    die "已有环境文件里的 POSTGRES_PASSWORD 仍是示例值，拒绝自动轮换（会与旧数据库不匹配）；请手工恢复"
  fi
  [[ "$postgres_password" == change-me* ]] && postgres_password="$(rand_hex 24)"
  [[ "$admin_password" == change-me* ]] && admin_password="$(rand_hex 18)"
  [[ "$agent_password" == change-me* ]] && agent_password="$(rand_hex 18)"
  [[ "$session_secret" == change-me* ]] && session_secret="$(rand_hex 32)"
  [[ "$rebuild_secret" == change-me* ]] && rebuild_secret="$(rand_hex 32)"
  [[ "$metrics_token" == change-me* ]] && metrics_token="$(rand_hex 24)"
  [[ "$internal_token" == change-me* ]] && internal_token="$(rand_hex 32)"
  [[ "$minio_password" == change-me* ]] && minio_password="$(rand_hex 24)"

  env_set "$env_file" SITE_DOMAIN "$DOMAIN"
  env_set "$env_file" ADMIN_DOMAIN "$ADMIN_DOMAIN"
  env_set "$env_file" SERVICE_DOMAIN "$SERVICE_DOMAIN"
  env_set "$env_file" ACME_EMAIL "$EMAIL"
  local db_name db_user media_driver upload_driver
  db_name="$(env_get "$env_file" POSTGRES_DB)"; db_name="${db_name:-customer_service}"
  db_user="$(env_get "$env_file" POSTGRES_USER)"; db_user="${db_user:-customer_service}"
  media_driver="$(env_get "$env_file" ADMIN_MEDIA_DRIVER)"; media_driver="${media_driver:-local}"
  upload_driver="$(env_get "$env_file" UPLOAD_DRIVER)"; upload_driver="${upload_driver:-local}"
  # 首次安装默认本地卷媒体，避免没有 S3 凭证直接失败；已有部署保持原驱动。
  if [[ "$existed" -eq 0 ]]; then media_driver="local"; upload_driver="local"; fi
  env_set "$env_file" POSTGRES_DB "$db_name"
  env_set "$env_file" POSTGRES_USER "$db_user"
  env_set "$env_file" POSTGRES_PASSWORD "$postgres_password"
  env_set "$env_file" DATABASE_URL "postgres://$db_user:$postgres_password@postgres:5432/$db_name?sslmode=disable"
  env_set "$env_file" DATA_ENCRYPTION_KEY "$data_key"
  env_set "$env_file" ADMIN_BOOTSTRAP_PASSWORD "$admin_password"
  env_set "$env_file" AGENT_BOOTSTRAP_PASSWORD "$agent_password"
  env_set "$env_file" ADMIN_2FA_SESSION_SECRET "$session_secret"
  env_set "$env_file" REBUILD_WEBHOOK_SECRET "$rebuild_secret"
  env_set "$env_file" METRICS_BEARER_TOKEN "$metrics_token"
  env_set "$env_file" INTERNAL_API_TOKEN "$internal_token"
  env_set "$env_file" MINIO_ROOT_USER "$(env_get "$env_file" MINIO_ROOT_USER || echo astreva-minio)"
  env_set "$env_file" MINIO_ROOT_PASSWORD "$minio_password"
  env_set "$env_file" ADMIN_SETUP_TOKEN "$setup_token"
  env_set "$env_file" CORS_ALLOWED_ORIGINS "https://$DOMAIN,https://$ADMIN_DOMAIN,https://$SERVICE_DOMAIN"
  env_set "$env_file" FRAME_ANCESTORS "https://$DOMAIN"
  env_set "$env_file" TRUSTED_PROXY_CIDRS "127.0.0.1/32,::1/128,172.16.0.0/12"
  env_set "$env_file" UPLOAD_DRIVER "$upload_driver"
  env_set "$env_file" UPLOAD_PUBLIC_BASE_URL "https://$SERVICE_DOMAIN"
  env_set "$env_file" ADMIN_MEDIA_DRIVER "$media_driver"
  env_set "$env_file" ADMIN_ALLOW_LOCAL_MEDIA "$([[ "$media_driver" == "local" ]] && echo true || echo false)"
  env_set "$env_file" ADMIN_S3_KEY_PREFIX "$(env_get "$env_file" ADMIN_S3_KEY_PREFIX || true)"
  if [[ "$media_driver" == "local" ]]; then
    env_set "$env_file" S3_ENDPOINT ""
    env_set "$env_file" S3_BUCKET ""
    env_set "$env_file" S3_ACCESS_KEY_ID ""
    env_set "$env_file" S3_SECRET_ACCESS_KEY ""
    env_set "$env_file" S3_SESSION_TOKEN ""
    env_set "$env_file" S3_PUBLIC_BASE_URL ""
  fi
  env_set "$env_file" PREVIEW_COOKIE_DOMAIN ""
  chmod 600 "$env_file"
  SETUP_TOKEN="$setup_token"
  ok "环境文件：$env_file"
}

compose() {
  docker compose -f "$INSTALL_DIR/deployments/ubuntu/docker-compose.ubuntu.yml" \
    --env-file "$INSTALL_DIR/deployments/ubuntu/.env.ubuntu" -p "$STACK_NAME" "$@"
}

backup_before_update() {
  CURRENT_STEP="更新前备份数据库"
  local env_file="$INSTALL_DIR/deployments/ubuntu/.env.ubuntu"
  local backup_dir
  backup_dir="$INSTALL_DIR/backups/pre-deploy-$(date +%Y%m%dT%H%M%SZ)"
  mkdir -p "$backup_dir"
  cp "$env_file" "$backup_dir/.env.ubuntu"
  chmod 600 "$backup_dir/.env.ubuntu"
  if compose ps -q postgres 2>/dev/null | grep -q .; then
    log "检测到旧版本 PostgreSQL，执行 pg_dump 备份..."
    COMPOSE_PROJECT_NAME="$STACK_NAME" \
    ASTREVA_UBUNTU_ENV="$env_file" \
    ASTREVA_BACKUP_DIR="$backup_dir" \
    bash "$INSTALL_DIR/scripts/backup-production.sh" || die "数据库备份失败，已停止更新（不会动数据库）"
  else
    log "首次安装或数据库未启动，跳过数据库备份"
  fi
  ok "更新前备份：$backup_dir"
}

fix_volume_ownership() {
  CURRENT_STEP="修正媒体卷权限"
  local name uid gid volume
  for spec in "admin-uploads:1001:1001" "admin-data:1001:1001" "servicebridge-uploads:10001:10001" "rebuild-webhook-home:10001:10001" "frontend-published:10001:10001"; do
    name="${spec%%:*}"; uid="$(echo "$spec" | cut -d: -f2)"; gid="$(echo "$spec" | cut -d: -f3)"
    volume="${STACK_NAME}_${name}"
    if docker volume inspect "$volume" >/dev/null 2>&1; then
      log "修正已有卷权限：$volume → $uid:$gid"
      local docker_root volume_path
      docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)"
      volume_path="${docker_root}/volumes/${volume}/_data"
      if [[ -n "$docker_root" && -d "$volume_path" ]] && chown -R "$uid:$gid" "$volume_path"; then
        continue
      fi
      docker run --rm -v "$volume:/volume" alpine:3.20 sh -c "chown -R $uid:$gid /volume" >/dev/null 2>&1 \
        || die "无法修正 $volume 权限；请手动执行 chown -R $uid:$gid（或检查 Docker 权限）"
    fi
  done
}

deploy_stack() {
  CURRENT_STEP="构建并启动服务"
  log "校验 Compose 配置..."
  compose config >/dev/null || die "Compose 配置校验失败，请检查 .env.ubuntu"
  log "拉取基础设施镜像（数据库/Redis/MinIO/Caddy）..."
  local service
  for service in postgres redis minio caddy; do
    compose pull "$service" >/dev/null 2>&1 || warn "拉取 $service 镜像失败，将尝试使用本地缓存/构建"
  done
  fix_volume_ownership
  log "启动/更新服务（不会 down -v，不会删除数据库卷）..."
  ASTREVA_STACK_NAME="$STACK_NAME" ASTREVA_UBUNTU_ENV="$INSTALL_DIR/deployments/ubuntu/.env.ubuntu" \
    bash "$INSTALL_DIR/scripts/ubuntu-one-click.sh" up
}

check_one() {
  local label="$1" url="$2" expected="${3:-200}" body code
  body="$(mktemp /tmp/astreva-health.XXXXXX)"
  code="$(curl -LfsS --max-time 20 -o "$body" -w '%{http_code}' "$url" 2>/dev/null || true)"
  if [[ "$code" == "$expected" ]]; then
    ok "$label → $code"
    rm -f "$body"
    return 0
  fi
  printf '\033[1;31m[fail]\033[0m %s → HTTP %s（期望 %s） %s\n' "$label" "${code:-000}" "$expected" "$url" >&2
  head -c 300 "$body" 2>/dev/null | sed 's/^/       /' >&2 || true
  rm -f "$body"
  return 1
}

check_any() {
  local label="$1" url="$2"; shift 2
  local allowed=("$@") code body
  body="$(mktemp /tmp/astreva-health.XXXXXX)"
  code="$(curl -LfsS --max-time 20 -o "$body" -w '%{http_code}' "$url" 2>/dev/null || true)"
  local item
  for item in "${allowed[@]}"; do
    if [[ "$code" == "$item" ]]; then
      ok "$label → $code（允许）"
      rm -f "$body"; return 0
    fi
  done
  printf '\033[1;31m[fail]\033[0m %s → HTTP %s（允许：%s） %s\n' "$label" "${code:-000}" "${allowed[*]}" "$url" >&2
  rm -f "$body"; return 1
}

health_checks() {
  CURRENT_STEP="最终健康检查"
  local failures=0
  check_one "官网首页" "https://$DOMAIN/" 200 || failures=$((failures+1))
  check_one "官网 robots" "https://$DOMAIN/robots.txt" 200 || failures=$((failures+1))
  check_one "官网 sitemap" "https://$DOMAIN/sitemap-index.xml" 200 || failures=$((failures+1))
  check_one "后台登录页" "https://$ADMIN_DOMAIN/site-admin/" 200 || failures=$((failures+1))
  check_one "后台健康检查" "https://$ADMIN_DOMAIN/healthz" 200 || failures=$((failures+1))
  check_one "官网内容 API" "https://$ADMIN_DOMAIN/site-admin-api/public-content?locale=zh-CN" 200 || failures=$((failures+1))
  check_one "后台语言推荐 API" "https://$ADMIN_DOMAIN/site-admin-api/public-locale" 200 || failures=$((failures+1))
  check_one "客服首页" "https://$SERVICE_DOMAIN/" 200 || failures=$((failures+1))
  check_one "客服健康检查" "https://$SERVICE_DOMAIN/readyz" 200 || failures=$((failures+1))
  check_one "客服联系方式 API" "https://$SERVICE_DOMAIN/api/contact-settings" 200 || failures=$((failures+1))
  check_one "客服公开配置 API" "https://$SERVICE_DOMAIN/api/app-config" 200 || failures=$((failures+1))
  # 开放 API 默认关闭，关闭时 503 是正常状态；开启后才应 200/401。
  check_any "开放 API 状态" "https://$SERVICE_DOMAIN/open/v1/me" 200 401 503 || failures=$((failures+1))

  log "检查容器运行状态..."
  local services running svc ids id state
  services="$(compose config --services 2>/dev/null || true)"
  running="$(compose ps --services --filter status=running 2>/dev/null || true)"
  while IFS= read -r svc; do
    [[ -n "$svc" ]] || continue
    if [[ "$svc" == "minio-init" ]]; then continue; fi
    if ! grep -qx "$svc" <<<"$running"; then
      printf '\033[1;31m[fail]\033[0m 容器未运行：%s\n' "$svc" >&2
      failures=$((failures+1))
    fi
  done <<<"$services"

  ids="$(compose ps -q 2>/dev/null || true)"
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    name="$(docker inspect -f '{{.Name}}' "$id" 2>/dev/null || echo "$id")"
    if [[ "$name" == *"minio-init"* ]]; then continue; fi
    state="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null || echo unknown)"
    case "$state" in
      healthy|running) ;;
      exited)
        local exit_code
        exit_code="$(docker inspect -f '{{.State.ExitCode}}' "$id" 2>/dev/null || echo 1)"
        [[ "$exit_code" == "0" ]] && continue
        printf '\033[1;31m[fail]\033[0m 容器异常退出：%s (%s)\n' "$name" "$exit_code" >&2
        failures=$((failures+1)) ;;
      *) printf '\033[1;31m[fail]\033[0m 容器状态异常：%s (%s)\n' "$name" "$state" >&2; failures=$((failures+1)) ;;
    esac
  done <<<"$ids"

  log "检查 TLS 证书..."
  local end_date end_epoch now_epoch
  end_date="$(echo | openssl s_client -servername "$DOMAIN" -connect "$DOMAIN:443" 2>/dev/null | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2 || true)"
  if [[ -n "$end_date" ]]; then
    end_epoch="$(date -d "$end_date" +%s 2>/dev/null || true)"
    now_epoch="$(date +%s)"
    if [[ -n "$end_epoch" && "$end_epoch" -gt "$now_epoch" ]]; then
      ok "TLS 证书有效至：$end_date"
    else
      printf '\033[1;31m[fail]\033[0m TLS 证书无效或已过期\n' >&2
      failures=$((failures+1))
    fi
  else
    printf '\033[1;31m[fail]\033[0m 无法读取 %s 的 TLS 证书\n' "$DOMAIN" >&2
    failures=$((failures+1))
  fi

  if [[ "$failures" -ne 0 ]]; then
    printf '\n\033[1;31m最终检查失败：%d 项异常。服务保持运行，请排查后再执行本脚本或重启。\033[0m\n' "$failures" >&2
    printf '常用排查：docker compose -f deployments/ubuntu/docker-compose.ubuntu.yml logs --tail=150 caddy admin-server servicebridge-backend-1\n' >&2
    exit 1
  fi
  ok "全部 HTTP / 容器 / 证书检查通过"
}

main() {
  log "============================================================"
  log " ASTREVA 一键部署（商业化）"
  log "============================================================"
  detect_os
  install_base_packages
  check_environment
  configure_docker_mirrors
  resolve_repo
  select_stack_name
  collect_domains
  check_dns_and_ports
  write_env
  backup_before_update
  deploy_stack
  health_checks

  cat <<EOF

============================================================
 部署完成
============================================================
官网：https://$DOMAIN/
后台：https://$ADMIN_DOMAIN/site-admin/
客服：https://$SERVICE_DOMAIN/
后台健康：https://$ADMIN_DOMAIN/healthz
客服健康：https://$SERVICE_DOMAIN/readyz

首次初始化令牌（仅首次需要）：
$SETUP_TOKEN

配置：$INSTALL_DIR/deployments/ubuntu/.env.ubuntu
证书：Caddy 自动签发并自动续期（数据卷 caddy-data）
更新：重新执行本脚本即可；会先备份 PostgreSQL，再原地更新，不会 down -v
日志：docker compose -f $INSTALL_DIR/deployments/ubuntu/docker-compose.ubuntu.yml logs -f
============================================================
EOF
}

main "$@"
