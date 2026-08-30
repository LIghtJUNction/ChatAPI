import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { getAssistProtocolAdapter } from './protocols.ts'
import type { ToolCallAssistInput } from './types.ts'

const input: ToolCallAssistInput = {
  instruction: 'Find the requested item',
  schema: {
    type: 'object',
    properties: { query: { type: 'string' } },
    required: ['query'],
  },
  toolName: 'lookup',
}

describe('tool call assist protocol request formats', () => {
  it('uses the Chat Completions JSON schema envelope', () => {
    const request = getAssistProtocolAdapter('chat_completions').buildRequest(input, 'gpt-4o-mini')
    const body = request.body as {
      response_format: { json_schema: Record<string, unknown>; type: string }
    }

    assert.deepEqual(body.response_format, {
      type: 'json_schema',
      json_schema: {
        name: 'tool_call_arguments',
        strict: false,
        schema: input.schema,
      },
    })
  })

  it('keeps the Responses JSON schema discriminator inside text.format', () => {
    const request = getAssistProtocolAdapter('responses').buildRequest(input, 'gpt-4o-mini')
    const body = request.body as {
      text: { format: Record<string, unknown> }
    }

    assert.deepEqual(body.text.format, {
      type: 'json_schema',
      name: 'tool_call_arguments',
      strict: false,
      schema: input.schema,
    })
  })
})
