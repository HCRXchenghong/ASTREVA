const proxyRoot = '/site-admin-api/servicebridge';
const titles = {
  content: ['内容管理', '官网页面、产品、媒体与 SEO'],
  translations: ['网站翻译', '中文为唯一源内容，自动生成英语与阿拉伯语'],
  media: ['R2 素材库', '图片、视频、文档、多尺寸变体与 AI 生图'],
  customers: ['客服管理', '客服账号、密码与接待容量'],
  workspace: ['工作台', '会话运行状态与服务质量概览'],
  settings: ['系统设置', '营业时间、欢迎语、AI 与运行状态'],
  keywords: ['关键词', '关键词自动回复与快捷回复规则'],
  audit: ['审计日志', '管理员与客服的敏感操作记录'],
  releases: ['App 版本', '安装包上传、强制更新与发布状态']
};
const appState = { agents: [], conversations: [], activeView: 'workspace' };

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
const esc = (value = '') => String(value).replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[char]);
const statusLabel = (value) => ({ online: '在线', busy: '忙碌', offline: '离线', waiting: '待接入', ai_serving: 'AI 接待', human_requested: '等待人工', assigned: '人工服务', closed: '已结束' })[value] || value || '-';
const localTime = (value) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-';
const bytesLabel = (value) => {
  const size = Number(value || 0);
  if (size >= 1024 * 1024 * 1024) return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${size} B`;
};
const ratingColor = (score) => {
  const value = Number(score || 0);
  if (value >= 5) return '#16835c';
  if (value >= 3) return '#b97708';
  return '#b42318';
};
const percentLabel = (value) => value == null ? '-' : `${Math.round(Number(value) * 100)}%`;
const ratingLevel = (score) => {
  const value = Number(score || 0);
  if (value >= 4) return 'good';
  if (value === 3) return 'neutral';
  return 'bad';
};

async function requestJson(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { ...(options.body && !(options.body instanceof FormData) ? { 'Content-Type': 'application/json' } : {}), ...(options.headers || {}) }
  });
  const text = await response.text();
  let payload = {};
  try { payload = text ? JSON.parse(text) : {}; } catch { payload = { message: text }; }
  if (response.status === 401) {
    location.href = '/admin-2fa?returnTo=' + encodeURIComponent(location.pathname + location.search);
    throw new Error('登录已失效，正在返回登录页。');
  }
  if (!response.ok) {
    const message = typeof payload.error === 'string' ? payload.message || payload.error : payload.error?.message || payload.message;
    throw new Error(message || `请求失败（HTTP ${response.status}）`);
  }
  return payload;
}
const api = (path, options = {}) => requestJson(`${proxyRoot}${path}`, options);
const siteApi = (path, options = {}) => requestJson(path, options);

let noticeTimer;
function notify(message, isError = false) {
  const notice = $('#notice');
  notice.textContent = message;
  notice.className = `notice show${isError ? ' error' : ''}`;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { notice.className = 'notice'; }, 3500);
}

function setView(view, updateUrl = true) {
  if (!titles[view]) view = 'workspace';
  appState.activeView = view;
  $$('.nav [data-view]').forEach((button) => button.classList.toggle('active', button.dataset.view === view));
  $$('.view').forEach((panel) => panel.classList.toggle('active', panel.id === `view-${view}`));
  $('#pageTitle').textContent = titles[view][0];
  $('#pageSubtitle').textContent = titles[view][1];
  $('#contentRoot').classList.toggle('content-mode', ['content', 'translations', 'media', 'settings'].includes(view));
  $('#contentRoot').scrollTop = 0;
  if (updateUrl) {
    const url = new URL(location.href);
    url.searchParams.set('view', view);
    history.replaceState({ view }, '', url);
  }
  refreshCurrent().catch((error) => notify(error.message, true));
}

async function refreshEngine() {
  try {
    await api('/api/admin/system-status');
    $('#engineState').textContent = '客服引擎在线';
    $('#engineState').classList.remove('down');
  } catch {
    $('#engineState').textContent = '客服引擎离线';
    $('#engineState').classList.add('down');
  }
}

async function loadAgents() {
  const [payload, ratingPayload] = await Promise.all([
    api('/api/admin/agents'),
    api('/api/admin/ratings/summaries')
  ]);
  appState.agents = payload.agents || [];
  appState.ratingSummaries = Object.fromEntries((ratingPayload.items || []).map((item) => [item.agent_id, item]));
  $('#agentCount').textContent = `${appState.agents.length} 个账号`;
  $('#agentRows').innerHTML = appState.agents.length ? appState.agents.map((agent) => `
    <tr>
      <td><strong>${esc(agent.account)}</strong><br><span>${esc(agent.name)}</span></td>
      <td>${esc(agent.group || '-')}</td>
      <td><span class="badge ${agent.disabled_at ? 'disabled' : esc(agent.status)}">${agent.disabled_at ? '已禁用' : statusLabel(agent.status)}</span></td>
      <td>${Number(agent.current_conversations || 0)}</td>
      <td>${Number(agent.max_conversations || 0)}</td>
      <td>${renderAgentRatingCell(appState.ratingSummaries[agent.id], false)}</td>
      <td>${renderAgentRatingCell(appState.ratingSummaries[agent.id], true)}</td>
      <td><div class="row-actions"><button data-edit-agent="${esc(agent.id)}">编辑</button><button data-agent-ratings="${esc(agent.id)}">详情</button><button data-set-rating="${esc(agent.id)}">设置好评率</button>${appState.ratingSummaries[agent.id]?.manual_satisfaction_rate != null ? `<button data-clear-rating="${esc(agent.id)}">清除手动</button>` : ''}<button data-reset-ratings="${esc(agent.id)}">重置评价</button><button data-reset-agent="${esc(agent.id)}">重置密码</button><button data-disable-agent="${esc(agent.id)}">禁用</button><button data-delete-agent="${esc(agent.id)}">删除</button></div></td>
    </tr>`).join('') : '<tr><td colspan="8" class="empty">暂无客服账号</td></tr>';
}

function renderAgentRatingCell(item, display) {
  const summary = item?.summary || {};
  const rate = display ? item?.display_satisfaction_rate : summary.satisfaction_rate;
  const count = Number(summary.total || 0);
  const manual = display && item?.manual_satisfaction_rate != null;
  return `<strong>${percentLabel(rate)}</strong>${count ? ` <small>(${count}条)</small>` : ''}${manual ? ' <span class="badge busy">手动</span>' : ''}`;
}

async function loadWorkspace() {
  const [dashboard, conversations, agents] = await Promise.all([
    api('/api/admin/dashboard'),
    api('/api/admin/conversations'),
    api('/api/admin/agents')
  ]);
  appState.agents = agents.agents || [];
  appState.conversations = conversations.conversations || [];
  const statItems = [
    ['当前会话', dashboard.active_conversations, '#0878dc'],
    ['等待人工', dashboard.human_requested + dashboard.waiting, '#b97708'],
    ['在线客服', dashboard.online_agents, '#16835c'],
    ['满意度', `${Number(dashboard.rating?.satisfaction_rate || 0).toFixed(0)}%`, '#18a5b8']
  ];
  $('#stats').innerHTML = statItems.map(([label, value, color]) => `<article class="stat" style="--accent:${color}"><small>${label}</small><strong>${value}</strong></article>`).join('');
  $('#conversationCount').textContent = `${appState.conversations.length} 条`;
  if (appState.selectedConversationID && !appState.conversations.some((item) => item.id === appState.selectedConversationID)) appState.selectedConversationID = '';
  if (!appState.selectedConversationID && appState.conversations.length) appState.selectedConversationID = appState.conversations[0].id;
  renderWorkspaceConversationList();
  if (appState.selectedConversationID) await selectWorkspaceConversation(appState.selectedConversationID, false);
  else clearWorkspaceConversation();
}

function conversationName(conversation) {
  return conversation?.visitor_remark || conversation?.visitor_ip || conversation?.id || '未知访客';
}

function visitorIPSourceLabel(source) {
  switch (source) {
    case 'cloudflare': return 'Cloudflare 校验';
    case 'xff': return '代理转发';
    case 'x-real-ip': return 'X-Real-IP';
    case 'direct': return '直连';
    default: return '历史数据';
  }
}

function visitorIPLabel(conversation) {
  const ip = conversation?.visitor_ip;
  if (!ip) return '-';
  return ip + '（' + visitorIPSourceLabel(conversation?.visitor_ip_source) + '）';
}

function renderWorkspaceConversationList() {
  const query = String($('#workspaceConversationSearch')?.value || '').trim().toLowerCase();
  const filtered = appState.conversations.filter((conversation) => {
    const agent = appState.agents.find((item) => item.id === conversation.assigned_agent_id);
    return [conversationName(conversation), conversation.source, conversation.last_message, agent?.name, agent?.account, statusLabel(conversation.status)].join(' ').toLowerCase().includes(query);
  });
  $('#conversationCount').textContent = `${filtered.length} / ${appState.conversations.length} 条`;
  $('#workspaceConversationList').innerHTML = filtered.length ? filtered.map((conversation) => {
    const name = conversationName(conversation);
    const unread = Number(conversation.unread_for_agent || 0);
    return `<button class="conversation-item${conversation.id === appState.selectedConversationID ? ' active' : ''}" data-workspace-conversation="${esc(conversation.id)}"><span class="conversation-avatar">${esc(name.slice(0, 1).toUpperCase())}</span><span class="conversation-copy"><strong>${esc(name)}</strong><small>${esc(conversation.last_message || statusLabel(conversation.status))}</small></span><span class="conversation-meta"><time>${esc(new Date(conversation.updated_at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }))}</time>${unread ? `<span class="unread-dot">${unread > 99 ? '99+' : unread}</span>` : `<span class="badge ${esc(conversation.status)}">${statusLabel(conversation.status)}</span>`}</span></button>`;
  }).join('') : '<div class="empty">没有匹配的会话</div>';
}

function clearWorkspaceConversation() {
  $('#workspaceChatTitle').textContent = '请选择一条会话';
  $('#workspaceChatActions').innerHTML = '';
  $('#workspaceMessageList').innerHTML = '<div class="empty">从左侧选择会话查看消息</div>';
  $('#workspaceVisitorFacts').innerHTML = '<div><dt>提示</dt><dd>选择会话后显示访客与接待信息</dd></div>';
}

async function selectWorkspaceConversation(id, updateList = true) {
  const conversation = appState.conversations.find((item) => item.id === id);
  if (!conversation) return clearWorkspaceConversation();
  appState.selectedConversationID = id;
  if (updateList) renderWorkspaceConversationList();
  const agent = appState.agents.find((item) => item.id === conversation.assigned_agent_id);
  $('#workspaceChatTitle').textContent = conversationName(conversation);
  $('#workspaceChatActions').innerHTML = conversation.status === 'closed' ? '<span class="badge closed">已结束</span>' : `<button class="btn" data-transfer="${esc(id)}">转接</button><button class="btn danger" data-close-conversation="${esc(id)}">结束</button>`;
  $('#workspaceVisitorFacts').innerHTML = [
    ['访客', conversationName(conversation)],
    ['状态', statusLabel(conversation.status)],
    ['接待客服', agent ? `${agent.name}（${agent.account}）` : '未分配'],
    ['客服分组', agent?.group || '未分组'],
    ['来源', conversation.source || '-'],
    ['访客 IP', visitorIPLabel(conversation)],
    ['会话 ID', conversation.id],
    ['创建时间', localTime(conversation.created_at)],
    ['更新时间', localTime(conversation.updated_at)]
  ].map(([label, value]) => `<div><dt>${esc(label)}</dt><dd>${esc(value)}</dd></div>`).join('');
  const payload = await api(`/api/conversations/${encodeURIComponent(id)}/messages?limit=100`);
  if (appState.selectedConversationID !== id) return;
  const messages = payload.messages || [];
  const list = $('#workspaceMessageList');
  list.innerHTML = messages.length ? messages.map((message) => {
    const sender = ['agent', 'ai', 'system'].includes(message.sender_type) ? message.sender_type : 'visitor';
    const senderName = ({ visitor: '访客', agent: '人工客服', ai: 'AI', system: '系统' })[sender];
    // 默认展示中文译文，原文可通过「查看原文」展开。
    const original = String(message.original_content || '').trim();
    const hasOriginal = original && original !== String(message.content || '').trim();
    const originalBlock = hasOriginal
      ? `<div class="message-original"><button type="button" class="view-original-btn">查看原文</button><div class="original-text" hidden>${esc(original)}</div></div>`
      : '';
    return `<article class="workspace-message ${sender}"><div class="bubble">${esc(message.content || '')}${originalBlock}</div><small>${senderName} · ${localTime(message.created_at)}</small></article>`;
  }).join('') : '<div class="empty">暂无消息</div>';
  list.querySelectorAll('.view-original-btn').forEach((button) => {
    button.addEventListener('click', () => {
      const box = button.nextElementSibling;
      if (!box) return;
      box.hidden = !box.hidden;
      button.textContent = box.hidden ? '查看原文' : '收起原文';
    });
  });
  list.scrollTop = list.scrollHeight;
}

async function loadKeywords() {
  const payload = await api('/api/admin/keyword-rules');
  appState.keywords = payload.rules || [];
  $('#keywordCount').textContent = `${appState.keywords.length} 条`;
  $('#keywordRows').innerHTML = appState.keywords.length ? appState.keywords.map((rule) => `
    <tr>
      <td><strong>${esc(rule.keyword)}</strong></td>
      <td>${esc(rule.match_type || '-')}</td>
      <td>${esc(rule.reply || '-')}</td>
      <td><span class="badge ${rule.enabled ? 'online' : 'offline'}">${rule.enabled ? '启用' : '停用'}</span></td>
      <td>${Number(rule.priority || 0)}</td>
      <td>${rule.show_in_quick_replies ? esc(rule.quick_reply_text || '是') : '否'}</td>
      <td><div class="row-actions"><button data-edit-keyword="${esc(rule.id)}">编辑</button><button data-delete-keyword="${esc(rule.id)}">删除</button></div></td>
    </tr>`).join('') : '<tr><td colspan="7" class="empty">暂无关键词规则</td></tr>';
}

async function loadAudit() {
  const [bridgeResult, siteResult] = await Promise.allSettled([
    api('/api/admin/audit-events?limit=200'),
    siteApi('/site-admin-api/audit-events?limit=500')
  ]);
  if (bridgeResult.status === 'rejected' && siteResult.status === 'rejected') throw bridgeResult.reason;
  const bridgeEvents = bridgeResult.status === 'fulfilled' ? bridgeResult.value.events || [] : [];
  const siteEvents = siteResult.status === 'fulfilled' ? siteResult.value.data || [] : [];
  appState.audits = [
    ...bridgeEvents.map((event) => normalizeAudit(event, 'servicebridge')),
    ...siteEvents.map((event) => normalizeAudit(event, 'site-admin'))
  ].sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt));
  renderAudit();
}

function auditRisk(action = '') {
  const value = String(action).toLowerCase();
  if (/(delete|remove|password|disable|reset|secret|key|credential|login\.failed)/.test(value)) return 'high';
  if (/(update|patch|save|transfer|publish|create|upload|close|activate|setting)/.test(value)) return 'medium';
  return 'low';
}

function auditActionLabel(action = '') {
  const labels = {
    'conversation.transfer': '转接会话', 'conversation.close': '结束会话', 'conversation.delete': '删除会话',
    'agent.create': '创建客服', 'agent.update': '更新客服', 'agent.disable': '禁用客服', 'agent.delete': '删除客服',
    'agent.password_reset': '重置客服密码', 'ai_settings.update': '更新客服 AI 配置', 'ai_settings.models_sync': '获取客服 AI 模型',
    'admin.login.succeeded': '管理员登录成功', 'admin.login.failed': '管理员登录失败',
    'agent.login.succeeded': '客服登录成功', 'agent.login.failed': '客服登录失败', 'account.password_change': '修改登录密码',
    'contact_settings.update': '更新联系方式', 'business_hours.update': '更新营业时间', 'keyword_rule.create': '创建关键词规则',
    'keyword_rule.update': '更新关键词规则', 'keyword_rule.delete': '删除关键词规则', 'publish.requested': '提交官网发布',
    'content.saved': '保存官网内容', 'translation.settings.updated': '更新网站翻译配置', 'translation.requested': '提交网站重译任务',
    'media.settings.updated': '更新媒体配置', 'system.media-settings.saved': '保存媒体系统配置'
  };
  return labels[action] || action || '未知操作';
}

function normalizeAudit(event, source) {
  const site = source === 'site-admin';
  const actorKind = site ? (event.actorEmail || event.actorId ? 'admin' : 'system') : event.actor_kind || 'system';
  return {
    id: String(event.id || `${source}-${Math.random()}`), source, actorKind,
    actorId: String(site ? event.actorId || '' : event.actor_id || ''),
    actorEmail: String(site ? event.actorEmail || '' : ''), action: String(event.action || ''),
    resource: String(event.resource || ''), resourceId: String(site ? event.resourceId || '' : event.resource_id || ''),
    ip: String(site ? event.ip || '' : event.ip_address || ''), userAgent: String(site ? event.userAgent || '' : event.user_agent || ''),
    description: String(site ? '' : event.description || ''), detail: site ? event.detail || {} : {},
    createdAt: site ? event.createdAt : event.created_at, risk: auditRisk(event.action)
  };
}

function filteredAudits() {
  const query = String($('#auditSearch')?.value || '').trim().toLowerCase();
  const source = $('#auditSource')?.value || '';
  const actor = $('#auditActor')?.value || '';
  const risk = $('#auditRisk')?.value || '';
  return (appState.audits || []).filter((event) => {
    const text = [event.action, auditActionLabel(event.action), event.resource, event.resourceId, event.actorKind, event.actorId, event.actorEmail, event.ip, event.description, JSON.stringify(event.detail)].join(' ').toLowerCase();
    return (!query || text.includes(query)) && (!source || event.source === source) && (!actor || event.actorKind === actor) && (!risk || event.risk === risk);
  });
}

function renderAudit() {
  const rows = filteredAudits();
  const stats = [
    ['当前结果', rows.length, '#0878dc'], ['高风险', rows.filter((event) => event.risk === 'high').length, '#b42318'],
    ['管理员操作', rows.filter((event) => event.actorKind === 'admin').length, '#7656c4'], ['客服操作', rows.filter((event) => event.actorKind === 'agent').length, '#16835c']
  ];
  $('#auditStats').innerHTML = stats.map(([label, value, color]) => `<article class="stat" style="--accent:${color}"><small>${label}</small><strong>${value}</strong></article>`).join('');
  $('#auditCount').textContent = `${rows.length} / ${(appState.audits || []).length} 条`;
  $('#auditRows').innerHTML = rows.length ? rows.map((event) => `<tr class="audit-row" data-audit-id="${esc(event.id)}" data-audit-source="${esc(event.source)}"><td>${localTime(event.createdAt)}</td><td>${event.source === 'servicebridge' ? '客服系统' : '官网后台'}</td><td><span class="risk ${event.risk}">${({ high: '高', medium: '中', low: '低' })[event.risk]}</span></td><td>${esc(event.actorEmail || `${event.actorKind} / ${event.actorId || '-'}`)}</td><td><strong>${esc(auditActionLabel(event.action))}</strong><br><small>${esc(event.action)}</small></td><td>${esc(event.resource || '-')}${event.resourceId ? `<br><small>${esc(event.resourceId)}</small>` : ''}</td><td>${esc(event.ip || '-')}</td></tr>`).join('') : '<tr><td colspan="7" class="empty">暂无匹配的审计记录</td></tr>';
}

function openAuditDetail(id, source) {
  const event = (appState.audits || []).find((item) => item.id === id && item.source === source);
  if (!event) return;
  $('#auditDialogTitle').textContent = auditActionLabel(event.action);
  const values = [
    ['ID', event.id], ['来源', event.source === 'servicebridge' ? '客服系统' : '官网后台'], ['风险等级', ({ high: '高风险', medium: '中风险', low: '低风险' })[event.risk]],
    ['操作者类型', event.actorKind], ['操作者 ID', event.actorId || '-'], ['操作者账号', event.actorEmail || '-'], ['动作', event.action],
    ['资源', event.resource || '-'], ['资源 ID', event.resourceId || '-'], ['IP', event.ip || '-'], ['User-Agent', event.userAgent || '-'],
    ['说明', event.description || '-'], ['详情', JSON.stringify(event.detail || {}, null, 2)], ['时间', localTime(event.createdAt)]
  ];
  $('#auditDetail').innerHTML = values.map(([key, value]) => `<dt>${esc(key)}</dt><dd>${esc(value)}</dd>`).join('');
  $('#auditDialog').showModal();
}

async function loadReleases() {
  const payload = await api('/api/admin/app-releases');
  appState.releases = payload.releases || [];
  $('#releaseCount').textContent = `${appState.releases.length} 个版本`;
  $('#releaseRows').innerHTML = appState.releases.length ? appState.releases.map((release) => `
    <tr>
      <td><strong>${esc(release.version_name)}</strong><br><small>code ${Number(release.version_code)}</small></td>
      <td>${release.platform === 'ios' ? 'iOS' : 'Android'}</td>
      <td>${bytesLabel(release.file_size)}</td>
      <td><span class="badge ${release.force_update ? 'disabled' : 'online'}">${release.force_update ? '强制更新' : '可选更新'}</span></td>
      <td><span class="badge ${release.enabled ? 'online' : 'offline'}">${release.enabled ? '已发布' : '已停用'}</span></td>
      <td>${localTime(release.created_at)}</td>
      <td><div class="row-actions"><button data-toggle-release="${esc(release.id)}" data-enabled="${release.enabled}">${release.enabled ? '停用' : '启用'}</button><button data-force-release="${esc(release.id)}" data-force="${release.force_update}">${release.force_update ? '改为可选' : '设为强制'}</button><button data-delete-release="${esc(release.id)}">删除</button></div></td>
    </tr>`).join('') : '<tr><td colspan="7" class="empty">暂无版本，请先上传安装包</td></tr>';
}

function fillForm(form, data) {
  Object.entries(data || {}).forEach(([key, value]) => {
    const field = form.elements.namedItem(key);
    if (!field) return;
    if (field.type === 'checkbox') { field.checked = Boolean(value); return; }
    field.value = typeof value === 'boolean' ? String(value) : value ?? '';
  });
}

function formJson(form) {
  return Object.fromEntries([...new FormData(form).entries()].map(([key, value]) => [key, value === 'true' ? true : value === 'false' ? false : value]));
}

async function refreshCurrent() {
  await refreshEngine();
  if (appState.activeView === 'customers') await loadAgents();
  if (appState.activeView === 'workspace') await loadWorkspace();
  if (appState.activeView === 'keywords') await loadKeywords();
  if (appState.activeView === 'audit') await loadAudit();
  if (appState.activeView === 'releases') await loadReleases();
}

function openAgent(agent) {
  const form = $('#agentForm');
  form.reset();
  const selectedGroup = agent?.group || '';
  const groups = [...new Set(appState.agents.map((item) => String(item.group || '').trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'zh-CN'));
  if (selectedGroup && !groups.includes(selectedGroup)) groups.push(selectedGroup);
  $('#agentGroupSelect').innerHTML = `<option value="">未分组</option>${groups.map((group) => `<option value="${esc(group)}">${esc(group)}</option>`).join('')}`;
  fillForm(form, agent || { max_conversations: 10 });
  form.elements.account.disabled = Boolean(agent);
  $('#passwordField').style.display = agent ? 'none' : 'grid';
  $('#agentDialogTitle').textContent = agent ? '编辑客服账号' : '新增客服账号';
  $('#agentDialog').showModal();
}

async function openAgentRatings(agentID) {
  const payload = await api(`/api/admin/agents/${encodeURIComponent(agentID)}/ratings`);
  const cachedAgent = appState.agents.find((item) => item.id === agentID) || {};
  const agent = { ...cachedAgent, ...(payload.agent || {}) };
  const summary = payload.summary || {};
  const ratings = payload.ratings || [];
  const agentName = agent.name || agent.account || agentID;
  const agentStatus = agent.disabled_at ? '已禁用' : statusLabel(agent.status);
  const agentStatusClass = agent.disabled_at ? 'disabled' : esc(agent.status || 'offline');
  $('#agentRatingDialogTitle').textContent = `客服详情 · ${agentName}`;
  $('#agentDetailFacts').innerHTML = `
    <div class="agent-detail-fact"><dt>登录账号</dt><dd>${esc(agent.account || '-')}</dd></div>
    <div class="agent-detail-fact"><dt>客服姓名</dt><dd>${esc(agent.name || '-')}</dd></div>
    <div class="agent-detail-fact"><dt>所属分组</dt><dd>${esc(agent.group || '未分组')}</dd></div>
    <div class="agent-detail-fact"><dt>当前状态</dt><dd><span class="badge ${agentStatusClass}">${esc(agentStatus)}</span></dd></div>
    <div class="agent-detail-fact"><dt>当前会话数</dt><dd>${Number(agent.current_conversations || 0)}</dd></div>
    <div class="agent-detail-fact"><dt>最大并发数</dt><dd>${Number(agent.max_conversations || 0)}</dd></div>`;
  $('#agentRatingSummary').innerHTML = `
    <article class="stat" style="--accent:#16835c"><small>真实好评率</small><strong>${percentLabel(summary.satisfaction_rate)}</strong></article>
    <article class="stat" style="--accent:#0878dc"><small>展示好评率</small><strong>${percentLabel(summary.display_satisfaction_rate)}</strong></article>
    <article class="stat" style="--accent:#18a5b8"><small>评价总数</small><strong>${Number(summary.total || 0)}</strong></article>
    <article class="stat" style="--accent:#b97708"><small>中评 / 差评</small><strong>${Number(summary.neutral || 0)} / ${Number(summary.unsatisfied || 0)}</strong></article>`;
  $('#agentRatingRecordCount').textContent = `${ratings.length} 条`;
  $('#agentRatingRowsDialog').innerHTML = ratings.length ? ratings.map((rating) => {
    const level = ratingLevel(rating.score);
    const comment = String(rating.comment || '');
    const hasComment = Boolean(comment.trim());
    return `<tr class="rating-${level}">
      <td><span class="rating-conversation">${esc(rating.conversation_id || '-')}</span></td>
      <td><strong style="color:${ratingColor(rating.score)}">${'★'.repeat(Number(rating.score || 0))}${'☆'.repeat(5 - Number(rating.score || 0))}</strong></td>
      <td>${esc((rating.tags || []).join('、') || '未选择标签')}</td>
      <td><div class="rating-comment${level === 'good' ? '' : ' negative'}${hasComment ? '' : ' empty'}">${hasComment ? esc(comment) : '访客未填写文字评价'}</div></td>
      <td>${localTime(rating.created_at)}</td>
    </tr>`;
  }).join('') : '<tr><td colspan="5" class="empty">该客服暂无评价</td></tr>';
  $('#agentRatingsDialog').showModal();
}

function openRatingDialog(agentID) {
  const agent = appState.agents.find((item) => item.id === agentID);
  const item = appState.ratingSummaries[agentID] || {};
  const form = $('#ratingForm');
  form.reset();
  form.elements.id.value = agentID;
  form.elements.satisfaction_rate.value = item.display_satisfaction_rate != null ? Math.round(Number(item.display_satisfaction_rate) * 100) : '';
  $('#ratingDialogTitle').textContent = `设置好评率 · ${agent?.name || agentID}`;
  $('#ratingRealInfo').textContent = `真实好评率：${percentLabel(item.summary?.satisfaction_rate)}（${Number(item.summary?.total || 0)} 条评价）`;
  $('#ratingDialog').showModal();
}

$('#ratingForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const agentID = form.elements.id.value;
  const rate = Number(form.elements.satisfaction_rate.value);
  if (!Number.isFinite(rate) || rate < 0 || rate > 100) {
    notify('好评率必须是 0-100 之间的数字', true);
    return;
  }
  await api(`/api/admin/agents/${encodeURIComponent(agentID)}/rating`, { method: 'POST', body: JSON.stringify({ satisfaction_rate: rate }) });
  form.reset();
  $('#ratingDialog').close();
  await loadAgents();
  notify('好评率已设置');
});

function openKeyword(rule) {
  const form = $('#keywordForm');
  form.reset();
  fillForm(form, rule || { match_type: 'contains', action: 'text', priority: 50, enabled: true });
  $('#keywordDialogTitle').textContent = rule ? '编辑关键词规则' : '新增关键词规则';
  $('#keywordDialog').showModal();
}

$$('.nav [data-view]').forEach((button) => button.addEventListener('click', () => setView(button.dataset.view)));
$$('[data-close]').forEach((button) => button.addEventListener('click', () => button.closest('dialog').close()));
$('#refresh').addEventListener('click', () => {
  if (appState.activeView === 'translations') {
    $('#view-translations iframe').contentWindow.location.reload();
    notify('网站翻译状态正在重新读取');
    return;
  }
  if (appState.activeView === 'settings') {
    $('#view-settings iframe').contentWindow.location.reload();
    notify('系统设置正在重新读取');
    return;
  }
  refreshCurrent().then(() => notify('已刷新')).catch((error) => notify(error.message, true));
});
$('#logout').addEventListener('click', async () => { await fetch('/site-admin-auth/logout', { method: 'POST' }); location.href = '/admin-2fa'; });
$('#createAgent').addEventListener('click', () => openAgent(null));
$('#addAgentGroup').addEventListener('click', () => {
  const value = String(prompt('请输入新分组名称：', '') || '').trim();
  if (!value) return;
  const select = $('#agentGroupSelect');
  let option = [...select.options].find((item) => item.value === value);
  if (!option) {
    option = document.createElement('option');
    option.value = value;
    option.textContent = value;
    select.append(option);
  }
  select.value = value;
});
$('#createKeyword').addEventListener('click', () => openKeyword(null));
$('#exportConversations').addEventListener('click', () => { location.href = `${proxyRoot}/api/admin/conversations/export.csv`; });
$('#exportAudit').addEventListener('click', () => {
  const header = ['时间', '来源', '风险', '操作者类型', '操作者ID', '操作者账号', '动作', '资源', '资源ID', 'IP', 'User-Agent', '说明', '详情'];
  const csvCell = (value) => `"${String(value ?? '').replace(/"/g, '""')}"`;
  const rows = filteredAudits().map((event) => [event.createdAt, event.source, event.risk, event.actorKind, event.actorId, event.actorEmail, event.action, event.resource, event.resourceId, event.ip, event.userAgent, event.description, JSON.stringify(event.detail || {})]);
  const blob = new Blob(['\uFEFF', [header, ...rows].map((row) => row.map(csvCell).join(',')).join('\r\n')], { type: 'text/csv;charset=utf-8' });
  const link = document.createElement('a');
  link.href = URL.createObjectURL(blob);
  link.download = `audit-events-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  setTimeout(() => URL.revokeObjectURL(link.href), 1000);
});
$('#workspaceConversationSearch').addEventListener('input', renderWorkspaceConversationList);
['auditSearch', 'auditSource', 'auditActor', 'auditRisk'].forEach((id) => $(`#${id}`).addEventListener(id === 'auditSearch' ? 'input' : 'change', renderAudit));
$('#auditRows').addEventListener('click', (event) => {
  const row = event.target.closest('[data-audit-id]');
  if (row) openAuditDetail(row.dataset.auditId, row.dataset.auditSource);
});

