import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useEffect, useReducer, useState, useSyncExternalStore } from 'react'
import { useConnection, useUnsavedFlash } from './connection'
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

function oldestData(qc: ReturnType<typeof useQueryClient>) {
  const times = qc.getQueryCache().findAll({ type: 'active' }).filter(q => q.state.data !== undefined).map(q => q.state.dataUpdatedAt)
  if (!times.length) return ''
  const d = new Date(Math.min(...times))
  const time = d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })
  return d.toDateString() === new Date().toDateString() ? time : `${d.toLocaleDateString(locale(), { day: 'numeric', month: 'short' })} ${time}`
}

function StatusBar({ text, action }: { text: string; action?: ReactNode }) {
  return (
    <div role="status" aria-live="polite" className="sticky top-0 z-50 flex items-center justify-center gap-3 border-b border-bd bg-card px-4 py-1.5 text-[13px] text-t2">
      <span>{text}</span>{action}
    </div>
  )
}

export function ConnectionGate({ children }: { children: ReactNode }) {
  useLang()
  const qc = useQueryClient()
  const { online, network, checking, retry } = useConnection()
  const unsaved = useUnsavedFlash()
  const fetching = useIsFetching()
  const [, rerender] = useReducer((n: number) => n + 1, 0)
  useEffect(() => qc.getQueryCache().subscribe(rerender), [qc])
  // After a reconnect keep the bar for at least 1.5 s so it does not flicker, and while the refetch runs.
  const [wasOffline, setWasOffline] = useState(false)
  const [hold, setHold] = useState(false)
  useEffect(() => {
    if (!online) { setWasOffline(true); return }
    if (!wasOffline) return
    setHold(true)
    const t = window.setTimeout(() => setHold(false), 1500)
    return () => window.clearTimeout(t)
  }, [online, wasOffline])
  useEffect(() => { if (online && !hold && fetching === 0) setWasOffline(false) }, [online, hold, fetching])
  if (online) return wasOffline && (hold || fetching > 0)
    ? <><StatusBar text={tr('Связь восстановлена, обновляем данные…')} />{children}</>
    : children
  if (!qc.getQueryData(['me'])) {
    // Nothing cached to show: the full-screen page. Without a network the device is at fault; with one, the server is down.
    return (
      <ErrorPage full kind="offline"
        title={network ? tr('Сервер Plandillo не отвечает') : tr('Нет интернета')}
        hint={network
          ? tr('Интернет есть, но сервер не отвечает. Если Plandillo запущен у вас, проверьте, что процесс или контейнер работает.')
          : tr('Устройство не подключено к сети. Plandillo откроется сам, как только связь появится.')}
        actions={<button type="button" className={btnPri} disabled={checking} onClick={retry}>{checking ? tr('Проверяем…') : tr('Проверить сейчас')}</button>} />
    )
  }
  const time = oldestData(qc)
  const text = unsaved ? tr('Не сохранено: нет связи с сервером')
    : network ? tr('Нет связи с сервером · данные от {time} · изменения не сохраняются', { time })
      : tr('Нет интернета · данные от {time} · изменения не сохраняются', { time })
  return (
    <>
      <StatusBar text={text} action={<button type="button" className={btn} disabled={checking} onClick={retry}>{checking ? tr('Проверяем…') : tr('Проверить сейчас')}</button>} />
      {children}
    </>
  )
}
