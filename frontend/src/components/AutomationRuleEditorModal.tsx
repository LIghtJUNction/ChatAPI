import { Button, Input, InputNumber, Modal, Segmented, Select, Space, Switch, Typography } from 'antd'
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  PlusOutlined,
} from '@ant-design/icons'

import type { AutomationAction, AutomationRule, AutomationStep } from '../types/chat'

type Props = {
  editingAutomationRule: AutomationRule | null
  open: boolean
  saving: boolean
  setEditingAutomationRule: (value: AutomationRule | null) => void
  onCancel: () => void
  onSave: (rule: AutomationRule) => void | Promise<void>
}

function actionLabel(action: AutomationAction) {
  if (action.kind === 'stream_delta') return action.mode === 'thinking' ? '思考增量' : '文本增量'
  if (action.kind === 'stream_complete') {
    if (action.mode === 'tool_call') return `Tool Call${action.tool_name ? ` · ${action.tool_name}` : ''}`
    return '结束输出'
  }
  if (action.kind === 'respond') return '一次性回复'
  if (action.kind === 'builtin_tool') return `内置工具 · ${action.builtin_tool_kind || 'unknown'}`
  if (action.kind === 'abort') return '返回错误'
  return action.kind
}

type EditableActionKind = 'stream_delta' | 'stream_complete' | 'respond' | 'builtin_tool' | 'abort'

function isTerminalKind(kind: string) {
  return kind === 'stream_complete' || kind === 'respond' || kind === 'abort'
}

function newStep(kind: EditableActionKind): AutomationStep {
  return {
    id: `step_${crypto.randomUUID()}`,
    delay_before_ms: 200,
    action: {
      kind,
    mode: kind === 'stream_delta' ? 'answer' : 'assistant_message',
      text: '',
    builtin_tool_kind: kind === 'builtin_tool' ? 'web_search' : undefined,
      error: kind === 'abort' ? 'automation aborted request' : undefined,
    },
  }
}

