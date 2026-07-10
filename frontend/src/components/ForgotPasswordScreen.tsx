import { useEffect, useRef, useState } from 'react'
import { Button, Card, Form, Input, Typography } from 'antd'

import { CosmicBackdrop } from './CosmicBackdrop'
import { GeetestCaptchaField, type GeetestCaptcha } from './GeetestCaptchaField'
import { appMessage } from '../lib/antdMessage'
import { requestJson } from '../lib/api'
import type { GeetestValidationResult, PasswordResetConfig } from '../types/chat'

type ForgotPasswordScreenProps = {
  onReset: () => void | Promise<void>
  onBackToLogin: () => void
}

export function ForgotPasswordScreen({ onReset, onBackToLogin }: ForgotPasswordScreenProps) {
  const [form] = Form.useForm()
  const [config, setConfig] = useState<PasswordResetConfig | null>(null)
  const [loading, setLoading] = useState(false)
  const [sendingCode, setSendingCode] = useState(false)
  const [codeCountdown, setCodeCountdown] = useState(0)
  const captchaRef = useRef<GeetestCaptcha | null>(null)

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const data = await requestJson<{ ok: boolean } & PasswordResetConfig>('/api/auth/password/config')
        if (!active) return
        setConfig({
          password_reset_enabled: Boolean(data.password_reset_enabled),
          geetest_enabled: Boolean(data.geetest_enabled),
          geetest_captcha_id: String(data.geetest_captcha_id ?? ''),
        })
      } catch {
        if (!active) return
        setConfig({
          password_reset_enabled: false,
          geetest_enabled: false,
          geetest_captcha_id: '',
        })
      }
    }
    void load()
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (codeCountdown <= 0) return
    const timer = setTimeout(() => setCodeCountdown((value) => value - 1), 1000)
    return () => clearTimeout(timer)
  }, [codeCountdown])

  async function handleSendCode() {
    const email = form.getFieldValue('email') as string
    if (!email || !email.includes('@')) {
      appMessage.warning('请先输入有效的邮箱地址')
      return
    }

    let geetestParams: GeetestValidationResult | undefined
    if (config?.geetest_enabled) {
      const result = captchaRef.current?.getValidate()
      if (!result) {
        appMessage.warning('请先完成人机验证')
        return
      }
      geetestParams = result
    }

    setSendingCode(true)
    try {
      await requestJson<{ ok: boolean }>('/api/auth/password/send-code', {
        method: 'POST',
        body: JSON.stringify({
          email,
          geetest_params: geetestParams,
        }),
      })
      appMessage.success('验证码已发送')
      setCodeCountdown(60)
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '发送验证码失败')
      captchaRef.current?.reset()
    } finally {
      setSendingCode(false)
    }
  }

  async function handleSubmit() {
    try {
      const values = await form.validateFields()
      if (values.password !== values.confirmPassword) {
        appMessage.error('两次输入的密码不一致')
        return
      }

      setLoading(true)
      await requestJson<{ ok: boolean }>('/api/auth/password/reset', {
        method: 'POST',
        body: JSON.stringify({
          email: values.email,
          code: values.code,
          password: values.password,
        }),
      })
      appMessage.success('密码已重置，请重新登录')
      onReset()
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '重置密码失败')
    } finally {
      setLoading(false)
    }
  }

  if (config === null) {
    return (
      <div className="login-screen">
        <CosmicBackdrop />
      </div>
    )
  }

  if (!config.password_reset_enabled) {
    return (
      <div className="login-screen">
        <CosmicBackdrop />
        <div className="login-glow login-glow-left" aria-hidden="true" />
        <div className="login-glow login-glow-right" aria-hidden="true" />
        <Card className="login-card">
          <div className="login-copy">
            <Typography.Title level={2} className="login-title">
              忘记密码
            </Typography.Title>
            <Typography.Paragraph className="login-desc" style={{ textAlign: 'center' }}>
              当前系统未配置邮件发送方式，暂时无法通过邮箱找回密码，请联系管理员处理。
            </Typography.Paragraph>
          </div>
          <div className="login-register-row">
            <Typography.Link className="login-register-link" onClick={onBackToLogin}>
              返回登录
            </Typography.Link>
          </div>
        </Card>
      </div>
    )
  }

  return (
    <div className="login-screen">
      <CosmicBackdrop />
      <div className="login-glow login-glow-left" aria-hidden="true" />
      <div className="login-glow login-glow-right" aria-hidden="true" />
      <Card className="login-card">
        <div className="login-copy">
          <Typography.Title level={2} className="login-title">
            忘记密码
          </Typography.Title>
          <Typography.Paragraph className="login-desc" style={{ textAlign: 'center', marginBottom: 0 }}>
            通过邮箱验证码重置密码。
          </Typography.Paragraph>
        </div>
        <Form
          form={form}
          layout="vertical"
          onFinish={() => void handleSubmit()}
          autoComplete="off"
          className="login-form"
          initialValues={{ email: '', code: '', password: '', confirmPassword: '' }}
        >
          <Form.Item
            label="邮箱"
            name="email"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效的邮箱地址' },
            ]}
          >
            <Input placeholder="邮箱地址" size="large" />
          </Form.Item>
          <GeetestCaptchaField
            enabled={config.geetest_enabled}
            captchaId={config.geetest_captcha_id}
            containerId="geetest-forgot-password-container"
            captchaRef={captchaRef}
          />
          <Form.Item
            label="验证码"
            name="code"
            rules={[{ required: true, message: '请输入邮箱验证码' }]}
          >
            <Input
              placeholder="6 位邮箱验证码"
              size="large"
              inputMode="numeric"
              maxLength={6}
              addonAfter={
                <Button
                  type="link"
                  size="small"
                  disabled={codeCountdown > 0 || sendingCode}
                  loading={sendingCode}
                  onClick={() => void handleSendCode()}
                  style={{ padding: 0, margin: 0 }}
                >
                  {codeCountdown > 0 ? `${codeCountdown}s` : '发送验证码'}
                </Button>
              }
            />
          </Form.Item>
          <Form.Item
            label="新密码"
            name="password"
            rules={[{ required: true, message: '请输入新密码' }]}
          >
            <Input.Password placeholder="至少 4 个字符" size="large" />
          </Form.Item>
          <Form.Item
            label="确认新密码"
            name="confirmPassword"
            dependencies={['password']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'))
                },
              }),
            ]}
          >
            <Input.Password placeholder="再次输入新密码" size="large" />
          </Form.Item>
          <Button type="primary" htmlType="submit" size="large" block loading={loading}>
            重置密码
          </Button>
          <div className="login-register-row">
            <Typography.Link className="login-register-link" onClick={onBackToLogin}>
              返回登录
            </Typography.Link>
          </div>
        </Form>
      </Card>
    </div>
  )
}
