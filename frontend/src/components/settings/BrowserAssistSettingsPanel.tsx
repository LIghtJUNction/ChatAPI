import { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Popconfirm, Select, Space, Typography } from 'antd'
import { DeleteOutlined, SaveOutlined } from '@ant-design/icons'

import { appMessage } from '../../lib/antdMessage'
import {
  DEFAULT_BROWSER_ASSIST_CONFIG,
  deleteBrowserAssistConfig,
  loadBrowserAssistConfig,
  saveBrowserAssistConfig,
} from '../../features/tool-call-assist/storage'
import type { BrowserAssistConfig } from '../../features/tool-call-assist/types'

export function BrowserAssistSettingsPanel({ open, userID }: { open: boolean; userID: string }) {
  const [form] = Form.useForm<BrowserAssistConfig>()
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (open && userID) form.setFieldsValue(loadBrowserAssistConfig(userID))
  }, [form, open, userID])

  function save(values: BrowserAssistConfig) {
    setSaving(true)
    try {
      saveBrowserAssistConfig(userID, {
        apiKey: values.apiKey.trim(),
        baseUrl: values.baseUrl.trim().replace(/\/+$/, ''),
        model: values.model.trim(),
        protocol: values.protocol,
      })
      appMessage.success('辅助模型配置已保存在当前浏览器')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="browser-assist-settings">
      <Alert
        type="info"
        showIcon
        message="纯浏览器端配置"
        description="配置按当前 ChatAPI 用户隔离并以明文保存在此浏览器，不会由 ChatAPI 后端代理或保存。同源脚本、浏览器扩展或共享设备上的其他操作者可能读取 API Key，请仅在可信设备使用。上游端点必须允许浏览器跨域请求。"
      />
      <Typography.Title level={4}>Tool Call 辅助模型</Typography.Title>
      <Form
        form={form}
        layout="vertical"
        initialValues={DEFAULT_BROWSER_ASSIST_CONFIG}
        onFinish={save}
        className="browser-assist-form"
      >
        <Form.Item name="protocol" label="兼容协议" rules={[{ required: true }]}>
          <Select options={[
            { label: 'OpenAI Responses', value: 'responses' },
            { label: 'OpenAI Chat Completions', value: 'chat_completions' },
          ]} />
        </Form.Item>
        <Form.Item name="baseUrl" label="Base URL" rules={[{ required: true, message: '请输入 Base URL' }]}>
          <Input placeholder="https://api.openai.com/v1" autoComplete="off" />
        </Form.Item>
        <Form.Item name="model" label="模型" rules={[{ required: true, message: '请输入模型名称' }]}>
          <Input placeholder="gpt-5-mini" autoComplete="off" />
        </Form.Item>
        <Form.Item name="apiKey" label="API Key" rules={[{ required: true, message: '请输入 API Key' }]}>
          <Input.Password placeholder="sk-..." autoComplete="off" />
        </Form.Item>
        <Space>
          <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={saving}>保存到当前浏览器</Button>
          <Button onClick={() => form.setFieldsValue(DEFAULT_BROWSER_ASSIST_CONFIG)}>重置表单</Button>
          <Popconfirm
            title="删除当前用户保存在此浏览器中的辅助模型配置？"
            onConfirm={() => {
              deleteBrowserAssistConfig(userID)
              form.setFieldsValue(DEFAULT_BROWSER_ASSIST_CONFIG)
              appMessage.success('本地辅助模型配置已删除')
            }}
          >
            <Button danger icon={<DeleteOutlined />}>删除本地配置</Button>
          </Popconfirm>
        </Space>
      </Form>
    </div>
  )
}
