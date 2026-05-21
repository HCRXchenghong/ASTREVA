# Nginx

这里放 WebSocket 反向代理和静态前端部署配置。

第一阶段目标：

- 用户端 Web 静态资源代理。
- 后端 HTTP API 代理。
- WebSocket 升级头配置。
- 多后端节点负载均衡配置。
- `nginx.conf` 已把 `worker_connections` 提到 65535，便于 5000 WebSocket 并发压测。

生产入口建议用云负载均衡或证书网关终止 TLS，然后转发到本 Nginx；如果直接由本 Nginx 终止 TLS，需要另加 `listen 443 ssl` 和证书挂载。
