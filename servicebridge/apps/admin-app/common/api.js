const API_BASE_KEY = 'admin_api_base'
const TOKEN_KEY = 'admin_token'
const ADMIN_KEY = 'admin_profile'
const DEFAULT_API_BASE = 'http://localhost:8080'

export function getAPIBase() {
  return normalizeAPIBase(uni.getStorageSync(API_BASE_KEY) || runtimeAPIBase() || DEFAULT_API_BASE)
}

export function setAPIBase(value) {
  const normalized = normalizeAPIBase(value)
  if (!normalized) throw new Error('服务地址必须以 http:// 或 https:// 开头')
  uni.setStorageSync(API_BASE_KEY, normalized)
  return normalized
}

export function getToken() {
  return uni.getStorageSync(TOKEN_KEY)
}

export function getAdmin() {
  return uni.getStorageSync(ADMIN_KEY)
}

export function clearAuth() {
  uni.removeStorageSync(TOKEN_KEY)
  uni.removeStorageSync(ADMIN_KEY)
}

function runtimeAPIBase() {
  if (typeof globalThis !== 'undefined' && globalThis.CUSTOMER_SERVICE_API_BASE) {
    return String(globalThis.CUSTOMER_SERVICE_API_BASE)
  }
  return ''
}

function normalizeAPIBase(value) {
  value = String(value || '').trim().replace(/\/+$/, '')
  if (!value) return ''
  if (!/^https?:\/\//i.test(value)) return ''
  return value
}

export async function request(path, options = {}) {
  const token = getToken()
  return new Promise((resolve, reject) => {
    const header = { 'Content-Type': 'application/json' }
    if (token) header.Authorization = `Bearer ${token}`
    const extraHeader = options.header || {}
    Object.keys(extraHeader).forEach((key) => {
      header[key] = extraHeader[key]
    })
    uni.request({
      url: getAPIBase() + path,
      method: options.method || 'GET',
      data: options.data,
      header,
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data)
          return
        }
        if (token && res.statusCode === 401) {
          clearAuth()
          uni.showToast({ title: '登录已失效，请重新登录', icon: 'none' })
          setTimeout(() => {
            uni.reLaunch({ url: '/pages/login/login' })
          }, 500)
        }
        reject(new Error((res.data && res.data.message) || `HTTP ${res.statusCode}`))
      },
      fail: reject
    })
  })
}

export async function login(account, password) {
  const data = await request('/api/admin/login', {
    method: 'POST',
    data: { account, password }
  })
  uni.setStorageSync(TOKEN_KEY, data.token)
  uni.setStorageSync(ADMIN_KEY, data.admin)
  return data
}

export function fetchConversations() {
  return request('/api/admin/conversations')
}

export function fetchDashboard() {
  return request('/api/admin/dashboard')
}

export function fetchSystemStatus() {
  return request('/api/admin/system-status')
}

export function fetchAgents() {
  return request('/api/admin/agents')
}

export function fetchRatingSummary() {
  return request('/api/admin/ratings/summary')
}

export function fetchRatings(limit = 20) {
  return request(`/api/admin/ratings?limit=${limit}`)
}

export function fetchAuditEvents(limit = 50) {
  return request(`/api/admin/audit-events?limit=${limit}`)
}

export function changePassword(currentPassword, newPassword) {
  return request('/api/account/password', {
    method: 'POST',
    data: {
      current_password: currentPassword,
      new_password: newPassword
    }
  })
}

export function exportAdminFile(path, filename) {
  const token = getToken()
  const base = getAPIBase().replace(/\/$/, '')

  if (typeof window !== 'undefined' && window.document) {
    return window.fetch(base + path, {
      headers: token ? { Authorization: `Bearer ${token}` } : {}
    }).then(async (res) => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const objectURL = window.URL.createObjectURL(blob)
      const link = window.document.createElement('a')
      link.href = objectURL
      link.download = filename
      link.style.display = 'none'
      window.document.body.appendChild(link)
      link.click()
      window.document.body.removeChild(link)
      window.URL.revokeObjectURL(objectURL)
      return { path: filename }
    })
  }

  return new Promise((resolve, reject) => {
    uni.downloadFile({
      url: base + path,
      header: token ? { Authorization: `Bearer ${token}` } : {},
      success: (res) => {
        if (res.statusCode < 200 || res.statusCode >= 300) {
          reject(new Error(`HTTP ${res.statusCode}`))
          return
        }
        if (typeof uni.saveFile !== 'function') {
          resolve({ path: res.tempFilePath })
          return
        }
        uni.saveFile({
          tempFilePath: res.tempFilePath,
          success: (saved) => resolve({ path: saved.savedFilePath }),
          fail: () => resolve({ path: res.tempFilePath })
        })
      },
      fail: reject
    })
  })
}

