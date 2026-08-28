# ServiceBridge 访客端

无构建依赖的静态访客聊天页面，入口为 `index.html`。

```bash
python3 -m http.server 5173 --bind 127.0.0.1 --directory servicebridge/apps/user-web
```

- 独立访问：`http://127.0.0.1:5173/`
- 官网嵌入：`http://127.0.0.1:5173/?embed=1`

API 地址优先读取 `<meta name="customer-service-api-base">` 或 `window.CUSTOMER_SERVICE_API_BASE`；未配置时，本地非 `8080` 端口会连接同主机的 `8080`，生产同域部署则使用当前 origin。

页面支持会话恢复、历史分页、文本/表情/图片/语音、撤回、联系方式、评价和结束会话。嵌入模式通过 `postMessage` 向官网报告 `servicebridge:ready`、`servicebridge:unread` 和 `servicebridge:conversation-closed` 事件；最后一个事件仅在访客成功提交评价（或重复提交已存在的评价）后发出，官网宿主应关闭聊天弹窗并清空未读数。宿主页面必须同时校验消息来源和 iframe 窗口。
