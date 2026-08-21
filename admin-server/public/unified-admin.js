const proxyRoot = '/site-admin-api/servicebridge';
const titles = {
  content: ['内容管理', '官网页面、产品、媒体与 SEO'],
  customers: ['客服管理', '客服账号、密码与接待容量'],
  workspace: ['工作台', '会话运行状态与服务质量概览'],
  settings: ['系统设置', '营业时间、欢迎语、AI 与运行状态'],
  keywords: ['关键词', '关键词自动回复与快捷回复规则'],
  ratings: ['服务评价', '访客满意度评分与建议'],
  audit: ['审计日志', '管理员与客服的敏感操作记录'],
  releases: ['App 版本', '安装包上传、强制更新与发布状态']
};
const appState = { agents: [], conversations: [], ratings: [], ratingFilter: 'all', activeView: 'workspace' };

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

async function api(path, options = {}) {
  const response = await fetch(`${proxyRoot}${path}`, {
    ...options,
    headers: { ...(options.body && !(options.body instanceof FormData) ? { 'Content-Type': 'application/json' } : {}), ...(options.headers || {}) }
  });
  const text = await response.text();
  let payload = {};
  try { payload = text ? JSON.parse(text) : {}; } catch { payload = { message: text }; }
  if (!response.ok) {
    const message = typeof payload.error === 'string' ? payload.message || payload.error : payload.error?.message || payload.message;
    throw new Error(message || `请求失败（HTTP ${response.status}）`);
  }
  return payload;
}

let noticeTimer;
function notify(message, isError = false) {
  const notice = $('#notice');
  notice.textContent = message;
  notice.className = `notice show${isError ? ' error' : ''}`;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { notice.className = 'notice'; }, 3500);
}

