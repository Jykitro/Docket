// "New task" (key C), "New project" and "Import Markdown" dialogs. Pickers are chips and segmented buttons, not native selects.
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type Project, addDays, api, errorKind, fmtDay, todayStr, useProjectsMeta } from './api'
import { useOnline } from './connection'
import { saveDraft } from './drafts'
import { PROJECT_COLORS, ProjectTile } from './icons'
import { ProjectIconPicker } from './ProjectIconPicker'
import { tr } from './i18n'
import { type ImportFile } from './launchFile'
import { MarkdownBody } from './MarkdownBody'
import { navigate } from './router'
import { Dialog, btn, btnPri, cap } from './ui'
import { useSpaces } from './Workspaces'
import { workspaceSlug } from './workspacePath'

const field = 'flex flex-col gap-1.5'
const chips = 'flex flex-wrap gap-1.5'
const choice = 'inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full border text-[12.5px] '
const choiceOn = choice + 'bg-acs border-transparent text-act font-medium'
const choiceOff = choice + 'bg-sf border-ln text-t2 hover:bg-sk'
const dateinput = 'h-7 border border-ln rounded-md bg-sf px-2 text-[12.5px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none'
const textin = 'h-9 border border-ln rounded-md bg-sf px-2.5 text-sm text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none'
const errline = 'text-rd text-[13px]'
const foot = 'flex items-center gap-2 justify-end'

