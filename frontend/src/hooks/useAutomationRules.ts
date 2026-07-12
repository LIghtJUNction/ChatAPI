import { useCallback, useState } from 'react'

import { requestJson } from '../lib/api'
import { appMessage } from '../lib/antdMessage'
import type { AutomationRule } from '../types/chat'

function buildEmptyAutomationRule(): AutomationRule {
  return {
    schema_version: 2,
    id: '',
    name: '新自动化规则',
    enabled: false,
    priority: 0,
    match: { target: 'last_user_text', pattern: '' },
    playback: { mode: 'recorded', initial_delay_ms: 0, fixed_interval_ms: 200, loop: false, loop_interval_ms: 1000 },
    steps: [],
  }
}

function cloneRule(rule: AutomationRule): AutomationRule {
  return {
    ...rule,
    match: { ...rule.match },
    playback: { ...rule.playback },
    steps: (Array.isArray(rule.steps) ? rule.steps : []).map((step) => ({ ...step, action: { ...step.action } })),
  }
}

export function useAutomationRules() {
  const [automationRulesModalOpen, setAutomationRulesModalOpen] = useState(false)
  const [automationRuleEditorOpen, setAutomationRuleEditorOpen] = useState(false)
  const [automationRules, setAutomationRules] = useState<AutomationRule[]>([])
  const [editingAutomationRule, setEditingAutomationRule] = useState<AutomationRule | null>(null)
  const [savingAutomationRules, setSavingAutomationRules] = useState(false)

  const loadAutomationRules = useCallback(async () => {
    const data = await requestJson<{ rules?: AutomationRule[] }>('/api/automation/rules')
    setAutomationRules(Array.isArray(data.rules) ? data.rules : [])
  }, [])

  async function saveRule(rule: AutomationRule, successText: string) {
    setSavingAutomationRules(true)
    try {
      const path = rule.id
        ? `/api/automation/rules/${encodeURIComponent(rule.id)}`
        : '/api/automation/rules'
      const response = await requestJson<{ rule: AutomationRule }>(path, {
        method: rule.id ? 'PUT' : 'POST',
        body: JSON.stringify(rule),
      })
      setAutomationRules((current) => {
        const remaining = current.filter((item) => item.id !== response.rule.id)
        return [response.rule, ...remaining]
      })
      appMessage.success(successText)
      return response.rule
    } finally {
      setSavingAutomationRules(false)
    }
  }

  async function handleSaveAutomationRule(rule: AutomationRule) {
    if (!rule.name.trim()) {
      appMessage.warning('请输入规则名称')
      return
    }
		if (rule.enabled && (!rule.match.pattern.trim() || rule.steps.length === 0)) {
      appMessage.warning('启用规则前需要填写匹配正则并至少保留一个步骤')
			return
		}
		if (rule.playback.loop && rule.playback.loop_interval_ms < 1) {
			appMessage.warning('循环间隔必须大于 0ms')
			return
		}
		if (rule.playback.loop && rule.steps.some((step) => ['stream_complete', 'respond', 'abort'].includes(step.action.kind))) {
			appMessage.warning('循环规则不能包含结束输出或错误步骤')
			return
		}
    try {
      await saveRule(cloneRule(rule), '规则已保存')
      setAutomationRuleEditorOpen(false)
      setEditingAutomationRule(null)
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '规则保存失败')
    }
  }

  async function handleDeleteAutomationRule(ruleId: string) {
    setSavingAutomationRules(true)
    try {
      await requestJson(`/api/automation/rules/${encodeURIComponent(ruleId)}`, { method: 'DELETE' })
      setAutomationRules((current) => current.filter((item) => item.id !== ruleId))
      appMessage.success('规则已删除')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '规则删除失败')
    } finally {
      setSavingAutomationRules(false)
    }
  }

  async function handleToggleAutomationRule(ruleId: string, enabled: boolean) {
    const rule = automationRules.find((item) => item.id === ruleId)
    if (!rule) return
    try {
      await saveRule({ ...cloneRule(rule), enabled }, enabled ? '规则已启用' : '规则已停用')
    } catch (error) {
      appMessage.error(error instanceof Error ? error.message : '规则状态更新失败')
    }
  }

  function handleCreateAutomationRule() {
    setEditingAutomationRule(buildEmptyAutomationRule())
    setAutomationRuleEditorOpen(true)
  }

  function handleEditAutomationRule(ruleId: string) {
    const rule = automationRules.find((item) => item.id === ruleId)
    if (!rule) return
    setEditingAutomationRule(cloneRule(rule))
    setAutomationRuleEditorOpen(true)
  }

  const openRecordedDraft = useCallback((rule: AutomationRule) => {
    setAutomationRules((current) => [rule, ...current.filter((item) => item.id !== rule.id)])
    setEditingAutomationRule(cloneRule(rule))
    setAutomationRuleEditorOpen(true)
  }, [])

  function resetAutomationRuleUi() {
    setAutomationRulesModalOpen(false)
    setAutomationRuleEditorOpen(false)
    setEditingAutomationRule(null)
  }

  return {
    automationRuleEditorOpen, automationRules, automationRulesModalOpen, editingAutomationRule,
    handleCreateAutomationRule, handleDeleteAutomationRule, handleEditAutomationRule,
    handleSaveAutomationRule, handleToggleAutomationRule, loadAutomationRules, openRecordedDraft,
    resetAutomationRuleUi, savingAutomationRules, setAutomationRuleEditorOpen,
    setAutomationRules, setAutomationRulesModalOpen, setEditingAutomationRule,
  }
}