$('#agentForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const data = formJson(form);
  data.max_conversations = Number(data.max_conversations);
  const id = data.id;
  delete data.id;
  if (id) {
    delete data.account;
    delete data.password;
    const current = appState.agents.find((agent) => agent.id === id);
    data.status = current?.status || 'offline';
    await api(`/api/admin/agents/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(data) });
  } else {
    await api('/api/admin/agents', { method: 'POST', body: JSON.stringify(data) });
  }
  $('#agentDialog').close();
  await loadAgents();
  notify(id ? '客服资料已更新' : '客服账号已创建');
});

$('#keywordForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const data = formJson(form);
  data.priority = Number(data.priority || 0);
  const id = data.id;
  delete data.id;
  if (id) {
    await api(`/api/admin/keyword-rules/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(data) });
  } else {
    await api('/api/admin/keyword-rules', { method: 'POST', body: JSON.stringify(data) });
  }
  $('#keywordDialog').close();
  await loadKeywords();
  notify(id ? '关键词规则已更新' : '关键词规则已创建');
});

$('#releaseForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const submitButton = form.querySelector('button[type="submit"], .form-actions .primary');
  if (submitButton) submitButton.disabled = true;
  try {
    const formData = new FormData(form);
    formData.delete('file');
    formData.append('file', form.elements.file.files[0]);
    await api('/api/admin/app-releases', { method: 'POST', body: formData });
    form.reset();
    await loadReleases();
    notify('新版本已上传并发布');
  } catch (error) {
    notify(error.message, true);
  } finally {
    if (submitButton) submitButton.disabled = false;
  }
});

