// Theme: light, dark or follow the system. The choice lives in localStorage; index.html applies it before the
// first paint (same logic as apply() below) so there is no flash.
import { useEffect, useState } from 'react'

export type ThemePref = 'light' | 'dark' | 'system'
const KEY = 'docket-theme'

function readPref(): ThemePref {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    /* storage can be blocked: fall back to the system theme */
  }
  return 'system'
}

function resolve(p: ThemePref): 'light' | 'dark' {
  return p === 'system' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : p
}

export function useTheme() {
  const [pref, setPref] = useState<ThemePref>(readPref)
  const [, tick] = useState(0)
  useEffect(() => {
    document.documentElement.dataset.theme = resolve(pref)
    if (pref !== 'system') return
    const mq = matchMedia('(prefers-color-scheme: dark)')
    const on = () => {
      document.documentElement.dataset.theme = resolve('system')
      tick((n) => n + 1)
    }
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [pref])
  const set = (p: ThemePref) => {
    try {
      localStorage.setItem(KEY, p)
    } catch {
      /* ignore */
    }
    setPref(p)
  }
  return { pref, resolved: resolve(pref), set }
}
