import { useEffect, useMemo, useState } from 'react'
import { Form } from 'antd'

import { appMessage } from '../../../lib/antdMessage'
import { requestJson } from '../../../lib/api'
import type { User } from '../../../types/chat'
import { useAdminMonitoring } from '../../../features/admin-settings/hooks/useAdminMonitoring'

type CreateUserValues = {
  username: string
  password: string
  role: string
}

export function useUserManagementState(open: boolean) {
  const [users, setUsers] = useState<User[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [totalUsers, setTotalUsers] = useState(0)
  const [searchText, setSearchText] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [reloadVersion, setReloadVersion] = useState(0)
  const [monitoredUserIDs, setMonitoredUserIDs] = useState<string[] | null>(null)
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

  useEffect(() => {
    const timeout = window.setTimeout(() => setSearchQuery(searchText.trim()), 300)
    return () => window.clearTimeout(timeout)
  }, [searchText])

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
        }>(`/api/admin/users?${new URLSearchParams({
          page: String(page),
          page_size: String(pageSize),
          ...(searchQuery ? { q: searchQuery } : {}),
        }).toString()}`)
        if (!active) return
        const items = Array.isArray(data.items) ? data.items : []
        setTotalUsers(data.total)
        setUsers(items.map((user) => ({
          ...user,
          current_connection_count: 0,
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
  }, [open, page, pageSize, reloadVersion, searchQuery])

  const monitoring = useAdminMonitoring(open, monitoredUserIDs)
  const usersWithConnections = useMemo(
    () => users.map((user) => ({
      ...user,
      current_connection_count: monitoring.userConnections[user.id] ?? 0,
    })),
    [monitoring.userConnections, users],
  )

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
      return true
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '创建用户失败')
      return false
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

  async function handleRoleChange(user: User, role: 'user' | 'admin') {
	try {
	  await requestJson(`/api/admin/users/${user.id}/role`, {
		method: 'PUT',
		body: JSON.stringify({ role }),
	  })
	  setReloadVersion((current) => current + 1)
	  appMessage.success(role === 'admin' ? `已将 ${user.username} 设为管理员` : `已撤销 ${user.username} 的管理员`)
	} catch (error) {
	  appMessage.error(error instanceof Error ? error.message : '修改角色失败')
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
    setDetailModalOpen(true)
  }

  function closeDetailModal() {
    setDetailModalOpen(false)
    setDetailUser(null)
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
    handleRoleChange,
    loading,
    page,
    pageSize,
    openDetailModal,
    openPasswordModal,
    pwForm,
    pwModalOpen,
    pwSubmitting,
    pwUsername,
    setPwModalOpen: closePasswordModal,
    users: usersWithConnections,
    setPage,
    setPageSize,
    searchText,
    setSearchText: (value: string) => {
      setSearchText(value)
      setPage(1)
    },
    totalUsers,
    closeDetailModal,
  }
}