export function NewTaskDialog({ projects, project, date, status, title: initial = '', description: shared = '', onClose }: {
  projects: Project[]; project?: Project; date?: string; status?: string; title?: string; description?: string; onClose: () => void
}) {
  const qc = useQueryClient()
  const [pid, setPid] = useState((project ?? projects[0])?.id)
  const [title, setTitle] = useState(initial)
  const [desc, setDesc] = useState(shared)
  const [st, setSt] = useState(status ?? '')
  const [prio, setPrio] = useState('P1')
  const [area, setArea] = useState('')
  const [day, setDay] = useState(date ?? '')
  const meta = useProjectsMeta(pid ? [pid] : [])[pid ?? 0]
  const proj = projects.find((p) => p.id === pid)
  const statuses = meta?.statuses ?? []
  const effStatus = statuses.some((s) => s.key === st) ? st : (statuses.find((s) => s.key === 'backlog') ?? statuses[0])?.key ?? ''

  const body = () => ({ project: proj!.key, title: title.trim(), description_md: desc.trim(), status: effStatus, priority: prio, area: area.trim(), planned_for: day })
  const create = useMutation({
    mutationFn: () => api.createTask(body()),
    onSuccess: () => {
      qc.invalidateQueries()
      onClose()
    },
  })
  // Without the server the task can wait on this device as a draft; the user sends it later from the notice.
  const offline = !useOnline() || (create.isError && errorKind(create.error) === 'offline')
  const [draftFailed, setDraftFailed] = useState(false)
  const submit = () => {
    if (!title.trim() || !proj || create.isPending) return
    if (!offline) return create.mutate()
    if (saveDraft(body())) onClose()
    else setDraftFailed(true)
  }
  const tomorrow = addDays(todayStr(), 1)

  return (
    <Dialog title={tr('Новая задача')} onClose={onClose}>
      <input className="border-b border-ln bg-transparent text-[17px] font-medium pt-1.5 pb-2.5 px-0 outline-none w-full focus:border-ac" autoFocus placeholder={tr('Что нужно сделать')} aria-label={tr('Название задачи')} value={title}
        onChange={(e) => setTitle(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
      {shared && ( // only for what was shared to the installed app; other tasks get a description in the task panel
        <textarea className="min-h-20 resize-y border border-ln rounded-md bg-sf px-2.5 py-2 text-[13px] text-tx focus:border-ring focus:ring-[3px] focus:ring-ring focus:outline-none" rows={3}
          aria-label={tr('Описание')} value={desc} onChange={(e) => setDesc(e.target.value)} />
      )}
      <div className={field}>
        <span className={cap}>{tr('Проект')}</span>
        <div className={chips}>
          {projects.map((p) => (
            <button key={p.id} type="button" className={p.id === pid ? choiceOn : choiceOff} onClick={() => { setPid(p.id); setSt(''); setArea('') }}>
              <ProjectTile project={p} size={16} radius={5} />{p.name}
            </button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>{tr('Статус')}</span>
        <div className={chips}>
          {statuses.map((s) => (
            <button key={s.id} type="button" className={s.key === effStatus ? choiceOn : choiceOff} onClick={() => setSt(s.key)}>{s.name}</button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>{tr('Приоритет')}</span>
        <div className={chips}>
          {['P0', 'P1', 'P2'].map((p) => (
            <button key={p} type="button" className={(p === prio ? choiceOn : choiceOff) + ' font-mono'} onClick={() => setPrio(p)}>{p}</button>
          ))}
        </div>
      </div>
      <div className={field}>
        <span className={cap}>{tr('Область')}</span>
        <div className={chips}>
          {(meta?.labels ?? []).map((l) => (
            <button key={l.id} type="button" className={l.name === area ? choiceOn : choiceOff} onClick={() => setArea(l.name === area ? '' : l.name)}>
              <span className="inline-block w-[7px] h-[7px] rounded-full flex-[0_0_7px]" style={{ background: l.color }} />{l.name}
            </button>
          ))}
          <input className={dateinput + ' w-[130px]'} placeholder={tr('Новая область')} aria-label={tr('Новая область')} value={area}
            onChange={(e) => setArea(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </div>
      </div>
      <div className={field}>
        <span className={cap}>{tr('Дата')}</span>
        <div className={chips}>
          {[todayStr(), tomorrow].map((d) => (
            <button key={d} type="button" className={day === d ? choiceOn : choiceOff} onClick={() => setDay(d)}>{fmtDay(d)}</button>
          ))}
          <button type="button" className={day === '' ? choiceOn : choiceOff} onClick={() => setDay('')}>{tr('Без даты')}</button>
          <input className={dateinput} type="date" aria-label={tr('Выбрать дату')} value={day} onChange={(e) => setDay(e.target.value)} />
        </div>
      </div>
      {offline ? <div className="text-t2 text-[13px]" role="status">{tr('Нет связи с сервером. Задачу можно сохранить черновиком на этом устройстве и отправить, когда связь вернётся.')}</div>
        : create.isError && <div className={errline} role="alert">{create.error.message}</div>}
      {draftFailed && <div className={errline} role="alert">{tr('Не удалось сохранить черновик на этом устройстве.')}</div>}
      <div className={foot}>
        <button type="button" className={btn} onClick={onClose}>{tr('Отмена')}</button>
        <button type="button" className={btnPri} disabled={!title.trim() || !proj || create.isPending} onClick={submit}>{offline ? tr('Сохранить черновик') : tr('Создать')}</button>
      </div>
    </Dialog>
  )
}

/** The "new project" fields and buttons; used by the dialog and by onboarding. */
export type ProjectDraft = { name: string; icon: string; png?: string; color: string }

export function NewProjectForm({ onCreated, onCancel, onDraft, cancelLabel = 'Отмена', submitLabel = 'Создать' }: {
  onCreated: (p: Project) => void; onCancel: () => void; onDraft?: (d: ProjectDraft) => void; cancelLabel?: string; submitLabel?: string
}) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [icon, setIcon] = useState('board')
  const [png, setPng] = useState<string>()
  const [iconBusy, setIconBusy] = useState(false)
  const [color, setColor] = useState(PROJECT_COLORS[0])
  const create = useMutation({
    mutationFn: () => api.createProject({ name: name.trim(), key: key.trim() || undefined, icon, icon_png: png, color }),
    onSuccess: (p) => {
      qc.invalidateQueries()
      onCreated(p)
    },
  })
  const submit = () => name.trim() && !iconBusy && !create.isPending && create.mutate()
  useEffect(() => onDraft?.({ name, icon, png, color }), [name, icon, png, color]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <div className={field}>
        <span className={cap}>{tr('Название')}</span>
        <input className={textin + ' w-full'} autoFocus value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} aria-label={tr('Название проекта')} />
      </div>
      <div className={field}>
        <span className={cap}>{tr('Ключ (2–5 символов, например TRK)')}</span>
        <input className={textin + ' font-mono w-[120px]'} maxLength={5} value={key} placeholder={tr('авто')}
          onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''))} onKeyDown={(e) => e.key === 'Enter' && submit()} aria-label={tr('Ключ проекта')} />
      </div>
      <div className={field}>
        <span className={cap}>{tr('Иконка')}</span>
        <ProjectIconPicker icon={icon} preview={png && 'data:image/png;base64,' + png} disabled={create.isPending} onBusy={setIconBusy}
          onSelect={(change) => { if (change.icon) setIcon(change.icon); setPng(change.icon_png) }} />
      </div>
      <div className={field}>
        <span className={cap}>{tr('Цвет')}</span>
        <div className="flex flex-wrap gap-2">
          {PROJECT_COLORS.map((c) => (
            <button key={c} type="button" className="size-7 rounded-full border-2 border-transparent p-0 aria-pressed:border-tx" style={{ background: c }} aria-label={c} aria-pressed={c === color} onClick={() => setColor(c)} />
          ))}
        </div>
      </div>
      {create.isError && <div className={errline} role="alert">{create.error.message}</div>}
      <div className={foot}>
        <button type="button" className={btn} onClick={onCancel}>{tr(cancelLabel)}</button>
        <button type="button" className={btnPri} disabled={!name.trim() || iconBusy || create.isPending} onClick={submit}>{tr(submitLabel)}</button>
      </div>
    </>
  )
}

export function NewProjectDialog({ onClose }: { onClose: () => void }) {
  return (
    <Dialog title={tr('Новый проект')} onClose={onClose}>
      <NewProjectForm onCancel={onClose} onCreated={(p) => { onClose(); navigate(`/p/${p.slug}`) }} />
    </Dialog>
  )
}

// Checklist items of a Markdown file ("- [ ] text"), the candidates for tasks.
const CHECK = /^\s*[-*+]\s+\[([ xX])\]\s+(.+?)\s*$/
const PAGE_MAX = 204800 // bytes, the server's wiki page limit
const MD_ACCEPT = '.md,.markdown,text/markdown'

/** Markdown file → a wiki page or tasks from its checklist, in this workspace. Opened from the sidebar or by a file opened with the app. */
export function ImportDialog({ projects, project, file: launched, onClose }: { projects: Project[]; project?: Project; file?: ImportFile; onClose: () => void }) {
  const qc = useQueryClient()
  const [file, setFile] = useState(launched)
  const [readError, setReadError] = useState('')
  const [pid, setPid] = useState((project ?? projects[0])?.id)
  const proj = projects.find((p) => p.id === pid)
  const space = useSpaces().data?.find((s) => s.slug === workspaceSlug)
  const text = file?.text ?? ''
  const items = text.split('\n').flatMap((l) => { const m = CHECK.exec(l); return m ? [{ title: m[2], done: m[1] !== ' ' }] : [] })
  const [mode, setMode] = useState<'page' | 'tasks'>('page')
  const [picked, setPicked] = useState<boolean[]>()
  const chosen = picked ?? items.map((i) => !i.done) // open items by default
  const [title, setTitle] = useState('')
  const pageTitle = (title || /^#\s+(.+)$/m.exec(text)?.[1] || file?.name.replace(/\.(md|markdown)$/i, '') || '').trim().slice(0, 120)
  const [batch] = useState(() => crypto.getRandomValues(new Uint32Array(2)).join('-')) // client_key prefix: a retry after a failure skips tasks already made
  const tooBig = new Blob([text]).size > PAGE_MAX

  const run = useMutation({
    mutationFn: async () => {
      if (mode === 'page') return `/p/${proj!.slug}/wiki/${(await api.createPage(proj!.id, { title: pageTitle, body_md: text })).id}`
      for (const [i, it] of items.entries()) if (chosen[i]) await api.createTask({ project: proj!.key, title: it.title, client_key: `${batch}-${i}` })
      return `/p/${proj!.slug}`
    },
    onSuccess: (to) => { void qc.invalidateQueries(); onClose(); navigate(to) },
  })
  const read = async (f?: File) => {
    if (!f) return
    setReadError('')
    try { setFile({ name: f.name, text: await f.text() }); setPicked(undefined); setTitle('') } catch { setReadError(tr('Не удалось прочитать файл')) }
  }
  const ready = !!proj && !!file && (mode === 'page' ? !!pageTitle && !tooBig : chosen.some(Boolean))

  return (
    <Dialog title={tr('Импорт Markdown')} onClose={onClose}>
      <label className="flex min-w-0 items-center gap-2 text-sm">
        <span className={btn}>{file ? tr('Другой файл') : tr('Выберите файл .md')}</span>
        {file && <span className="truncate text-t2">{file.name}</span>}
        <input type="file" className="sr-only" accept={MD_ACCEPT} onChange={(e) => void read(e.target.files?.[0])} />
      </label>
      {readError && <div className={errline} role="alert">{readError}</div>}
      {file && <>
        <div className="max-h-56 overflow-auto rounded-md border border-ln bg-sf px-3 py-2 text-[13px]"><div className="space-y-2"><MarkdownBody source={text} /></div></div>
        <div className={field}>
          <span className={cap}>{space ? tr('Проект в пространстве «{name}»', { name: space.name }) : tr('Проект')}</span>
          <div className={chips}>
            {projects.map((p) => (
              <button key={p.id} type="button" className={p.id === pid ? choiceOn : choiceOff} onClick={() => setPid(p.id)}><ProjectTile project={p} size={16} radius={5} />{p.name}</button>
            ))}
          </div>
        </div>
        <div className={field}>
          <span className={cap}>{tr('Что создать')}</span>
          <div className={chips}>
            <button type="button" className={mode === 'page' ? choiceOn : choiceOff} onClick={() => setMode('page')}>{tr('Страницу вики')}</button>
            <button type="button" className={mode === 'tasks' ? choiceOn : choiceOff} disabled={!items.length} onClick={() => setMode('tasks')}>{tr('Задачи из чеклиста · {n}', { n: items.length })}</button>
          </div>
        </div>
        {mode === 'page' ? <>
          <input className={textin} aria-label={tr('Название страницы')} maxLength={120} value={title || pageTitle} onChange={(e) => setTitle(e.target.value)} />
          {tooBig && <div className={errline} role="alert">{tr('Файл больше 200 КБ: страница вики не может быть такой большой.')}</div>}
        </> : (
          <ul className="flex max-h-48 flex-col gap-1 overflow-auto">
            {items.map((it, i) => (
              <li key={i}><label className="flex items-start gap-2 text-[13px]">
                <input type="checkbox" className="mt-0.5" checked={chosen[i]} onChange={(e) => setPicked(chosen.map((c, j) => (j === i ? e.target.checked : c)))} />
                <span className={it.done ? 'text-t3 line-through' : ''}>{it.title}</span>
              </label></li>
            ))}
          </ul>
        )}
      </>}
      {run.isError && <div className={errline} role="alert">{run.error.message}</div>}
      <div className={foot}>
        <button type="button" className={btn} onClick={onClose}>{tr('Отмена')}</button>
        <button type="button" className={btnPri} disabled={!ready || run.isPending} onClick={() => run.mutate()}>
          {mode === 'page' ? tr('Создать страницу') : tr('Создать задачи · {n}', { n: chosen.filter(Boolean).length })}
        </button>
      </div>
    </Dialog>
  )
}
