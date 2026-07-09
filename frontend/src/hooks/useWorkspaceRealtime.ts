import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react'

import { resolveWebSocketUrl } from '../lib/api'
import type {
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
  setDraftBuffers: Dispatch<SetStateAction<Record<string, string>>>
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
  setDraftBuffers,
  setTimelineByConversation,
  setMessagesLoading,
  setSelectedConversationId,
}: UseWorkspaceRealtimeParams) {
  const conversationsRef = useRef<Conversation[]>([])
  const selectedConversationIdRef = useRef('')
  const socketRef = useRef<WebSocket | null>(null)
  const subscribedConversationIdRef = useRef('')
  const commandSeqRef = useRef(0)

  function resolvePreferredConversationId(items: Conversation[]) {
    const requested = selectedConversationIdRef.current || localStorage.getItem(STORAGE_KEY) || ''
    if (requested && items.some((item) => item.id === requested)) {
      return requested
    }
    return items[0]?.id ?? ''
  }

  function sendJSON(payload: unknown) {
    if (socketRef.current?.readyState !== WebSocket.OPEN) return
    socketRef.current.send(JSON.stringify(payload))
  }

  function sendWorkspaceCommand(command: Omit<WorkspaceCommand, 'command_id'>) {
    const commandId = `cmd_${Date.now()}_${(commandSeqRef.current += 1)}`
    sendJSON({
      type: 'workspace.command',
      command: {
        command_id: commandId,
        ...command,
      },
    })
    return commandId
  }

  function subscribeConversation(conversationId: string) {
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
  }

  function applySelectedConversation(nextConversationId: string) {
    selectedConversationIdRef.current = nextConversationId
    setSelectedConversationId((current) => (current === nextConversationId ? current : nextConversationId))
    if (nextConversationId) {
      localStorage.setItem(STORAGE_KEY, nextConversationId)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
    subscribeConversation(nextConversationId)
  }

  useEffect(() => {
    conversationsRef.current = conversations
  }, [conversations])

  useEffect(() => {
    selectedConversationIdRef.current = selectedConversationId
  }, [selectedConversationId])

  useEffect(() => {
    setDraftBuffers((prev) => {
      let changed = false
      const next = { ...prev }
      for (const conversation of conversations) {
        const draftText = conversation.draft_text
        if (typeof draftText !== 'string') continue
        if (draftText) {
          if (next[conversation.id] !== draftText) {
            next[conversation.id] = draftText
            changed = true
          }
        } else if (next[conversation.id]) {
          delete next[conversation.id]
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [conversations, setDraftBuffers])

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

        if (payload.type === 'workspace.command_ack' || payload.type === 'workspace.command_error') {
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
      window.clearTimeout(reconnectTimer)
      socketRef.current?.close()
      socketRef.current = null
      subscribedConversationIdRef.current = ''
    }
  }, [
    authenticated,
    onConnectionCountChange,
    setConversations,
    setMessagesLoading,
    setSelectedConversationId,
    setTimelineByConversation,
  ])

  return {
    applySelectedConversation,
    sendWorkspaceCommand,
  }
}
