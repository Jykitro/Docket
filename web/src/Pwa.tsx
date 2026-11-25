import { type ReactNode, useEffect, useState, useSyncExternalStore } from 'react'
import { tr, useLang } from './i18n'
import { btn, btnPri } from './ui'

type InstallEvent = Event & { prompt(): Promise<unknown> }
let installEvent: InstallEvent | null = null
const changed = new EventTarget()
function setPrompt(value: InstallEvent | null) {
  installEvent = value
  changed.dispatchEvent(new Event('change'))
}
window.addEventListener('beforeinstallprompt', e => {
  e.preventDefault()
  setPrompt(e as InstallEvent)
})
window.addEventListener('appinstalled', () => setPrompt(null))
function subscribe(listener: () => void) {
  changed.addEventListener('change', listener)
  return () => changed.removeEventListener('change', listener)
}

export function InstallPlandillo({ fallback = null }: { fallback?: ReactNode }) {
  const prompt = useSyncExternalStore(subscribe, () => installEvent)
  if (matchMedia('(display-mode: standalone)').matches || !window.isSecureContext) return fallback
  if (prompt) return <button type="button" className={btn} onClick={() => {
    setPrompt(null)
    void prompt.prompt().catch(() => {})
  }}>{tr('Установить Plandillo')}</button>
  if (/Safari\//.test(navigator.userAgent) && !/Chrome\/|Chromium\/|Android/.test(navigator.userAgent)) {
    const mobile = /iPad|iPhone|iPod/.test(navigator.userAgent) || navigator.maxTouchPoints > 1
    return <p className="text-sm text-t2">{mobile
      ? tr('Установка: «Поделиться» → «На экран Домой».')
      : tr('Установка: в Safari выберите «Файл» → «Добавить в Dock».')}</p>
  }
  return fallback
}

export function ConnectionGate({ children }: { children: ReactNode }) {
  useLang()
  const [offline, setOffline] = useState(!navigator.onLine)
  const retry = async () => {
    try {
      if ((await fetch('/healthz', { cache: 'no-store' })).ok) location.reload()
    } catch { /* keep the retry button available */ }
  }
  useEffect(() => {
    const lost = () => setOffline(true)
    window.addEventListener('offline', lost)
    window.addEventListener('docket:disconnected', lost)
    return () => {
      window.removeEventListener('offline', lost)
      window.removeEventListener('docket:disconnected', lost)
    }
  }, [])
  useEffect(() => {
    if (!offline) return
    window.addEventListener('online', retry)
    const timer = window.setInterval(retry, 5000)
    return () => { window.removeEventListener('online', retry); window.clearInterval(timer) }
  }, [offline])
  if (!offline) return children
  return <main className="grid min-h-dvh place-content-center gap-4 text-center">
    <h1 className="text-2xl font-semibold">{tr('Нет связи')}</h1>
    <button className={btnPri} type="button" onClick={retry}>{tr('Повторить')}</button>
  </main>
}
