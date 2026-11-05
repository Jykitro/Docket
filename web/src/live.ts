// Live updates: /api/events (SSE) invalidates the queries, so changes made through MCP show up at once.
// Tasks touched by claude are highlighted for 3 seconds.
import { useEffect, useSyncExternalStore } from 'react'
import type { QueryClient } from '@tanstack/react-query'

const fresh = new Map<number, number>() // task id -> expiry timestamp
let version = 0
const listeners = new Set<() => void>()
const bump = () => {
  version++
  listeners.forEach((l) => l())
}

function markFresh(id: number) {
  fresh.set(id, Date.now() + 3000)
  bump()
  setTimeout(() => {
    if ((fresh.get(id) ?? 0) <= Date.now()) {
      fresh.delete(id)
      bump()
    }
  }, 3050)
}

/** True for 3 seconds after claude changed the task. */
export function useFresh(id: number) {
  useSyncExternalStore(
    (cb) => {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    () => version,
  )
  return (fresh.get(id) ?? 0) > Date.now()
}

export function useLive(qc: QueryClient) {
  useEffect(() => {
    const es = new EventSource('/api/events')
    let timer: number | undefined
    const refresh = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => qc.invalidateQueries(), 120) // coalesce bursts (a batch emits many events)
    }
    es.onmessage = (m) => {
      try {
        const e = JSON.parse(m.data) as { actor: string; task_id?: number }
        if (e.actor === 'claude' && e.task_id) markFresh(e.task_id)
      } catch {
        /* a malformed event still triggers a refresh */
      }
      refresh()
    }
    es.onerror = () => refresh() // the browser reconnects by itself; refetch once it is back
    return () => {
      window.clearTimeout(timer)
      es.close()
    }
  }, [qc])
}
