import type { BrowserAssistConfig } from './types'

const STORAGE_PREFIX = 'chatapi.browser-tool-call-assist.v1'

function storageKey(userID: string) {
  if (!userID.trim()) throw new Error('缺少当前用户，无法读取辅助模型配置')
  return `${STORAGE_PREFIX}.${userID.trim()}`
}

export const DEFAULT_BROWSER_ASSIST_CONFIG: BrowserAssistConfig = {
  apiKey: '',
  baseUrl: 'https://api.openai.com/v1',
  model: '',
  protocol: 'responses',
}

export function loadBrowserAssistConfig(userID: string): BrowserAssistConfig {
  if (typeof window === 'undefined') return DEFAULT_BROWSER_ASSIST_CONFIG
  try {
    const value = JSON.parse(window.localStorage.getItem(storageKey(userID)) || '{}') as Partial<BrowserAssistConfig>
    return {
      apiKey: typeof value.apiKey === 'string' ? value.apiKey : '',
      baseUrl: typeof value.baseUrl === 'string' ? value.baseUrl : DEFAULT_BROWSER_ASSIST_CONFIG.baseUrl,
      model: typeof value.model === 'string' ? value.model : '',
      protocol: value.protocol === 'chat_completions' ? 'chat_completions' : 'responses',
    }
  } catch {
    return DEFAULT_BROWSER_ASSIST_CONFIG
  }
}

export function saveBrowserAssistConfig(userID: string, config: BrowserAssistConfig) {
  window.localStorage.setItem(storageKey(userID), JSON.stringify(config))
}

export function deleteBrowserAssistConfig(userID: string) {
  window.localStorage.removeItem(storageKey(userID))
}
