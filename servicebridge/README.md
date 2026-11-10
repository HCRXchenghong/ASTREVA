# ServiceBridge 在线客服

> 版本：v1.4.5 · 更新时间：2026-11-10

ServiceBridge 是 ASTREVA 自研在线客服子系统，包括访客聊天端、Go 实时后端、统一控制台中的客服模块，以及 iOS/Android “在线客服”App。

## 结构

- `apps/user-web/`：访客独立页和官网 iframe 页面。
- `apps/agent-mobile/`：Flutter 客服 App，支持 iOS 和 Android。
- `backend/`：Go HTTP/WebSocket 服务。
- `backend/migrations/`：PostgreSQL 初始化和增量 SQL。
- `deployments/`：开发/生产 Compose 与 Nginx 示例。
- `scripts/`：冒烟、运维、容量测试和商业验收脚本。
- `docs/`：接口、数据、部署和验收说明。

## 本地运行

```bash
cd backend
STORE_DRIVER=memory \
HTTP_ADDR=127.0.0.1:8080 \
CORS_ALLOWED_ORIGINS=http://127.0.0.1:4321,http://127.0.0.1:5173 \
go run ./cmd/server
```

快速体验可用 `STORE_DRIVER=memory`，但重启即丢；需要完整功能/持久化时使用 `STORE_DRIVER=postgres` + `DATABASE_URL`（仓库根目录 `run-local-stack.sh` 默认就是 PostgreSQL）。`HTTP_ADDR=127.0.0.1:8080` 是默认安全值；只有同一局域网真机联调时才改成 `0.0.0.0:8080`，并确认网络可信。`ADMIN_BOOTSTRAP_PASSWORD` 未设置时会生成本进程随机口令并打印一次，需与 admin-server 的 `SERVICEBRIDGE_ADMIN_PASSWORD` 保持一致。另开终端运行访客端：

```bash
python3 -m http.server 5173 --bind 127.0.0.1 --directory apps/user-web
```

访客端地址为 `http://127.0.0.1:5173/?embed=1`。统一控制台位于 `http://127.0.0.1:1337/site-admin/`（登录页 `/site-admin/login`）；客服账号在「客服管理 → 客服账号」中创建。

仓库根目录的 `bash scripts/run-local-stack.sh` 会同时启动官网、统一控制台、访客端和 Go 后端（控制台缺失构建产物时会先自动构建）。

## 已实现

- 访客创建/恢复会话、历史分页、文本/表情/图片/语音、撤回、结束和评价。
- 客服 App 账号密码登录、在线状态、会话与未读、历史消息、文本/图片回复、结束会话和改密。
- 客服 App WebSocket 心跳、断线重连、会话事件刷新和 15 秒历史补偿轮询。
- 统一控制台客服账号新增、编辑、重置密码、禁用和删除。
- 统一控制台数据概览、会话消息、转接、结束、CSV 导出，以及营业时间、欢迎语、联系方式和 AI 设置。
- WebSocket 鉴权、消息幂等、登录态撤销、Redis 跨节点广播、PostgreSQL 持久化、本地/S3 上传、安全响应头和限流。
- 统一控制台关键词规则、服务评价（独立页面）、审计日志、App 版本与 API 管理页面。
- Android 自建更新通道，包括控制台上传 APK、强制/可选更新配置、App 内检测、下载、SHA-256 校验和安装引导。
- 后端 FCM HTTP v1（服务账号自动换取 OAuth2 令牌）与 APNs（.p8 令牌签名）通知通道、客服设备 token（含平台）上报接口，以及新会话分配、转接和访客新消息推送触发点；推送参数在统一控制台「系统设置 → 推送设置」里配置并加密入库。
- 访客到客服的 Go 端自动化闭环测试，覆盖建会话、WebSocket 消息、幂等、转接、关闭、评价、审计、关键词和 App 更新。

## 尚需完善

- 生产推送凭据与真机验证：Firebase 服务账号、APNs `.p8` 需要在控制台填入并真机验证（锁屏/后台/进程回收三种状态）。
- App Store/Google Play 正式签名、隐私清单、商店素材和发布流程。
- 浏览器端到端自动化、生产备份监控、故障演练和容量验收。

这些增强项不影响当前前台发起咨询、控制台管理客服账号、客服 App 在线接待的本地闭环，但完成前不能判定为商业上线。详见 [功能完善任务.md](../功能完善任务.md)。

## 验证与部署

```bash
make test
make compose-check
make smoke
make commercial-acceptance
cd apps/agent-mobile && dart analyze && flutter test
```

接口见 `docs/API与WebSocket协议.md`，生产要求见 `docs/生产部署与容量规划.md` 和 `docs/商业上线验收清单.md`。
