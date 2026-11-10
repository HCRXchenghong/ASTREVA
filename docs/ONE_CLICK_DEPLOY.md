# ASTREVA 一键部署

适用于 **Ubuntu 22.04 / 24.04**（也兼容 Debian 12、RHEL 9 系）的全新安装和旧版本升级。

## 一条命令

仓库地址：`https://github.com/HCRXchenghong/ASTREVA`。

```bash
curl -fsSL https://raw.githubusercontent.com/HCRXchenghong/ASTREVA/main/scripts/install.sh \
  | sudo bash -s -- \
      --repo https://github.com/HCRXchenghong/ASTREVA.git \
      --domain www.example.com \
      --email ops@example.com
```

域名和邮箱可以省略，脚本会通过 `/dev/tty` 询问；`--repo` 不能省略（除非预先 clone 后执行 `sudo bash scripts/install.sh`）。

## 脚本会做什么

1. 检查系统、CPU 架构、root 权限、systemd；
2. 安装/升级 `curl git openssl ca-certificates python3 iproute2` 等基础依赖；
3. 检查 Docker 与 Compose 版本，低于最低版本自动升级；设置 `ASTREVA_DOCKER_VERSION` 可安装指定版本；
4. 对 GitHub、Docker 安装脚本、Docker Registry 做延迟探测，优先选择可用且较快的渠道；
5. 克隆或更新仓库到 `/opt/astreva`；
6. 询问/确认主域名、后台域名、客服域名、ACME 邮箱；
7. 生成 `.env.ubuntu`，已有部署会保留数据库密码、数据加密密钥、管理员口令等关键密钥；
8. 已有旧版本时先 `pg_dump` 备份 PostgreSQL，再原地 `docker compose up -d --build`；
9. Caddy 自动申请 SSL 证书并自动续期（`caddy-data` 卷持久化）；
10. 最终检查官网、后台、客服 API、公开接口、容器健康、TLS 证书；全部正常才退出 0。

## 域名和证书

- `--domain www.example.com` 会默认推导：
  - 主站：`www.example.com`
  - 后台：`admin.example.com`
  - 客服：`service.example.com`
- 也可以显式指定：

```bash
--admin-domain admin.example.com
--service-domain service.example.com
```

- 三个域名都需要提前添加 A/AAAA 记录指向服务器公网 IP；
- 服务器安全组/防火墙需要放通 TCP 80、443；
- Caddy 使用 HTTP-01/TLS-ALPN 自动签发证书，不需要额外安装 certbot；
- 证书自动续期，数据保存在 Docker 卷 `caddy-data`，不要执行 `down -v`。

## 升级旧版本

重新执行同一条命令即可：

```bash
curl -fsSL https://raw.githubusercontent.com/HCRXchenghong/ASTREVA/main/scripts/install.sh \
  | sudo bash -s -- \
      --repo https://github.com/HCRXchenghong/ASTREVA.git
```

- 自动读取已有 `.env.ubuntu`，不会重新生成 `DATA_ENCRYPTION_KEY` 或 `POSTGRES_PASSWORD`；
- 更新前自动备份数据库到 `backups/pre-deploy-<时间>/`；
- 只执行 `up -d --build`，不执行 `down -v`，数据库卷不会被删除；
- 源码目录有未提交改动时会中止；确认后用 `--force`（会先备份 patch）。

## 镜像与供应链

默认开启镜像测速，但第三方 GitHub/Registry 镜像存在供应链风险：

```bash
# 只用官方直连
curl ... | sudo env ASTREVA_USE_MIRRORS=0 bash -s -- --repo ...
```

更严格的生产环境可以固定 commit：

```bash
ASTREVA_COMMIT=<40位commit> ASTREVA_USE_MIRRORS=0
```

脚本会用 GitHub 官方 `git ls-remote` 校验 HEAD；不一致会拒绝部署。

## 媒体存储

首次安装默认使用本地 Docker 卷（`ADMIN_MEDIA_DRIVER=local`、`UPLOAD_DRIVER=local`、`ADMIN_ALLOW_LOCAL_MEDIA=true`），不依赖外部对象存储。

已有部署如果原来是 `ADMIN_MEDIA_DRIVER=s3`，脚本会保留原 S3 配置，不会清空。生产 Compose 不再内置 MinIO；素材使用外部 R2/S3 或单机本地卷。

## 非交互示例

```bash
curl -fsSL https://raw.githubusercontent.com/HCRXchenghong/ASTREVA/main/scripts/install.sh \
  | sudo env \
      ASTREVA_DOMAIN=www.example.com \
      ASTREVA_EMAIL=ops@example.com \
      ASTREVA_NONINTERACTIVE=1 \
      ASTREVA_USE_MIRRORS=0 \
      bash -s -- --repo https://github.com/HCRXchenghong/ASTREVA.git
```

## 失败时

脚本退出非 0 时不会关闭已启动的服务，方便排查：

```bash
cd /opt/astreva
docker compose -f deployments/ubuntu/docker-compose.ubuntu.yml ps
docker compose -f deployments/ubuntu/docker-compose.ubuntu.yml logs --tail=200 caddy admin-server servicebridge-backend-1
tail -200 /var/log/astreva-install.log
```

修好后重新执行安装命令即可，数据库和 `.env.ubuntu` 不会被清空。
