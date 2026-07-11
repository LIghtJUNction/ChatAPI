import { useState } from 'react'
import { Button, Modal, Tabs, Typography } from 'antd'
import { ControlOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'

import type { AutomationRule, AuthUser } from '../../types/chat'
import { ApiKeyManagementPanel } from './ApiKeyManagementPanel'
import { AutomationRulesPanel } from './AutomationRulesPanel'
import { UserManagementPanel } from './UserManagementPanel'
import { UserSettingsPanel } from './UserSettingsPanel'

type SettingsModalProps = {
  automationRuleEditorOpen: boolean
  automationRules: AutomationRule[]
  onCreateAutomationRule: () => void | Promise<void>
  onDeleteAutomationRule: (ruleId: string) => void | Promise<void>
  onEditAutomationRule: (ruleId: string) => void | Promise<void>
  onToggleAutomationRule: (ruleId: string, enabled: boolean) => void | Promise<void>
  open: boolean
  onClose: () => void
  savingAutomationRules: boolean
  user: AuthUser | null
  totpEnabled: boolean
  onTotpRefresh: () => void
}

type TabKey = 'user-settings' | 'api-keys' | 'automation' | 'users' | 'system'

export function SettingsModal({
  automationRuleEditorOpen,
  automationRules,
  onCreateAutomationRule,
  onDeleteAutomationRule,
  onEditAutomationRule,
  onToggleAutomationRule,
  open,
  onClose,
  savingAutomationRules,
  user,
  totpEnabled,
  onTotpRefresh,
}: SettingsModalProps) {
	const navigate = useNavigate()
  const isAdmin = user?.role === 'admin'
  const [activeTab, setActiveTab] = useState<TabKey>('user-settings')

  const handleTabChange = (key: string) => {
    setActiveTab(key as TabKey)
  }

  const commonTabs = [
    {
      key: 'automation',
      label: '自动化规则',
      children: (
        <AutomationRulesPanel
          automationRules={automationRules}
          onCreateAutomationRule={onCreateAutomationRule}
          onDeleteAutomationRule={onDeleteAutomationRule}
          onEditAutomationRule={onEditAutomationRule}
          onToggleAutomationRule={onToggleAutomationRule}
          savingAutomationRules={savingAutomationRules}
        />
      ),
    },
    {
      key: 'api-keys',
      label: 'API Keys',
      children: <ApiKeyManagementPanel open={open && activeTab === 'api-keys'} />,
    },
  ]

  const userSettingsTab = {
    key: 'user-settings',
    label: isAdmin ? <span style={{ color: '#13c2c2' }}>我的设置</span> : '我的设置',
    children: (
      <UserSettingsPanel
        open={open && activeTab === 'user-settings'}
        onClose={onClose}
        totpEnabled={totpEnabled}
        onTotpRefresh={onTotpRefresh}
      />
    ),
  }

  const adminTabs = [
	{
	  key: 'system',
	  label: <span style={{ color: '#13c2c2' }}>系统设置</span>,
	  children: <div className="admin-settings-entry"><ControlOutlined/><Typography.Title level={4}>系统管理设置</Typography.Title><Typography.Paragraph type="secondary">管理认证、访问限制、聊天、媒体、自动化和实时通信策略。</Typography.Paragraph><Button type="primary" onClick={()=>{onClose();navigate('/admin/settings/overview')}}>打开管理控制面</Button></div>,
	},
    {
      key: 'users',
      label: <span style={{ color: '#13c2c2' }}>用户管理</span>,
      children: <UserManagementPanel open={open && activeTab === 'users'} />,
    },
  ]

  const tabs = isAdmin ? [...commonTabs, userSettingsTab, ...adminTabs] : [...commonTabs, userSettingsTab]

  return (
    <Modal
      title="设置"
      width={1120}
      open={open}
      onCancel={() => {
        if (savingAutomationRules || automationRuleEditorOpen) return
        onClose()
      }}
      footer={null}
      destroyOnHidden
      className="settings-modal"
    >
      <Tabs
        activeKey={activeTab}
        onChange={handleTabChange}
        items={tabs}
      />
    </Modal>
  )
}
