import { Input, InputNumber, Tag, Typography } from 'antd'

import type { SettingField, SettingSource } from '../model/types'

type Props = {
  requestsField: SettingField
  windowField: SettingField
  requestsSource: SettingSource
  windowSource: SettingSource
  requestsValue: unknown
  windowValue: unknown
  disabled?: boolean
  onRequestsChange: (value: number) => void
  onWindowChange: (value: string) => void
}

function subjectLabel(title: string) {
  const subject = title.replace(/请求上限$/, '').trim()
  return subject === '匿名访问' ? '同 IP 匿名' : subject
}

export function RateLimitFieldRow({
  requestsField,
  windowField,
  requestsSource,
  windowSource,
  requestsValue,
  windowValue,
  disabled: formDisabled = false,
  onRequestsChange,
  onWindowChange,
}: Props) {
  const requestsDisabled = formDisabled || !requestsField.editable || requestsSource === 'environment'
  const windowDisabled = formDisabled || !windowField.editable || windowSource === 'environment'
  const sources = new Set([requestsSource, windowSource])
  const subject = subjectLabel(requestsField.title)

  return (
    <div className="admin-setting-row admin-rate-limit-row">
      <div className="admin-setting-copy">
        <div className="admin-setting-title">
          <Typography.Text strong>{subject}请求次数上限</Typography.Text>
          {sources.has('environment') ? <Tag color="blue">含环境变量配置</Tag> : null}
          {sources.has('database') ? <Tag>已覆盖</Tag> : null}
        </div>
        <Typography.Text type="secondary">同一限流主体在统计窗口内允许的请求数，0 表示禁用。</Typography.Text>
      </div>
      <div className="admin-rate-limit-control">
        <Input
          aria-label={`${subject}限流窗口`}
          value={String(windowValue ?? '')}
          disabled={windowDisabled}
          placeholder="例如 1m"
          onChange={(event) => onWindowChange(event.target.value)}
        />
        <Typography.Text>内</Typography.Text>
        <InputNumber
          aria-label={`${subject}请求次数上限`}
          value={typeof requestsValue === 'number' ? requestsValue : Number(requestsValue ?? 0)}
          min={requestsField.minimum}
          max={requestsField.maximum}
          precision={0}
          disabled={requestsDisabled}
          onChange={(value) => onRequestsChange(Number(value ?? 0))}
        />
        <Typography.Text>次</Typography.Text>
      </div>
    </div>
  )
}
