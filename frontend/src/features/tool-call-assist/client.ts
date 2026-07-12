import { toolArgumentsToFormValues, validateToolArguments } from '../../lib/tool-arguments'
import { getAssistProtocolAdapter } from './protocols'
import type { BrowserAssistConfig, ToolCallAssistInput, ToolCallAssistResult } from './types'

const REQUEST_TIMEOUT_MS = 45_000
const MAX_RESPONSE_BYTES = 1_048_576

function endpoint(baseUrl: string, path: string) {
  let base: URL
  try {
    base = new URL(baseUrl)
  } catch {
    throw new Error('Base URL 必须是完整的 http 或 https 地址')
  }
  if (base.protocol !== 'https:' && base.protocol !== 'http:') {
    throw new Error('Base URL 只支持 http 或 https')
  }
  if (typeof window !== 'undefined' && base.origin === window.location.origin) {
    throw new Error('辅助模型地址不能指向当前 ChatAPI 服务')
  }
  return new URL(`${base.pathname.replace(/\/+$/, '')}/${path}`, base).toString()
}

function parseResult(text: string, schema: ToolCallAssistInput['schema']): ToolCallAssistResult {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch (error) {
    throw new Error('上游没有返回有效 JSON', { cause: error })
  }
  validateToolArguments(parsed, schema)
  return toolArgumentsToFormValues(parsed, schema)
}

async function readJSONResponse(response: Response): Promise<unknown> {
  const declaredSize = Number(response.headers.get('content-length') || 0)
  if (Number.isFinite(declaredSize) && declaredSize > MAX_RESPONSE_BYTES) {
    throw new Error('辅助模型响应超过 1 MiB 上限')
  }
  if (!response.body) return null
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    total += value.byteLength
    if (total > MAX_RESPONSE_BYTES) {
      await reader.cancel()
      throw new Error('辅助模型响应超过 1 MiB 上限')
    }
    chunks.push(value)
  }
  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.byteLength
  }
  const text = new TextDecoder().decode(bytes)
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch (error) {
    throw new Error('辅助模型返回了无效 JSON 响应', { cause: error })
  }
}

export async function generateToolCallAssist(
  config: BrowserAssistConfig,
  input: ToolCallAssistInput,
  signal?: AbortSignal,
): Promise<ToolCallAssistResult> {
  if (!config.baseUrl.trim() || !config.model.trim() || !config.apiKey.trim()) {
    throw new Error('请先在设置中完整配置辅助模型')
  }
  const adapter = getAssistProtocolAdapter(config.protocol)
  const request = adapter.buildRequest(input, config.model.trim())
  const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS)
  const combinedSignal = signal ? AbortSignal.any([signal, timeout]) : timeout
  let response: Response
  try {
    response = await fetch(endpoint(config.baseUrl.trim(), request.path), {
      method: 'POST',
      headers: { Authorization: `Bearer ${config.apiKey.trim()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(request.body),
      signal: combinedSignal,
      redirect: 'error',
    })
  } catch (error) {
    if (combinedSignal.aborted) {
      throw new Error(signal?.aborted ? '已取消 AI 填写' : '辅助模型请求超时', { cause: error })
    }
    throw new Error(error instanceof Error ? `无法连接辅助模型：${error.message}` : '无法连接辅助模型', { cause: error })
  }
  const payload = await readJSONResponse(response)
  if (!response.ok) {
    const detail = payload && typeof payload === 'object'
      ? (payload as { error?: { message?: string }; message?: string }).error?.message || (payload as { message?: string }).message
      : ''
    throw new Error(detail || `辅助模型请求失败 (${response.status})`)
  }
  return parseResult(adapter.extractText(payload), input.schema)
}
