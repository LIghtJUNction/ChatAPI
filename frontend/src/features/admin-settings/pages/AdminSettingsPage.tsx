import { useEffect, useState } from 'react'
import {
  ArrowLeftOutlined,
  DashboardOutlined,
  TeamOutlined,
  LockOutlined,
  PictureOutlined,
  RobotOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Button, Collapse, Layout, Menu, Spin, Statistic, Tag, Typography } from 'antd'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { useAuthSession } from '../../../hooks/useAuthSession'
import { getOverview, getRuntime } from '../api/settings'
import { DomainSettingsSection } from '../sections/DomainSettingsSection'
import { UserManagementPanel } from '../../../components/settings/UserManagementPanel'
import { useAdminMonitoring } from '../hooks/useAdminMonitoring'
import './admin-settings.css'

const domains = [
  { key: 'auth', label: '访问与认证', icon: <LockOutlined /> },
  { key: 'access', label: '访问限流', icon: <ThunderboltOutlined /> },
  { key: 'media', label: '媒体', icon: <PictureOutlined /> },
  { key: 'automation', label: '自动化', icon: <RobotOutlined /> },
]

const items = [
  { key: 'overview', label: '概览', icon: <DashboardOutlined /> },
  { key: 'users', label: '用户管理', icon: <TeamOutlined /> },
  ...domains,
]

const sectionKeys = new Set(items.map((item) => item.key))
const noMonitoredUsers: string[] = []

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
          <Overview overview={overview} runtime={runtime} />
        ) : selected === 'users' ? (
          <UserManagementPanel open />
        ) : (
          <DomainSettingsSection key={selected} domain={selected} />
        )}
      </main>
    </Layout>
  )
}

function Overview({ overview, runtime: runtimeDocument }: { overview: Record<string, unknown> | null; runtime: Record<string, unknown> | null }) {
  const runtimeSummary = (overview?.runtime ?? {}) as Record<string, unknown>
  const configuredDomains = (overview?.domains ?? {}) as Record<string, unknown>
  const monitoring = useAdminMonitoring(true, noMonitoredUsers)
  const metrics = monitoring.metrics
  return (
    <section className="admin-overview">
      <Typography.Title level={2}>系统概览</Typography.Title>
      <div className="admin-overview-status">
        <Typography.Text type="secondary">实例运行状态</Typography.Text>
        <Tag color={monitoring.connected ? 'green' : 'default'}>{monitoring.connected ? '实时' : '连接中'}</Tag>
      </div>
      <div className="admin-overview-stats admin-overview-metrics">
        <Statistic title="实例连接" value={monitoring.totalConnections} />
        <Statistic title="Goroutine" value={metrics?.goroutines ?? 0} />
        <Statistic title="Go 堆内存" value={formatBytes(metrics?.heap_alloc_bytes ?? 0)} />
        <Statistic title="运行时间" value={formatUptime(metrics?.uptime_seconds ?? 0)} />
        <Statistic title="CPU 使用率" value={metrics?.cpu_usage_percent ?? 0} precision={1} suffix="%" />
        <Statistic title="CPU 逻辑核心" value={metrics?.cpu_count ?? 0} />
        <Statistic title="主机内存" value={formatMemoryUsage(metrics?.memory_total_bytes ?? 0, metrics?.memory_available_bytes ?? 0)} />
        <Statistic title="Swap" value={formatUsage(metrics?.swap_used_bytes ?? 0, metrics?.swap_total_bytes ?? 0)} />
      </div>
      <div className="admin-overview-stats">
        <Statistic title="配置领域" value={Object.keys(configuredDomains).length} />
        <Statistic title="运行模式" value={String(runtimeSummary.mode ?? '-')} />
        <Statistic title="数据库" value={String(runtimeSummary.database_driver ?? '-')} />
        <Statistic title="邮件服务" value={runtimeSummary.smtp_configured ? '已配置' : '未配置'} />
      </div>
      <Collapse ghost items={[{ key: 'runtime', label: '运行环境', children: <Runtime runtime={runtimeDocument} /> }]} />
    </section>
  )
}

function Runtime({ runtime }: { runtime: Record<string, unknown> | null }) {
  const config = (runtime?.config ?? {}) as Record<string, unknown>
  return (
    <section>
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

function formatBytes(value: number) {
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KiB`
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MiB`
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GiB`
}

function formatUsage(used: number, total: number) {
  if (total <= 0) return '未启用'
  return `${formatBytes(used)} / ${formatBytes(total)}`
}

function formatMemoryUsage(total: number, available: number) {
  return formatUsage(Math.max(0, total - available), total)
}

function formatUptime(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
}
