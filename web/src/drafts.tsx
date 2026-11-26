// New tasks saved on this device while the server was out of reach. Only the user sends them (DraftsDialog);
// every other change still fails offline. Scoped like the offline cache: sign-in stamp + workspace, wiped with it.
import { useEffect, useState, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type NewTask, api, errText, fmtDay, offlineStamp } from './api'
import { useOnline } from './connection'
import { tr } from './i18n'
import { DRAFTS } from './offlineCache'
import { Dialog, btn, btnPri } from './ui'
import { workspaceSlug } from './workspacePath'

// `id` goes to the server as client_key: sending the same draft twice (double click, lost answer, two tabs) creates one task.
type Draft = { id: string; task: NewTask; saved: number }

const key = () => `${DRAFTS}${offlineStamp()}:${workspaceSlug}`
const listeners = new Set<() => void>()
let raw: string | null = null
let list: Draft[] = []

// The same array while the stored text is unchanged, as useSyncExternalStore needs.
function read(): Draft[] {
  let r: string | null = null
  try { r = localStorage.getItem(key()) } catch { /* blocked storage: no drafts */ }
  if (r !== raw) {
    raw = r
    try { list = r ? JSON.parse(r) as Draft[] : [] } catch { list = [] }
  }
  return list
}

function write(next: Draft[]): boolean {
  try {
    if (next.length) localStorage.setItem(key(), JSON.stringify(next))
    else localStorage.removeItem(key())
  } catch { return false }
  listeners.forEach(l => l())
  return true
}

window.addEventListener('storage', e => { if (e.key === key() || e.key === null) listeners.forEach(l => l()) })

/** False when the device refused to store it (private mode, quota). */
export function saveDraft(task: NewTask): boolean {
  return write([...read(), { id: crypto.getRandomValues(new Uint32Array(4)).join('-'), task, saved: Date.now() }])
}

const dropDraft = (id: string) => write(read().filter(d => d.id !== id))

export const useDrafts = () => useSyncExternalStore(cb => { listeners.add(cb); return () => listeners.delete(cb) }, read)

/** The drafts with Send / Discard each; nothing is sent until the user presses Send. */
export function DraftsDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const drafts = useDrafts()
  const online = useOnline()
  const [busy, setBusy] = useState<string>()
  const [errors, setErrors] = useState<Record<string, string>>({})
  useEffect(() => { if (!drafts.length) onClose() }, [drafts.length]) // eslint-disable-line react-hooks/exhaustive-deps
  const send = async (d: Draft) => {
    setBusy(d.id)
    try {
      await api.createTask({ ...d.task, client_key: d.id })
      dropDraft(d.id)
      void qc.invalidateQueries()
    } catch (e) {
      setErrors(x => ({ ...x, [d.id]: errText(e) }))
    } finally { setBusy(undefined) }
  }
  return (
    <Dialog title={tr('Черновики задач')} onClose={onClose}>
      <p className="text-t2 text-[13px]">{online ? tr('Сохранены на этом устройстве, пока не было связи. Отправьте нужные, остальные удалите.')
        : tr('Нет связи с сервером. Черновики можно отправить, когда связь вернётся.')}</p>
      <ul className="flex flex-col gap-2">
        {drafts.map(d => (
          <li key={d.id} className="flex flex-wrap items-center gap-2 rounded-md border border-ln px-3 py-2">
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{d.task.title}</div>
              <div className="font-mono text-[12px] text-t3">{[d.task.project, d.task.planned_for && fmtDay(d.task.planned_for)].filter(Boolean).join(' · ')}</div>
              {errors[d.id] && <div className="text-[13px] text-rd" role="alert">{errors[d.id]}</div>}
            </div>
            <button type="button" className={btn} disabled={busy === d.id} onClick={() => dropDraft(d.id)}>{tr('Удалить')}</button>
            <button type="button" className={btnPri} disabled={!online || !!busy} onClick={() => void send(d)}>{tr('Отправить')}</button>
          </li>
        ))}
      </ul>
      <div className="flex justify-end"><button type="button" className={btn} onClick={onClose}>{tr('Закрыть')}</button></div>
    </Dialog>
  )
}
