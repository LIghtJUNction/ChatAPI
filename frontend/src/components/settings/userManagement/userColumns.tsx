import { Badge, Button, Popconfirm, Space, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, KeyOutlined, SafetyCertificateOutlined } from '@ant-design/icons'

import type { User } from '../../../types/chat'

type UserColumnsOptions = {
  deletingId: string
  onDelete: (userId: string) => void
  onOpenPassword: (user: User) => void
  onRoleChange: (user: User, role: 'user' | 'admin') => void
  canManageRoles: boolean
}

export function buildUserColumns({
  deletingId,
  onDelete,
  onOpenPassword,
  onRoleChange,
  canManageRoles,
}: UserColumnsOptions) {
  return [
    {
      title: '用户',
      dataIndex: 'username',
      key: 'username',
      width: 220,
      render: (username: string, user: User) => (
        <div className="admin-user-identity">
          <Typography.Text strong>{username}</Typography.Text>
          <Typography.Text type="secondary" copyable={{ text: user.id }}>{shortID(user.id)}</Typography.Text>
        </div>
      ),
    },
    {
      title: '邮箱',
      dataIndex: 'email',
      key: 'email',
      width: 250,
      render: (email: string) => email ? <Typography.Text copyable={{ text: email }}>{email}</Typography.Text> : <Typography.Text type="secondary">未设置</Typography.Text>,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 100,
      render: (role: string) => <Tag color={role === 'superadmin' ? 'gold' : role === 'admin' ? 'blue' : undefined}>{role === 'superadmin' ? '超级管理员' : role === 'admin' ? '管理员' : '用户'}</Tag>,
    },
    {
      title: 'API Key',
      dataIndex: 'api_key_count',
      key: 'api_key_count',
      width: 90,
      align: 'right' as const,
      render: (value: number | undefined) => <Typography.Text className="admin-tabular-number">{value ?? 0}</Typography.Text>,
    },
    {
      title: '连接',
      dataIndex: 'current_connection_count',
      key: 'current_connection_count',
      width: 100,
      render: (value: number | undefined) => {
        const count = value ?? 0
        return <Badge status={count > 0 ? 'success' : 'default'} text={count > 0 ? `${count} 个` : '离线'} />
      },
    },
    {
      title: '状态',
      dataIndex: 'is_active',
      key: 'is_active',
      width: 90,
      render: (active: boolean) => <Badge status={active ? 'success' : 'error'} text={active ? '正常' : '停用'} />,
    },
    {
      title: '最近登录',
      dataIndex: 'last_login_at',
      key: 'last_login_at',
      width: 170,
      render: (value?: string) => value ? formatDateTime(value) : '-',
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: User) => (
        <Space size={4}>
          {canManageRoles && record.role !== 'superadmin' ? (
            <Popconfirm
              title={record.role === 'admin' ? `撤销 ${record.username} 的管理员？` : `将 ${record.username} 设为管理员？`}
              okText="确认"
              cancelText="取消"
              onConfirm={() => onRoleChange(record, record.role === 'admin' ? 'user' : 'admin')}
            >
              <Tooltip title={record.role === 'admin' ? '撤销管理员' : '设为管理员'}>
                <Button type="text" size="small" icon={<SafetyCertificateOutlined />} aria-label={`修改 ${record.username} 的角色`} />
              </Tooltip>
            </Popconfirm>
          ) : null}
          <Tooltip title="重置密码">
            <Button type="text" size="small" icon={<KeyOutlined />} aria-label={`重置 ${record.username} 的密码`} onClick={() => onOpenPassword(record)} />
          </Tooltip>
          <Popconfirm
            title={`删除用户：${record.username}`}
            description="删除后该用户的所有会话、消息和 API Key 都会被清理，且无法恢复。"
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => onDelete(record.id)}
          >
            <Tooltip title="删除用户">
              <Button type="text" size="small" danger icon={<DeleteOutlined />} aria-label={`删除用户 ${record.username}`} loading={deletingId === record.id} />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ]
}

function shortID(value: string) {
  return value.length > 18 ? `${value.slice(0, 15)}...` : value
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}
