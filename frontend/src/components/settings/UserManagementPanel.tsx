import { Button, Form, Input, Select, Table, Typography } from 'antd'
import { PlusOutlined } from '@ant-design/icons'

import { useUserManagementState } from './userManagement/useUserManagementState'
import { buildUserColumns } from './userManagement/userColumns'
import { UserPasswordModal } from './userManagement/UserPasswordModal'

type UserManagementPanelProps = {
  open: boolean
}

export function UserManagementPanel({ open }: UserManagementPanelProps) {
  const {
    creating,
    deletingId,
    form,
    handleCreate,
    handleDelete,
    handlePasswordChange,
    loading,
    openPasswordModal,
    pwForm,
    pwModalOpen,
    pwSubmitting,
    pwUsername,
    setPwModalOpen,
    users,
  } = useUserManagementState(open)

  const columns = buildUserColumns({
    deletingId,
    onDelete: handleDelete,
    onOpenPassword: openPasswordModal,
  })

  return (
    <div className="user-management-panel">
      <div className="user-management-header">
        <Typography.Text className="user-management-subtitle">
          管理系统中的所有用户账户。
        </Typography.Text>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => form.submit()}>
          添加用户
        </Button>
      </div>

      <Form form={form} layout="inline" onFinish={handleCreate} className="user-management-form">
        <Form.Item
          name="username"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input placeholder="用户名" allowClear />
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
            添加
          </Button>
        </Form.Item>
      </Form>

      <Table
        className="user-management-table"
        columns={columns}
        dataSource={users}
        rowKey="id"
        loading={loading}
        pagination={{
          defaultPageSize: 10,
          showSizeChanger: true,
          showQuickJumper: true,
          pageSizeOptions: ['5', '10', '20', '50'],
          showTotal: (total) => `共 ${total} 条`,
        }}
        size="small"
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
