import { useEffect, useRef, useState } from 'react'
import { Button, Input, Popover, Space, Typography } from 'antd'
import { CloseOutlined, RobotOutlined, SendOutlined } from '@ant-design/icons'

import type { ToolSchemaOption } from '../../types/chat'
import { generateToolCallAssist } from './client'
import { loadBrowserAssistConfig } from './storage'
import type { ToolCallAssistResult } from './types'

const { TextArea } = Input

export function ToolCallAssistPopover({
  disabled,
  onApply,
  userID,
  schema,
}: {
  disabled: boolean
  onApply: (result: ToolCallAssistResult) => void
  schema: ToolSchemaOption | null
  userID: string
}) {
  const [open, setOpen] = useState(false)
  const [instruction, setInstruction] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(false)

  function close() {
    controllerRef.current?.abort()
    controllerRef.current = null
    setLoading(false)
    setInstruction('')
    setError('')
    setOpen(false)
  }

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      controllerRef.current?.abort()
      controllerRef.current = null
    }
  }, [])
  async function submit() {
    if (!schema || !instruction.trim() || loading) return
    const controller = new AbortController()
    controllerRef.current = controller
    setLoading(true)
    setError('')
    try {
      const result = await generateToolCallAssist(loadBrowserAssistConfig(userID), {
        instruction: instruction.trim(),
        schema: schema.parameters,
        toolDescription: schema.description,
        toolName: schema.name,
      }, controller.signal)
      if (!mountedRef.current || controller.signal.aborted || controllerRef.current !== controller) return
      onApply(result)
      close()
    } catch (requestError) {
      if (!controller.signal.aborted) {
        setError(requestError instanceof Error ? requestError.message : 'AI 填写失败')
      }
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null
        if (mountedRef.current) setLoading(false)
      }
    }
  }

  return (
    <Popover
      arrow={false}
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close()
        else setOpen(true)
      }}
      placement="topRight"
      trigger="click"
      content={(
        <div className="tool-assist-popover">
          <div className="tool-assist-heading">
            <div>
              <Typography.Text strong>AI 填写 {schema?.name}</Typography.Text>
              <Typography.Text type="secondary" className="tool-assist-subtitle">
                Schema 和下方内容将由浏览器直接发送给辅助模型
              </Typography.Text>
            </div>
            <Button type="text" icon={<CloseOutlined />} onClick={close} aria-label="关闭" />
          </div>
          <TextArea
            autoFocus
            value={instruction}
            onChange={(event) => setInstruction(event.target.value)}
            onKeyDown={(event) => {
              if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') void submit()
            }}
            autoSize={{ minRows: 4, maxRows: 9 }}
            placeholder="告诉模型如何根据当前任务填写这些参数"
            disabled={loading}
          />
          {error ? <Typography.Text type="danger" className="tool-assist-error">{error}</Typography.Text> : null}
          <Space className="tool-assist-actions">
            {loading ? <Button onClick={() => controllerRef.current?.abort()}>取消</Button> : null}
            <Button
              type="primary"
              icon={<SendOutlined />}
              loading={loading}
              disabled={!instruction.trim()}
              onClick={() => void submit()}
            >
              填写表单
            </Button>
          </Space>
        </div>
      )}
    >
      <Button icon={<RobotOutlined />} disabled={disabled || !schema}>AI 填写</Button>
    </Popover>
  )
}
