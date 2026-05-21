# 客服端 uni-app

客服端按 `客服端前端UI示例.html` 一比一实现为 uni-app。

当前文件：

- `ui-reference.html`：UI 基准文件。
- `common/api.js`：HTTP API 接入层。
- `common/realtime.js`：WebSocket 接入层。
- `pages/login/login.vue`：客服登录和推送 token 注册。
- `pages/sessions/sessions.vue`：会话列表、在线状态和通知入口。
- `pages/chat/chat.vue`：聊天详情、历史补拉、图片展示、接管、备注和快捷动作。
- `pages/settings/settings.vue`：客服端设置。

实现要求：

- Android / iOS App。
- 登录、会话列表、聊天详情、访客资料、备注修改。
- 登录密码自助修改，修改后要求重新登录。
- 收到服务端 `session.revoked` 后清理本地登录态、停止重连并返回登录页。
- 文本、表情、相册/拍照图片消息发送和历史图片展示。
- 前台横幅通知、提示音、震动、推送 token 注册和后台推送 webhook 对接。
- AI 接待状态下可人工接管。

后端地址：

- 默认 `http://localhost:8080`，登录页“服务”输入框可改为生产接口地址，例如 `https://api.example.com`。
- 输入值会写入本地存储键 `agent_api_base`，下一次打开 App 会自动带出。
- App 打包时也可在入口注入 `globalThis.CUSTOMER_SERVICE_API_BASE`，避免生产包写死本地地址。
