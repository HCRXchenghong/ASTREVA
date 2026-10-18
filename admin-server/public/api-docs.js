const esc = (value = '') => String(value).replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[char]);

let noticeTimer;
function notify(message, isError = false) {
  const notice = document.getElementById('notice');
  notice.textContent = message;
  notice.className = `notice show${isError ? ' error' : ''}`;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { notice.className = 'notice'; }, 3500);
}

function apiDocsData(root) {
  const base = root || 'https://你的客服引擎域名';
  return {
    root: base,
    wsRoot: base.replace(/^http/, 'ws'),
    endpoints: [
      ['GET', '/open/v1/me', '连通性测试，返回当前授权应用信息'],
      ['POST', '/open/v1/conversations', '创建会话，body：{"external_user_id":"对方平台用户ID","lang":"zh|en|ar"}'],
      ['GET', '/open/v1/conversations', '列出本应用创建的会话（最多 200 条，按更新时间倒序）'],
      ['GET', '/open/v1/conversations/{id}', '查询单个会话状态'],
      ['GET', '/open/v1/conversations/{id}/messages?limit=50&before={server_msg_id}', '拉取历史消息（访客视角，已按会话语言本地化，支持翻页）'],
      ['POST', '/open/v1/conversations/{id}/messages', '代访客发消息：{"content":"…","message_type":"text|emoji|image","client_msg_id":"幂等ID"}'],
      ['POST', '/open/v1/conversations/{id}/close', '结束会话'],
      ['GET', '/open/v1/ws?conversation_id={id}', 'WebSocket 实时订阅该会话的消息与状态事件（只读）']
    ],
    wsEvents: [
      ['connected', '连接建立，附带会话信息'],
      ['message.ack', '本应用刚发送的消息回执'],
      ['message.receive', '新消息：AI 回复、人工客服回复、系统消息（sender_type 区分）'],
      ['conversation.typing', '客服 / AI「正在输入」指示，可用于渲染动效'],
      ['conversation.status_changed', '会话状态变化（等待中 / AI 接待 / 人工服务 / 已结束等）'],
      ['pong', '心跳响应（建议每 30-60 秒发送 {"event":"ping"} 保活，70 秒无活动会断开）'],
      ['error', '错误提示（如发送了不被接受的上行事件）']
    ],
    faq: [
      ['401 unauthorized', 'Key 错误、已被重置或授权被禁用；在后台「API 管理」确认授权状态。'],
      ['404 not found', '会话不存在，或该会话由其他应用创建（会话按应用严格隔离）。'],
      ['429 rate limit', '超过该应用的每分钟限速；降低调用频率或联系管理员调高限速。'],
      ['收不到人工回复', '确认 WebSocket 仍在连接（70 秒无心跳会断开）；可用 GET messages?before= 轮询兜底补消息。'],
      ['多节点部署', '客服引擎多副本 + Redis 时事件经 Redis 总线分发，订阅任意节点均可收到全部事件。']
    ]
  };
}

