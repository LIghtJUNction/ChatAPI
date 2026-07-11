import { useEffect, useRef, useState } from 'react'
import { Form } from 'antd'

import { appMessage } from '../../../lib/antdMessage'
import { requestJson, resolveEventSourceUrl } from '../../../lib/api'
import type { AdminUserHistoryMessage, User } from '../../../types/chat'

type CreateUserValues = {
  username: string
  password: string
  role: string
}

export type AdminRuntimeMetrics = {
  sampled_at: string
  uptime_seconds: number
  cpu_count: number
  goroutines: number
  heap_alloc_bytes: number
  heap_inuse_bytes: number
  sys_bytes: number
}

type MonitoringEvent = {
  type: 'monitor.snapshot' | 'user.connection.updated' | 'system.metrics.updated'
  user_id?: string
  connection_count?: number
  total_connections: number
  user_connections?: Record<string, number>
  metrics?: AdminRuntimeMetrics
  sequence: number
}

export function useUserManagementState(open: boolean) {
  const [users, setUsers] = useState<User[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [totalUsers, setTotalUsers] = useState(0)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [monitoredUserIDs, setMonitoredUserIDs] = useState<string[] | null>(null)
  const connectionCounts = useRef<Record<string, number>>({})
  const monitoringSequence = useRef(0)
  const monitoringGeneration = useRef(0)
  const [monitorConnected, setMonitorConnected] = useState(false)
  const [totalConnections, setTotalConnections] = useState(0)
  const [runtimeMetrics, setRuntimeMetrics] = useState<AdminRuntimeMetrics | null>(null)
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [deletingId, setDeletingId] = useState('')
  const [form] = Form.useForm<CreateUserValues>()

  const [pwModalOpen, setPwModalOpen] = useState(false)
  const [pwUserId, setPwUserId] = useState('')
  const [pwUsername, setPwUsername] = useState('')
  const [pwForm] = Form.useForm<{ password: string; confirmPassword: string }>()
  const [pwSubmitting, setPwSubmitting] = useState(false)

  const [detailModalOpen, setDetailModalOpen] = useState(false)
  const [detailUser, setDetailUser] = useState<User | null>(null)
  const [historyMessages, setHistoryMessages] = useState<AdminUserHistoryMessage[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)

  useEffect(() => {
    if (!open) return
    let active = true

    async function loadUsers() {
      setLoading(true)
      setMonitoredUserIDs(null)
      try {
        const data = await requestJson<{
          ok: boolean
          items: User[]
          page: number
          page_size: number
          total: number
        }>(`/api/admin/users?page=${page}&page_size=${pageSize}`)
        if (!active) return
        const items = Array.isArray(data.items) ? data.items : []
        setTotalUsers(data.total)
        setUsers(items.map((user) => ({
          ...user,
          current_connection_count: connectionCounts.current[user.id] ?? 0,
        })))
        setMonitoredUserIDs(items.map((user) => user.id))
      } catch (error) {
        if (!active) return
        appMessage.error(error instanceof Error ? error.message : '加载用户列表失败')
      } finally {
        if (active) setLoading(false)
      }
    }

    void loadUsers()
    return () => {
      active = false
    }
  }, [open, page, pageSize, reloadVersion])

  useEffect(() => {
    if (!open || monitoredUserIDs === null) return
    const generation = ++monitoringGeneration.current
    let active = true
    monitoringSequence.current = 0
    connectionCounts.current = {}
    const target = new URL(resolveEventSourceUrl('/api/admin/monitor/stream'))
    target.searchParams.set('user_ids', monitoredUserIDs.join(','))
    const source = new EventSource(target.toString(), {
      withCredentials: true,
    })
    source.onopen = () => {
      if (active && monitoringGeneration.current === generation) setMonitorConnected(true)
    }
    source.onerror = () => {
      if (active && monitoringGeneration.current === generation) setMonitorConnected(false)
    }
    const receive = (raw: Event) => {
      if (!active || monitoringGeneration.current !== generation) return
      const event = raw as MessageEvent<string>
      try {
        const payload = JSON.parse(event.data) as MonitoringEvent
        if (payload.sequence < monitoringSequence.current) return
        monitoringSequence.current = payload.sequence
        setTotalConnections(payload.total_connections)
        if (payload.metrics) setRuntimeMetrics(payload.metrics)
        if (payload.user_connections) {
          connectionCounts.current = payload.user_connections
          setUsers((current) => current.map((user) => ({
            ...user,
            current_connection_count: payload.user_connections?.[user.id] ?? 0,
          })))
        }
        if (payload.type === 'user.connection.updated' && payload.user_id) {
          connectionCounts.current = {
            ...connectionCounts.current,
            [payload.user_id]: payload.connection_count ?? 0,
          }
          setUsers((current) => current.map((user) => (
            user.id === payload.user_id
              ? { ...user, current_connection_count: payload.connection_count ?? 0 }
              : user
          )))
        }
      } catch {
        // Ignore malformed monitoring frames; EventSource will continue.
      }
    }
    source.addEventListener('monitor.snapshot', receive)
    source.addEventListener('user.connection.updated', receive)
    source.addEventListener('system.metrics.updated', receive)
    return () => {
      active = false
      source.removeEventListener('monitor.snapshot', receive)
      source.removeEventListener('user.connection.updated', receive)
      source.removeEventListener('system.metrics.updated', receive)
      source.close()
      setMonitorConnected(false)
    }
  }, [open, monitoredUserIDs])

  async function handleCreate(values: CreateUserValues) {
    setCreating(true)
    try {
      await requestJson<{ ok: boolean; user: User }>('/api/admin/users', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      form.resetFields()
      setPage(1)
      setReloadVersion((current) => current + 1)
      appMessage.success('用户已创建')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '创建用户失败')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(userId: string) {
    setDeletingId(userId)
    try {
      await requestJson(`/api/admin/users/${userId}`, { method: 'DELETE' })
      if (users.length === 1 && page > 1) setPage((current) => current - 1)
      else setReloadVersion((current) => current + 1)
      if (detailUser?.id === userId) {
        closeDetailModal()
      }
      appMessage.success('用户已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除用户失败')
    } finally {
      setDeletingId('')
    }
  }

  function openPasswordModal(user: User) {
    setPwUserId(user.id)
    setPwUsername(user.username)
    pwForm.resetFields()
    setPwModalOpen(true)
  }

  function closePasswordModal() {
    setPwModalOpen(false)
  }

  function openDetailModal(user: User) {
    setDetailUser(user)
    setHistoryMessages([])
    setHistoryLoading(false)
    setDetailModalOpen(true)
  }

  function closeDetailModal() {
    setDetailModalOpen(false)
    setDetailUser(null)
    setHistoryMessages([])
  }

  async function handlePasswordChange() {
    try {
      const values = await pwForm.validateFields()
      setPwSubmitting(true)
      await requestJson(`/api/admin/users/${pwUserId}/password`, {
        method: 'PUT',
        body: JSON.stringify({ password: values.password }),
      })
      appMessage.success(`已修改 ${pwUsername} 的密码`)
      setPwModalOpen(false)
    } catch (error) {
      if (error instanceof Error) {
        appMessage.error(error.message)
      }
    } finally {
      setPwSubmitting(false)
    }
  }

  return {
    creating,
    deletingId,
    detailModalOpen,
    detailUser,
    form,
    handleCreate,
    handleDelete,
    handlePasswordChange,
    historyLoading,
    historyMessages,
    loading,
    monitorConnected,
    page,
    pageSize,
    openDetailModal,
    openPasswordModal,
    pwForm,
    pwModalOpen,
    pwSubmitting,
    pwUsername,
    setPwModalOpen: closePasswordModal,
    users,
    totalConnections,
    runtimeMetrics,
    setPage,
    setPageSize,
    totalUsers,
    closeDetailModal,
  }
}
