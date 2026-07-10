import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'

import { resolveWebSocketUrl } from '../lib/api'
import type {
  AutomationExecutionState,
  AutomationExecutionStateEvent,
  AutomationRecordAckEvent,
  AutomationRecordErrorEvent,
  AutomationRecordingState,
  AutomationRecordStateEvent,
  Conversation,
  TimelineItem,
  WorkspaceCommand,
  WorkspaceCommandAckEvent,
  WorkspaceCommandErrorEvent,
  WorkspaceConnectionCountEvent,
  WorkspaceConversationDeleteEvent,
  WorkspaceConversationUpsertEvent,
  WorkspaceSnapshotEvent,
  WorkspaceTimelineItemAppendEvent,
  WorkspaceTimelineResetEvent,
} from '../types/chat'

const STORAGE_KEY = 'chatapi.conversationId'

function sortConversations(items: Conversation[]) {
  return [...items].sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at))
}

type UseWorkspaceRealtimeParams = {
  authenticated: boolean
  conversations: Conversation[]
  onConnectionCountChange: (value: number) => void
  selectedConversationId: string
  setConversations: Dispatch<SetStateAction<Conversation[]>>
  setTimelineByConversation: Dispatch<SetStateAction<Record<string, TimelineItem[]>>>
  setMessagesLoading: Dispatch<SetStateAction<boolean>>
  setSelectedConversationId: Dispatch<SetStateAction<string>>
}

