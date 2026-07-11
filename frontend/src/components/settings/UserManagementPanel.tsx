import { useState } from 'react'
import { Button, Form, Input, Select, Table, Typography } from 'antd'
import { CloseOutlined, PlusOutlined } from '@ant-design/icons'

import { useUserManagementState } from './userManagement/useUserManagementState'
import { buildUserColumns } from './userManagement/userColumns'
import { UserPasswordModal } from './userManagement/UserPasswordModal'

type UserManagementPanelProps = {
  open: boolean
}

export function UserManagementPanel({ open }: UserManagementPanelProps) {
  const [createOpen, setCreateOpen] = useState(false)
  const {
    creating,
    deletingId,
    form,
    handleCreate,
    handleDelete,
    handlePasswordChange,
    loading,
    page,
    pageSize,
    openPasswordModal,
    pwForm,
    pwModalOpen,
    pwSubmitting,
    pwUsername,
    setPwModalOpen,
    users,
    setPage,
    setPageSize,
    totalUsers,
  } = useUserManagementState(open)

  const columns = buildUserColumns({
    deletingId,
    onDelete: handleDelete,
    onOpenPassword: openPasswordModal,
  })

  return (
    <div className="user-management-panel">
      <div className="user-management-header">
        <div>
          <Typography.Title level={2}>用户管理</Typography.Title>
          <Typography.Text type="secondary">共 {totalUsers} 个账户</Typography.Text>
        </div>
        <Button
          type={createOpen ? 'default' : 'primary'}
          icon={createOpen ? <CloseOutlined /> : <PlusOutlined />}
          onClick={() => setCreateOpen((current) => !current)}
        >
          {createOpen ? '取消创建' : '创建用户'}
        </Button>
      </div>

      {createOpen ? <Form form={form} layout="inline" onFinish={async (values) => {
        if (await handleCreate(values)) setCreateOpen(false)
      }} className="user-management-form">
        <Typography.Text strong className="user-management-form-title">新账户</Typography.Text>
        <Form.Item
          name="username"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input placeholder="用户名" autoFocus allowClear />
        </Form.Item>
        <Form.Item
          name="password"
          rules={[{ required: true, min: 4, message: '密码至少 4 个字符' }]}
        >
          <Input.Password placeholder="密码" allowClear />
        </Form.Item>
        <Form.Item name="role" initialValue="user">
          <Select
            style={{ width: 100 }}
            options={[
              { label: '普通用户', value: 'user' },
              { label: '管理员', value: 'admin' },
            ]}
          />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" icon={<PlusOutlined />} loading={creating}>
            创建
          </Button>
        </Form.Item>
      </Form> : null}

      <div className="user-management-list-heading">
        <Typography.Text strong>账户列表</Typography.Text>
        <Typography.Text type="secondary">连接状态每 2 秒校准</Typography.Text>
      </div>
      <Table
        className="user-management-table"
        columns={columns}
        dataSource={users}
        rowKey="id"
        loading={loading}
        pagination={{
          current: page,
          pageSize,
          total: totalUsers,
          showSizeChanger: true,
          showQuickJumper: true,
          pageSizeOptions: ['5', '10', '20', '50'],
          showTotal: (total) => `共 ${total} 条`,
          onChange: (nextPage, nextPageSize) => {
            if (nextPageSize !== pageSize) {
              setPageSize(nextPageSize)
              setPage(1)
              return
            }
            setPage(nextPage)
          },
        }}
        size="small"
        scroll={{ x: 840 }}
      />

      <UserPasswordModal
        open={pwModalOpen}
        username={pwUsername}
        form={pwForm}
        submitting={pwSubmitting}
        onCancel={setPwModalOpen}
        onSubmit={handlePasswordChange}
      />
    </div>
  )
}
