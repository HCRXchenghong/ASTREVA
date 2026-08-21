# 更新记录

## v1.1.3 - 2026-08-21

本版本基于本地工作区相对 `origin/main` / `v1.1.2` 的差异整理。`main` 与 `origin/main` 的提交基线一致，本地新增和修改内容作为发布提交进入此 tag。

### 重点变更

- 统一后台切换为单一控制台资源，覆盖内容管理、客服账号、工作台、关键词、评价、审计和 App 版本管理。
- ServiceBridge 移除废弃外部接待直连接入，补齐自研客服的会话、评价、审计、关键词、上传、App 版本发布和 PostgreSQL/内存存储能力。
- 新增 Flutter `online_customer_service` 客服 App，支持 iOS/Android、账号登录、会话接待、图片消息、更新检测、Android 安装引导和 FCM 客户端接入。
- 后端新增通知抽象和 FCM HTTP v1 推送通道，支持客服设备 token 上报，并在会话分配和访客新消息时触发推送。
- Ubuntu/Compose、Nginx、环境变量和运维脚本按官网、统一后台、客服引擎和访客端一体化部署刷新。
- 文档同步到当前商业上线边界，新增 App 发布、更新与推送说明，并更新功能完善清单。

### 发布验证

以下检查已在本地通过：

- `node --check admin-server/server.mjs`
- `npm run build:frontend`
- `cd servicebridge/backend && go test ./...`
- `cd servicebridge/backend && go vet ./...`
- `cd servicebridge/apps/agent-mobile && dart analyze`
- `cd servicebridge/apps/agent-mobile && flutter test`
