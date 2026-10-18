# ServiceBridge 压测

本目录提供 WebSocket 阶梯压测和目标并发验收脚本。脚本存在不代表生产容量已经验证，结果必须来自目标部署环境。

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

> **重要：仓库里有压测工具 ≠ 已经达到容量。**
> 本文只给出执行方法、记录项和通过标准。任何“已支持 N 并发”的结论都必须来自目标部署环境的实测记录，
> 并注明测试日期、代码版本（commit）、机器规格、网络拓扑与限流配置。**不要**把下面的示例命令或阈值当成实测数据。

## 阶梯压测：100 → 500 → 1000 → 2500 → 5000

按级递增，不要跳级。每级通过后再进入下一级；某级不通过就先定位瓶颈，而不是继续加大目标数。

### 每级执行命令

把 `STEPS` 换成当前要跑的一级（示例为 500）：

```bash
cd /path/to/ASTREVA-main

HTTP_BASE=http://<被测地址>:8088 \
WS_BASE=ws://<被测地址>:8088 \
DURATION=60s \
INTERVAL=1s \
MESSAGES_PER_CONN=3 \
STEPS="500" \
scripts/loadtest/run-steps.sh
```

五级分别把 `STEPS` 设为 `100`、`500`、`1000`、`2500`、`5000`。
也可以在已充分预热、且每级都能单独定位问题时一次性跑完整阶梯：

```bash
HTTP_BASE=http://<被测地址>:8088 \
WS_BASE=ws://<被测地址>:8088 \
DURATION=60s \
STEPS="100 500 1000 2500 5000" \
scripts/loadtest/run-steps.sh
```

### 每级之间的固定动作

1. 上一级结束后等待 5–10 分钟，确认 `/metrics` 中 `customer_service_ws_connections` 回落到 0（或业务基线）、`customer_service_go_goroutines` 回到基线附近。
2. 记录本级的服务端基线（CPU、内存、协程数、连接数）后再开始。
3. 压测期间采集服务端指标与客户端资源；不要只看 `wsload` 的最终输出。

采集示例（需在 `.env.ubuntu` 中配置 `METRICS_BEARER_TOKEN`）：

```bash
# 服务端连接数 / 协程数（把 <token> 与地址替换为实际值）
curl -fsS -H "Authorization: Bearer <token>" http://<后端地址>:8080/metrics \
  | grep -E 'customer_service_(ws_connections|go_goroutines)'

# 容器资源（CPU% / 内存 / 网络）
docker stats --no-stream
```

### 每级记录项（复制成表格填写）

| 记录项 | 来源 | 100 | 500 | 1000 | 2500 | 5000 |
| --- | --- | --- | --- | --- | --- | --- |
| 目标连接数 `n` | 本次配置 | | | | | |
| `connected` | wsload 输出 | | | | | |
| `failed` | wsload 输出 | | | | | |
| 建连成功率 = `connected / n` | 计算 | | | | | |
| `setup` 建连耗时 | wsload 输出 | | | | | |
| `sent` / `acked` | wsload 输出 | | | | | |
| ACK 成功率 = `acked / sent` | 计算 | | | | | |
| P95 ACK 延迟 | wsload `ack_latency_p95` | | | | | |
| 最大 ACK 延迟 | wsload `ack_latency_max` | | | | | |
| 服务端 WS 连接数 | `/metrics` | | | | | |
| 服务端协程数 | `/metrics` | | | | | |
| 服务端 CPU / 内存 | `docker stats` | | | | | |
| 客户端 CPU / 内存 / 句柄 | `top` / Activity Monitor | | | | | |
| 容器重启次数 | `docker inspect -f '{{.RestartCount}}'` | | | | | |
| 反代 5xx / 超时 | Caddy 日志 | | | | | |
| 备注（报错、瓶颈现象） | 人工 | | | | | |

> 表格只给记录位，**不要预填任何数字**。空着的格子代表“未测”，不是“通过”。

### 通过标准（每级都必须满足）

- **建连成功率 100%**：`failed = 0` 且 `connected = n`；建连耗时随 `n` 增长应接近线性，不应出现阶跃式超时。
- **连接稳定**：消息阶段结束时 `connected` 仍为 `n`，没有批量掉线；到达目标数后能稳定保持 ≥ 5 分钟。
- **消息可靠**：`acked / sent = 100%`（若业务允许丢消息，必须在此显式写明允许比例和依据）。
- **延迟可控**：P95 ACK 延迟 < 500 ms（按业务 SLA 调整），且整级过程中不持续恶化。
- **资源无失控**：服务端 CPU 持续 < 70%；内存峰值后能回落，不单调上升；`customer_service_go_goroutines` 在连接建立完成后趋于平稳而非单调增长。
- **无隐性故障**：容器 `RestartCount` 不增长；无 OOM；Redis 无超时错误；反代无 5xx 突增；文件句柄未耗尽。

任一级不满足：把该级记为“不通过”，在该级而不是更高一级排查（数据库、Redis、单实例 CPU、句柄上限、反代连接数、限流），修复后从**上一已通过级**重跑。

### 完整验收（可入 CI / 上线前手工执行）

`acceptance-5000.sh` 会在任意阶梯出现连接失败或连接数不足时直接非零退出：

```bash
HTTP_BASE=http://<被测地址>:8088 \
WS_BASE=ws://<被测地址>:8088 \
DURATION=60s \
STEPS="100 500 1000 2500 5000" \
scripts/loadtest/acceptance-5000.sh
```

注意：脚本通过只证明“这些阶梯的连接建立与保持达标”，ACK 延迟与资源占用仍需人工按上表记录并对照通过标准。
上线结论请把实测表格归档到运维记录，并在 `docs/OPERATIONS.md` 的容量章节引用。
