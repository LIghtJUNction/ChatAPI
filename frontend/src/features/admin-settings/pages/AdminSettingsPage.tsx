import { useEffect, useState } from 'react'
import {
  ArrowLeftOutlined,
  DashboardOutlined,
  LockOutlined,
  MessageOutlined,
  PictureOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { Button, Layout, Menu, Spin, Statistic, Typography } from 'antd'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { useAuthSession } from '../../../hooks/useAuthSession'
import { getOverview, getRuntime } from '../api/settings'
import { DomainSettingsSection } from '../sections/DomainSettingsSection'
import './admin-settings.css'

const domains = [
  { key: 'auth', label: '访问与认证', icon: <LockOutlined /> },
  { key: 'access', label: '访问限流', icon: <ThunderboltOutlined /> },
  { key: 'chat', label: '聊天与协议', icon: <MessageOutlined /> },
  { key: 'media', label: '媒体', icon: <PictureOutlined /> },
  { key: 'automation', label: '自动化', icon: <RobotOutlined /> },
  { key: 'realtime', label: '实时通信', icon: <SettingOutlined /> },
]

const items = [
  { key: 'overview', label: '概览', icon: <DashboardOutlined /> },
  ...domains,
  { key: 'runtime', label: '运行环境', icon: <ToolOutlined /> },
]

const sectionKeys = new Set(items.map((item) => item.key))

export function AdminSettingsPage() {
  const auth = useAuthSession()
  const navigate = useNavigate()
  const params = useParams()
  const selected = params['*'] ?? ''
  const [overview, setOverview] = useState<Record<string, unknown> | null>(null)
  const [runtime, setRuntime] = useState<Record<string, unknown> | null>(null)

  useEffect(() => {
    if (!auth.session.authenticated || auth.session.user?.role !== 'admin') return
    void getOverview().then(setOverview)
    void getRuntime().then(setRuntime)
  }, [auth.session.authenticated, auth.session.user?.role])

  if (auth.loading) return <div className="boot-screen"><Spin /></div>
  if (!auth.session.authenticated) return <Navigate to="/login" replace />
  if (auth.session.user?.role !== 'admin') return <Navigate to="/app" replace />
  if (!sectionKeys.has(selected)) return <Navigate to="/admin/settings/overview" replace />

  return (
    <Layout className="admin-settings-shell">
      <aside className="admin-settings-nav">
        <div className="admin-settings-brand">
          <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => navigate('/app')} />
          <div>
            <Typography.Text strong>系统设置</Typography.Text>
            <Typography.Text type="secondary">管理员控制面</Typography.Text>
          </div>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selected]}
          items={items}
          onSelect={({ key }) => navigate(`/admin/settings/${key}`)}
        />
      </aside>
      <main className="admin-settings-main">
        {selected === 'overview' ? (
          <Overview overview={overview} />
        ) : selected === 'runtime' ? (
          <Runtime runtime={runtime} />
        ) : (
          <DomainSettingsSection key={selected} domain={selected} />
        )}
      </main>
    </Layout>
  )
}

function Overview({ overview }: { overview: Record<string, unknown> | null }) {
  const runtime = (overview?.runtime ?? {}) as Record<string, unknown>
  const configuredDomains = (overview?.domains ?? {}) as Record<string, unknown>
  return (
    <section className="admin-overview">
      <Typography.Title level={2}>系统概览</Typography.Title>
      <div className="admin-overview-stats">
        <Statistic title="配置领域" value={Object.keys(configuredDomains).length} />
        <Statistic title="运行模式" value={String(runtime.mode ?? '-')} />
        <Statistic title="数据库" value={String(runtime.database_driver ?? '-')} />
        <Statistic title="邮件服务" value={runtime.smtp_configured ? '已配置' : '未配置'} />
      </div>
      <Typography.Paragraph type="secondary">
        常用策略按领域独立保存。环境变量管理的启动项请在“运行环境”中查看。
      </Typography.Paragraph>
    </section>
  )
}

function Runtime({ runtime }: { runtime: Record<string, unknown> | null }) {
  const config = (runtime?.config ?? {}) as Record<string, unknown>
  return (
    <section>
      <Typography.Title level={2}>运行环境</Typography.Title>
      <Typography.Paragraph type="secondary">
        这些值来自进程启动配置，只读且敏感内容已经脱敏。修改环境变量后需要重启服务。
      </Typography.Paragraph>
      <div className="admin-runtime-grid">
        {Object.entries(config).map(([key, value]) => (
          <div key={key}>
            <Typography.Text type="secondary">{key}</Typography.Text>
            <Typography.Text>
              {Array.isArray(value) ? value.join(', ') : String(value ?? '')}
            </Typography.Text>
          </div>
        ))}
      </div>
    </section>
  )
}
