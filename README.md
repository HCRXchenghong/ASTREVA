# ASTREVA 独立站

这个仓库包含三个子系统：

- `frontend/`：Astro + React + Tailwind 静态独立站。
- `admin-server/`：自研官网后台，维护站点文案、图片、视频、SEO、品类、产品、FAQ 和表单线索。
- `servicebridge/`：基于 `HCRXchenghong/ServiceBridge` 的在线客服服务；访客端仍为 Web，人工客服入口已改为飞书内直接回复。

## 本地运行

```bash
npm install
npm run dev:admin
npm run dev:frontend
```

一键拉起前台、自研后台、重建服务和客服联调服务：

```bash
scripts/run-local-stack.sh
```

后台首次访问 `http://127.0.0.1:1337/admin-2fa`，先创建管理员邮箱和密码，再用 Microsoft Authenticator 扫码绑定 2FA。后续登录必须输入 2FA 验证码。

ServiceBridge 本地联调：

```bash
npm run servicebridge:backend
npm run servicebridge:web
```

主站在线客服 iframe 默认地址为 `PUBLIC_SERVICEBRIDGE_URL/?embed=1`，本地默认使用 `http://127.0.0.1:5173/?embed=1`。

## 本地端口

- 前台独立站：`http://127.0.0.1:4321/`
- 后台登录：`http://127.0.0.1:1337/admin-2fa`
- 官网后台：`http://127.0.0.1:1337/site-admin/`
- 后台 API：`http://127.0.0.1:1337/site-admin-api`
- 静态重建 Webhook：`http://127.0.0.1:8787/healthz`
- ServiceBridge 用户端：`http://127.0.0.1:5173/?embed=1`

## 后台闭环

完整后台使用、内容初始化、自动重建、询盘跟进流程见：

- [docs/CMS_CLOSED_LOOP.md](/Users/seron-cheng/Desktop/网站/docs/CMS_CLOSED_LOOP.md)
