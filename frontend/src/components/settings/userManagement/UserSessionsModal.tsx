import { useEffect, useState } from 'react'
import { Button, Descriptions, Empty, Modal, Popconfirm, Space, Spin, Table, Tag, Typography } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'

import { appMessage } from '../../../lib/antdMessage'
import { requestJson } from '../../../lib/api'
import type { Conversation, MessageItem, User } from '../../../types/chat'

type Props = { open: boolean; user: User | null; onClose: () => void }

export function UserSessionsModal({ open, user, onClose }: Props) {
  const [sessions, setSessions] = useState<Conversation[]>([])
  const [loading, setLoading] = useState(false)
  const [messages, setMessages] = useState<Record<string, MessageItem[]>>({})
  const [messageLoading, setMessageLoading] = useState('')
  const [deleting, setDeleting] = useState('')

  useEffect(() => {
    if (!open || !user) return
    let active = true
    setLoading(true)
    setSessions([])
    setMessages({})
    requestJson<{ items: Conversation[] }>(`/api/admin/users/${user.id}/conversations`)
      .then((data) => { if (active) setSessions(Array.isArray(data.items) ? data.items : []) })
      .catch((error) => { if (active) appMessage.error(error instanceof Error ? error.message : '加载用户会话失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [open, user])

  async function loadMessages(session: Conversation) {
    if (messages[session.id]) return
    setMessageLoading(session.id)
    try {
      const data = await requestJson<{ items: MessageItem[] }>(`/api/admin/conversations/${session.id}/messages`)
      setMessages((current) => ({ ...current, [session.id]: Array.isArray(data.items) ? data.items : [] }))
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '加载消息失败')
    } finally {
      setMessageLoading('')
    }
  }

  async function deleteSession(session: Conversation) {
    if (!user) return
    setDeleting(session.id)
    try {
      await requestJson(`/api/admin/users/${user.id}/conversations/${session.id}`, { method: 'DELETE' })
      setSessions((current) => current.filter((item) => item.id !== session.id))
      appMessage.success('会话已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '删除会话失败')
    } finally {
      setDeleting('')
    }
  }

  return <Modal open={open} title={`用户会话 · ${user?.username ?? ''}`} onCancel={onClose} footer={null} width={1000} destroyOnHidden>
    <div className="user-history-modal">
      {user ? <Descriptions className="user-history-descriptions" size="small" column={3} bordered items={[
        { key: 'username', label: '用户名', children: user.username },
        { key: 'role', label: '角色', children: user.role },
        { key: 'count', label: '会话数', children: sessions.length },
      ]} /> : null}
      <Table className="user-history-table" rowKey="id" loading={loading} dataSource={sessions} pagination={{ pageSize: 10 }} size="small"
        locale={{ emptyText: <Empty description="该用户暂无会话" /> }}
        expandable={{
          onExpand: (expanded, record) => { if (expanded) void loadMessages(record) },
          expandedRowRender: (record) => messageLoading === record.id ? <Spin size="small" /> : <MessageList items={messages[record.id] ?? []} />,
        }}
        columns={[
          { title: '会话', dataIndex: 'title', key: 'title', render: (value: string) => <Typography.Text strong>{value || '未命名会话'}</Typography.Text> },
          { title: '消息', dataIndex: 'message_count', key: 'message_count', width: 80 },
          { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180, render: formatDateTime },
          { title: '操作', key: 'action', width: 90, render: (_: unknown, record: Conversation) => <Popconfirm title="删除这个会话？" description="会话及其中的全部消息将被永久删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => deleteSession(record)}><Button danger type="text" size="small" icon={<DeleteOutlined />} loading={deleting === record.id}>删除</Button></Popconfirm> },
        ]} />
    </div>
  </Modal>
}

function MessageList({ items }: { items: MessageItem[] }) {
  if (items.length === 0) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无消息" />
  return <Space direction="vertical" size={8} style={{ width: '100%' }}>{items.map((item) => <div key={item.id} className="user-session-message"><Space><Tag color={item.role === 'assistant' ? 'blue' : item.role === 'user' ? 'green' : undefined}>{item.role}</Tag><Typography.Text type="secondary">{formatDateTime(item.created_at)}</Typography.Text></Space><Typography.Paragraph ellipsis={{ rows: 6, expandable: true, symbol: '展开' }}>{item.content || '（空消息）'}</Typography.Paragraph></div>)}</Space>
}

function formatDateTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(date)
}