export function exportConversationsCSV() {
  return exportAdminFile('/api/admin/conversations/export.csv', `conversations-${Date.now()}.csv`)
}

export function exportAuditEventsCSV(limit = 500) {
  return exportAdminFile(`/api/admin/audit-events/export.csv?limit=${limit}`, `audit-events-${Date.now()}.csv`)
}

export function createAgent(data) {
  return request('/api/admin/agents', {
    method: 'POST',
    data
  })
}

export function updateAgent(id, data) {
  return request(`/api/admin/agents/${id}`, {
    method: 'PATCH',
    data
  })
}

export function resetAgentPassword(id, password = '') {
  return request(`/api/admin/agents/${id}/reset-password`, {
    method: 'POST',
    data: password ? { password } : {}
  })
}

export function disableAgent(id) {
  return request(`/api/admin/agents/${id}/disable`, {
    method: 'POST'
  })
}

export function deleteAgent(id) {
  return request(`/api/admin/agents/${id}`, {
    method: 'DELETE'
  })
}

export function transferConversation(conversationId, data) {
  return request(`/api/admin/conversations/${conversationId}/transfer`, {
    method: 'POST',
    data
  })
}

export function fetchAISettings() {
  return request('/api/admin/ai-settings')
}

export function updateAISettings(data) {
  return request('/api/admin/ai-settings', {
    method: 'PATCH',
    data
  })
}

export function testAISettings(input) {
  return request('/api/admin/ai-settings/test', {
    method: 'POST',
    data: { input }
  })
}

export function fetchBusinessHours() {
  return request('/api/admin/business-hours')
}

export function updateBusinessHours(data) {
  return request('/api/admin/business-hours', {
    method: 'PATCH',
    data
  })
}

export function updateContactSettings(data) {
  return request('/api/admin/contact-settings', {
    method: 'PATCH',
    data
  })
}

export function fetchKeywordRules() {
  return request('/api/admin/keyword-rules')
}

export function updateKeywordRule(id, data) {
  return request(`/api/admin/keyword-rules/${id}`, {
    method: 'PATCH',
    data
  })
}

export function createKeywordRule(data) {
  return request('/api/admin/keyword-rules', {
    method: 'POST',
    data
  })
}

export function uploadFile(filePath) {
  const token = getToken()
  return new Promise((resolve, reject) => {
    uni.uploadFile({
      url: getAPIBase() + '/api/uploads',
      filePath,
      name: 'file',
      header: token ? { Authorization: `Bearer ${token}` } : {},
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          try {
            const data = JSON.parse(res.data)
            if (data.url && String(data.url).indexOf('/') === 0) {
              data.url = getAPIBase().replace(/\/$/, '') + data.url
            }
            resolve(data)
          } catch (err) {
            reject(err)
          }
          return
        }
        reject(new Error(`HTTP ${res.statusCode}`))
      },
      fail: reject
    })
  })
}

export function fetchMessages(conversationId, options = {}) {
  const query = []
  if (options.limit) query.push(`limit=${encodeURIComponent(options.limit)}`)
  if (options.before) query.push(`before=${encodeURIComponent(options.before)}`)
  return request(`/api/conversations/${conversationId}/messages${query.length ? '?' + query.join('&') : ''}`)
}

export function updateRemark(conversationId, remark) {
  return request(`/api/conversations/${conversationId}/remark`, {
    method: 'PATCH',
    data: { remark }
  })
}

export function closeConversation(conversationId) {
  return request(`/api/conversations/${conversationId}/close`, {
    method: 'POST'
  })
}

export function deleteConversation(conversationId) {
  return request(`/api/conversations/${conversationId}`, {
    method: 'DELETE'
  })
}
