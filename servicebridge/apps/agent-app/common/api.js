const API_BASE_KEY = 'agent_api_base'
const TOKEN_KEY = 'agent_token'
const AGENT_KEY = 'agent_profile'
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

export function getAgent() {
  return uni.getStorageSync(AGENT_KEY)
}

export function clearAuth() {
  uni.removeStorageSync(TOKEN_KEY)
  uni.removeStorageSync(AGENT_KEY)
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
  const data = await request('/api/agent/login', {
    method: 'POST',
    data: { account, password }
  })
  uni.setStorageSync(TOKEN_KEY, data.token)
  uni.setStorageSync(AGENT_KEY, data.agent)
  return data
}

export function setStatus(status) {
  return request('/api/agent/status', {
    method: 'POST',
    data: { status }
  })
}

export function fetchConversations() {
  return request('/api/agent/conversations')
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

export function fetchMessages(conversationId, options = {}) {
  const query = []
  if (options.limit) query.push(`limit=${encodeURIComponent(options.limit)}`)
  if (options.before) query.push(`before=${encodeURIComponent(options.before)}`)
  return request(`/api/conversations/${conversationId}/messages${query.length ? '?' + query.join('&') : ''}`)
}

export function markConversationRead(conversationId) {
  return request(`/api/conversations/${conversationId}/read`, {
    method: 'POST'
  })
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

export function revokeMessage(conversationId, messageId) {
  return request(`/api/conversations/${conversationId}/messages/${messageId}/revoke`, {
    method: 'POST'
  })
}

export function deleteConversation(conversationId) {
  return request(`/api/conversations/${conversationId}`, {
    method: 'DELETE'
  })
}

export function registerPushDevice(data) {
  return request('/api/agent/push-device', {
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
