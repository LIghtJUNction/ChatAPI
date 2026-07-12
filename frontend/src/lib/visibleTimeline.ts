import type { TimelineItem, VisibleTimelineItem } from '../types/chat'

export function buildVisibleTimeline(
  items: TimelineItem[],
  draftBuffer: string,
): VisibleTimelineItem[] {
  const visible: VisibleTimelineItem[] = []
  const toolResultIndexByCallId = new Map<string, number>()

  for (const item of items) {
    if (item.kind !== 'message' || !item.message) {
      visible.push(item)
      continue
    }

    const message = item.message
    const isToolResult = message.metadata?.response_mode === 'tool_result'
    const toolCallId = String(message.metadata?.tool_call_id ?? '').trim()

    if (isToolResult && toolCallId) {
      const existingIndex = toolResultIndexByCallId.get(toolCallId)
      if (existingIndex != null) {
        visible[existingIndex] = item
        continue
      }
      toolResultIndexByCallId.set(toolCallId, visible.length)
    }

    visible.push(item)
  }

  if (!draftBuffer) {
    return visible
  }

  return [
    ...visible,
    {
      id: 'draft-buffer',
      kind: 'draft',
      created_at: new Date().toISOString(),
      draft: true,
      content: draftBuffer,
    },
  ]
}