function setView(view) {
  appState.activeView = view;
  $$('.nav [data-view]').forEach((button) => button.classList.toggle('active', button.dataset.view === view));
  $$('.view').forEach((panel) => panel.classList.toggle('active', panel.id === `view-${view}`));
  $('#pageTitle').textContent = titles[view][0];
  $('#pageSubtitle').textContent = titles[view][1];
  $('#contentRoot').classList.toggle('content-mode', view === 'content');
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
      <td><div class="row-actions"><button data-edit-agent="${esc(agent.id)}">编辑</button><button data-agent-ratings="${esc(agent.id)}">评价</button><button data-set-rating="${esc(agent.id)}">设置好评率</button>${appState.ratingSummaries[agent.id]?.manual_satisfaction_rate != null ? `<button data-clear-rating="${esc(agent.id)}">清除手动</button>` : ''}<button data-reset-ratings="${esc(agent.id)}">重置评价</button><button data-reset-agent="${esc(agent.id)}">重置密码</button><button data-disable-agent="${esc(agent.id)}">禁用</button><button data-delete-agent="${esc(agent.id)}">删除</button></div></td>
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
  $('#conversationRows').innerHTML = appState.conversations.length ? appState.conversations.map((conversation) => {
    const agent = appState.agents.find((item) => item.id === conversation.assigned_agent_id);
    return `<tr><td><strong>${esc(conversation.visitor_remark || conversation.visitor_ip || conversation.id)}</strong><br><small>${esc(conversation.source || '')}</small></td><td><span class="badge ${esc(conversation.status)}">${statusLabel(conversation.status)}</span></td><td>${esc(conversation.last_message || '-')}</td><td>${Number(conversation.unread_for_agent || 0)}</td><td>${esc(agent?.name || '未分配')}</td><td>${localTime(conversation.updated_at)}</td><td><div class="row-actions"><button data-messages="${esc(conversation.id)}">消息</button>${conversation.status !== 'closed' ? `<button data-transfer="${esc(conversation.id)}">转接</button><button data-close-conversation="${esc(conversation.id)}">结束</button>` : ''}</div></td></tr>`;
  }).join('') : '<tr><td colspan="7" class="empty">暂无会话</td></tr>';
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

async function loadRatings() {
  const [summary, list, agents, summaries] = await Promise.all([
    api('/api/admin/ratings/summary'),
    api('/api/admin/ratings'),
    api('/api/admin/agents'),
    api('/api/admin/ratings/summaries')
  ]);
  appState.agents = agents.agents || [];
  appState.ratings = list.ratings || [];
  appState.ratingSummaries = Object.fromEntries((summaries.items || []).map((item) => [item.agent_id, item]));
  $('#ratingStats').innerHTML = [
    ['评价总数', summary.total, '#0878dc'],
    ['平均分', Number(summary.average || 0).toFixed(2), '#18a5b8'],
    ['非常满意', summary.satisfied, '#16835c'],
    ['中评 / 差评', `${summary.neutral} / ${summary.unsatisfied}`, '#b97708']
  ].map(([label, value, color]) => `<article class="stat" style="--accent:${color}"><small>${label}</small><strong>${value}</strong></article>`).join('');
  const filtered = appState.ratingFilter === 'all'
    ? appState.ratings
    : appState.ratings.filter((rating) => ratingLevel(rating.score) === appState.ratingFilter);
  $('#ratingCount').textContent = `${filtered.length} 条`;
  $('#ratingRows').innerHTML = filtered.length ? filtered.map((rating) => {
    const agent = appState.agents.find((item) => item.id === rating.assigned_agent_id);
    return `<tr class="rating-${ratingLevel(rating.score)}"><td>${esc(rating.conversation_id)}</td><td><strong style="color:${ratingColor(rating.score)}">${'★'.repeat(Number(rating.score || 0))}${'☆'.repeat(5 - Number(rating.score || 0))}</strong></td><td>${esc((rating.tags || []).join('、'))}</td><td>${esc(rating.comment || '-')}</td><td>${esc(agent?.name || '-')}</td><td>${localTime(rating.created_at)}</td></tr>`;
  }).join('') : '<tr><td colspan="6" class="empty">暂无评价记录</td></tr>';
  const agentRows = (appState.agents || []).map((agent) => {
    const item = appState.ratingSummaries[agent.id] || {};
    const count = Number(item.summary?.total || 0);
    return `<tr>
      <td><strong>${esc(agent.name)}</strong><br><span>${esc(agent.account)}</span></td>
      <td>${percentLabel(item.summary?.satisfaction_rate)}${count ? ` <small>(${count}条)</small>` : ''}</td>
      <td>${percentLabel(item.display_satisfaction_rate)}${item.manual_satisfaction_rate != null ? ' <span class="badge busy">手动</span>' : ''}</td>
      <td><div class="row-actions"><button data-agent-ratings="${esc(agent.id)}">明细</button><button data-set-rating="${esc(agent.id)}">设置好评率</button>${item.manual_satisfaction_rate != null ? `<button data-clear-rating="${esc(agent.id)}">清除手动</button>` : ''}<button data-reset-ratings="${esc(agent.id)}">重置评价</button></div></td>
    </tr>`;
  }).join('');
  $('#agentRatingRows').innerHTML = agentRows || '<tr><td colspan="4" class="empty">暂无客服账号</td></tr>';
  $$('#ratingFilterBar [data-rating-filter]').forEach((button) => button.classList.toggle('active', button.dataset.ratingFilter === appState.ratingFilter));
}

async function loadAudit() {
  const payload = await api('/api/admin/audit-events');
  appState.audits = payload.events || [];
  $('#auditCount').textContent = `${appState.audits.length} 条`;
  $('#auditRows').innerHTML = appState.audits.length ? appState.audits.map((event) => `
    <tr>
      <td>${localTime(event.created_at)}</td>
      <td>${esc(event.actor_kind)} / ${esc(event.actor_id)}</td>
      <td><strong>${esc(event.action)}</strong></td>
      <td>${esc(event.resource)}${event.resource_id ? ` / ${esc(event.resource_id)}` : ''}</td>
      <td>${esc(event.ip_address || '-')}</td>
      <td>${esc(event.description || '-')}</td>
    </tr>`).join('') : '<tr><td colspan="6" class="empty">暂无审计记录</td></tr>';
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

async function loadSettings() {
  const [business, contact, status, ai] = await Promise.all([
    api('/api/admin/business-hours'),
    api('/api/contact-settings'),
    api('/api/admin/system-status'),
    api('/api/admin/ai-settings')
  ]);
  fillForm($('#businessForm'), business);
  fillForm($('#contactForm'), contact);
  fillForm($('#aiForm'), ai);
  $('#systemStatus').innerHTML = `
    <p><strong>数据库：</strong><span class="badge ${status.database?.ok ? 'online' : 'disabled'}">${status.database?.ok ? '正常' : '异常'}</span></p>
    <p><strong>AI：</strong>${status.ai?.enabled ? '已启用' : '已停用'} / ${esc(status.ai?.mode || '-')} / ${esc(status.ai?.model || '-')}</p>
    <p><strong>WebSocket：</strong>访客 ${Number(status.websocket?.Visitors || status.websocket?.visitors || 0)}，客服 ${Number(status.websocket?.Agents || status.websocket?.agents || 0)}，管理员 ${Number(status.websocket?.Admins || status.websocket?.admins || 0)}</p>
    <p><strong>服务时间：</strong>${localTime(status.time)}</p>`;
}

async function refreshCurrent() {
  await refreshEngine();
  if (appState.activeView === 'customers') await loadAgents();
  if (appState.activeView === 'workspace') await loadWorkspace();
  if (appState.activeView === 'settings') await loadSettings();
  if (appState.activeView === 'keywords') await loadKeywords();
  if (appState.activeView === 'ratings') await loadRatings();
  if (appState.activeView === 'audit') await loadAudit();
  if (appState.activeView === 'releases') await loadReleases();
}

function openAgent(agent) {
  const form = $('#agentForm');
  form.reset();
  fillForm(form, agent || { max_conversations: 10 });
  form.elements.account.disabled = Boolean(agent);
  $('#passwordField').style.display = agent ? 'none' : 'grid';
  $('#agentDialogTitle').textContent = agent ? '编辑客服账号' : '新增客服账号';
  $('#agentDialog').showModal();
}

async function openAgentRatings(agentID) {
  const payload = await api(`/api/admin/agents/${encodeURIComponent(agentID)}/ratings`);
  const agent = payload.agent || {};
  const summary = payload.summary || {};
  const ratings = payload.ratings || [];
  $('#agentRatingDialogTitle').textContent = `评价明细 · ${agent.name || agent.account || agentID}`;
  $('#agentRatingSummary').innerHTML = `
    <article class="stat" style="--accent:#16835c"><small>真实好评率</small><strong>${percentLabel(summary.satisfaction_rate)}</strong></article>
    <article class="stat" style="--accent:#0878dc"><small>展示好评率</small><strong>${percentLabel(summary.display_satisfaction_rate)}</strong></article>
    <article class="stat" style="--accent:#18a5b8"><small>评价总数</small><strong>${Number(summary.total || 0)}</strong></article>
    <article class="stat" style="--accent:#b97708"><small>中评 / 差评</small><strong>${Number(summary.neutral || 0)} / ${Number(summary.unsatisfied || 0)}</strong></article>`;
  $('#agentRatingRowsDialog').innerHTML = ratings.length ? ratings.map((rating) => `
    <tr class="rating-${ratingLevel(rating.score)}">
      <td><strong style="color:${ratingColor(rating.score)}">${'★'.repeat(Number(rating.score || 0))}${'☆'.repeat(5 - Number(rating.score || 0))}</strong></td>
      <td>${esc((rating.tags || []).join('、'))}</td>
      <td>${esc(rating.comment || '-')}</td>
      <td>${localTime(rating.created_at)}</td>
    </tr>`).join('') : '<tr><td colspan="4" class="empty">该客服暂无评价</td></tr>';
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
  await Promise.all([loadAgents(), loadRatings()]);
  notify('好评率已设置');
});

function openKeyword(rule) {
  const form = $('#keywordForm');
  form.reset();
  fillForm(form, rule || { match_type: 'contains', action: 'text', priority: 50, enabled: true });
  $('#keywordDialogTitle').textContent = rule ? '编辑关键词规则' : '新增关键词规则';
  $('#keywordDialog').showModal();
}

async function showMessages(id) {
  const payload = await api(`/api/conversations/${encodeURIComponent(id)}/messages?limit=100`);
  const messages = payload.messages || [];
  $('#messageTitle').textContent = `会话 ${id}`;
  $('#messageList').innerHTML = messages.length ? messages.map((message) => `<div class="message ${message.sender_type === 'agent' ? 'agent' : ''}">${esc(message.content)}<small>${statusLabel(message.sender_type)} · ${localTime(message.created_at)}</small></div>`).join('') : '<div class="empty">暂无消息</div>';
  $('#messageDialog').showModal();
}

$$('.nav [data-view]').forEach((button) => button.addEventListener('click', () => setView(button.dataset.view)));
$$('[data-close]').forEach((button) => button.addEventListener('click', () => button.closest('dialog').close()));
$('#refresh').addEventListener('click', () => refreshCurrent().then(() => notify('已刷新')).catch((error) => notify(error.message, true)));
$('#logout').addEventListener('click', async () => { await fetch('/site-admin-auth/logout', { method: 'POST' }); location.href = '/admin-2fa'; });
$('#createAgent').addEventListener('click', () => openAgent(null));
$('#createKeyword').addEventListener('click', () => openKeyword(null));
$('#exportConversations').addEventListener('click', () => { location.href = `${proxyRoot}/api/admin/conversations/export.csv`; });
$('#exportAudit').addEventListener('click', () => { location.href = `${proxyRoot}/api/admin/audit-events/export.csv`; });

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

$('#businessForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  await api('/api/admin/business-hours', { method: 'PATCH', body: JSON.stringify(formJson(event.currentTarget)) });
  notify('营业时间已保存');
});

