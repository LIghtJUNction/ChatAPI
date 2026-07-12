import { formValuesToToolArguments } from '../../lib/tool-arguments'
import type { JsonSchema, ToolFieldValue } from '../../types/chat'

type BuildToolCallPayloadParams = {
  selectedToolSchema: { parameters: JsonSchema } | null
  toolFormValues: Record<string, ToolFieldValue>
  toolName: string
}

export function buildToolCallPayload({
  selectedToolSchema,
  toolFormValues,
  toolName,
}: BuildToolCallPayloadParams): string {
  if (!toolName.trim()) {
    throw new Error('请先选择一个 tool')
  }

  return JSON.stringify(formValuesToToolArguments(toolFormValues, selectedToolSchema?.parameters ?? {}))
}
