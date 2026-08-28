(() => {
  const embedded = new URLSearchParams(location.search).get('embed') === '1';
  document.body.classList.toggle('embedded', embedded);

  const state = {
    section: 'dashboard',
    items: [],
    categories: [],
    stats: {},
    runtime: {},
    settings: null,
    meta: { page: 1, pages: 0, total: 0 },
    pageSize: 24,
    filterCategory: '',
    view: 'grid',
    selected: new Set(),
    uploadMode: 'smart',
    uploadQueue: [],
    aiPreviews: [],
    aiModels: [],
    detail: null,
    detailVariants: [],
    busy: false
  };

  const sectionInfo = {
    dashboard: ['素材仪表盘', '媒体、存储与服务器运行概览'],
    gallery: ['素材管理', '搜索、上传、替换与分类管理'],
    ai: ['AI 生图工作站', '临时生成、确认入库或放弃'],
    settings: ['系统设置', '存储、图片、上传、AI 与安全配置'],
    guide: ['R2 接入指南', 'Cloudflare R2 配置与验证步骤']
  };
  const supportedMimes = [
    ['image/jpeg', 'JPEG 图片'], ['image/png', 'PNG 图片'], ['image/webp', 'WebP 图片'],
    ['image/gif', 'GIF 图片'], ['image/avif', 'AVIF 图片'], ['video/mp4', 'MP4 视频'],
    ['video/webm', 'WebM 视频'], ['video/quicktime', 'MOV 视频'], ['application/pdf', 'PDF 文档'],
    ['application/zip', 'ZIP 压缩包'], ['application/acad', 'DWG 图纸'], ['application/dxf', 'DXF 图纸'],
    ['image/vnd.dwg', 'DWG 图纸（兼容类型）']
  ];

  const $ = (selector) => document.querySelector(selector);
  const $$ = (selector) => [...document.querySelectorAll(selector)];
  const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;' })[char]);
  const formatBytes = (bytes) => {
    const value = Number(bytes || 0);
    if (value < 1024) return `${value} B`;
    if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MB`;
    return `${(value / 1024 ** 3).toFixed(2)} GB`;
  };
  const formatDate = (value) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
  const formatDuration = (seconds) => {
    const value = Math.max(0, Number(seconds || 0));
    const days = Math.floor(value / 86400);
    const hours = Math.floor(value % 86400 / 3600);
    const minutes = Math.floor(value % 3600 / 60);
    return days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分` : `${minutes} 分钟`;
  };
  const ratio = (used, total) => total > 0 ? Math.max(0, Math.min(100, used / total * 100)) : 0;
  let toastTimer;

  function toast(message, error = false) {
    const element = $('#toast');
    element.textContent = message;
    element.className = `toast show${error ? ' error' : ''}`;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { element.className = 'toast'; }, 3800);
  }

  async function request(url, options = {}) {
    const response = await fetch(url, options);
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) {
      location.href = `/admin-2fa?returnTo=${encodeURIComponent(location.pathname + location.search + location.hash)}`;
      throw new Error('登录已失效，正在返回登录页。');
    }
    if (!response.ok) throw new Error(payload.error?.message || payload.message || `请求失败（HTTP ${response.status}）`);
    return payload;
  }

  function jsonOptions(method, payload) {
    return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) };
  }

  async function copyText(text) {
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
    else {
      const area = document.createElement('textarea');
      area.value = text;
      document.body.append(area);
      area.select();
      document.execCommand('copy');
      area.remove();
    }
    toast('已复制到剪贴板。');
  }

  function navigate(section, updateHash = true) {
    if (!sectionInfo[section]) section = 'dashboard';
    state.section = section;
    $$('.module-section').forEach((element) => element.classList.toggle('active', element.id === `section-${section}`));
    $$('[data-section]').forEach((button) => button.classList.toggle('active', button.dataset.section === section));
    if (updateHash && location.hash !== `#${section}`) history.replaceState(null, '', `${location.pathname}${location.search}#${section}`);
    if (section === 'gallery') loadMedia().catch((error) => toast(error.message, true));
    if (section === 'settings') Promise.all([loadSettings(), loadRuntime()]).catch((error) => toast(error.message, true));
  }

  function summaryRows(rows) {
    return rows.map(([label, value, html = false]) => `<div><dt>${esc(label)}</dt><dd>${html ? value : esc(value)}</dd></div>`).join('');
  }

  async function loadStats() {
    state.stats = (await request('/site-admin-api/media/stats')).data || {};
    renderDashboard();
  }

  async function loadRuntime() {
    state.runtime = (await request('/site-admin-api/media/runtime')).data || {};
    renderDashboard();
    renderSettings();
  }

  function renderDashboard() {
    const stats = state.stats || {};
    const runtime = state.runtime || {};
    const storage = stats.storage || runtime.storage || {};
    const memory = runtime.memory || {};
    const disk = runtime.disk || {};
    const service = runtime.service || {};
    const cards = [
      ['图片总数', stats.images || 0, `全部素材 ${stats.total || 0}`, '#0878dc'],
      ['存储占用', formatBytes(stats.storedBytes ?? stats.totalBytes), '含原图与变体', '#16835c'],
      ['进程内存', formatBytes(memory.rss), `堆内存 ${formatBytes(memory.heapUsed)}`, '#7656c4'],
      ['分类数量', stats.categories || state.categories.length || 0, '支持快捷筛选', '#d36c18'],
      ['运行时长', formatDuration(service.uptimeSeconds), `PID ${service.pid || '—'}`, '#1685a5']
    ];
    $('#stats').innerHTML = cards.map(([label, value, note, accent]) => `<article class="stat" style="--accent:${accent}"><small>${esc(label)}</small><strong>${esc(value)}</strong><span>${esc(note)}</span></article>`).join('');

    const storageOk = Boolean(storage.ok);
    $('#sideStorageDot').className = storage.ok === undefined ? '' : storageOk ? 'ok' : 'bad';
    $('#sideStorageText').textContent = storage.ok === undefined ? '存储检查中' : storageOk ? `${storage.bucket || 'S3'} 已连通` : '存储连接异常';
    $('#serviceHealth').textContent = service.ok === undefined ? '检查中' : service.ok && storageOk ? '全部正常' : '存在异常';
    $('#serviceHealth').className = `status-tag ${service.ok && storageOk ? 'ok' : service.ok === undefined ? '' : 'bad'}`;

    const systemMemoryUsed = Math.max(0, Number(memory.systemTotal || 0) - Number(memory.systemFree || 0));
    const resourceRows = [
      ['system-cpu', '系统 CPU', Number(runtime.cpu?.usagePercent || 0), 100, `${Number(runtime.cpu?.usagePercent || 0).toFixed(1)}%`],
      ['process-cpu', 'Node 进程 CPU', Number(runtime.cpu?.processUsagePercent || 0), 100, `${Number(runtime.cpu?.processUsagePercent || 0).toFixed(1)}%`],
      ['process-rss', 'Node 进程 RSS', memory.rss || 0, memory.systemTotal || 0, `${formatBytes(memory.rss)} / ${formatBytes(memory.systemTotal)}`],
      ['system-memory', '系统内存', systemMemoryUsed, memory.systemTotal || 0, `${formatBytes(systemMemoryUsed)} / ${formatBytes(memory.systemTotal)}`],
      ['disk', '磁盘空间', disk.used || 0, disk.total || 0, disk.available ? `${formatBytes(disk.used)} / ${formatBytes(disk.total)}` : '宿主机未提供'],
      ['heap', 'Node 堆内存', memory.heapUsed || 0, memory.heapTotal || 0, `${formatBytes(memory.heapUsed)} / ${formatBytes(memory.heapTotal)}`]
    ];
    if (!$('#resourceStatus').querySelector('[data-resource]')) {
      $('#resourceStatus').innerHTML = resourceRows.map(([key, label]) => `<div class="resource-row" data-resource="${esc(key)}"><div class="resource-label"><span>${esc(label)}</span><span data-resource-text>读取中</span></div><div class="meter"><i></i></div></div>`).join('');
    }
    resourceRows.forEach(([key, label, used, total, value]) => {
      const row = $('#resourceStatus').querySelector(`[data-resource="${key}"]`);
      if (!row) return;
      row.querySelector('.resource-label span:first-child').textContent = label;
      row.querySelector('[data-resource-text]').textContent = value;
      row.querySelector('.meter i').style.width = `${ratio(used, total).toFixed(1)}%`;
    });

    const environment = [
      ['Node.js', service.node || '—'], ['运行平台', service.platform || '—'],
      ['数据库', runtime.database?.driver || '—'], ['媒体存储', storage.driver ? storage.driver.toUpperCase() : '—'],
      ['图片处理', runtime.processing?.engine || '—'], ['CPU 核心', runtime.cpu?.cores || '—'],
      ['Bucket', storage.bucket || '—'], ['启动时间', formatDate(service.startedAt)]
    ];
    $('#environmentFacts').innerHTML = environment.map(([label, value]) => `<div><dt>${esc(label)}</dt><dd>${esc(value)}</dd></div>`).join('');

    const maxCount = Math.max(1, ...state.categories.map((item) => Number(item.count || 0)));
    $('#categoryDistribution').innerHTML = state.categories.length
      ? state.categories.map((item) => `<div class="distribution-row"><b title="${esc(item.name)}">${esc(item.name)}</b><div class="meter"><i style="width:${(Number(item.count || 0) / maxCount * 100).toFixed(1)}%"></i></div><span>${item.count || 0}</span></div>`).join('')
      : '<div class="empty-state"><p>暂无分类数据</p></div>';
  }

  function categoryOptions(selected = '') {
    return state.categories.map((item) => `<option value="${esc(item.name)}" ${item.name === selected ? 'selected' : ''}>${esc(item.name)}（${item.count}）</option>`).join('');
  }

  async function loadCategories() {
    state.categories = (await request('/site-admin-api/media/categories')).data || [];
    if (state.filterCategory && !state.categories.some((item) => item.name === state.filterCategory)) state.filterCategory = '';
    renderCategories();
    renderDashboard();
  }

  function renderCategories() {
    $('#categoryTabs').innerHTML = `<button class="${state.filterCategory ? '' : 'active'}" data-filter-category="">全部（${state.stats.total || 0}）</button>` + state.categories.map((item) => `<button class="${state.filterCategory === item.name ? 'active' : ''}" data-filter-category="${esc(item.name)}">${esc(item.name)}（${item.count}）</button>`).join('');
    const uploadCurrent = $('#uploadCategory').value || '未分类';
    const aiCurrent = $('#aiCategory').value || '未分类';
    $('#uploadCategory').innerHTML = categoryOptions(uploadCurrent);
    $('#aiCategory').innerHTML = categoryOptions(aiCurrent);
    if (state.categories.some((item) => item.name === uploadCurrent)) $('#uploadCategory').value = uploadCurrent;
    if (state.categories.some((item) => item.name === aiCurrent)) $('#aiCategory').value = aiCurrent;
    $('#categoryList').innerHTML = state.categories.map((item) => `<div class="category-item"><div><b>${esc(item.name)}</b><span>${item.count} 个素材</span></div>${item.name === '未分类' ? '<span>默认分类</span>' : `<button data-delete-category="${esc(item.name)}">删除</button>`}</div>`).join('');
  }

  function mediaQuery() {
    const params = new URLSearchParams({ page: String(state.meta.page || 1), pageSize: String(state.pageSize) });
    const query = $('#searchInput').value.trim();
    if (query) params.set('q', query);
    if (state.filterCategory) params.set('category', state.filterCategory);
    if ($('#filterType').value) params.set('type', $('#filterType').value);
    return params;
  }

  async function loadMedia() {
    const payload = await request(`/site-admin-api/media?${mediaQuery()}`);
    state.items = payload.data || [];
    state.meta = payload.meta || { page: 1, pages: 0, total: 0 };
    state.selected = new Set([...state.selected].filter((id) => state.items.some((item) => item.id === id)));
    renderMedia();
  }

  function mediaPreview(item) {
    if (item.type === 'image') return `<img src="${esc(item.url)}" alt="${esc(item.name)}" loading="lazy">`;
    if (item.type === 'video') return `<video src="${esc(item.url)}" preload="metadata" muted></video>`;
    return '<span class="file-icon">▤</span>';
  }

  function renderMedia() {
    const meta = state.meta;
    $('#resultSummary').textContent = `共 ${meta.total || 0} 个素材${state.selected.size ? `，已选 ${state.selected.size} 个` : ''}`;
    $('#pageState').textContent = `第 ${meta.page || 1} / ${Math.max(1, meta.pages || 1)} 页`;
    $('#previousPage').disabled = (meta.page || 1) <= 1;
    $('#nextPage').disabled = !meta.pages || meta.page >= meta.pages;
    $('#batchDelete').disabled = state.selected.size === 0;
    $('#mediaGrid').className = `media-grid${state.view === 'list' ? ' list' : ''}`;
    if (!state.items.length) {
      $('#mediaGrid').innerHTML = '<div class="empty-state"><div class="empty-icon">▧</div><h3>没有找到素材</h3><p>调整筛选条件，或上传新的图片、视频和文档。</p></div>';
      return;
    }
    $('#mediaGrid').innerHTML = state.items.map((item) => `<article class="media-card ${state.selected.has(item.id) ? 'selected' : ''}"><div class="media-preview" data-detail-media="${esc(item.id)}"><input class="media-select" type="checkbox" data-select-media="${esc(item.id)}" ${state.selected.has(item.id) ? 'checked' : ''} aria-label="选择 ${esc(item.name)}">${mediaPreview(item)}<span class="media-type">${esc(item.type)}</span></div><div class="media-info"><h3 title="${esc(item.name)}">${esc(item.name)}</h3><p><span>${esc(item.category || '未分类')}</span><span>${formatBytes(item.size)}</span></p></div><div class="media-actions"><button data-detail-media="${esc(item.id)}">详情</button><button data-copy-url="${esc(item.id)}">复制链接</button><button class="delete" data-delete-media="${esc(item.id)}">删除</button></div></article>`).join('');
  }

  function srcset(item) {
    const variants = Array.isArray(item?.variants?.webp) ? item.variants.webp : [];
    return variants.map((entry) => `${entry.url} ${entry.width}w`).join(', ');
  }

  async function openDetail(id) {
    const item = (await request(`/site-admin-api/media/${encodeURIComponent(id)}`)).data;
    state.detail = item;
    state.detailVariants = [
      { label: 'WebP 主图', url: item.url, mime: item.mime, size: item.size, dimensions: item.width && item.height ? `${item.width} × ${item.height}` : '—' },
      ...(Array.isArray(item.variants?.webp) ? item.variants.webp.map((entry) => ({ label: `${entry.width}px 变体`, ...entry, dimensions: `${entry.width} × ${entry.height}` })) : []),
      ...(item.variants?.original ? [{ label: '保留原图', ...item.variants.original, dimensions: '原始尺寸' }] : [])
    ];
    const preview = item.type === 'image' ? `<img src="${esc(item.url)}" alt="${esc(item.name)}">` : item.type === 'video' ? `<video src="${esc(item.url)}" controls></video>` : '<span class="detail-file">▤</span>';
    const variants = state.detailVariants.map((entry, index) => `<tr><td>${esc(entry.label)}</td><td>${esc(entry.mime || '—')}</td><td>${esc(entry.dimensions || '—')}</td><td>${formatBytes(entry.size)}</td><td><button class="link-button" data-copy-variant="${index}">复制</button></td></tr>`).join('');
    $('#detailContent').innerHTML = `<div class="detail-layout"><div class="detail-preview">${preview}</div><div class="detail-meta"><h3>${esc(item.name)}</h3><p>${esc(item.key)}</p><table class="detail-table"><tbody><tr><th>分类</th><td>${esc(item.category)}</td></tr><tr><th>类型</th><td>${esc(item.mime)} / ${esc(item.type)}</td></tr><tr><th>主图大小</th><td>${formatBytes(item.size)}</td></tr><tr><th>来源</th><td>${esc(item.sourceType || 'upload')}</td></tr><tr><th>更新时间</th><td>${formatDate(item.updatedAt)}</td></tr><tr><th>SHA-256</th><td>${esc(item.checksumSha256 || '—')}</td></tr></tbody></table><div class="detail-actions"><button class="button" data-detail-copy="url">复制链接</button><button class="button" data-detail-copy="markdown">复制 Markdown</button><button class="button" data-detail-copy="srcset" ${srcset(item) ? '' : 'disabled'}>复制 srcset</button><button class="button" data-detail-references>查看官网引用</button><button class="primary" data-detail-replace>替换（URL 不变）</button><button class="danger" data-detail-delete>删除</button></div></div></div><h3 class="variant-title">文件与多尺寸变体</h3><div class="table-scroll"><table><thead><tr><th>文件</th><th>格式</th><th>尺寸</th><th>大小</th><th>操作</th></tr></thead><tbody>${variants}</tbody></table></div>`;
    openModal('detail');
  }

  async function showReferences(id) {
    const refs = (await request(`/site-admin-api/media/${encodeURIComponent(id)}/references`)).data.references || [];
    alert(refs.length ? `当前素材被以下官网字段引用：\n\n${refs.join('\n')}` : '当前素材没有被官网内容引用。');
  }

  async function deleteMedia(id) {
    const item = state.items.find((entry) => entry.id === id) || state.detail;
    if (!confirm(`确认删除“${item?.name || ''}”及对象存储中的全部变体？`)) return;
    await request(`/site-admin-api/media/${encodeURIComponent(id)}`, { method: 'DELETE' });
    if (state.detail?.id === id) closeModal('detail');
    toast('素材已删除。');
    await Promise.all([loadMedia(), loadStats(), loadCategories()]);
  }

  async function replaceMedia(item) {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = item?.type === 'image' ? 'image/jpeg,image/png,image/webp,image/gif,image/avif' : '*/*';
    input.addEventListener('change', async () => {
      const file = input.files?.[0];
      if (!file || !confirm(`确认用“${file.name}”替换“${item.name}”？主 URL 会保持不变。`)) return;
      try {
        const form = new FormData();
        form.append('file', file);
        form.append('mode', item.type === 'image' ? 'smart' : 'original');
        form.append('category', item.category || '未分类');
        form.append('preserveOriginal', String(state.settings?.config?.image?.keepOriginal !== false));
        await request(`/site-admin-api/media/${encodeURIComponent(item.id)}/replace`, { method: 'PUT', body: form });
        closeModal('detail');
        toast('素材已替换，主 URL 未变。');
        await Promise.all([loadMedia(), loadStats()]);
      } catch (error) { toast(error.message, true); }
    }, { once: true });
    input.click();
  }

  function openModal(name) {
    const element = $(`#${name}Modal`);
    if (!element) return;
    element.hidden = false;
    document.body.classList.add('modal-open');
  }

  function closeModal(name) {
    const element = $(`#${name}Modal`);
    if (element) element.hidden = true;
    if (!$$('.modal').some((modal) => !modal.hidden)) document.body.classList.remove('modal-open');
  }

  function allowedAccept() {
    const allowed = String(state.settings?.config?.image?.allowedMimeTypes || '').split(',').filter(Boolean);
    return allowed.join(',');
  }

  function setUploadMode(mode) {
    state.uploadMode = mode;
    $$('#uploadModes [data-mode]').forEach((button) => button.classList.toggle('active', button.dataset.mode === mode));
    const input = $('#uploadFiles');
    const configured = allowedAccept();
    input.accept = mode === 'smart' ? configured.split(',').filter((mime) => mime.startsWith('image/')).join(',') : configured;
    $('#preserveOriginal').closest('label').style.visibility = mode === 'smart' ? 'visible' : 'hidden';
    $('#uploadHint').textContent = mode === 'smart' ? '智能模式会校验位图并生成 WebP 主图与多尺寸变体。' : mode === 'direct' ? '浏览器直接 PUT 到 R2，完成后由后台校验大小、类型和魔数。' : '保留文件原始内容，适合视频、文档或无需变体的图片。';
  }

  function addUploadFiles(files) {
    for (const file of files) state.uploadQueue.push({ id: `${Date.now()}-${Math.random()}`, file, status: 'waiting', message: '等待上传' });
    renderUploadQueue();
  }

  function renderUploadQueue() {
    $('#uploadQueue').innerHTML = state.uploadQueue.map((item, index) => `<div class="queue-item"><b title="${esc(item.file.name)}">${esc(item.file.name)}</b><span>${formatBytes(item.file.size)}</span><span class="queue-state ${esc(item.status)}">${esc(item.message)}</span>${item.status === 'uploading' ? '' : `<button class="link-button" data-remove-queue="${index}">移除</button>`}</div>`).join('');
  }

  async function uploadOne(file, mode, category, preserveOriginal) {
    if (mode === 'direct') {
      const extension = file.name.toLowerCase().split('.').pop();
      const declaredMime = file.type || ({ jpg: 'image/jpeg', jpeg: 'image/jpeg', png: 'image/png', webp: 'image/webp', gif: 'image/gif', avif: 'image/avif', mp4: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', pdf: 'application/pdf', zip: 'application/zip', dwg: 'application/acad', dxf: 'application/dxf' })[extension] || '';
      const direct = (await request('/site-admin-api/media/presign', jsonOptions('POST', { name: file.name, mime: declaredMime, size: file.size, category }))).data;
      const uploaded = await fetch(direct.uploadUrl, { method: 'PUT', headers: direct.headers, body: file });
      if (!uploaded.ok) throw new Error(`R2 直传失败（HTTP ${uploaded.status}），请检查 Bucket CORS。`);
      return (await request('/site-admin-api/media/confirm', jsonOptions('POST', { token: direct.token }))).data;
    }
    const form = new FormData();
    form.append('file', file);
    form.append('mode', mode);
    form.append('category', category);
    form.append('preserveOriginal', String(preserveOriginal));
    return (await request('/site-admin-api/media/upload', { method: 'POST', body: form })).data;
  }

  async function runUpload() {
    const pending = state.uploadQueue.filter((item) => ['waiting', 'error'].includes(item.status));
    if (!pending.length) return toast('请先选择要上传的文件。', true);
    const maxBytes = Number(state.settings?.config?.image?.maxUploadBytes || Infinity);
    if (pending.some((item) => item.file.size > maxBytes)) return toast(`单文件不能超过 ${formatBytes(maxBytes)}。`, true);
    if (state.uploadMode === 'smart' && pending.some((item) => !item.file.type.startsWith('image/'))) return toast('智能模式只允许位图，视频和文档请选择原文件模式。', true);
    const button = $('#uploadButton');
    button.disabled = true;
    $('#uploadProgress').textContent = '上传队列处理中…';
    try {
      for (const item of pending) {
        item.status = 'uploading'; item.message = '上传中'; renderUploadQueue();
        try {
          await uploadOne(item.file, state.uploadMode, $('#uploadCategory').value, $('#preserveOriginal').checked);
          item.status = 'done'; item.message = '已完成';
        } catch (error) {
          item.status = 'error'; item.message = error.message;
        }
        renderUploadQueue();
      }
      const done = pending.filter((item) => item.status === 'done').length;
      const failed = pending.length - done;
      $('#uploadProgress').textContent = `完成 ${done} 个，失败 ${failed} 个。`;
      $('#uploadProgress').className = `form-state ${failed ? 'error' : 'success'}`;
      toast(`上传完成：成功 ${done} 个，失败 ${failed} 个。`, failed > 0);
      state.meta.page = 1;
      await Promise.all([loadMedia(), loadStats(), loadCategories()]);
      if (!failed) setTimeout(() => closeModal('upload'), 350);
    } finally { button.disabled = false; }
  }

  async function batchDelete() {
    if (!state.selected.size || !confirm(`确认删除已选的 ${state.selected.size} 个素材？被官网引用的素材会自动阻止。`)) return;
    const result = (await request('/site-admin-api/media/batch-delete', jsonOptions('POST', { ids: [...state.selected] }))).data;
    state.selected.clear();
    const message = `已删除 ${result.deleted.length} 个，引用阻止 ${result.blocked.length} 个，失败 ${result.failed.length} 个。`;
    toast(message, Boolean(result.blocked.length || result.failed.length));
    if (result.blocked.length) alert(`${message}\n\n${result.blocked.map((item) => `${item.name}：${item.references.join('、')}`).join('\n')}`);
    await Promise.all([loadMedia(), loadStats(), loadCategories()]);
  }

  async function loadAiModels(refresh = false) {
    const capability = (await request(`/site-admin-api/media/ai-models${refresh ? '?refresh=true' : ''}`)).data || {};
    state.aiModels = capability.models || [];
    $('#aiModel').innerHTML = state.aiModels.map((model) => `<option value="${esc(model)}" ${model === capability.defaultModel ? 'selected' : ''}>${esc(model)}</option>`).join('');
    renderAiStatus(capability);
  }

  function renderAiStatus(capability = null) {
    const configured = capability?.configured ?? state.settings?.config?.aiImage?.apiKeyConfigured ?? false;
    $('#aiConfiguredTag').textContent = configured ? '● AI 已配置' : '● AI 未配置';
    $('#aiConfiguredTag').className = `status-tag ${configured ? 'ok' : 'bad'}`;
    $('#aiNotConfigured').hidden = configured;
    $('#aiWorkbench').hidden = !configured;
  }

  function renderAiResults() {
    $('#aiResultCount').textContent = `${state.aiPreviews.length} 张`;
    if (!state.aiPreviews.length) {
      $('#aiResults').innerHTML = '<div class="empty-state"><div class="empty-icon">▧</div><h3>还没有生成结果</h3><p>在左侧输入提示词并点击“生成图片”。</p></div>';
      return;
    }
    $('#aiResults').innerHTML = state.aiPreviews.map((item, index) => `<article class="ai-result-card"><div class="ai-result-image"><img src="${item.dataUrl}" alt="AI 生成结果 ${index + 1}"></div><div class="ai-result-info"><div class="ai-result-meta"><span>${esc(item.size)} · ${formatBytes(item.bytes)}</span><span class="inline-tag ${item.status === 'error' ? 'bad' : ''}">${item.status === 'uploaded' ? '已上传' : item.status === 'uploading' ? '上传中' : item.status === 'error' ? '失败' : '待处理'}</span></div><div class="ai-result-actions"><button class="primary" data-ai-upload="${esc(item.id)}" ${item.status !== 'pending' && item.status !== 'error' ? 'disabled' : ''}>${item.status === 'uploaded' ? '已上传' : '直接上传使用'}</button><button class="button" data-ai-discard="${esc(item.id)}" ${item.status === 'uploading' ? 'disabled' : ''}>${item.status === 'uploaded' ? '移除结果' : '放弃'}</button></div></div></article>`).join('');
  }

  async function generatePreview() {
    const prompt = $('#aiPrompt').value.trim();
    if (!prompt) return toast('请先输入生图提示词。', true);
    const button = $('#generateButton');
    button.disabled = true;
    $('#aiState').textContent = '正在请求 AI 生图，结果不会自动入库…';
    $('#aiState').className = 'form-state';
    try {
      const preview = (await request('/site-admin-api/media/ai-preview', jsonOptions('POST', { prompt, model: $('#aiModel').value, size: $('#aiSize').value, quality: $('#aiQuality').value }))).data;
      state.aiPreviews.unshift({ ...preview, status: 'pending' });
      renderAiResults();
      $('#aiState').textContent = `生成成功，临时结果将在 ${formatDate(preview.expiresAt)} 前有效。`;
      $('#aiState').className = 'form-state success';
      toast('生成成功，请在结果区选择上传或放弃。');
    } catch (error) {
      $('#aiState').textContent = error.message;
      $('#aiState').className = 'form-state error';
      toast(error.message, true);
    } finally { button.disabled = false; }
  }

  async function commitAiPreview(id) {
    const item = state.aiPreviews.find((entry) => entry.id === id);
    if (!item) return;
    item.status = 'uploading'; renderAiResults();
    try {
      item.media = (await request(`/site-admin-api/media/ai-preview/${encodeURIComponent(id)}/commit`, jsonOptions('POST', { category: $('#aiCategory').value, preserveOriginal: state.settings?.config?.image?.keepOriginal !== false }))).data;
      item.status = 'uploaded';
      toast(`已上传到素材管理（${item.media.category}）。`);
      await Promise.all([loadMedia(), loadStats(), loadCategories()]);
    } catch (error) { item.status = 'error'; toast(error.message, true); }
    renderAiResults();
  }

  async function discardAiPreview(id) {
    const index = state.aiPreviews.findIndex((entry) => entry.id === id);
    if (index < 0) return;
    const item = state.aiPreviews[index];
    try {
      if (item.status !== 'uploaded') await request(`/site-admin-api/media/ai-preview/${encodeURIComponent(id)}`, { method: 'DELETE' });
      state.aiPreviews.splice(index, 1);
      renderAiResults();
      toast(item.status === 'uploaded' ? '已从结果区移除。' : '临时结果已放弃。');
    } catch (error) { toast(error.message, true); }
  }

  async function loadSettings() {
    state.settings = (await request('/site-admin-api/system/media-settings')).data;
    renderSettings();
    renderAiStatus();
    const keep = state.settings?.config?.image?.keepOriginal !== false;
    $('#preserveOriginal').checked = keep;
  }

  function renderSettings() {
    if (!state.settings) return;
    const config = state.settings.config || {};
    const storage = config.storage || {};
    const image = config.image || {};
    const ai = config.aiImage || {};
    const health = state.settings.storageHealth || state.runtime.storage || {};
    $('#storageSummary').innerHTML = summaryRows([
      ['存储驱动', 'Cloudflare R2 / S3'], ['Bucket', storage.bucket || '—'],
      ['公开域名', storage.publicBaseUrl || '未设置'], ['连通状态', `<span class="inline-tag ${health.ok ? '' : 'bad'}">${health.ok ? '已连通' : '异常'}</span>`, true]
    ]);
    $('#imageSummary').innerHTML = summaryRows([
      ['生成尺寸', `${storage.driver ? image.variantWidths : image.variantWidths || '—'} px`], ['输出格式', 'WebP'],
      ['压缩质量', `${image.webpQuality || '—'}%`], ['保留原图', image.keepOriginal === false ? '否' : '是']
    ]);
    const mimeCount = String(image.allowedMimeTypes || '').split(',').filter(Boolean).length;
    $('#uploadSummary').innerHTML = summaryRows([
      ['单文件上限', formatBytes(image.maxUploadBytes)], ['允许类型', `${mimeCount} 种`],
      ['直传校验', '大小 / Content-Type / 魔数'], ['SVG', '<span class="inline-tag bad">禁止入库</span>', true]
    ]);
    $('#aiSummary').innerHTML = summaryRows([
      ['接口地址', ai.baseUrl || '—'], ['默认模型', ai.defaultModel || '—'],
      ['API Key', `<span class="inline-tag ${ai.apiKeyConfigured ? '' : 'bad'}">${ai.apiKeyConfigured ? `已配置 ${esc(ai.apiKeyMasked || '')}` : '未配置'}</span>`, true],
      ['请求超时', `${Math.round(Number(ai.requestTimeoutMs || 0) / 1000)} 秒`]
    ]);
    $('#securitySummary').innerHTML = summaryRows([
      ['管理员登录', '<span class="inline-tag">统一 2FA Cookie</span>', true], ['密钥持久化', 'PostgreSQL 加密字段'],
      ['密钥加密', 'AES-256-GCM'], ['敏感操作', '写入审计日志']
    ]);
    const runtime = state.runtime || {};
    $('#runtimeSummary').innerHTML = summaryRows([
      ['数据库', runtime.database?.driver || 'PostgreSQL'], ['图片处理', runtime.processing?.engine || 'sharp / libvips'],
      ['Node.js', runtime.service?.node || '—'], ['服务健康', `<span class="inline-tag ${runtime.service?.ok ? '' : 'bad'}">${runtime.service?.ok ? '正常' : '检查中'}</span>`, true]
    ]);
  }

  function inputValue(value) { return esc(value ?? ''); }

  async function openSetting(type) {
    if (!state.settings) await loadSettings();
    const config = state.settings.config || {};
    const storage = config.storage || {};
    const image = config.image || {};
    const ai = config.aiImage || {};
    const titles = {
      storage: ['存储配置', '保存前会真实验证 Bucket；已有素材时禁止直接切换存储身份。'],
      image: ['图片处理', '配置 WebP 压缩、多尺寸变体与默认原图保留策略。'],
      upload: ['上传限制', '配置单文件大小上限和服务器实际允许的媒体类型。'],
      ai: ['AI 生图', 'Key 仅加密保存在服务端，浏览器永远不会读到明文。'],
      security: ['安全状态', '沿用 ASTREVA 统一 2FA 鉴权，不建立第二套登录。'],
      runtime: ['运行环境', '当前 Node 后台、数据库、存储与宿主机信息。']
    };
    $('#settingModalTitle').textContent = titles[type]?.[0] || '系统设置';
    $('#settingModalSubtitle').textContent = titles[type]?.[1] || '';
    let body = '';
    if (type === 'storage') {
      body = `<form class="settings-form" data-setting-form="storage"><label class="wide">S3 / R2 Endpoint<input name="endpoint" required value="${inputValue(storage.endpoint)}" placeholder="https://账户ID.r2.cloudflarestorage.com"></label><label>Region<input name="region" required value="${inputValue(storage.region || 'auto')}"></label><label>Bucket<input name="bucket" required value="${inputValue(storage.bucket)}"></label><label class="wide">Public Base URL<input name="publicBaseUrl" required value="${inputValue(storage.publicBaseUrl)}" placeholder="https://img.example.com"></label><label>对象前缀<input name="prefix" value="${inputValue(storage.prefix)}"></label><label>寻址方式<select name="forcePathStyle"><option value="false" ${storage.forcePathStyle ? '' : 'selected'}>虚拟主机（R2 推荐）</option><option value="true" ${storage.forcePathStyle ? 'selected' : ''}>Path Style（MinIO）</option></select></label><label>Access Key ID<input name="accessKeyId" autocomplete="off" placeholder="${inputValue(storage.accessKeyMasked || '留空则不修改')}"></label><label>Secret Access Key<input name="secretAccessKey" type="password" autocomplete="new-password" placeholder="${storage.secretAccessKeyConfigured ? '已配置；留空则不修改' : '尚未配置'}"></label><details><summary>高级存储参数</summary><div class="advanced-fields"><label>Session Token<input name="sessionToken" type="password" autocomplete="new-password" placeholder="${storage.sessionTokenConfigured ? '已配置；留空则不修改' : '可选'}"></label><label>服务端加密标识<input name="serverSideEncryption" value="${inputValue(storage.serverSideEncryption)}" placeholder="例如 AES256（R2 通常留空）"></label><label class="checkbox"><input name="clearSessionToken" type="checkbox">清除已保存的 Session Token</label></div></details><div class="form-tip warning">当前有 ${state.stats.total || 0} 条素材。Endpoint、Bucket、对象前缀或公开域名变更会改变旧 URL，因此已有素材时后台会阻止直接切换。<button type="button" class="link-button" data-modal-go="guide">不知道字段在哪里？查看 R2 接入指南</button></div><div class="form-actions"><span class="form-state">保存时执行 HeadBucket 真实验证</span><button type="button" class="button" data-close-modal="setting">取消</button><button class="primary">保存并验证</button></div></form>`;
    } else if (type === 'image') {
      body = `<form class="settings-form" data-setting-form="image"><label class="wide">生成宽度（px，逗号分隔）<input name="variantWidths" required value="${inputValue(image.variantWidths)}" placeholder="400,800,1200,1600"></label><label>WebP 压缩质量<input name="webpQuality" type="number" min="40" max="100" required value="${inputValue(image.webpQuality)}"></label><label class="checkbox"><input name="keepOriginal" type="checkbox" ${image.keepOriginal === false ? '' : 'checked'}>智能上传默认保留原图</label><div class="form-tip">新的设置只影响后续上传和替换；已有素材不会被后台静默重编码。</div><div class="form-actions"><span class="form-state">保存后立即生效并持久化</span><button type="button" class="button" data-close-modal="setting">取消</button><button class="primary">保存图片处理配置</button></div></form>`;
    } else if (type === 'upload') {
      const allowed = new Set(String(image.allowedMimeTypes || '').split(',').filter(Boolean));
      body = `<form class="settings-form" data-setting-form="upload"><label>单文件上限（MB）<input name="maxUploadMb" type="number" min="1" max="2048" step="1" required value="${Math.round(Number(image.maxUploadBytes || 0) / 1024 / 1024)}"></label><div></div><div class="wide"><label>允许的媒体 Content-Type</label><div class="mime-options">${supportedMimes.map(([mime, label]) => `<label><input type="checkbox" name="mime" value="${esc(mime)}" ${allowed.has(mime) ? 'checked' : ''}>${esc(label)}<small>${esc(mime)}</small></label>`).join('')}</div></div><div class="form-tip warning">SVG 始终禁止入库，避免可执行脚本进入官网素材域。预签名直传还会校验对象大小、Content-Type 与文件魔数。</div><div class="form-actions"><span class="form-state">限制同时作用于后台上传与 R2 直传</span><button type="button" class="button" data-close-modal="setting">取消</button><button class="primary">保存上传限制</button></div></form>`;
    } else if (type === 'ai') {
      body = `<form class="settings-form" data-setting-form="ai"><label class="wide">Images API Base URL<input name="baseUrl" required value="${inputValue(ai.baseUrl)}" placeholder="https://api.openai.com/v1"></label><label>API Key<input name="apiKey" type="password" autocomplete="new-password" placeholder="${ai.apiKeyConfigured ? `已配置 ${inputValue(ai.apiKeyMasked)}；留空不修改` : '尚未配置'}"></label><label>默认模型<input name="defaultModel" list="aiModelOptions" required value="${inputValue(ai.defaultModel)}"><datalist id="aiModelOptions">${String(ai.configuredModels || '').split(',').filter(Boolean).map((model) => `<option value="${esc(model)}"></option>`).join('')}</datalist></label><label class="wide">允许的模型（逗号分隔）<input name="configuredModels" value="${inputValue(ai.configuredModels)}"></label><label>请求超时（毫秒）<input name="requestTimeoutMs" type="number" min="10000" max="600000" value="${inputValue(ai.requestTimeoutMs)}"></label><label class="checkbox"><input name="clearApiKey" type="checkbox">清除已保存的 AI API Key</label><details><summary>高级网络白名单</summary><div class="advanced-fields"><label>API 请求主机白名单<input name="allowedHosts" value="${inputValue(ai.allowedHosts)}"></label><label>图片下载主机白名单<input name="downloadHosts" value="${inputValue(ai.downloadHosts)}"></label></div></details><div class="form-tip">AI 生图与客服接待 AI 完全分开。测试连接只访问模型接口，不会生成图片或产生生图费用。</div><div class="form-actions"><span class="form-state">${ai.apiKeyConfigured ? 'API Key 已加密保存' : '请填写 API Key'}</span><button type="button" class="button" data-test-ai>测试并同步模型</button><button class="primary">保存 AI 配置</button></div></form>`;
    } else if (type === 'security') {
      body = `<dl class="readonly-facts"><div><dt>管理员鉴权</dt><dd>ASTREVA 统一 2FA Cookie</dd></div><div><dt>第二套账号 / JWT</dt><dd>未启用</dd></div><div><dt>会话 Cookie</dt><dd>HttpOnly · SameSite=Lax</dd></div><div><dt>运行配置</dt><dd>PostgreSQL site_runtime_settings</dd></div><div><dt>密钥加密</dt><dd>AES-256-GCM</dd></div><div><dt>敏感操作审计</dt><dd>上传、删除、配置变更均记录</dd></div><div><dt>密钥页面回显</dt><dd>只显示末四位掩码，不返回明文</dd></div></dl><div class="form-tip">登录和 2FA 管理由统一后台负责。素材中心复用同一个会话，避免出现第二套密码、权限和退出状态。</div>`;
    }
    $('#settingModalBody').innerHTML = body;
    openModal('setting');
  }

  function aiPayload(form) {
    const data = Object.fromEntries(new FormData(form).entries());
    data.requestTimeoutMs = Number(data.requestTimeoutMs);
    data.clearApiKey = form.elements.clearApiKey.checked;
    return data;
  }

  async function submitSetting(form) {
    const type = form.dataset.settingForm;
    const button = form.querySelector('button[type="submit"], button.primary:last-child');
    const status = form.querySelector('.form-state');
    button.disabled = true;
    status.textContent = '正在验证并保存…';
    try {
      let path;
      let payload;
      if (type === 'storage') {
        path = '/storage';
        payload = Object.fromEntries(new FormData(form).entries());
        payload.forcePathStyle = payload.forcePathStyle === 'true';
        payload.clearSessionToken = form.elements.clearSessionToken.checked;
      } else if (type === 'image') {
        path = '/image';
        payload = Object.fromEntries(new FormData(form).entries());
        payload.webpQuality = Number(payload.webpQuality);
        payload.keepOriginal = form.elements.keepOriginal.checked;
      } else if (type === 'upload') {
        path = '/image';
        payload = { maxUploadMb: Number(form.elements.maxUploadMb.value), allowedMimeTypes: [...form.querySelectorAll('[name="mime"]:checked')].map((input) => input.value).join(',') };
      } else {
        path = '/ai-image';
        payload = aiPayload(form);
      }
      state.settings = (await request(`/site-admin-api/system/media-settings${path}`, jsonOptions('PATCH', payload))).data;
      status.textContent = '保存成功，当前进程已立即生效。';
      status.className = 'form-state success';
      toast('配置已保存并立即生效；重启后仍会保留。');
      renderSettings();
      renderAiStatus();
      setTimeout(() => closeModal('setting'), 450);
      if (type === 'ai') await loadAiModels(false);
      if (['image', 'upload'].includes(type)) setUploadMode(state.uploadMode);
    } catch (error) {
      status.textContent = error.message;
      status.className = 'form-state error';
      toast(error.message, true);
    } finally { button.disabled = false; }
  }

  async function testAiSetting(form, button) {
    const status = form.querySelector('.form-state');
    button.disabled = true;
    status.textContent = '正在请求模型接口验证…';
    try {
      const capability = (await request('/site-admin-api/system/media-settings/ai-image/test', jsonOptions('POST', aiPayload(form)))).data;
      state.aiModels = capability.models || [];
      const list = form.querySelector('#aiModelOptions');
      if (list) list.innerHTML = state.aiModels.map((model) => `<option value="${esc(model)}"></option>`).join('');
      status.textContent = `验证通过，发现 ${state.aiModels.length} 个可用模型。`;
      status.className = 'form-state success';
      toast('AI 接口验证通过。');
    } catch (error) {
      status.textContent = error.message;
      status.className = 'form-state error';
      toast(error.message, true);
    } finally { button.disabled = false; }
  }

  async function refreshCurrent() {
    const button = $('#refreshAll');
    button.disabled = true;
    try {
      if (state.section === 'dashboard') await Promise.all([loadStats(), loadRuntime(), loadCategories()]);
      else if (state.section === 'gallery') await Promise.all([loadMedia(), loadStats(), loadCategories()]);
      else if (state.section === 'ai') await Promise.all([loadAiModels(true), loadCategories()]);
      else if (state.section === 'settings') await Promise.all([loadSettings(), loadRuntime()]);
      toast('当前页面已刷新。');
    } catch (error) { toast(error.message, true); }
    finally { button.disabled = false; }
  }

  $$('.module-nav [data-section]').forEach((button) => button.addEventListener('click', () => navigate(button.dataset.section)));
  document.addEventListener('click', (event) => {
    const go = event.target.closest('[data-go]');
    if (go) navigate(go.dataset.go);
    const open = event.target.closest('[data-open-setting]');
    if (open) setTimeout(() => openSetting(open.dataset.openSetting).catch((error) => toast(error.message, true)));
    const modalGo = event.target.closest('[data-modal-go]');
    if (modalGo) { closeModal('setting'); navigate(modalGo.dataset.modalGo); }
    const close = event.target.closest('[data-close-modal]');
    if (close) closeModal(close.dataset.closeModal);
  });
  $('#refreshAll').addEventListener('click', refreshCurrent);
  $('#logout').addEventListener('click', async () => { await fetch('/site-admin-auth/logout', { method: 'POST' }); location.href = '/admin-2fa'; });

  $('#openUpload').addEventListener('click', () => { state.uploadQueue = []; renderUploadQueue(); $('#uploadProgress').textContent = ''; setUploadMode('smart'); openModal('upload'); });
  $('#openCategories').addEventListener('click', () => openModal('category'));
  $('#uploadModes').addEventListener('click', (event) => { const button = event.target.closest('[data-mode]'); if (button) setUploadMode(button.dataset.mode); });
  $('#uploadFiles').addEventListener('change', (event) => { addUploadFiles([...event.target.files]); event.target.value = ''; });
  $('#uploadQueue').addEventListener('click', (event) => { const button = event.target.closest('[data-remove-queue]'); if (!button) return; state.uploadQueue.splice(Number(button.dataset.removeQueue), 1); renderUploadQueue(); });
  $('#uploadButton').addEventListener('click', () => runUpload().catch((error) => toast(error.message, true)));
  const dropZone = $('#dropZone');
  ['dragenter', 'dragover'].forEach((name) => dropZone.addEventListener(name, (event) => { event.preventDefault(); dropZone.classList.add('drag'); }));
  ['dragleave', 'drop'].forEach((name) => dropZone.addEventListener(name, (event) => { event.preventDefault(); dropZone.classList.remove('drag'); if (name === 'drop') addUploadFiles([...event.dataTransfer.files]); }));

  $('#categoryTabs').addEventListener('click', (event) => { const button = event.target.closest('[data-filter-category]'); if (!button) return; state.filterCategory = button.dataset.filterCategory; state.meta.page = 1; renderCategories(); loadMedia().catch((error) => toast(error.message, true)); });
  $('#clearFilters').addEventListener('click', () => { $('#searchInput').value = ''; $('#filterType').value = ''; state.filterCategory = ''; state.meta.page = 1; renderCategories(); loadMedia().catch((error) => toast(error.message, true)); });
  let searchTimer;
  $('#searchInput').addEventListener('input', () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => { state.meta.page = 1; loadMedia().catch((error) => toast(error.message, true)); }, 300); });
  $('#filterType').addEventListener('change', () => { state.meta.page = 1; loadMedia().catch((error) => toast(error.message, true)); });
  $('#previousPage').addEventListener('click', () => { state.meta.page -= 1; loadMedia().catch((error) => toast(error.message, true)); });
  $('#nextPage').addEventListener('click', () => { state.meta.page += 1; loadMedia().catch((error) => toast(error.message, true)); });
  $$('[data-view]').forEach((button) => button.addEventListener('click', () => { state.view = button.dataset.view; $$('[data-view]').forEach((entry) => entry.classList.toggle('active', entry === button)); renderMedia(); }));
  $('#batchDelete').addEventListener('click', () => batchDelete().catch((error) => toast(error.message, true)));

  $('#mediaGrid').addEventListener('change', (event) => {
    const input = event.target.closest('[data-select-media]');
    if (!input) return;
    if (input.checked) state.selected.add(input.dataset.selectMedia); else state.selected.delete(input.dataset.selectMedia);
    renderMedia();
  });
  $('#mediaGrid').addEventListener('click', (event) => {
    if (event.target.closest('[data-select-media]')) return;
    const detail = event.target.closest('[data-detail-media]');
    const copy = event.target.closest('[data-copy-url]');
    const remove = event.target.closest('[data-delete-media]');
    if (detail) openDetail(detail.dataset.detailMedia).catch((error) => toast(error.message, true));
    if (copy) { const item = state.items.find((entry) => entry.id === copy.dataset.copyUrl); if (item) copyText(item.url).catch((error) => toast(error.message, true)); }
    if (remove) deleteMedia(remove.dataset.deleteMedia).catch((error) => toast(error.message, true));
  });

  $('#detailContent').addEventListener('click', (event) => {
    const item = state.detail;
    if (!item) return;
    const copy = event.target.closest('[data-detail-copy]');
    if (copy?.dataset.detailCopy === 'url') copyText(item.url).catch((error) => toast(error.message, true));
    if (copy?.dataset.detailCopy === 'markdown') copyText(`![${item.name}](${item.url})`).catch((error) => toast(error.message, true));
    if (copy?.dataset.detailCopy === 'srcset') copyText(srcset(item)).catch((error) => toast(error.message, true));
    const variant = event.target.closest('[data-copy-variant]');
    if (variant) copyText(state.detailVariants[Number(variant.dataset.copyVariant)]?.url || '').catch((error) => toast(error.message, true));
    if (event.target.closest('[data-detail-references]')) showReferences(item.id).catch((error) => toast(error.message, true));
    if (event.target.closest('[data-detail-replace]')) replaceMedia(item);
    if (event.target.closest('[data-detail-delete]')) deleteMedia(item.id).catch((error) => toast(error.message, true));
  });

  $('#categoryForm').addEventListener('submit', async (event) => {
    event.preventDefault();
    const name = $('#categoryName').value.trim();
    if (!name) return;
    try {
      await request('/site-admin-api/media/categories', jsonOptions('POST', { name }));
      $('#categoryName').value = '';
      toast('分类已创建。');
      await Promise.all([loadCategories(), loadStats()]);
    } catch (error) { toast(error.message, true); }
  });
  $('#categoryList').addEventListener('click', async (event) => {
    const button = event.target.closest('[data-delete-category]');
    if (!button || !confirm(`确认删除分类“${button.dataset.deleteCategory}”？其素材会移入“未分类”。`)) return;
    try {
      const result = (await request(`/site-admin-api/media/categories/${encodeURIComponent(button.dataset.deleteCategory)}`, { method: 'DELETE' })).data;
      toast(`分类已删除，${result.reassigned} 个素材已重新归类。`);
      await Promise.all([loadCategories(), loadStats(), loadMedia()]);
    } catch (error) { toast(error.message, true); }
  });

  $('#aiPrompt').addEventListener('input', () => { $('#promptCount').textContent = $('#aiPrompt').value.length; });
  $('#generateButton').addEventListener('click', generatePreview);
  $('#aiResults').addEventListener('click', (event) => {
    const upload = event.target.closest('[data-ai-upload]');
    const discard = event.target.closest('[data-ai-discard]');
    if (upload) commitAiPreview(upload.dataset.aiUpload);
    if (discard) discardAiPreview(discard.dataset.aiDiscard);
  });

  $('#refreshSettings').addEventListener('click', () => Promise.all([loadSettings(), loadRuntime()]).then(() => toast('配置已刷新。')).catch((error) => toast(error.message, true)));
  $('#refreshRuntime').addEventListener('click', () => loadRuntime().then(() => toast('运行状态已刷新。')).catch((error) => toast(error.message, true)));
  $('#settingModalBody').addEventListener('submit', (event) => { const form = event.target.closest('[data-setting-form]'); if (!form) return; event.preventDefault(); submitSetting(form); });
  $('#settingModalBody').addEventListener('click', (event) => { const button = event.target.closest('[data-test-ai]'); if (button) testAiSetting(button.form, button); });
  $('#copyCors').addEventListener('click', () => copyText($('#corsExample').textContent).catch((error) => toast(error.message, true)));
  document.addEventListener('keydown', (event) => { if (event.key === 'Escape') $$('.modal').filter((modal) => !modal.hidden).forEach((modal) => closeModal(modal.id.replace('Modal', ''))); });
  window.addEventListener('hashchange', () => navigate(location.hash.slice(1), false));
  window.addEventListener('unhandledrejection', (event) => { if (event.reason?.message) toast(event.reason.message, true); });

  async function initialize() {
    navigate(location.hash.slice(1) || 'dashboard', false);
    const results = await Promise.allSettled([loadSettings(), loadRuntime(), loadStats(), loadCategories()]);
    const failed = results.find((result) => result.status === 'rejected');
    if (failed) toast(failed.reason?.message || '部分素材数据加载失败。', true);
    await Promise.allSettled([loadMedia(), loadAiModels(false)]);
    setUploadMode('smart');
  }

  initialize().catch((error) => toast(error.message, true));
  setInterval(() => {
    if (document.visibilityState === 'visible' && state.section === 'dashboard') loadRuntime().catch(() => {});
  }, 2_000);
  setInterval(() => {
    if (document.visibilityState === 'visible' && state.section === 'dashboard') Promise.allSettled([loadStats(), loadCategories()]);
  }, 15_000);
})();
