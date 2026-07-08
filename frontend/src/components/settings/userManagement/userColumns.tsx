import { Button, Popconfirm, Space } from 'antd'
import { DeleteOutlined, SafetyOutlined } from '@ant-design/icons'

import type { User } from '../../../types/chat'

type UserColumnsOptions = {
  deletingId: string
  onDelete: (userId: string) => void
  onOpenPassword: (user: User) => void
}

export function buildUserColumns({
  deletingId,
  onDelete,
  onOpenPassword,
}: UserColumnsOptions) {
  return [
    { title: '用户名', dataIndex: 'username', key: 'username' },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      render: (role: string) => (role === 'admin' ? '管理员' : '普通用户'),
    },
    {
      title: 'API Keys',
      dataIndex: 'api_key_count',
      key: 'api_key_count',
      render: (value: number | undefined) => value ?? 0,
    },
    {
      title: '当前连接数',
      dataIndex: 'current_connection_count',
      key: 'current_connection_count',
      render: (value: number | undefined) => value ?? 0,
    },
    {
      title: '操作',
      key: 'action',
      render: (_: unknown, record: User) => (
        <Space size={8} wrap>
          <Button size="small" icon={<SafetyOutlined />} onClick={() => onOpenPassword(record)}>
            重置密码
          </Button>
          <Popconfirm
            title={`删除用户：${record.username}`}
            description="删除后该用户的所有会话、消息和 API Key 都会被清理，且无法恢复。"
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => onDelete(record.id)}
          >
            <Button size="small" danger icon={<DeleteOutlined />} loading={deletingId === record.id}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]
}
