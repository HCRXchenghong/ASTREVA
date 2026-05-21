import { clearAuth, getAPIBase, getToken } from './api.js'

let socketTask = null
let listeners = []
let stateListeners = []
let reconnectEnabled = true
let realtimeState = {
  connected: false,
  connecting: false,
  last_error: ''
}

function wsBase() {
  const base = getAPIBase()
  if (base.indexOf('https://') === 0) return base.replace('https://', 'wss://')
  if (base.indexOf('http://') === 0) return base.replace('http://', 'ws://')
  return 'ws://localhost:8080'
}

export function connectRealtime() {
  const token = getToken()
  if (!token) return
  if (socketTask) return socketTask
  reconnectEnabled = true
  updateRealtimeState({ connected: false, connecting: true, last_error: '' })

  socketTask = uni.connectSocket({
    url: `${wsBase()}/ws?role=admin&token=${encodeURIComponent(token)}`,
    complete: () => {}
  })

  socketTask.onOpen(() => {
    updateRealtimeState({ connected: true, connecting: false, last_error: '' })
  })

  socketTask.onError((err) => {
    updateRealtimeState({
      connected: false,
      connecting: false,
      last_error: (err && err.errMsg) || 'WebSocket 连接失败'
    })
  })

  socketTask.onMessage((res) => {
    let payload
    try {
      payload = JSON.parse(res.data)
    } catch (err) {
      return
    }
    if (payload.event === 'session.revoked') {
      handleSessionRevoked()
      return
    }
    listeners.forEach((listener) => listener(payload))
  })

  socketTask.onClose(() => {
    socketTask = null
    updateRealtimeState({ connected: false, connecting: false })
    if (reconnectEnabled) setTimeout(connectRealtime, 2000)
  })

  return socketTask
}

export function disconnectRealtime() {
  reconnectEnabled = false
  if (socketTask) {
    const task = socketTask
    socketTask = null
    task.close({})
  }
  listeners = []
  updateRealtimeState({ connected: false, connecting: false, last_error: '' })
}

function handleSessionRevoked() {
  reconnectEnabled = false
  clearAuth()
  if (socketTask) {
    const task = socketTask
    socketTask = null
    task.close({})
  }
  updateRealtimeState({ connected: false, connecting: false, last_error: '登录会话已失效' })
  uni.showToast({ title: '登录已失效，请重新登录', icon: 'none' })
  setTimeout(() => {
    uni.reLaunch({ url: '/pages/login/login' })
  }, 500)
}

export function onRealtime(listener) {
  listeners.push(listener)
  return () => {
    listeners = listeners.filter((item) => item !== listener)
  }
}

export function getRealtimeState() {
  return Object.assign({}, realtimeState)
}

export function onRealtimeState(listener) {
  stateListeners.push(listener)
  listener(getRealtimeState())
  return () => {
    stateListeners = stateListeners.filter((item) => item !== listener)
  }
}

function updateRealtimeState(patch) {
  realtimeState = Object.assign({}, realtimeState, patch || {})
  stateListeners.forEach((listener) => listener(getRealtimeState()))
}
