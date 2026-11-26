import { onlineManager } from '@tanstack/react-query'
import { useSyncExternalStore } from 'react'

// One online/offline signal for the whole app: fetch failures, proxy errors (502-504) and the browser's own events
// all end up in TanStack's onlineManager, and this store mirrors it for the UI.
type State = { online: boolean; network: boolean; checking: boolean; lastTry?: Date }
let state: State = { online: navigator.onLine, network: navigator.onLine, checking: false }
const listeners = new Set<() => void>()
let timer: number | undefined
let started = false

function set(patch: Partial<State>) {
  state = { ...state, ...patch }
  listeners.forEach(l => l())
}

function setOnline(online: boolean) {
  onlineManager.setOnline(online)
  set({ online, network: navigator.onLine })
  window.clearInterval(timer)
  timer = online ? undefined : window.setInterval(probe, 5000)
}

async function probe() {
  if (state.checking) return
  set({ checking: true })
  let ok = false
  try { ok = (await fetch('/healthz', { cache: 'no-store' })).ok } catch { /* stay offline */ }
  if (ok) setOnline(true)
  set({ checking: false, lastTry: new Date(), network: navigator.onLine })
}

/** Call once at startup. */
export function startConnectionWatch() {
  if (started) return
  started = true
  onlineManager.setEventListener(() => () => {}) // the probe decides when we are online, not the browser's `online` event
  const lost = () => { if (state.online) setOnline(false) }
  window.addEventListener('offline', lost)
  window.addEventListener('docket:disconnected', lost)
  window.addEventListener('online', () => { if (!state.online) void probe() })
  if (!state.online) setOnline(false)
}

export const useOnline = () => useSyncExternalStore(onlineManager.subscribe, () => onlineManager.isOnline())

export function useConnection() {
  const s = useSyncExternalStore(cb => { listeners.add(cb); return () => listeners.delete(cb) }, () => state)
  return { ...s, retry: () => void probe() }
}

let unsavedTimer: number | undefined
const unsavedListeners = new Set<() => void>()
let unsaved = false
function setUnsaved(v: boolean) { unsaved = v; unsavedListeners.forEach(l => l()) }

/** A change could not be sent because we are offline. */
export function reportUnsaved() {
  window.clearTimeout(unsavedTimer)
  unsavedTimer = window.setTimeout(() => setUnsaved(false), 5000)
  setUnsaved(true)
}

/** True for 5 seconds after the last reportUnsaved. */
export function useUnsavedFlash() {
  return useSyncExternalStore(cb => { unsavedListeners.add(cb); return () => unsavedListeners.delete(cb) }, () => unsaved)
}
