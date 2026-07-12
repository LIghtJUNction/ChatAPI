import { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Form, Input, Select, Typography } from 'antd'

import { CosmicBackdrop } from './CosmicBackdrop'
import { GeetestCaptchaField, type GeetestCaptcha } from './GeetestCaptchaField'
import { appMessage } from '../lib/antdMessage'
import { requestJson } from '../lib/api'
import type { GeetestValidationResult, RegisterConfig } from '../types/chat'

type RegistrationScreenProps = {
  onRegistered: () => void | Promise<void>
  onBackToLogin: () => void
}

const DEFAULT_EMAIL_DOMAINS = ['qq.com', 'gmail.com', '163.com', 'outlook.com', 'hotmail.com']

export function RegistrationScreen({ onRegistered, onBackToLogin }: RegistrationScreenProps) {
  const [form] = Form.useForm()
  const [config, setConfig] = useState<RegisterConfig | null>(null)
  const [loading, setLoading] = useState(false)
  const [sendingCode, setSendingCode] = useState(false)
  const [codeCountdown, setCodeCountdown] = useState(0)
  const captchaRef = useRef<GeetestCaptcha | null>(null)

  const emailDomainOptions = useMemo(() => {
    const configuredDomains = (config?.registration_email_domains ?? '')
      .split(',')
      .map((domain) => domain.trim().toLowerCase())
      .filter(Boolean)
    const domains = config?.registration_email_domain_restriction_enabled
      ? configuredDomains
      : Array.from(new Set([...configuredDomains, ...DEFAULT_EMAIL_DOMAINS]))
    return domains.map((domain) => ({
      label: `@${domain}`,
      value: domain,
    }))
  }, [config])

  useEffect(() => {
    if (emailDomainOptions.length === 0) return
    const currentDomain = form.getFieldValue('emailDomain') as string | undefined
    if (!currentDomain || !emailDomainOptions.some((option) => option.value === currentDomain)) {
      form.setFieldValue('emailDomain', emailDomainOptions[0]?.value)
    }
  }, [emailDomainOptions, form])

  function buildEmailFromForm() {
    const emailLocalPart = String(form.getFieldValue('emailLocalPart') ?? '').trim()
    const emailDomain = String(form.getFieldValue('emailDomain') ?? '').trim().toLowerCase()
    if (!emailLocalPart || !emailDomain) {
      return ''
    }
    return `${emailLocalPart}@${emailDomain}`.toLowerCase()
  }

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const data = await requestJson<{ ok: boolean } & RegisterConfig>('/api/auth/register/config')
        if (!active) return
        setConfig({
          registration_enabled: Boolean(data.registration_enabled),
          email_verification_enabled: Boolean(data.email_verification_enabled),
          registration_email_domain_restriction_enabled: Boolean(data.registration_email_domain_restriction_enabled),
          registration_email_domains: String(data.registration_email_domains ?? ''),
          geetest_enabled: Boolean(data.geetest_enabled),
          geetest_captcha_id: String(data.geetest_captcha_id ?? ''),
        })
      } catch {
        if (!active) return
        setConfig({
          registration_enabled: false,
          email_verification_enabled: false,
          registration_email_domain_restriction_enabled: false,
          registration_email_domains: '',
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
    const timer = setTimeout(() => setCodeCountdown((c) => c - 1), 1000)
    return () => clearTimeout(timer)
  }, [codeCountdown])

  async function handleSendCode() {
    const email = buildEmailFromForm()
    if (!email) {
      try {
        await form.validateFields(['emailLocalPart', 'emailDomain'])
      } catch {
        return
      }
    }
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
      await requestJson<{ ok: boolean }>('/api/auth/register/send-code', {
        method: 'POST',
        body: JSON.stringify({ email, geetest_params: geetestParams }),
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
      const email = buildEmailFromForm()
      if (!email) {
        appMessage.error('请输入有效的邮箱地址')
        return
      }
      if (values.password !== values.confirmPassword) {
        appMessage.error('两次输入的密码不一致')
        return
      }

      setLoading(true)

      let geetestParams: GeetestValidationResult | undefined
      if (config?.geetest_enabled && !config?.email_verification_enabled && captchaRef.current) {
        const result = captchaRef.current.getValidate()
        if (!result) {
          appMessage.warning('请先完成人机验证')
          setLoading(false)
          return
        }
        geetestParams = result
      }

      await requestJson<{ ok: boolean; user?: unknown }>('/api/auth/register', {
        method: 'POST',
        body: JSON.stringify({
          email,
          password: values.password,
          code: values.code || '',
          geetest_params: geetestParams,
        }),
      })

      appMessage.success('注册成功，请登录')
      onRegistered()
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '注册失败')
      if (captchaRef.current) {
        captchaRef.current.reset()
      }
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

  if (!config.registration_enabled) {
    return (
      <div className="login-screen">
        <CosmicBackdrop />
        <div className="login-glow login-glow-left" aria-hidden="true" />
        <div className="login-glow login-glow-right" aria-hidden="true" />
        <Card className="login-card">
          <div className="login-copy">
            <Typography.Title level={2} className="login-title">
              注册未开放
            </Typography.Title>
            <Typography.Paragraph className="login-desc" style={{ textAlign: 'center' }}>
              当前系统未开放外部注册，请联系管理员开通。
            </Typography.Paragraph>
          </div>
          <div className="login-register-row">
            <Typography.Text>已有账号？</Typography.Text>
            <Typography.Link className="login-register-link" onClick={onBackToLogin}>
              登录
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
            ChatAPI 注册
          </Typography.Title>
        </div>
        <Form
          form={form}
          layout="vertical"
          onFinish={() => void handleSubmit()}
          autoComplete="off"
          className="login-form"
          initialValues={{
            emailLocalPart: '',
            emailDomain: emailDomainOptions[0]?.value ?? DEFAULT_EMAIL_DOMAINS[0],
            password: '',
            confirmPassword: '',
            code: '',
          }}
        >
          <Form.Item label="邮箱" required style={{ marginBottom: 24 }}>
            <Input.Group compact className="register-email-group">
              <Form.Item
                name="emailLocalPart"
                noStyle
                rules={[
                  { required: true, message: '请输入邮箱' },
                  {
                    validator(_, value) {
                      const localPart = String(value ?? '').trim()
                      if (!localPart) {
                        return Promise.resolve()
                      }
                      if (/^[A-Za-z0-9._%+-]+$/.test(localPart)) {
                        return Promise.resolve()
                      }
                      return Promise.reject(new Error('请输入有效的邮箱地址'))
                    },
                  },
                ]}
              >
                <Input
                  placeholder="邮箱用户名"
                  size="large"
                  className="register-email-input"
                />
              </Form.Item>
              <Form.Item
                name="emailDomain"
                noStyle
                rules={[{ required: true, message: '请选择邮箱域名' }]}
              >
                <Select
                  size="large"
                  className="register-email-domain"
                  options={emailDomainOptions}
                  popupMatchSelectWidth={false}
                />
              </Form.Item>
            </Input.Group>
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder="密码（至少 4 个字符）" size="large" />
          </Form.Item>
          <Form.Item
            label="确认密码"
            name="confirmPassword"
            dependencies={['password']}
            rules={[
              { required: true, message: '请再次输入密码' },
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
            <Input.Password placeholder="再次输入密码" size="large" />
          </Form.Item>
          <GeetestCaptchaField
            enabled={config.geetest_enabled}
            captchaId={config.geetest_captcha_id}
            containerId="geetest-register-container"
            captchaRef={captchaRef}
          />
          {config.email_verification_enabled ? (
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
          ) : null}
          <Button type="primary" htmlType="submit" size="large" block loading={loading}>
            注册
          </Button>
          <div className="login-register-row">
            <Typography.Text>已有账号？</Typography.Text>
            <Typography.Link className="login-register-link" onClick={onBackToLogin}>
              登录
            </Typography.Link>
          </div>
        </Form>
      </Card>
    </div>
  )
}
