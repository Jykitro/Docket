import { type ReactNode, useEffect, useState, useSyncExternalStore } from 'react'
import { locale, tr, useLang } from './i18n'
import { ErrorPage, btn, btnPri } from './ui'

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
  const [network, setNetwork] = useState(navigator.onLine)
  const [checking, setChecking] = useState(false)
  const [lastTry, setLastTry] = useState<Date>()
  const retry = async () => {
    setChecking(true)
    try {
      if ((await fetch('/healthz', { cache: 'no-store' })).ok) return location.reload()
    } catch { /* keep the retry button available */ }
    setChecking(false)
    setLastTry(new Date())
    setNetwork(navigator.onLine)
  }
  useEffect(() => {
    const lost = () => { setOffline(true); setNetwork(navigator.onLine) }
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
  // Without a network the device is at fault; with one, the Plandillo server is down or unreachable.
  return (
    <ErrorPage full kind="offline"
      title={network ? tr('Сервер Plandillo не отвечает') : tr('Нет интернета')}
      hint={network
        ? tr('Интернет есть, но сервер не отвечает. Если Plandillo запущен у вас, проверьте, что процесс или контейнер работает.')
        : tr('Устройство не подключено к сети. Plandillo откроется сам, как только связь появится.')}
      actions={<button type="button" className={btnPri} disabled={checking} onClick={retry}>{checking ? tr('Проверяем…') : tr('Проверить сейчас')}</button>}>
      <p className="mt-6 flex items-center gap-2 text-[13px] text-t3" role="status">
        <span className="relative flex size-2"><span className="absolute inset-0 rounded-full bg-dg opacity-60 motion-safe:animate-ping" /><span className="relative size-2 rounded-full bg-dg" /></span>
        {lastTry ? tr('Проверяем каждые 5 секунд. Последняя попытка: {time}', { time: lastTry.toLocaleTimeString(locale()) }) : tr('Проверяем соединение каждые 5 секунд')}
      </p>
    </ErrorPage>
  )
}
