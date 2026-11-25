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

// Accent colour and corner radius: device-local like the theme. They land on <html> as data-accent / data-radius
// (index.html does it before the first paint); the colours and sizes themselves are in styles.css.
export const ACCENTS = [
  { id: 'purple', label: 'Фиолетовый', color: '#7c5cc4' },
  { id: 'blue', label: 'Синий', color: 'oklch(0.55 0.17 258)' },
  { id: 'teal', label: 'Бирюзовый', color: 'oklch(0.55 0.17 195)' },
  { id: 'green', label: 'Зелёный', color: 'oklch(0.55 0.17 155)' },
  { id: 'orange', label: 'Оранжевый', color: 'oklch(0.55 0.17 45)' },
  { id: 'pink', label: 'Розовый', color: 'oklch(0.55 0.17 350)' },
  { id: 'graphite', label: 'Графит', color: 'oklch(0.35 0 0)' },
] as const
export const RADII = [
  { id: 'sm', label: 'Строгие' },
  { id: 'md', label: 'Обычные' },
  { id: 'lg', label: 'Мягкие' },
] as const
/** Page scale in percent, 75–150 in steps of 5; it lands on <html> as --zoom. */
export const SCALES = Array.from({ length: 16 }, (_, i) => ({ id: String(75 + i * 5) }))
export const WEIGHTS = [
  { id: 'light', label: 'Тоньше' },
  { id: 'normal', label: 'Обычный' },
  { id: 'bold', label: 'Жирнее' },
] as const
export const FONTS = [
  { id: 'inter', label: 'Inter', family: "'Inter', sans-serif" },
  { id: 'system', label: 'Системный', family: 'system-ui, sans-serif' },
  { id: 'serif', label: 'С засечками', family: 'ui-serif, Georgia, serif' },
  { id: 'mono', label: 'Моноширинный', family: "'JetBrains Mono', monospace" },
] as const

/** The page zoom chosen in Appearance (1 by default). Code that turns mouse coordinates into CSS pixels divides by it. */
export const zoom = () => parseFloat(getComputedStyle(document.documentElement).zoom) || 1

function stored<T extends string>(key: string, ok: readonly { id: T }[], fallback: T): T {
  try {
    const v = localStorage.getItem(key)
    const hit = ok.find((o) => o.id === v)
    if (hit) return hit.id
  } catch {
    /* storage can be blocked: use the default */
  }
  return fallback
}

/** One Appearance choice: kept as plandillo-<attr> in localStorage and shown on <html> as data-<attr>. */
export function useLookPref<T extends string>(attr: 'accent' | 'radius' | 'scale' | 'weight' | 'font', options: readonly { id: T }[], fallback: T) {
  const key = 'plandillo-' + attr
  const [value, setValue] = useState<T>(() => stored(key, options, fallback))
  const set = (v: T) => {
    try {
      localStorage.setItem(key, v)
    } catch {
      /* ignore */
    }
    document.documentElement.dataset[attr] = v
    if (attr === 'scale') document.documentElement.style.setProperty('--zoom', String(Number(v) / 100))
    setValue(v)
  }
  return [value, set] as const
}
