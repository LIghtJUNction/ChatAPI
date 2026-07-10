import type { MutableRefObject } from 'react'
import { Button, Card, Form, Input, Typography } from 'antd'

import { CosmicBackdrop } from './CosmicBackdrop'
import { GeetestCaptchaField, type GeetestCaptcha } from './GeetestCaptchaField'
import { appMessage } from '../lib/antdMessage'
import type { LoginFormValues } from '../types/chat'

type LoginScreenProps = {
  loading: boolean
  totpEnabled: boolean
  registrationEnabled: boolean
  geetestEnabled: boolean
  geetestCaptchaId: string
  geetestCaptchaRef: MutableRefObject<GeetestCaptcha | null>
  onSubmit: (values: LoginFormValues) => void | Promise<void>
  onNavigateToRegister: () => void
  onNavigateToForgotPassword: () => void
}

export function LoginScreen({
  loading,
  onSubmit,
  totpEnabled,
  registrationEnabled,
  geetestEnabled,
  geetestCaptchaId,
  geetestCaptchaRef,
  onNavigateToRegister,
  onNavigateToForgotPassword,
}: LoginScreenProps) {
  const [form] = Form.useForm<LoginFormValues>()

  async function handleFinish(values: LoginFormValues) {
    let geetestParams: LoginFormValues['geetest_params']
    if (geetestEnabled) {
      const result = geetestCaptchaRef.current?.getValidate()
      if (!result) {
        appMessage.warning('请先完成人机验证')
        return
      }
      geetestParams = result
    }

    await onSubmit({
      ...values,
      geetest_params: geetestParams,
    })
  }

  return (
    <div className="login-screen">
      <CosmicBackdrop />
      <div className="login-glow login-glow-left" aria-hidden="true" />
      <div className="login-glow login-glow-right" aria-hidden="true" />
      <Card className="login-card">
        <div className="login-copy">
          <Typography.Title level={2} className="login-title">
            ChatAPI 登录
          </Typography.Title>
        </div>
        <Form
          form={form}
          layout="vertical"
          onFinish={(values) => void handleFinish(values)}
          autoComplete="off"
          className="login-form"
          initialValues={{ username: '', password: '', totp: '' }}
        >
          <Form.Item
            label="邮箱或用户名"
            name="username"
            rules={[{ required: true, message: '请输入账号' }]}
          >
            <Input placeholder="使用邮箱或用户名登录" size="large" />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder="密码" size="large" />
          </Form.Item>
          {totpEnabled ? (
            <Form.Item
              label="验证码"
              name="totp"
              rules={[{ required: true, message: '请输入验证码' }]}
            >
              <Input
                placeholder="6 位 TOTP 验证码"
                size="large"
                inputMode="numeric"
                maxLength={6}
              />
            </Form.Item>
          ) : null}
          <GeetestCaptchaField
            enabled={geetestEnabled}
            captchaId={geetestCaptchaId}
            containerId="geetest-login-container"
            captchaRef={geetestCaptchaRef}
          />
          <Button type="primary" htmlType="submit" size="large" block loading={loading}>
            登录
          </Button>
          {registrationEnabled ? (
            <div className="login-register-row">
              <Typography.Text>没有账号？</Typography.Text>
              <Typography.Link className="login-register-link" onClick={onNavigateToRegister}>
                注册
              </Typography.Link>
              <Typography.Text className="login-register-separator">或</Typography.Text>
              <Typography.Link className="login-register-link" onClick={onNavigateToForgotPassword}>
                忘记密码？
              </Typography.Link>
            </div>
          ) : (
            <div className="login-register-row">
              <Typography.Link className="login-register-link" onClick={onNavigateToForgotPassword}>
                忘记密码？
              </Typography.Link>
            </div>
          )}
        </Form>
      </Card>
    </div>
  )
}