$('#contactForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const current = await api('/api/contact-settings');
  await api('/api/admin/contact-settings', { method: 'PATCH', body: JSON.stringify({ ...current, ...formJson(event.currentTarget) }) });
  notify('访客端配置已保存');
});

$('#aiForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  const current = await api('/api/admin/ai-settings');
  const next = { ...current, ...formJson(event.currentTarget) };
  next.timeout_seconds = Number(next.timeout_seconds);
  next.max_output_tokens = Number(next.max_output_tokens);
  delete next.api_key_masked;
  await api('/api/admin/ai-settings', { method: 'PATCH', body: JSON.stringify(next) });
  notify('AI 设置已保存');
});

document.addEventListener('click', async (event) => {
  const button = event.target.closest('button');
  if (!button) return;
  try {
    if (button.dataset.ratingFilter) {
      appState.ratingFilter = button.dataset.ratingFilter;
      await loadRatings();
      return;
    }
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
      if (appState.activeView === 'ratings') await loadRatings();
      await loadAgents();
      notify('手动好评率已清除');
      return;
    }
    if (button.dataset.resetRatings && confirm('确定重置该客服的真实评价数据？所有评价记录和手动好评率都会清空，不可恢复。')) {
      const agentID = button.dataset.resetRatings;
      await api(`/api/admin/agents/${encodeURIComponent(agentID)}/ratings/reset`, { method: 'POST', body: '{}' });
      if (appState.activeView === 'ratings') await loadRatings();
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
    if (button.dataset.messages) await showMessages(button.dataset.messages);
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
refreshCurrent().catch((error) => notify(error.message, true));
setInterval(() => { if (appState.activeView === 'workspace') loadWorkspace().catch(() => {}); }, 10000);
