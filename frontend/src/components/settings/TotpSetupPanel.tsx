import { useState } from 'react'
import { Button, Image, Input, Space, Typography } from 'antd'

import { appMessage } from '../../lib/antdMessage'
import { requestJson } from '../../lib/api'
import type { TotpSetup } from '../../types/chat'

type TotpSetupPanelProps = {
  totpEnabled: boolean
  onRefresh: () => void
}

export function TotpSetupPanel({ totpEnabled, onRefresh }: TotpSetupPanelProps) {
  const [setup, setSetup] = useState<TotpSetup | null>(null)
  const [loading, setLoading] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [code, setCode] = useState('')

  async function handleSetup() {
    setLoading(true)
    try {
      const data = await requestJson<{ ok: boolean } & TotpSetup>('/api/auth/totp/setup')
      setSetup(data)
      setCode('')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '获取 TOTP 信息失败')
    } finally {
      setLoading(false)
    }
  }

  async function handleConfirm() {
    if (!setup || !code.trim()) {
      appMessage.warning('请输入验证码')
      return
    }
    setConfirming(true)
    try {
      await requestJson('/api/auth/totp/confirm', {
        method: 'POST',
        body: JSON.stringify({ secret: setup.secret, code: code.trim() }),
      })
      appMessage.success('TOTP 已启用')
      setSetup(null)
      setCode('')
      onRefresh()
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '验证码不正确')
    } finally {
      setConfirming(false)
    }
  }

  async function handleReset() {
    setResetting(true)
    try {
      await requestJson('/api/auth/totp/reset', { method: 'POST' })
      appMessage.success('TOTP 已重置')
      setSetup(null)
      setCode('')
      onRefresh()
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '重置 TOTP 失败')
    } finally {
      setResetting(false)
    }
  }

  if (totpEnabled) {
    return (
      <div className="totp-section">
        <Typography.Text className="totp-section-title">TOTP 双因素认证</Typography.Text>
        <Space direction="vertical" size="middle">
          <Typography.Text>TOTP 已启用，登录时需要输入验证码。</Typography.Text>
          <Button danger onClick={handleReset} loading={resetting}>
            重置 TOTP
          </Button>
        </Space>
      </div>
    )
  }

  if (setup) {
    return (
      <div className="totp-section">
        <Typography.Text className="totp-section-title">设置 TOTP 双因素认证</Typography.Text>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Typography.Text>使用身份验证器 App 扫描下方二维码：</Typography.Text>
          {setup.qr_base64 && (
            <Image
              src={`data:image/png;base64,${setup.qr_base64}`}
              alt="TOTP QR Code"
              width={200}
              height={200}
              style={{ imageRendering: 'pixelated' }}
            />
          )}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            手动输入密钥：{setup.secret}
          </Typography.Text>
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder="输入 6 位验证码确认"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              maxLength={6}
              style={{ width: 200 }}
            />
            <Button type="primary" onClick={handleConfirm} loading={confirming}>
              确认启用
            </Button>
          </Space.Compact>
          <Button type="link" onClick={() => setSetup(null)} style={{ padding: 0 }}>
            取消
          </Button>
        </Space>
      </div>
    )
  }

  return (
    <div className="totp-section">
      <Typography.Text className="totp-section-title">TOTP 双因素认证</Typography.Text>
      <Space direction="vertical" size="middle">
        <Typography.Text>未启用双因素认证，开启后登录需要输入验证码。</Typography.Text>
        <Button type="primary" onClick={handleSetup} loading={loading}>
          启用 TOTP
        </Button>
      </Space>
    </div>
  )
}
