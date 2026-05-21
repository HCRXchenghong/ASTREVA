import { clearAuth, getAPIBase, getToken } from './api.js'

let socketTask = null
let listeners = []
let reconnectEnabled = true
let socketOpen = false
let pendingPackets = []

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
  socketOpen = false

  socketTask = uni.connectSocket({
    url: `${wsBase()}/ws?role=agent&token=${encodeURIComponent(token)}`,
    complete: () => {}
  })

  socketTask.onOpen(() => {
    socketOpen = true
    flushPendingPackets()
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
    socketOpen = false
    if (reconnectEnabled) setTimeout(connectRealtime, 2000)
  })

  return socketTask
}

export function disconnectRealtime() {
  reconnectEnabled = false
  if (socketTask) {
    const task = socketTask
    socketTask = null
    socketOpen = false
    task.close({})
  }
  pendingPackets = []
  listeners = []
}

function handleSessionRevoked() {
  reconnectEnabled = false
  clearAuth()
  if (socketTask) {
    const task = socketTask
    socketTask = null
    socketOpen = false
    task.close({})
  }
  pendingPackets = []
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

function flushPendingPackets() {
  if (!socketTask || !socketOpen || !pendingPackets.length) return
  const queue = pendingPackets.slice()
  pendingPackets = []
  queue.forEach((data) => {
    socketTask.send({ data })
  })
}

function enqueuePacket(payload) {
  const data = JSON.stringify(payload)
  if (!socketTask) connectRealtime()
  if (!socketTask || !socketOpen) {
    pendingPackets.push(data)
    return false
  }
  socketTask.send({ data })
  return true
}

export function sendMessage(conversationId, content, messageType = 'text') {
  const clientMsgId = `agent_${Date.now()}_${Math.random().toString(36).slice(2)}`
  enqueuePacket({
    event: 'message.send',
    data: {
      conversation_id: conversationId,
      client_msg_id: clientMsgId,
      message_type: messageType,
      content
    }
  })
  return clientMsgId
}

export function closeRealtimeConversation(conversationId) {
  enqueuePacket({
    event: 'conversation.close',
    data: { conversation_id: conversationId }
  })
}