export function AutomationRuleEditorModal({
  editingAutomationRule,
  open,
  saving,
  setEditingAutomationRule,
  onCancel,
  onSave,
}: Props) {
  function updateRule(update: (rule: AutomationRule) => AutomationRule) {
    if (editingAutomationRule) setEditingAutomationRule(update(editingAutomationRule))
  }

  function updateStep(index: number, update: (step: AutomationStep) => AutomationStep) {
    updateRule((rule) => ({
      ...rule,
      steps: rule.steps.map((step, current) => (current === index ? update(step) : step)),
    }))
  }

  function moveStep(index: number, direction: -1 | 1) {
    updateRule((rule) => {
      const target = index + direction
      if (target < 0 || target >= rule.steps.length) return rule
      const steps = [...rule.steps]
      ;[steps[index], steps[target]] = [steps[target], steps[index]]
    if (steps.some((step, current) => isTerminalKind(step.action.kind) && current !== steps.length - 1)) return rule
      return { ...rule, steps }
    })
  }

  function addStep(kind: EditableActionKind) {
  updateRule((rule) => {
    const step = newStep(kind)
		if (isTerminalKind(kind)) {
			return {
				...rule,
				playback: { ...rule.playback, loop: false },
				steps: [...rule.steps.filter((item) => !isTerminalKind(item.action.kind)), step],
			}
    }
    const terminalIndex = rule.steps.findIndex((item) => isTerminalKind(item.action.kind))
    if (terminalIndex < 0) return { ...rule, steps: [...rule.steps, step] }
    return { ...rule, steps: [...rule.steps.slice(0, terminalIndex), step, ...rule.steps.slice(terminalIndex)] }
  })
  }

  function changeStepKind(index: number, kind: EditableActionKind) {
    updateRule((rule) => {
      const replacement = { ...rule.steps[index], action: newStep(kind).action }
      if (!isTerminalKind(kind)) {
        return { ...rule, steps: rule.steps.map((step, current) => (current === index ? replacement : step)) }
      }
			return {
				...rule,
				playback: { ...rule.playback, loop: false },
				steps: [...rule.steps.filter((step, current) => current !== index && !isTerminalKind(step.action.kind)), replacement],
      }
    })
  }

  return (
    <Modal
      title={editingAutomationRule?.name || '自动化规则'}
      width={860}
      open={open}
      onCancel={onCancel}
      onOk={() => editingAutomationRule && void onSave(editingAutomationRule)}
      okText="保存规则"
      cancelText="取消"
      okButtonProps={{ loading: saving }}
      cancelButtonProps={{ disabled: saving }}
      destroyOnHidden
    >
      {editingAutomationRule ? (
        <Space direction="vertical" size={18} style={{ width: '100%' }}>
          <div className="automation-editor-grid">
            <div className="automation-editor-inline-field">
              <Typography.Text className="prune-input-label">名称</Typography.Text>
              <Input
                value={editingAutomationRule.name}
                onChange={(event) => updateRule((rule) => ({ ...rule, name: event.target.value }))}
              />
            </div>
            <div className="automation-editor-inline-field">
              <Typography.Text className="prune-input-label">优先级</Typography.Text>
              <InputNumber
                precision={0}
                value={editingAutomationRule.priority}
                onChange={(value) => updateRule((rule) => ({ ...rule, priority: Number(value ?? 0) }))}
              />
            </div>
			<div className="automation-editor-inline-field automation-editor-status-field">
				<Typography.Text className="prune-input-label">状态</Typography.Text>
				<Switch
					checked={editingAutomationRule.enabled}
					checkedChildren="启用"
					unCheckedChildren="停用"
					onChange={(enabled) => updateRule((rule) => ({ ...rule, enabled }))}
				/>
				<Typography.Text className="prune-input-label">循环</Typography.Text>
				<Switch
					checked={editingAutomationRule.playback.loop}
					disabled={editingAutomationRule.steps.some((step) => isTerminalKind(step.action.kind))}
					onChange={(loop) => updateRule((rule) => ({
						...rule,
						playback: {
							...rule.playback,
							loop,
							loop_interval_ms: rule.playback.loop_interval_ms > 0 ? rule.playback.loop_interval_ms : 1000,
						},
					}))}
				/>
				{editingAutomationRule.playback.loop ? (
					<InputNumber
						addonBefore="循环间隔"
						addonAfter="ms"
						min={1}
						value={editingAutomationRule.playback.loop_interval_ms}
						onChange={(value) => updateRule((rule) => ({
							...rule,
							playback: { ...rule.playback, loop_interval_ms: Number(value ?? 1000) },
						}))}
					/>
				) : null}
			</div>
          </div>

          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">匹配</Typography.Title>
            <Typography.Text className="prune-input-label">最后一条 User 消息（Go RE2 正则）</Typography.Text>
            <Input
              value={editingAutomationRule.match.pattern}
              onChange={(event) => updateRule((rule) => ({
                ...rule,
                match: { target: 'last_user_text', pattern: event.target.value },
              }))}
              placeholder="例如：^(帮我|请).*(搜索|查询)"
            />
          </div>

          <div className="automation-editor-section">
            <Typography.Title level={5} className="automation-editor-title">播放节奏</Typography.Title>
            <Space wrap size={12}>
              <Segmented
                value={editingAutomationRule.playback.mode}
                options={[{ label: '录制时的真实节奏', value: 'recorded' }, { label: '固定操作间隔', value: 'fixed' }]}
                onChange={(mode) => updateRule((rule) => ({
                  ...rule,
                  playback: { ...rule.playback, mode: mode as 'recorded' | 'fixed' },
                }))}
              />
              {editingAutomationRule.playback.mode === 'fixed' ? (
                <>
                  <InputNumber
                    addonBefore="首次延时"
                    addonAfter="ms"
                    min={0}
                    value={editingAutomationRule.playback.initial_delay_ms}
                    onChange={(value) => updateRule((rule) => ({
                      ...rule,
                      playback: { ...rule.playback, initial_delay_ms: Number(value ?? 0) },
                    }))}
                  />
                  <InputNumber
                    addonBefore="步骤间隔"
                    addonAfter="ms"
                    min={0}
                    value={editingAutomationRule.playback.fixed_interval_ms}
                    onChange={(value) => updateRule((rule) => ({
                      ...rule,
                      playback: { ...rule.playback, fixed_interval_ms: Number(value ?? 0) },
                    }))}
                  />
                </>
              ) : null}
            </Space>
          </div>

          <div className="automation-editor-section">
            <div className="automation-rules-header">
              <Typography.Title level={5} className="automation-editor-title">
                操作流程 · {editingAutomationRule.steps.length} 步
              </Typography.Title>
              <Space>
                <Button icon={<PlusOutlined />} onClick={() => addStep('stream_delta')}>文本</Button>
        <Button icon={<PlusOutlined />} onClick={() => addStep('respond')}>回复</Button>
        <Button icon={<PlusOutlined />} onClick={() => addStep('builtin_tool')}>搜索</Button>
                <Button icon={<PlusOutlined />} onClick={() => addStep('stream_complete')}>结束</Button>
                <Button danger icon={<PlusOutlined />} onClick={() => addStep('abort')}>错误</Button>
              </Space>
            </div>
            <div className="automation-step-list">
              {editingAutomationRule.steps.length === 0 ? (
                <Typography.Text type="secondary">尚未录制操作，也可以手动添加步骤。</Typography.Text>
              ) : editingAutomationRule.steps.map((step, index) => (
                <div className="automation-step-row" key={step.id}>
                  <div className="automation-step-index">{index + 1}</div>
                  <div className="automation-step-body">
                    <div className="automation-step-heading">
                      <Typography.Text strong>{actionLabel(step.action)}</Typography.Text>
                      <InputNumber
                        addonBefore="等待"
                        addonAfter="ms"
                        min={0}
                        value={step.delay_before_ms}
                        onChange={(value) => updateStep(index, (item) => ({ ...item, delay_before_ms: Number(value ?? 0) }))}
                      />
                    </div>
                    <Space wrap size={8}>
                      <Select
                        aria-label="操作类型"
                        value={step.action.kind as EditableActionKind}
                        options={[
                          { label: '文本增量', value: 'stream_delta' },
                          { label: '结束输出', value: 'stream_complete' },
                          { label: '一次性回复', value: 'respond' },
                          { label: '内置搜索', value: 'builtin_tool' },
                          { label: '返回错误', value: 'abort' },
                        ]}
            onChange={(kind: EditableActionKind) => changeStepKind(index, kind)}
                      />
                      {step.action.kind === 'stream_delta' || step.action.kind === 'stream_complete' || step.action.kind === 'respond' ? (
                        <Select
                          aria-label="输出模式"
                          value={step.action.mode || 'assistant_message'}
                          options={[
                            { label: '助手消息', value: 'assistant_message' },
                            { label: '正文', value: 'answer' },
                            { label: '思考', value: 'thinking' },
                            { label: 'Tool Call', value: 'tool_call' },
                          ]}
                          onChange={(mode) => updateStep(index, (item) => ({ ...item, action: { ...item.action, mode } }))}
                        />
                      ) : null}
                    </Space>
                    {(step.action.kind === 'stream_delta' || step.action.kind === 'stream_complete' || step.action.kind === 'respond') && step.action.mode !== 'tool_call' ? (
                      <Input.TextArea
                        value={step.action.text ?? ''}
                        autoSize={{ minRows: 2, maxRows: 8 }}
                        placeholder={step.action.kind === 'stream_complete' ? '结束时附带的内容，可留空' : '输出内容'}
                        onChange={(event) => updateStep(index, (item) => ({
                          ...item, action: { ...item.action, text: event.target.value },
                        }))}
                      />
                    ) : null}
                    {step.action.mode === 'thinking' ? (
                      <Select
                        aria-label="思考流模式"
                        value={step.action.reasoning_stream_mode || 'reasoning'}
                        options={[{ label: 'Reasoning', value: 'reasoning' }, { label: 'Summary', value: 'summery' }]}
                        onChange={(reasoning_stream_mode) => updateStep(index, (item) => ({
                          ...item, action: { ...item.action, reasoning_stream_mode },
                        }))}
                      />
                    ) : null}
                    {step.action.mode === 'tool_call' ? (
                      <Space direction="vertical" size={8} style={{ width: '100%' }}>
                        <Input value={step.action.tool_name ?? ''} placeholder="Tool 名称" onChange={(event) => updateStep(index, (item) => ({ ...item, action: { ...item.action, tool_name: event.target.value } }))} />
                        <Input value={step.action.tool_call_id ?? ''} placeholder="Tool Call ID" onChange={(event) => updateStep(index, (item) => ({ ...item, action: { ...item.action, tool_call_id: event.target.value } }))} />
            <Input.TextArea value={step.action.text ?? ''} placeholder="Tool Call 参数 JSON" onChange={(event) => updateStep(index, (item) => ({ ...item, action: { ...item.action, text: event.target.value, output: undefined } }))} />
                      </Space>
                    ) : null}
                    {step.action.kind === 'builtin_tool' ? (
                      <Input value={step.action.builtin_tool_query ?? ''} placeholder="搜索词" onChange={(event) => updateStep(index, (item) => ({
                        ...item, action: { ...item.action, builtin_tool_kind: 'web_search', builtin_tool_query: event.target.value },
                      }))} />
                    ) : null}
                    {step.action.kind === 'abort' ? (
                      <Input
                        value={step.action.error ?? ''}
                        onChange={(event) => updateStep(index, (item) => ({
                          ...item, action: { ...item.action, error: event.target.value },
                        }))}
                      />
                    ) : null}
                  </div>
                  <Space direction="vertical" size={4}>
                    <Button type="text" icon={<ArrowUpOutlined />} disabled={index === 0} onClick={() => moveStep(index, -1)} />
                    <Button type="text" icon={<ArrowDownOutlined />} disabled={index === editingAutomationRule.steps.length - 1} onClick={() => moveStep(index, 1)} />
                    <Button danger type="text" icon={<DeleteOutlined />} onClick={() => updateRule((rule) => ({
                      ...rule, steps: rule.steps.filter((_, current) => current !== index),
                    }))} />
                  </Space>
                </div>
              ))}
            </div>
          </div>
        </Space>
      ) : null}
    </Modal>
  )
}
