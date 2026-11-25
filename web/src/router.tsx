// A tiny history-API router: the app has five routes and one query parameter (?task=), no library needed.
import { type ComponentProps, type MouseEvent, useSyncExternalStore } from 'react'

import { workspacePath } from './workspacePath'

const EVENT = 'docket:navigate'

function subscribe(cb: () => void) {
  window.addEventListener('popstate', cb)
  window.addEventListener(EVENT, cb)
  return () => {
    window.removeEventListener('popstate', cb)
    window.removeEventListener(EVENT, cb)
  }
}

const snapshot = () => location.pathname + location.search

export function useRoute() {
  const s = useSyncExternalStore(subscribe, snapshot)
  const [path, search = ''] = s.split('?')
  return { path: path.replace(/^\/w\/[1-9]\d*(?=\/|$)/, '') || '/', params: new URLSearchParams(search) }
}

export function navigate(to: string, replace = false) {
  to = workspacePath(to)
  if (to === snapshot()) return
  history[replace ? 'replaceState' : 'pushState'](null, '', to)
  window.dispatchEvent(new Event(EVENT))
}

export function scrollToSection(element: HTMLElement | null) {
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  element?.scrollIntoView({ behavior: reducedMotion ? 'instant' : 'smooth', block: 'start' })
}

/** Sets or removes one query parameter of the current URL (null removes it). */
export function setParam(key: string, value: string | null, replace = true) {
  const p = new URLSearchParams(location.search)
  if (value === null) p.delete(key)
  else p.set(key, value)
  const q = p.toString()
  navigate(location.pathname + (q ? '?' + q : ''), replace)
}

/** The current URL with one query parameter set (null removes it), for links such as ?task=TRK-07. */
export function hrefWith(key: string, value: string | null) {
  const p = new URLSearchParams(location.search)
  if (value === null) p.delete(key)
  else p.set(key, value)
  const q = p.toString()
  return location.pathname + (q ? '?' + q : '')
}

export function Link({ to, onClick, ...rest }: { to: string } & ComponentProps<'a'>) {
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e)
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    navigate(to)
  }
  return <a href={workspacePath(to)} onClick={click} {...rest} />
}
