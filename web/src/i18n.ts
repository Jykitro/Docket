// Localization: English, Russian, Ukrainian. The Russian source text is the key (tr('Настройки')), so Russian needs no
// table and a missing translation falls back to Russian. The choice lives in localStorage; the browser language decides
// the first time. App and Gate call useLang(), so a change re-renders everything below them.
import { useSyncExternalStore } from 'react'
import { MESSAGES } from './messages'

export type Lang = 'en' | 'ru' | 'uk'
export const LANGS: { id: Lang; name: string }[] = [
  { id: 'en', name: 'English' },
  { id: 'ru', name: 'Русский' },
  { id: 'uk', name: 'Українська' },
]
const KEY = 'docket-lang'
const LOCALES: Record<Lang, string> = { en: 'en-GB', ru: 'ru-RU', uk: 'uk-UA' }

function initial(): Lang {
  try {
    const v = localStorage.getItem(KEY)
    if (v === 'en' || v === 'ru' || v === 'uk') return v
  } catch {
    /* storage can be blocked: fall back to the browser language */
  }
  for (const l of navigator.languages) {
    const c = l.slice(0, 2)
    if (c === 'en' || c === 'ru' || c === 'uk') return c
  }
  return 'en'
}

let lang = initial()
const listeners = new Set<() => void>()
document.documentElement.lang = lang

export const getLang = () => lang
/** BCP 47 locale for Intl and toLocale*String. */
export const locale = () => LOCALES[lang]

export function setLang(l: Lang) {
  try {
    localStorage.setItem(KEY, l)
  } catch {
    /* ignore: the choice just is not remembered */
  }
  lang = l
  document.documentElement.lang = l
  listeners.forEach((f) => f())
}

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => void listeners.delete(cb)
}
export const useLang = () => useSyncExternalStore(subscribe, getLang)

/** Translates a Russian source string; {name} placeholders are filled from vars. */
export function tr(ru: string, vars?: Record<string, string | number>): string {
  const s = lang === 'ru' ? ru : MESSAGES[ru]?.[lang === 'en' ? 0 : 1] ?? ru
  return vars ? s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m)) : s
}

/** Picks a plural form. The Russian source lists three ("{n} задача|{n} задачи|{n} задач"); English has two. */
export function plural(n: number, ru: string): string {
  const f = tr(ru).split('|')
  const m = n % 100
  const r = n % 10
  const i = lang === 'en' ? (n === 1 ? 0 : 1) : m >= 11 && m <= 14 ? 2 : r === 1 ? 0 : r >= 2 && r <= 4 ? 1 : 2
  return f[i].replace('{n}', String(n))
}