function buildDocsHtml(docs) {
  return `
  <section class="panel">
    <h2>1. 获取 API Key</h2>
    <p>在「API 管理」中为接入平台「新增授权」后生成形如 <code>ast_live_xxxxxxxx…</code> 的 Key。<strong>完整 Key 只显示这一次</strong>，请立即发给接入方妥善保管；系统只保存 Key 的哈希，后台无法再次查看。Key 泄露时随时「重置 Key」（旧 Key 立即失效），也可禁用或删除授权。</p>
    <p>所有请求携带以下任一鉴权信息：</p>
    <pre><code>Authorization: Bearer ast_live_xxxxxxxx…
X-API-Key: ast_live_xxxxxxxx…</code></pre>
    <p>WebSocket 订阅额外支持 query 参数 <code>key=ast_live_…</code>（浏览器 WebSocket 无法自定义请求头）。</p>
  </section>
  <section class="panel">
    <h2>2. 服务地址与通用约定</h2>
    <p>开放接口部署在客服引擎域名下：<span class="badge-blue">${esc(docs.root)}</span>（即「全站设置 → ServiceBridge 地址」）。</p>
    <ul>
      <li>请求与响应均为 <code>application/json; charset=utf-8</code>；错误响应形如 <code>{"error": "..."}</code>。</li>
      <li>会话归属：每个应用只能访问自己通过 API 创建的会话，跨应用访问返回 404。</li>
      <li>限速：令牌桶，默认每分钟 300 次，可在后台按应用单独设置；超限返回 429。</li>
      <li>接入会话与官网访客完全同流程：欢迎语、关键词规则、AI 接待、转人工、评价与审计。</li>
    </ul>
    <h3>接口一览</h3>
    <div class="table-wrap"><table><thead><tr><th>方法</th><th>路径</th><th>说明</th></tr></thead><tbody>
      ${docs.endpoints.map(([method, path, note]) => `<tr><td><strong>${esc(method)}</strong></td><td><code>${esc(path)}</code></td><td>${esc(note)}</td></tr>`).join('')}
    </tbody></table></div>
  </section>
  <section class="panel">
    <h2>3. 创建会话</h2>
    <pre><code>curl -X POST '${esc(docs.root)}/open/v1/conversations' \\
  -H 'Authorization: Bearer ast_live_xxx' \\
  -H 'Content-Type: application/json' \\
  -d '{"external_user_id": "user-1001", "lang": "en"}'</code></pre>
    <p><code>external_user_id</code>（可选，≤128 字符）会显示为客服工作台中的访客备注，便于人工客服识别对方平台的用户；<code>lang</code> 可选 <code>zh</code> / <code>en</code> / <code>ar</code>，决定 AI 回复语言与消息本地化。</p>
  </section>
  <section class="panel">
    <h2>4. 发送消息</h2>
    <pre><code>curl -X POST '${esc(docs.root)}/open/v1/conversations/{会话ID}/messages' \\
  -H 'Authorization: Bearer ast_live_xxx' \\
  -H 'Content-Type: application/json' \\
  -d '{"content": "Hello, I want a quote", "message_type": "text", "client_msg_id": "msg-0001"}'</code></pre>
    <p>响应中的 <code>generated</code> 为本次触发的即时回复（关键词自动回复、转人工提示等）；AI 回复异步生成，通过 WebSocket 或 <code>GET messages</code> 获取。<code>client_msg_id</code> 为幂等键，重试不会产生重复消息。</p>
  </section>
  <section class="panel">
    <h2>5. 实时接收回复（WebSocket）</h2>
    <p>对每个进行中的会话建立一条订阅连接：</p>
    <pre><code>GET ${esc(docs.wsRoot)}/open/v1/ws?conversation_id={会话ID}&amp;key=ast_live_xxx</code></pre>
    <h3>推送事件</h3>
    <div class="table-wrap"><table><thead><tr><th>事件</th><th>说明</th></tr></thead><tbody>
      ${docs.wsEvents.map(([name, note]) => `<tr><td><code>${esc(name)}</code></td><td>${esc(note)}</td></tr>`).join('')}
    </tbody></table></div>
    <p>消息事件中的 <code>content</code> 已按会话语言本地化，可直接渲染到接入平台的聊天界面。</p>
    <pre><code>const ws = new WebSocket('${esc(docs.wsRoot)}/open/v1/ws?conversation_id=' + conversationId + '&amp;key=' + apiKey);
ws.onmessage = (event) =&gt; {
  const { event: name, data } = JSON.parse(event.data);
  if (name === 'message.receive') {
    renderMessage(data); // data.sender_type: ai / agent / system
  }
};
setInterval(() =&gt; ws.readyState === 1 &amp;&amp; ws.send('{"event":"ping"}'), 30000);</code></pre>
    <p class="tip">该连接为只读订阅：发送消息请使用 POST /open/v1/conversations/{id}/messages。</p>
  </section>
  <section class="panel">
    <h2>6. 结束会话与常见问题</h2>
    <pre><code>curl -X POST '${esc(docs.root)}/open/v1/conversations/{会话ID}/close' \\
  -H 'Authorization: Bearer ast_live_xxx'</code></pre>
    <p>结束后会话只读，仍可拉取历史；再次咨询重新创建会话即可。</p>
    <div class="table-wrap"><table><thead><tr><th>现象</th><th>处理</th></tr></thead><tbody>
      ${docs.faq.map(([q, a]) => `<tr><td><strong>${esc(q)}</strong></td><td>${esc(a)}</td></tr>`).join('')}
    </tbody></table></div>
  </section>`;
}

function buildDocsText(docs) {
  return [
    'ASTREVA 客服引擎 · 第三方平台 API 接入文档',
    '',
    `服务地址：${docs.root}`,
    '鉴权：请求头 Authorization: Bearer 你的API_KEY（或 X-API-Key；WebSocket 可用 query 参数 key）',
    'Key 在后台「API 管理」新增授权时生成，只显示一次；泄露可随时重置。',
    '',
    '接口一览：',
    ...docs.endpoints.map(([method, path, note]) => `  ${method} ${path} — ${note}`),
    '',
    'WebSocket 事件：',
    ...docs.wsEvents.map(([name, note]) => `  ${name} — ${note}`),
    '',
    '创建会话示例：',
    `curl -X POST '${docs.root}/open/v1/conversations' \\`,
    `  -H 'Authorization: Bearer ast_live_xxx' \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d '{"external_user_id": "user-1001", "lang": "en"}'`,
    '',
    '发送消息示例：',
    `curl -X POST '${docs.root}/open/v1/conversations/{会话ID}/messages' \\`,
    `  -H 'Authorization: Bearer ast_live_xxx' \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d '{"content": "Hello, I want a quote", "client_msg_id": "msg-0001"}'`,
    '',
    '实时订阅：',
    `  GET ${docs.wsRoot}/open/v1/ws?conversation_id={会话ID}&key=ast_live_xxx（只读）`,
    '',
    '常见问题：',
    ...docs.faq.map(([q, a]) => `  ${q} — ${a}`)
  ].join('\n');
}

async function resolveApiBaseUrl() {
  try {
    const response = await fetch('/site-admin-api/public-content?locale=zh-CN');
    const payload = await response.json();
    return String(payload?.data?.site?.serviceBridgeUrl || '').replace(/\/+$/, '');
  } catch {
    return '';
  }
}

async function init() {
  const root = document.getElementById('docsRoot');
  const base = await resolveApiBaseUrl();
  const docs = apiDocsData(base);
  root.innerHTML = buildDocsHtml(docs);
  document.getElementById('copyDocs').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(buildDocsText(docs));
      notify('接入文档已复制，可直接发给接入方');
    } catch (error) {
      notify(`复制失败：${error.message}`, true);
    }
  });
}

init();
