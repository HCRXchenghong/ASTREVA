# 用户端 Web

用户端 Web 按 `用户端前端UI示例.html` 一比一实现。

当前入口：

- `index.html`
- `assets/tailwind.css`：已编译好的本地 Tailwind 样式，生产不依赖 `cdn.tailwindcss.com`。
- `assets/vendor/`：用户端运行所需的 GSAP、Font Awesome 等本地静态资源。

后端地址配置：

- 默认同源访问；本地 `file://` 或静态端口访问时默认连 `http://127.0.0.1:8080`。
- 生产独立域名部署时，可修改 `index.html` 的 `customer-service-api-base` meta，或在页面加载前注入 `window.CUSTOMER_SERVICE_API_BASE = 'https://api.example.com'`。

运行方式：

```bash
cd apps/user-web
npx serve .
```

如调整了 HTML 中的 Tailwind 类名，重新生成本地 CSS：

```bash
npx tailwindcss@3.4.17 -i apps/user-web/tailwind.input.css -o apps/user-web/assets/tailwind.css --content apps/user-web/index.html --minify
```

实现要求：

- 手机浏览器全屏聊天体验。
- 电脑浏览器居中手机外壳体验。
- PC 端允许在现有样式基础上优化尺寸、居中和最大高度。
- 聊天气泡、底部面板、Toast、评价弹层和状态提示保持示例风格。
