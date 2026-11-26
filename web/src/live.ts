import { workspaceApi } from './workspacePath'
// Live updates: /api/events (SSE) invalidates the queries, so changes made through MCP, another tab or another
// device show up at once on every page (board, wiki, integrations log...).
// Tasks touched by agents are highlighted for 3 seconds.
import { useEffect, useSyncExternalStore } from 'react'
import { isAgent } from './api'
import { onlineManager, type QueryClient } from '@tanstack/react-query'

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

/** True for 3 seconds after an agent changed the task. */
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

/** Keeps the stream open while the app is on screen (like a native app): closed when hidden, reopened with a full
 *  refetch when it is shown again, the network returns, or the server drops a stream the browser gave up on. */
export function useLive(qc: QueryClient) {
  useEffect(() => {
    let es: EventSource | undefined
    let timer: number | undefined
    let retryTimer: number | undefined
    let delay = 1000
    const refresh = () => {
      if (timer !== undefined) return
      timer = window.setTimeout(() => {
        timer = undefined
        void qc.invalidateQueries(undefined, { cancelRefetch: false })
      }, 120)
    }
    const visible = () => document.visibilityState === 'visible'
    const close = () => {
      window.clearTimeout(retryTimer)
      es?.close()
      es = undefined
    }
    const open = () => {
      if (!visible() || (es && es.readyState !== EventSource.CLOSED)) return
      close()
      const s = (es = new EventSource(workspaceApi('/api/events')))
      s.onopen = () => {
        delay = 1000
        refresh() // catch up on whatever changed while the stream was closed
      }
      s.onmessage = (m) => {
        try {
          const e = JSON.parse(m.data) as { actor: string; task_id?: number }
          if (isAgent(e.actor) && e.task_id) markFresh(e.task_id)
        } catch {
          /* a malformed event still triggers a refresh */
        }
        refresh()
      }
      // While CONNECTING the browser retries by itself; a CLOSED stream (HTTP error, proxy restart) never comes back.
      s.onerror = () => {
        if (s.readyState !== EventSource.CLOSED) return
        window.clearTimeout(retryTimer)
        retryTimer = window.setTimeout(open, delay)
        delay = Math.min(delay * 2, 30_000)
      }
    }
    const sync = () => (visible() ? open() : close())
    sync()
    document.addEventListener('visibilitychange', sync)
    window.addEventListener('pageshow', sync) // back from the back/forward cache
    const unsubscribe = onlineManager.subscribe(online => { if (online) open() })
    return () => {
      document.removeEventListener('visibilitychange', sync)
      window.removeEventListener('pageshow', sync)
      unsubscribe()
      window.clearTimeout(timer)
      close()
    }
  }, [qc])
}