document.addEventListener('click', async (event) => {
  const button = event.target.closest('button');
  if (!button) return;
  try {
    if (button.dataset.agentRatings) {
      await openAgentRatings(button.dataset.agentRatings);
      return;
    }
    if (button.dataset.setRating) {
      openRatingDialog(button.dataset.setRating);
      return;
    }
    if (button.dataset.clearRating && confirm('确定清除该客服的手动好评率？将恢复显示真实好评率。')) {
      const agentID = button.dataset.clearRating;
      await api(`/api/admin/agents/${encodeURIComponent(agentID)}/rating`, { method: 'DELETE' });
      await loadAgents();
      notify('手动好评率已清除');
      return;
    }
    if (button.dataset.resetRatings && confirm('确定重置该客服的真实评价数据？所有评价记录和手动好评率都会清空，不可恢复。')) {
      const agentID = button.dataset.resetRatings;
      await api(`/api/admin/agents/${encodeURIComponent(agentID)}/ratings/reset`, { method: 'POST', body: '{}' });
      await loadAgents();
      notify('真实评价已重置');
      return;
    }
    if (button.dataset.editAgent) openAgent(appState.agents.find((agent) => agent.id === button.dataset.editAgent));
    if (button.dataset.resetAgent) {
      const password = prompt('请输入新的临时密码（至少 10 位），留空则自动生成：', '');
      if (password === null) return;
      const payload = await api(`/api/admin/agents/${encodeURIComponent(button.dataset.resetAgent)}/reset-password`, { method: 'POST', body: JSON.stringify({ password }) });
      notify(payload.temporary_password ? `临时密码：${payload.temporary_password}` : '密码已重置，请通知客服重新登录');
      await loadAgents();
    }
    if (button.dataset.disableAgent && confirm('确定禁用该客服账号？当前登录会立即失效。')) {
      await api(`/api/admin/agents/${encodeURIComponent(button.dataset.disableAgent)}/disable`, { method: 'POST', body: '{}' });
      await loadAgents(); notify('客服账号已禁用');
    }
    if (button.dataset.deleteAgent && confirm('确定永久删除该客服账号？')) {
      await api(`/api/admin/agents/${encodeURIComponent(button.dataset.deleteAgent)}`, { method: 'DELETE' });
      await loadAgents(); notify('客服账号已删除');
    }
    if (button.dataset.workspaceConversation) await selectWorkspaceConversation(button.dataset.workspaceConversation);
    if (button.dataset.closeConversation && confirm('确定结束该会话？')) {
      await api(`/api/conversations/${encodeURIComponent(button.dataset.closeConversation)}/close`, { method: 'POST', body: '{}' });
      await loadWorkspace(); notify('会话已结束');
    }
    if (button.dataset.transfer) {
      const options = appState.agents.filter((agent) => !agent.disabled_at).map((agent) => `${agent.account} | ${agent.name}`).join('\n');
      const account = prompt(`输入要转接的客服账号：\n${options}`);
      if (!account) return;
      const agent = appState.agents.find((item) => item.account === account.trim());
      if (!agent) throw new Error('未找到该客服账号');
      await api(`/api/admin/conversations/${encodeURIComponent(button.dataset.transfer)}/transfer`, { method: 'POST', body: JSON.stringify({ agent_id: agent.id }) });
      await loadWorkspace(); notify('会话已转接');
    }
    if (button.dataset.editKeyword) openKeyword(appState.keywords.find((rule) => rule.id === button.dataset.editKeyword));
    if (button.dataset.deleteKeyword && confirm('确定删除该关键词规则？')) {
      await api(`/api/admin/keyword-rules/${encodeURIComponent(button.dataset.deleteKeyword)}`, { method: 'DELETE' });
      await loadKeywords(); notify('关键词规则已删除');
    }
    if (button.dataset.toggleRelease) {
      await api(`/api/admin/app-releases/${encodeURIComponent(button.dataset.toggleRelease)}`, { method: 'PATCH', body: JSON.stringify({ enabled: button.dataset.enabled !== 'true' }) });
      await loadReleases(); notify('发布状态已更新');
    }
    if (button.dataset.forceRelease) {
      await api(`/api/admin/app-releases/${encodeURIComponent(button.dataset.forceRelease)}`, { method: 'PATCH', body: JSON.stringify({ force_update: button.dataset.force !== 'true' }) });
      await loadReleases(); notify('更新策略已调整');
    }
    if (button.dataset.deleteRelease && confirm('确定删除该版本？安装包文件也会被移除。')) {
      await api(`/api/admin/app-releases/${encodeURIComponent(button.dataset.deleteRelease)}`, { method: 'DELETE' });
      await loadReleases(); notify('版本已删除');
    }
  } catch (error) { notify(error.message, true); }
});

window.addEventListener('unhandledrejection', (event) => notify(event.reason?.message || '后台请求失败', true));
const initialView = new URLSearchParams(location.search).get('view');
setView(titles[initialView] ? initialView : 'workspace', false);
setInterval(() => {
  if (document.visibilityState !== 'visible') return;
  if (appState.activeView === 'workspace') loadWorkspace().catch(() => {});
  if (appState.activeView === 'audit') loadAudit().catch(() => {});
}, 10000);
