import { App } from 'antd'

type MessageApi = ReturnType<typeof App.useApp>['message']
type MessageMethod = 'success' | 'error' | 'info' | 'warning' | 'loading' | 'open'
type PendingCall = {
  method: MessageMethod
  args: unknown[]
}

let messageApi: MessageApi | null = null
const pendingCalls: PendingCall[] = []

function flushPendingCalls() {
  if (!messageApi || pendingCalls.length === 0) return
  const calls = pendingCalls.splice(0, pendingCalls.length)
  for (const call of calls) {
    ;(messageApi[call.method] as (...args: unknown[]) => void)(...call.args)
  }
}

function invokeMessage(method: MessageMethod, ...args: unknown[]) {
  if (!messageApi) {
    pendingCalls.push({ method, args })
    return
  }
  return (messageApi[method] as (...innerArgs: unknown[]) => void)(...args)
}

export function bindMessageApi(api: MessageApi) {
  messageApi = api
  flushPendingCalls()
  return () => {
    if (messageApi === api) messageApi = null
  }
}

export const appMessage = {
  destroy(key?: string) {
    messageApi?.destroy(key)
  },
  error(...args: unknown[]) {
    return invokeMessage('error', ...args)
  },
  info(...args: unknown[]) {
    return invokeMessage('info', ...args)
  },
  loading(...args: unknown[]) {
    return invokeMessage('loading', ...args)
  },
  open(...args: unknown[]) {
    return invokeMessage('open', ...args)
  },
  success(...args: unknown[]) {
    return invokeMessage('success', ...args)
  },
  warning(...args: unknown[]) {
    return invokeMessage('warning', ...args)
  },
}
