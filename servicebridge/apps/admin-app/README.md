# 管理端 uni-app

管理端按 `管理端前端UI示例.html` 一比一实现为 uni-app。

当前文件：

- `ui-reference.html`：UI 基准文件。
- `common/api.js`：HTTP API 接入层。
- `common/realtime.js`：WebSocket 接入层。
- `pages/login/login.vue`：管理登录。
- `pages/dashboard/dashboard.vue`：概览、服务评价和实时刷新。
- `pages/monitor/monitor.vue`：会话监控、历史补拉、图片展示、强制转接和结束。
- `pages/agents/agents.vue`：客服创建、修改、禁用、重置密码。
- `pages/ai/ai.vue`：OpenAI 原生/兼容配置和联调。
- `pages/settings/settings.vue`：联系方式、二维码上传、关键词和系统配置。

实现要求：

- Android / iOS App。
- 数据概览、会话监控、客服管理、AI 配置、系统配置。
- 强制转接、强制结束、客服禁用、重置密码。
- 管理员登录密码自助修改，修改后要求重新登录。
- 收到服务端 `session.revoked` 后清理本地登录态、停止重连并返回登录页。
- 联系方式图文 / 纯文本配置。
- 二维码图文模式必须先上传图片，避免把演示占位图写入生产配置。
- 管理操作审计查看。
- 会话监控 CSV 导出、管理审计 CSV 导出。

后端地址：

- 默认 `http://localhost:8080`，登录页“服务”输入框可改为生产接口地址，例如 `https://api.example.com`。
- 输入值会写入本地存储键 `admin_api_base`，下一次打开 App 会自动带出。
- App 打包时也可在入口注入 `globalThis.CUSTOMER_SERVICE_API_BASE`，避免生产包写死本地地址。
