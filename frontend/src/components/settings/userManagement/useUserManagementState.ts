import { useEffect, useState } from 'react'
import { Form } from 'antd'

import { appMessage } from '../../../lib/antdApp'
import { requestJson } from '../../../lib/api'
import type { AdminUserHistoryMessage, User } from '../../../types/chat'

type CreateUserValues = {
  username: string
  password: string
  role: string
}

export function useUserManagementState(open: boolean) {
  const [users, setUsers] = useState<User[]>([])
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
      try {
        const data = await requestJson<{ ok: boolean; items: User[] }>('/api/admin/users')
        if (!active) return
        setUsers(Array.isArray(data.items) ? data.items : [])
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
  }, [open])

  async function handleCreate(values: CreateUserValues) {
    setCreating(true)
    try {
      const data = await requestJson<{ ok: boolean; user: User }>('/api/admin/users', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      setUsers((prev) => [...prev, data.user])
      form.resetFields()
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
      setUsers((prev) => prev.filter((user) => user.id !== userId))
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
    openDetailModal,
    openPasswordModal,
    pwForm,
    pwModalOpen,
    pwSubmitting,
    pwUsername,
    setPwModalOpen: closePasswordModal,
    users,
    closeDetailModal,
  }
}
