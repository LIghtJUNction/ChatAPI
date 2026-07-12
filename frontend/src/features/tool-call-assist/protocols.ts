import type { JsonSchema } from '../../types/chat'
import type { BrowserAssistProtocol, ToolCallAssistInput } from './types'

export type AssistProtocolRequest = { body: unknown; path: string }

export type AssistProtocolAdapter = {
  buildRequest(input: ToolCallAssistInput, model: string): AssistProtocolRequest
  extractText(payload: unknown): string
}

function responseFormat(schema: JsonSchema) {
  return { type: 'json_schema', name: 'tool_call_arguments', strict: false, schema }
}

function systemInstruction(input: ToolCallAssistInput) {
  const description = input.toolDescription ? `\nTool description: ${input.toolDescription}` : ''
  return `Fill the arguments for function ${input.toolName}.${description}\nReturn exactly one JSON object matching the supplied schema. Do not include markdown or explanation. Use only information in the operator instruction. Leave uncertain optional fields absent.`
}

function requireRecord(payload: unknown) {
  if (!payload || typeof payload !== 'object') throw new Error('上游返回了无效响应')
  return payload as Record<string, unknown>
}

const responsesAdapter: AssistProtocolAdapter = {
  buildRequest(input, model) {
    return {
      path: 'responses',
      body: {
        model,
        instructions: systemInstruction(input),
        input: input.instruction,
        text: { format: responseFormat(input.schema) },
      },
    }
  },
  extractText(payload) {
    const record = requireRecord(payload)
    if (typeof record.output_text === 'string' && record.output_text) return record.output_text
    const output = Array.isArray(record.output) ? record.output : []
    for (const item of output) {
      if (!item || typeof item !== 'object') continue
      const content = Array.isArray((item as Record<string, unknown>).content)
        ? ((item as Record<string, unknown>).content as unknown[])
        : []
      for (const part of content) {
        if (!part || typeof part !== 'object') continue
        const text = (part as Record<string, unknown>).text
        if (typeof text === 'string' && text) return text
      }
    }
    throw new Error('Responses 响应中没有可用的 JSON 文本')
  },
}

const chatCompletionsAdapter: AssistProtocolAdapter = {
  buildRequest(input, model) {
    return {
      path: 'chat/completions',
      body: {
        model,
        messages: [
          { role: 'system', content: systemInstruction(input) },
          { role: 'user', content: input.instruction },
        ],
        response_format: { type: 'json_schema', json_schema: responseFormat(input.schema) },
      },
    }
  },
  extractText(payload) {
    const record = requireRecord(payload)
    const choices = Array.isArray(record.choices) ? record.choices : []
    const message = choices[0] && typeof choices[0] === 'object'
      ? (choices[0] as Record<string, unknown>).message
      : null
    const content = message && typeof message === 'object'
      ? (message as Record<string, unknown>).content
      : null
    if (typeof content === 'string' && content) return content
    throw new Error('Chat Completions 响应中没有可用的 JSON 文本')
  },
}

export function getAssistProtocolAdapter(protocol: BrowserAssistProtocol) {
  return protocol === 'chat_completions' ? chatCompletionsAdapter : responsesAdapter
}
