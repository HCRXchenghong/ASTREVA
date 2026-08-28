const proxyRoot = '/site-admin-api/servicebridge';

async function request(url, options = {}) {
  const response = await fetch(url, {
    ...options,
    headers: { ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.headers || {}) }
  });
  const text = await response.text();
  let payload = {};
  try { payload = text ? JSON.parse(text) : {}; } catch { payload = { message: text }; }
  if (response.status === 401) {
    location.href = '/admin-2fa?returnTo=' + encodeURIComponent(location.pathname + location.search);
    throw new Error('登录已失效，正在返回登录页。');
  }
  if (!response.ok) {
    const message = typeof payload.error === 'string'
      ? payload.message || payload.error
      : payload.error?.message || payload.message;
    throw new Error(message || `请求失败（HTTP ${response.status}）`);
  }
  return payload;
}

const api = (path, options = {}) => request(`${proxyRoot}${path}`, options);
const mediaSettingsApi = (path = '', options = {}) => request(`/site-admin-api/system/media-settings${path}`, options);

let noticeTimer;
function notify(message, error = false) {
  const notice = document.querySelector('#notice');
  notice.textContent = message;
  notice.className = `notice show${error ? ' error' : ''}`;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { notice.className = 'notice'; }, 3600);
}

function formObject(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function setFormState(form, message, type = '') {
  const state = form.querySelector('.form-state');
  state.textContent = message;
  state.className = `form-state${type ? ` ${type}` : ''}`;
}

function fillModelSelect(select, models, preferred) {
  const values = [...new Set([preferred, ...(models || [])].map((item) => String(item || '').trim()).filter(Boolean))];
  select.innerHTML = values.map((model) => `<option value="${model.replace(/[&<>"]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[char])}">${model.replace(/[&<>]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[char])}</option>`).join('');
  if (preferred && values.includes(preferred)) select.value = preferred;
}

async function submitForm(form, work, successMessage) {
  const button = form.querySelector('button[type="submit"]');
  button.disabled = true;
  setFormState(form, '正在保存…');
  try {
    await work();
    setFormState(form, successMessage, 'success');
    notify(successMessage);
  } catch (error) {
    setFormState(form, error.message, 'error');
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
}

document.querySelector('#businessForm').addEventListener('submit', (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  submitForm(form, async () => {
    const payload = formObject(form);
    payload.enabled = payload.enabled === 'true';
    await api('/api/admin/business-hours', { method: 'PATCH', body: JSON.stringify(payload) });
  }, '营业时间已保存');
});

document.querySelector('#contactForm').addEventListener('submit', (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  submitForm(form, async () => {
    await api('/api/admin/contact-settings', { method: 'PATCH', body: JSON.stringify(formObject(form)) });
  }, '访客配置已保存');
});

document.querySelector('#aiForm').addEventListener('submit', (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  submitForm(form, async () => {
    const payload = formObject(form);
    payload.enabled = payload.enabled === 'true';
    for (const key of ['temperature', 'timeout_seconds', 'max_output_tokens']) {
      payload[key] = Number(payload[key]);
    }
    if (!payload.api_key) delete payload.api_key;
    await api('/api/admin/ai-settings', { method: 'PATCH', body: JSON.stringify(payload) });
  }, 'AI 设置已保存');
});

document.querySelector('#fetchAiModels').addEventListener('click', async (event) => {
  const form = event.currentTarget.form;
  const button = event.currentTarget;
  button.disabled = true;
  setFormState(form, '正在从上游接口获取模型…');
  try {
    const payload = {
      base_url: form.elements.base_url.value,
      api_key: form.elements.api_key.value,
      api_type: form.elements.api_type.value,
      timeout_seconds: Number(form.elements.timeout_seconds.value)
    };
    const response = await api('/api/admin/ai-settings/models', { method: 'POST', body: JSON.stringify(payload) });
    fillModelSelect(form.elements.model, response.models || [], form.elements.model.value);
    setFormState(form, `已获取 ${response.models?.length || 0} 个上游模型，请选择后保存`, 'success');
    notify('上游 AI 模型已更新');
  } catch (error) {
    setFormState(form, error.message, 'error');
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
});

document.querySelector('#storageForm').addEventListener('submit', (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  submitForm(form, async () => {
    const payload = formObject(form);
    payload.forcePathStyle = payload.forcePathStyle === 'true';
    payload.clearSessionToken = form.elements.clearSessionToken.checked;
    await mediaSettingsApi('/storage', { method: 'PATCH', body: JSON.stringify(payload) });
    form.elements.accessKeyId.value = '';
    form.elements.secretAccessKey.value = '';
    form.elements.sessionToken.value = '';
    form.elements.clearSessionToken.checked = false;
  }, 'R2/S3 已真实连通并加密保存；当前已生效，后台重启后仍会生效');
});

function aiImagePayload(form) {
  const payload = formObject(form);
  payload.requestTimeoutMs = Number(payload.requestTimeoutMs);
  payload.clearApiKey = form.elements.clearApiKey.checked;
  return payload;
}

document.querySelector('#aiImageForm').addEventListener('submit', (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  submitForm(form, async () => {
    await mediaSettingsApi('/ai-image', { method: 'PATCH', body: JSON.stringify(aiImagePayload(form)) });
    form.elements.apiKey.value = '';
    form.elements.clearApiKey.checked = false;
  }, 'AI 生图配置已加密保存；当前已生效，后台重启后仍会生效');
});

document.querySelector('#fetchAiImageModels').addEventListener('click', async (event) => {
  const form = event.currentTarget.form;
  const button = event.currentTarget;
  button.disabled = true;
  setFormState(form, '正在从上游生图接口获取模型…');
  try {
    const response = await mediaSettingsApi('/ai-image/test', { method: 'POST', body: JSON.stringify(aiImagePayload(form)) });
    const models = response.data?.models || [];
    const current = form.elements.defaultModel.value;
    fillModelSelect(form.elements.defaultModel, models, current);
    form.elements.configuredModels.value = [...new Set(models)].join(',');
    setFormState(form, `已获取 ${models.length} 个上游生图模型，请选择后保存`, 'success');
    notify('上游生图模型已更新');
  } catch (error) {
    setFormState(form, error.message, 'error');
    notify(error.message, true);
  } finally {
    button.disabled = false;
  }
});

document.querySelector('#refreshSettings').addEventListener('click', () => location.reload());
window.addEventListener('unhandledrejection', (event) => notify(event.reason?.message || '后台请求失败', true));
