# ServiceBridge Nginx

`nginx.conf` 配置 worker 和连接参数，`customer-service.conf` 提供：

- `/`：访客静态页面。
- `/assets/`：带长期缓存的静态资源。
- `/api/`、`/uploads/`：反代两个 Go 后端。
- `/ws`：WebSocket 升级和长连接超时。

生产必须将 CSP `frame-ancestors` 中的示例站点替换为实际官网域名，且与后端 `CORS_ALLOWED_ORIGINS`/`FRAME_ANCESTORS` 一致。上传体积同时受 Nginx `client_max_body_size` 和后端 `UPLOAD_MAX_BYTES` 限制。
