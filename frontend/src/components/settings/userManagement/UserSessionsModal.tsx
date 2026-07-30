import { useEffect, useState } from 'react'
import { Button, Descriptions, Empty, Modal, Popconfirm, Space, Spin, Table, Tabs, Tag, Typography } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'

import { appMessage } from '../../../lib/antdMessage'
import { requestJson } from '../../../lib/api'
import type { ApiKeyInfo, Conversation, MessageItem, ModelKeyInfo, User } from '../../../types/chat'

type Props = { open: boolean; user: User | null; onClose: () => void }

export function UserSessionsModal({ open, user, onClose }: Props) {
  const [sessions, setSessions] = useState<Conversation[]>([])
  const [appKeys, setAppKeys] = useState<ApiKeyInfo[]>([])
  const [modelKeys, setModelKeys] = useState<ModelKeyInfo[]>([])
  const [loadedUserID, setLoadedUserID] = useState('')
  const [failedUserID, setFailedUserID] = useState('')
  const [messages, setMessages] = useState<Record<string, MessageItem[]>>({})
  const [messageLoading, setMessageLoading] = useState('')
  const [deleting, setDeleting] = useState('')

  useEffect(() => {
    if (!open || !user) return
    let active = true
    const userID = user.id
    Promise.all([
      requestJson<{ items: Conversation[] }>(`/api/admin/users/${userID}/conversations`),
      requestJson<{ items: ApiKeyInfo[] }>(`/api/admin/users/${userID}/app-keys`),
      requestJson<{ items: ModelKeyInfo[] }>(`/api/admin/users/${userID}/model-keys`),
    ]).then(([sessionData, appKeyData, modelKeyData]) => {
      if (!active) return
      setSessions(Array.isArray(sessionData.items) ? sessionData.items : [])
      setAppKeys(Array.isArray(appKeyData.items) ? appKeyData.items : [])
      setModelKeys(Array.isArray(modelKeyData.items) ? modelKeyData.items : [])
      setMessages({})
      setLoadedUserID(userID)
      setFailedUserID('')
    }).catch((error) => {
      if (!active) return
      setFailedUserID(userID)
      appMessage.error(error instanceof Error ? error.message : '加载用户详情失败')
    })
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

  const showingCurrentUser = loadedUserID === user?.id
  const loading = Boolean(open && user && !showingCurrentUser && failedUserID !== user.id)
  const visibleSessions = showingCurrentUser ? sessions : []
  const visibleAppKeys = showingCurrentUser ? appKeys : []
  const visibleModelKeys = showingCurrentUser ? modelKeys : []

  return <Modal open={open} title={`用户详情 · ${user?.username ?? ''}`} onCancel={onClose} footer={null} width={1080} destroyOnHidden>
    <div className="user-history-modal">
      {user ? <Descriptions className="user-history-descriptions" size="small" column={3} bordered items={[
        { key: 'username', label: '用户名', children: user.username },
        { key: 'role', label: '角色', children: user.role },
        { key: 'count', label: '资源', children: `${visibleSessions.length} 个会话 · ${visibleAppKeys.length + visibleModelKeys.length} 个 Key` },
      ]} /> : null}
      <Tabs items={[
        { key: 'sessions', label: `历史会话 (${visibleSessions.length})`, children: <SessionTable sessions={visibleSessions} loading={loading} messages={messages} messageLoading={messageLoading} deleting={deleting} onLoadMessages={loadMessages} onDelete={deleteSession} /> },
        { key: 'app-keys', label: `应用 API Key (${visibleAppKeys.length})`, children: <AppKeyTable items={visibleAppKeys} loading={loading} /> },
        { key: 'model-keys', label: `虚拟模型 Key (${visibleModelKeys.length})`, children: <ModelKeyTable items={visibleModelKeys} loading={loading} /> },
      ]} />
    </div>
  </Modal>
}

function SessionTable({ sessions, loading, messages, messageLoading, deleting, onLoadMessages, onDelete }: {
  sessions: Conversation[]
  loading: boolean
  messages: Record<string, MessageItem[]>
  messageLoading: string
  deleting: string
  onLoadMessages: (session: Conversation) => Promise<void>
  onDelete: (session: Conversation) => Promise<void>
}) {
  return <Table className="user-history-table" rowKey="id" loading={loading} dataSource={sessions} pagination={{ pageSize: 10 }} size="small"
        locale={{ emptyText: <Empty description="该用户暂无会话" /> }}
        expandable={{
          onExpand: (expanded, record) => { if (expanded) void onLoadMessages(record) },
          expandedRowRender: (record) => messageLoading === record.id ? <Spin size="small" /> : <MessageList items={messages[record.id] ?? []} />,
        }}
        columns={[
          { title: '会话', dataIndex: 'title', key: 'title', render: (value: string) => <Typography.Text strong>{value || '未命名会话'}</Typography.Text> },
          { title: '消息', dataIndex: 'message_count', key: 'message_count', width: 80 },
          { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180, render: formatDateTime },
          { title: '操作', key: 'action', width: 90, render: (_: unknown, record: Conversation) => <Popconfirm title="删除这个会话？" description="会话及其中的全部消息将被永久删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => onDelete(record)}><Button danger type="text" size="small" icon={<DeleteOutlined />} loading={deleting === record.id}>删除</Button></Popconfirm> },
        ]} />
}

function AppKeyTable({ items, loading }: { items: ApiKeyInfo[]; loading: boolean }) {
  return <Table className="user-history-table" rowKey="id" loading={loading} dataSource={items} pagination={{ pageSize: 10 }} size="small" locale={{ emptyText: <Empty description="该用户暂无应用 API Key" /> }} columns={[
    { title: '名称', dataIndex: 'name', key: 'name', render: valueOrDash },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'key_prefix', render: renderPrefix },
    { title: '权限范围', dataIndex: 'scopes', key: 'scopes', render: (values: string[] | undefined) => values?.length ? <Space size={[4, 4]} wrap>{values.map((value) => <Tag key={value}>{value}</Tag>)}</Space> : '-' },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: formatDateTime },
    { title: '最近使用', dataIndex: 'last_used_at', key: 'last_used_at', width: 180, render: formatOptionalDateTime },
    { title: '过期时间', dataIndex: 'expires_at', key: 'expires_at', width: 180, render: formatOptionalDateTime },
  ]} />
}

function ModelKeyTable({ items, loading }: { items: ModelKeyInfo[]; loading: boolean }) {
  return <Table className="user-history-table" rowKey="id" loading={loading} dataSource={items} pagination={{ pageSize: 10 }} size="small" locale={{ emptyText: <Empty description="该用户暂无虚拟模型 Key" /> }} columns={[
    { title: '名称', dataIndex: 'name', key: 'name', render: valueOrDash },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'key_prefix', render: renderPrefix },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: formatDateTime },
    { title: '最近使用', dataIndex: 'last_used_at', key: 'last_used_at', width: 180, render: formatOptionalDateTime },
  ]} />
}

function valueOrDash(value?: string) { return value || '-' }
function renderPrefix(value?: string) { return <Typography.Text code copyable={Boolean(value)}>{value || '-'}</Typography.Text> }
function formatOptionalDateTime(value?: string | null) { return value ? formatDateTime(value) : '-' }

function MessageList({ items }: { items: MessageItem[] }) {
  if (items.length === 0) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无消息" />
  return <Space direction="vertical" size={8} style={{ width: '100%' }}>{items.map((item) => <div key={item.id} className="user-session-message"><Space><Tag color={item.role === 'assistant' ? 'blue' : item.role === 'user' ? 'green' : undefined}>{item.role}</Tag><Typography.Text type="secondary">{formatDateTime(item.created_at)}</Typography.Text></Space><Typography.Paragraph ellipsis={{ rows: 6, expandable: true, symbol: '展开' }}>{item.content || '（空消息）'}</Typography.Paragraph></div>)}</Space>
}

function formatDateTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(date)
}
