import type { JsonSchema, ToolFieldValue } from '../../types/chat'

export type BrowserAssistProtocol = 'responses' | 'chat_completions'

export type BrowserAssistConfig = {
  apiKey: string
  baseUrl: string
  model: string
  protocol: BrowserAssistProtocol
}

export type ToolCallAssistInput = {
  instruction: string
  schema: JsonSchema
  toolDescription?: string
  toolName: string
}

export type ToolCallAssistResult = Record<string, ToolFieldValue>
