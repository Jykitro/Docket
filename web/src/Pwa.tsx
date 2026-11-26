import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useEffect, useReducer, useRef, useState, useSyncExternalStore } from 'react'
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
// The build this page runs (index.html, set by vite.config.ts; absent from the dev server). sw.js carries the build the
// server has now and is never answered from the worker's cache, so reading it tells an open tab or installed app
// that a deploy happened: the worker is updated in the background and the status bar offers a reload.
const pageBuild = document.querySelector<HTMLMetaElement>('meta[name="plandillo-build"]')?.content
let updateReady = false
if (pageBuild && window.isSecureContext && 'serviceWorker' in navigator) {
  let registration: ServiceWorkerRegistration | undefined
  const check = async () => {
    if (updateReady || document.hidden) return
    try {
      const build = /const BUILD = '([0-9a-f]+)'/.exec(await (await fetch('/sw.js', { cache: 'no-store' })).text())?.[1]
      if (!build || build === pageBuild) return
      await registration?.update().catch(() => {})
      updateReady = true
      changed.dispatchEvent(new Event('change'))
    } catch { /* offline: the next check retries */ }
  }
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { updateViaCache: 'none' }).then(r => { registration = r }, err => console.warn('Plandillo service worker', err))
    document.addEventListener('visibilitychange', () => void check())
    window.addEventListener('online', () => void check())
    window.setInterval(() => void check(), 30 * 60_000)
  })
}

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

// One live region that stays mounted, so a screen reader announces the first notice too; hidden while empty.
function StatusBar({ text, action }: { text: string; action?: ReactNode }) {
  return (
    <div className={text ? 'fixed bottom-[max(1rem,env(safe-area-inset-bottom))] left-4 z-[90] flex max-w-[calc(100vw-2rem)] flex-wrap items-center gap-3 rounded-lg border border-ln bg-sf px-4 py-2 text-sm text-tx shadow-lg' : 'sr-only'}>
      <span role="status">{text}</span>{text && action}
    </div>
  )
}

export function ConnectionGate({ children }: { children: ReactNode }) {
  useLang()
  const qc = useQueryClient()
  const { online, network, checking, retry } = useConnection()
  const unsaved = useUnsavedFlash()
  const fetching = useIsFetching()
  const update = useSyncExternalStore(subscribe, () => updateReady)
  const [, rerender] = useReducer((n: number) => n + 1, 0)
  useEffect(() => qc.getQueryCache().subscribe(rerender), [qc])
  // After a reconnect the notice stays at least 1.5 s, so it does not flicker, and while the refetch runs.
  const [refreshing, setRefreshing] = useState(false)
  const [minShown, setMinShown] = useState(false)
  const prev = useRef(online)
  useEffect(() => {
    const reconnected = online && !prev.current
    prev.current = online
    if (!online) { setRefreshing(false); return }
    if (!reconnected) return
    setRefreshing(true)
    setMinShown(true)
    const t = window.setTimeout(() => setMinShown(false), 1500)
    return () => window.clearTimeout(t)
  }, [online])
  useEffect(() => { if (refreshing && !minShown && fetching === 0) setRefreshing(false) }, [refreshing, minShown, fetching])
  if (!online && !qc.getQueryData(['me'])) {
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
  let text = ''
  let offerUpdate = false
  if (!online) {
    const time = oldestData(qc)
    text = unsaved ? tr('Не сохранено: нет связи с сервером')
      : network ? tr('Нет связи с сервером · данные от {time} · изменения не сохраняются', { time })
        : tr('Нет интернета · данные от {time} · изменения не сохраняются', { time })
  } else if (refreshing && (minShown || fetching > 0)) text = tr('Связь восстановлена, обновляем данные…')
  else if (update) { text = tr('Доступна новая версия Plandillo'); offerUpdate = true }
  // Always the same element tree around children: a changing shape would remount the page and lose unsaved drafts.
  return (
    <>
      <StatusBar text={text} action={!online ? <button type="button" className={btn} disabled={checking} onClick={retry}>{checking ? tr('Проверяем…') : tr('Проверить сейчас')}</button>
        : offerUpdate && <button type="button" className={btnPri} onClick={() => location.reload()}>{tr('Обновить')}</button>} />
      {children}
    </>
  )
}