export function useWorkspaceRealtime({
  authenticated,
  conversations,
  onConnectionCountChange,
  selectedConversationId,
  setConversations,
  setTimelineByConversation,
  setMessagesLoading,
  setSelectedConversationId,
}: UseWorkspaceRealtimeParams) {
  const conversationsRef = useRef<Conversation[]>([])
  const selectedConversationIdRef = useRef('')
  const socketRef = useRef<WebSocket | null>(null)
  const subscribedConversationIdRef = useRef('')
  const commandSeqRef = useRef(0)
  const automationSnapshotRevisionRef = useRef(0)
  const automationRecordingRevisionRef = useRef(0)
  const automationExecutionRevisionsRef = useRef<Record<string, number>>({})
  const [automationRecording, setAutomationRecording] = useState<AutomationRecordingState>({ revision: 0, active: false, steps: [] })
  const [automationExecutions, setAutomationExecutions] = useState<Record<string, AutomationExecutionState>>({})
  const pendingCommandsRef = useRef(new Map<string, {
    reject: (error: Error) => void
    resolve: (ack: unknown) => void
    timeout: number
  expectedRequestId?: string
  }>())

  const resolvePreferredConversationId = useCallback((items: Conversation[]) => {
    const requested = selectedConversationIdRef.current || localStorage.getItem(STORAGE_KEY) || ''
    if (requested && items.some((item) => item.id === requested)) {
      return requested
    }
    return items[0]?.id ?? ''
  }, [])

  const sendJSON = useCallback((payload: unknown) => {
    if (socketRef.current?.readyState !== WebSocket.OPEN) return
    socketRef.current.send(JSON.stringify(payload))
  }, [])

  const rejectPendingCommands = useCallback((message: string) => {
    for (const pending of pendingCommandsRef.current.values()) {
      window.clearTimeout(pending.timeout)
      pending.reject(new Error(message))
    }
    pendingCommandsRef.current.clear()
  }, [])

  const sendWorkspaceCommand = useCallback((command: Omit<WorkspaceCommand, 'command_id'>) => {
    const commandId = `cmd_${Date.now()}_${(commandSeqRef.current += 1)}`
    const socket = socketRef.current
    if (socket?.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('实时连接尚未就绪'))
    }
    return new Promise<WorkspaceCommandAckEvent>((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        pendingCommandsRef.current.delete(commandId)
        reject(new Error('等待服务端确认超时'))
      }, 15_000)
    pendingCommandsRef.current.set(commandId, {
      reject,
      resolve: (ack) => resolve(ack as WorkspaceCommandAckEvent),
      timeout,
      expectedRequestId: command.request_id,
    })
      try {
        socket.send(JSON.stringify({
          type: 'workspace.command',
          command: {
            command_id: commandId,
            ...command,
          },
        }))
      } catch (error) {
        window.clearTimeout(timeout)
        pendingCommandsRef.current.delete(commandId)
        reject(error instanceof Error ? error : new Error('发送实时命令失败'))
      }
    })
  }, [])

  const sendAutomationRecordCommand = useCallback((
    action: 'start' | 'stop' | 'cancel' | 'get',
    conversationId: string,
  ) => {
    const commandId = `automation_${Date.now()}_${(commandSeqRef.current += 1)}`
    const socket = socketRef.current
    if (socket?.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('实时连接尚未就绪'))
    }
    return new Promise<AutomationRecordAckEvent>((resolve, reject) => {
      const timeout = window.setTimeout(() => {
        pendingCommandsRef.current.delete(commandId)
        reject(new Error('等待服务端确认超时'))
      }, 15_000)
      pendingCommandsRef.current.set(commandId, {
        reject,
        resolve: (ack) => resolve(ack as AutomationRecordAckEvent),
        timeout,
      })
      try {
        socket.send(JSON.stringify({
          type: `automation.record.${action}`,
          command_id: commandId,
          conversation_id: conversationId,
        }))
      } catch (error) {
        window.clearTimeout(timeout)
        pendingCommandsRef.current.delete(commandId)
        reject(error instanceof Error ? error : new Error('发送录制命令失败'))
      }
    })
  }, [])

  const subscribeConversation = useCallback((conversationId: string) => {
    const nextID = conversationId.trim()
    const previousID = subscribedConversationIdRef.current
    if (previousID && previousID !== nextID) {
      sendJSON({ type: 'timeline.unsubscribe', conversation_id: previousID })
    }
    subscribedConversationIdRef.current = nextID
    if (nextID) {
      setMessagesLoading(true)
      sendJSON({ type: 'timeline.subscribe', conversation_id: nextID })
      return
    }
    setMessagesLoading(false)
  }, [sendJSON, setMessagesLoading])

  const applySelectedConversation = useCallback((nextConversationId: string) => {
    selectedConversationIdRef.current = nextConversationId
    setSelectedConversationId((current) => (current === nextConversationId ? current : nextConversationId))
    if (nextConversationId) {
      localStorage.setItem(STORAGE_KEY, nextConversationId)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
    subscribeConversation(nextConversationId)
  }, [setSelectedConversationId, subscribeConversation])

  useEffect(() => {
    conversationsRef.current = conversations
  }, [conversations])

  useEffect(() => {
    selectedConversationIdRef.current = selectedConversationId
  }, [selectedConversationId])

  useEffect(() => {
    if (!authenticated) return
    let active = true
    let reconnectTimer = 0

    function connect() {
      const socket = new WebSocket(resolveWebSocketUrl('/api/ws'))
      socketRef.current = socket
      subscribedConversationIdRef.current = ''
      setMessagesLoading(true)

      socket.addEventListener('open', () => {
        sendJSON({ type: 'workspace.ping' })
        void sendAutomationRecordCommand('get', selectedConversationIdRef.current).catch(() => undefined)
      })

      socket.addEventListener('message', (event) => {
        if (!active) return
        let payload:
          | WorkspaceSnapshotEvent
          | WorkspaceConnectionCountEvent
          | WorkspaceConversationUpsertEvent
          | WorkspaceConversationDeleteEvent
          | WorkspaceTimelineResetEvent
          | WorkspaceTimelineItemAppendEvent
          | WorkspaceCommandAckEvent
          | WorkspaceCommandErrorEvent
          | AutomationRecordAckEvent
          | AutomationRecordErrorEvent
          | AutomationRecordStateEvent
          | AutomationExecutionStateEvent
          | { type: 'disconnect'; reason?: string }
          | { type: 'workspace.ping' }
        try {
          payload = JSON.parse(event.data) as
            | WorkspaceSnapshotEvent
            | WorkspaceConnectionCountEvent
            | WorkspaceConversationUpsertEvent
            | WorkspaceConversationDeleteEvent
            | WorkspaceTimelineResetEvent
            | WorkspaceTimelineItemAppendEvent
            | WorkspaceCommandAckEvent
            | WorkspaceCommandErrorEvent
            | AutomationRecordAckEvent
            | AutomationRecordErrorEvent
            | AutomationRecordStateEvent
            | AutomationExecutionStateEvent
            | { type: 'disconnect'; reason?: string }
            | { type: 'workspace.ping' }
        } catch {
          return
        }

        if (payload.type === 'workspace.ping') return
        if (payload.type === 'disconnect') {
          socket.close()
          return
        }

        if (payload.type === 'workspace.snapshot') {
          const nextConversations = sortConversations(payload.conversations)
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
          return
        }

        if (payload.type === 'workspace.connections') {
          onConnectionCountChange(payload.current_connection_count)
          return
        }

        if (payload.type === 'workspace.command_ack') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
      if (pending.expectedRequestId && pending.expectedRequestId !== payload.request_id) {
        pending.reject(new Error('服务端确认的请求已发生变化'))
        return
      }
            pending.resolve(payload)
          }
          return
        }

        if (payload.type === 'workspace.command_error') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.reject(new Error(payload.message || payload.code))
          }
          return
        }

        if (payload.type === 'automation.record.ack') {
          if (payload.revision >= automationSnapshotRevisionRef.current) {
            automationSnapshotRevisionRef.current = payload.revision
            if (payload.state.revision >= automationRecordingRevisionRef.current) {
              automationRecordingRevisionRef.current = payload.state.revision
              setAutomationRecording(payload.state)
            }
            if (payload.executions) {
              setAutomationExecutions((current) => {
        const next = Object.fromEntries(
          payload.executions!
            .filter((item) => item.revision >= (automationExecutionRevisionsRef.current[item.conversation_id] ?? 0))
            .map((item) => [item.conversation_id, item]),
        )
                for (const [conversationId, item] of Object.entries(current)) {
                  const snapshotItem = next[conversationId]
                  if (item.revision > payload.revision || (snapshotItem && item.revision > snapshotItem.revision)) {
                    next[conversationId] = item
                  }
                }
        for (const [conversationId, item] of Object.entries(next)) {
          automationExecutionRevisionsRef.current[conversationId] = Math.max(
            automationExecutionRevisionsRef.current[conversationId] ?? 0,
            item.revision,
          )
        }
                return next
              })
            }
          }
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.resolve(payload)
          }
          return
        }

        if (payload.type === 'automation.record.error') {
          const pending = pendingCommandsRef.current.get(payload.command_id)
          if (pending) {
            window.clearTimeout(pending.timeout)
            pendingCommandsRef.current.delete(payload.command_id)
            pending.reject(new Error(payload.message || payload.code))
          }
          return
        }

        if (payload.type === 'automation.record.state') {
      if (payload.state.revision < automationRecordingRevisionRef.current) return
      automationRecordingRevisionRef.current = payload.state.revision
          setAutomationRecording(payload.state)
          return
        }

        if (payload.type === 'automation.execution.state') {
          const conversationId = payload.execution.conversation_id
          if (payload.execution.revision < (automationExecutionRevisionsRef.current[conversationId] ?? 0)) return
          automationExecutionRevisionsRef.current[conversationId] = payload.execution.revision
      if (payload.execution.status === 'removed') {
        setAutomationExecutions((current) => {
          const next = { ...current }
          delete next[conversationId]
          return next
        })
        return
      }
          setAutomationExecutions((current) => ({
            ...current,
        [conversationId]: payload.execution,
          }))
          return
        }

        if (payload.type === 'conversation.upsert') {
          const remaining = conversationsRef.current.filter((item) => item.id !== payload.conversation.id)
          const nextConversations = sortConversations([payload.conversation, ...remaining])
          conversationsRef.current = nextConversations
          setConversations(nextConversations)
          if (!selectedConversationIdRef.current) {
            applySelectedConversation(resolvePreferredConversationId(nextConversations))
          }
          return
        }

        if (payload.type === 'timeline.reset') {
          setTimelineByConversation((current) => ({
            ...current,
            [payload.conversation_id]: payload.items,
          }))
          setMessagesLoading(false)
          return
        }

        if (payload.type === 'timeline.append') {
          setTimelineByConversation((current) => {
            const existing = current[payload.conversation_id] ?? []
            if (existing.some((item) => item.id === payload.item.id)) {
              return current
            }
            return {
              ...current,
              [payload.conversation_id]: [...existing, payload.item],
            }
          })
          return
        }

        const nextConversations = conversationsRef.current.filter((item) => item.id !== payload.conversation_id)
        conversationsRef.current = nextConversations
        setConversations(nextConversations)
        setTimelineByConversation((current) => {
          if (!Object.prototype.hasOwnProperty.call(current, payload.conversation_id)) {
            return current
          }
          const next = { ...current }
          delete next[payload.conversation_id]
          return next
        })
        if (selectedConversationIdRef.current === payload.conversation_id) {
          applySelectedConversation(resolvePreferredConversationId(nextConversations))
        }
      })

      socket.addEventListener('close', () => {
        if (!active) return
        rejectPendingCommands('实时连接已断开')
    automationSnapshotRevisionRef.current = 0
    automationRecordingRevisionRef.current = 0
    automationExecutionRevisionsRef.current = {}
    setAutomationRecording({ revision: 0, active: false, steps: [] })
        setAutomationExecutions({})
        if (socketRef.current === socket) {
          socketRef.current = null
        }
        subscribedConversationIdRef.current = ''
        setMessagesLoading(true)
        reconnectTimer = window.setTimeout(() => {
          connect()
        }, 1000)
      })

      socket.addEventListener('error', () => {
        socket.close()
      })
    }

    connect()

    return () => {
      active = false
      rejectPendingCommands('实时连接已关闭')
      window.clearTimeout(reconnectTimer)
      socketRef.current?.close()
      socketRef.current = null
      subscribedConversationIdRef.current = ''
    }
  }, [
    applySelectedConversation,
    authenticated,
    onConnectionCountChange,
    rejectPendingCommands,
    resolvePreferredConversationId,
    sendJSON,
    sendAutomationRecordCommand,
    setConversations,
    setMessagesLoading,
    setSelectedConversationId,
    setTimelineByConversation,
  ])

  return {
    applySelectedConversation,
    automationExecutions,
    automationRecording,
    sendAutomationRecordCommand,
    sendWorkspaceCommand,
  }
}
