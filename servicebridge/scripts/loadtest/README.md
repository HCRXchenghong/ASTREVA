# Load Test

这里放 5000 WebSocket 并发压测脚本。

压测必须覆盖：

- 建立连接。
- 鉴权。
- 心跳。
- 文本消息发送。
- 断线重连。
- 跨节点消息投递。

当前压测入口：

```bash
cd backend
go run ./cmd/wsload -http http://localhost:8080 -ws ws://localhost:8080 -n 5000 -duration 60s -interval 1s -messages-per-conn 3
```

`wsload` 会先用可控并发建立连接，再统一进入消息阶段，避免把本机瞬时建连抖动误判成服务端长连接容量问题。常用参数：

- `-connect-parallel`：建连阶段并发 worker 数，默认 256。
- `-setup-timeout`：建连阶段最大等待时间，默认 2 分钟。
- `-duration`：连接建好后的消息阶段持续时间。

开发机可先从小并发开始：

```bash
go run ./cmd/wsload -n 100 -duration 30s
```

阶梯压测：

```bash
HTTP_BASE=http://localhost:8088 \
WS_BASE=ws://localhost:8088 \
DURATION=60s \
STEPS="100 500 1000 2500 5000" \
scripts/loadtest/run-steps.sh
```

验收压测：

```bash
HTTP_BASE=http://localhost:8088 \
WS_BASE=ws://localhost:8088 \
DURATION=60s \
STEPS="100 500 1000 2500 5000" \
scripts/loadtest/acceptance-5000.sh
```

`acceptance-5000.sh` 会在任意阶梯出现连接失败或连接数不足时直接非零退出，适合 CI 或上线前手工验收。

输出会包含：

- `connected`：成功建立的 WebSocket 连接数。
- `setup`：建连阶段耗时。
- `sent`：发送消息数。
- `acked`：服务端确认消息数。
- `received`：收到的全部事件数。
- `ack_latency_avg / p95 / max`：发送到 ACK 的延迟。

正式 5000 并发测试需要调整系统文件句柄、反代连接限制和机器资源。
如果压测流量来自单台机器或少量出口 IP，需要在测试环境临时关闭节点级限流，或把 `RATE_LIMIT_RPS/RATE_LIMIT_BURST` 调高到覆盖建连峰值；生产公网不建议关闭限流。
