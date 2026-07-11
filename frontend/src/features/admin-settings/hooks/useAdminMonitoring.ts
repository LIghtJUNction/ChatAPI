import { useEffect, useRef, useState } from 'react'

import { resolveEventSourceUrl } from '../../../lib/api'

export type AdminRuntimeMetrics = {
  sampled_at: string
  uptime_seconds: number
  cpu_count: number
  cpu_usage_percent: number
  goroutines: number
  heap_alloc_bytes: number
  heap_inuse_bytes: number
  sys_bytes: number
  memory_total_bytes: number
  memory_available_bytes: number
  swap_total_bytes: number
  swap_used_bytes: number
}

type MonitoringEvent = {
  type: 'monitor.snapshot' | 'user.connection.updated' | 'system.metrics.updated'
  user_id?: string
  connection_count?: number
  total_connections: number
  user_connections?: Record<string, number>
  metrics?: AdminRuntimeMetrics
  sequence: number
}

export function useAdminMonitoring(active: boolean, userIDs: string[] | null) {
  const sequence = useRef(0)
  const generation = useRef(0)
  const [connected, setConnected] = useState(false)
  const [totalConnections, setTotalConnections] = useState(0)
  const [userConnections, setUserConnections] = useState<Record<string, number>>({})
  const [metrics, setMetrics] = useState<AdminRuntimeMetrics | null>(null)

  useEffect(() => {
    if (!active || userIDs === null) return
    const currentGeneration = ++generation.current
    let mounted = true
    sequence.current = 0
    const target = new URL(resolveEventSourceUrl('/api/admin/monitor/stream'))
    target.searchParams.set('user_ids', userIDs.join(','))
    const source = new EventSource(target.toString(), { withCredentials: true })
    source.onopen = () => {
      if (mounted && generation.current === currentGeneration) setConnected(true)
    }
    source.onerror = () => {
      if (mounted && generation.current === currentGeneration) setConnected(false)
    }
    const receive = (raw: Event) => {
      if (!mounted || generation.current !== currentGeneration) return
      try {
        const payload = JSON.parse((raw as MessageEvent<string>).data) as MonitoringEvent
        if (payload.sequence < sequence.current) return
        sequence.current = payload.sequence
        setTotalConnections(payload.total_connections)
        if (payload.metrics) setMetrics(payload.metrics)
        if (payload.user_connections) setUserConnections(payload.user_connections)
        if (payload.type === 'user.connection.updated' && payload.user_id) {
          setUserConnections((current) => ({
            ...current,
            [payload.user_id!]: payload.connection_count ?? 0,
          }))
        }
      } catch {
        // EventSource remains usable after a malformed monitoring frame.
      }
    }
    source.addEventListener('monitor.snapshot', receive)
    source.addEventListener('user.connection.updated', receive)
    source.addEventListener('system.metrics.updated', receive)
    return () => {
      mounted = false
      source.close()
      setConnected(false)
    }
  }, [active, userIDs])

  return { connected, metrics, totalConnections, userConnections }
}
