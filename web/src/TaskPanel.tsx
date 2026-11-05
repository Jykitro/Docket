// Task panel: a right-hand drawer over any screen (?task=TRK-07) or a full page (/t/TRK-07).
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type Activity, type Field, type Status, type TaskDetail, addDays, api, fmtDay, fmtStamp, isoDay, statusChip, statusColor, todayStr,
  useProjects, useProjectsMeta,
} from './api'
import { Icon, ProjectTile } from './icons'
import { MD_NOTE, MD_PROSE, renderMarkdown, toggleCheckbox } from './markdown'
import { Link, hrefWith, navigate, setParam } from './router'
import { ClaudeBadge, EmptyState, ErrorState, MENUITEM, Menu, SkeletonRows, useFocusTrap } from './ui'

const PRIORITIES: Record<string, { name: string; cls: string }> = {
  P0: { name: 'Критично', cls: 'bg-rds text-rd' },
  P1: { name: 'Обычно', cls: 'bg-sk text-t2' },
  P2: { name: 'Низкий', cls: 'border border-ln text-t3' },
}
const BTN = 'inline-flex h-[34px] items-center gap-2 whitespace-nowrap rounded-lg border border-ln bg-sf px-3 text-[13.5px] font-medium text-tx no-underline hover:bg-sk disabled:cursor-default disabled:opacity-50'
const BTN_PRI = 'inline-flex h-[34px] items-center gap-2 whitespace-nowrap rounded-lg border border-ink bg-ink px-3 text-[13.5px] font-medium text-inkt no-underline hover:opacity-90 disabled:cursor-default disabled:opacity-50'
const HEAD_BTN = 'inline-flex size-8 items-center justify-center whitespace-nowrap rounded-lg border border-transparent bg-transparent p-0 text-[13.5px] font-medium text-t2 hover:bg-sk hover:text-tx disabled:cursor-default disabled:opacity-50'
const CHIP = 'inline-flex h-[26px] items-center gap-[7px] rounded-[7px] px-2.5 text-[13px] font-medium'
const PKBTN = 'inline-flex cursor-pointer rounded-[7px] bg-transparent p-0 hover:opacity-[.85]'
const DOT = 'inline-block size-2 shrink-0 rounded-full'
const MUTED = 'text-t3 text-[13.5px]'
const ERR = 'text-rd text-[13px]'
const SEC = 'mb-2.5 flex items-center gap-2.5'
const PSEC = 'border-t border-ln pt-[22px]'
const SEC_H = 'text-[15px] font-semibold'
const CHK = 'ml-auto text-act'
const MONO_T3 = 'font-mono text-[11.5px] text-t3'
const CODE = /\b([A-Z][A-Z0-9]{1,4}-\d{1,4})\b/g

function dateLabel(day: string) {
  const long = new Date(day + 'T12:00:00').toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' })
  const rel = day === todayStr() ? 'Сегодня, ' : day === addDays(todayStr(), 1) ? 'Завтра, ' : ''
  return rel + long
}

function histText(a: Activity): string {
  let d: Record<string, [unknown, unknown]> = {}
  try {
    d = JSON.parse(a.diff_json)
  } catch {
    /* an unreadable diff falls back to the action name */
  }
  const v = (x: unknown) => (x === null || x === undefined || x === '' ? '—' : String(x))
  const parts = Object.entries(d).map(([k, [from, to]]) => {
    switch (k) {
      case 'status': return `статус: ${v(from)} → ${v(to)}`
      case 'priority': return `приоритет: ${v(from)} → ${v(to)}`
      case 'area': return `область: ${v(from)} → ${v(to)}`
      case 'planned_for': return `дата: ${from ? fmtDay(String(from)) : '—'} → ${to ? fmtDay(String(to)) : '—'}`
      case 'title': return 'переименовал задачу'
      case 'description_md': return 'изменил описание'
      case 'custom': return 'изменил поле'
      case 'parent_id': return 'изменил родителя'
      case 'project_id': return 'перенёс в другой проект'
      default: return k
    }
  })
  switch (a.action) {
    case 'created': return 'создал задачу'
    case 'note_added': return 'добавил заметку'
    case 'deleted': return 'удалил в корзину'
    case 'restored': return 'восстановил из корзины'
    case 'carried_over': return `перенёс на ${fmtDay(String(d.planned_for?.[1] ?? ''))}`
    default: return parts.join('; ') || a.action
  }
}

function Prop({ icon, label, children }: { icon: string; label: string; children: ReactNode }) {
  return (
    <div className="grid min-h-[34px] grid-cols-[128px_minmax(0,1fr)] items-center gap-3">
      <span className="flex items-center gap-2 text-[13px] text-t3"><Icon name={icon} size={15} sw={1.8} />{label}</span>
      <span className="min-w-0">{children}</span>
    </div>
  )
}

export function TaskPanel({ code, mode }: { code: string; mode: 'drawer' | 'page' }) {
  const close = () => (mode === 'drawer' ? setParam('task', null) : navigate('/'))

  useEffect(() => {
    if (mode !== 'drawer') return
    const key = (e: KeyboardEvent) => {
      // Esc first closes an open menu or dialog on top of the panel.
      if (e.key === 'Escape' && !document.querySelector('[data-menu], [role="dialog"]:not([data-drawer])')) close()
    }
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  })

  const trap = useFocusTrap<HTMLElement>()
  const body = <PanelBody code={code} mode={mode} onClose={close} />
  if (mode === 'page') return <section className="flex w-full grow flex-col bg-sf" aria-label={`Задача ${code}`}>{body}</section>
  return (
    <div className="fixed inset-0 z-50 flex items-stretch justify-end overflow-y-auto bg-[rgba(15,17,22,.45)]" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <section className="flex w-full max-w-[700px] flex-col overflow-y-auto border-l border-ln bg-sf shadow-pop" role="dialog" aria-label={`Задача ${code}`} data-drawer ref={trap} tabIndex={-1}>{body}</section>
    </div>
  )
}

function PanelBody({ code, mode, onClose }: { code: string; mode: 'drawer' | 'page'; onClose: () => void }) {
  const q = useQuery({ queryKey: ['task', code], queryFn: () => api.task(code) })
  const projects = useProjects()
  const d = q.data
  const project = projects.data?.find((p) => p.id === d?.project_id)
  const meta = useProjectsMeta(d ? [d.project_id] : [])[d?.project_id ?? 0]
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const [copied, setCopied] = useState(false)

  const patch = useMutation({
    mutationFn: (b: Record<string, unknown>) => api.updateTask(code, b),
    onMutate: async (b) => {
      setErr('')
      if (typeof b.description_md !== 'string' || !d) return
      qc.setQueryData<TaskDetail>(['task', code], { ...d, description_md: b.description_md }) // checkbox ticks show at once
    },
    onError: (e: Error) => setErr(e.message),
    onSettled: () => qc.invalidateQueries(),
  })
  const del = useMutation({
    mutationFn: () => api.deleteTask(code),
    onSuccess: () => { qc.invalidateQueries(); onClose() },
    onError: (e: Error) => setErr(e.message),
  })
  const restore = useMutation({ mutationFn: () => api.restoreTask(code), onSettled: () => qc.invalidateQueries() })

  const head = (
    <div className="sticky top-0 z-[2] flex items-center gap-1.5 border-b border-ln bg-sf px-4 py-2.5">
      {project && <ProjectTile icon={project.icon} color={project.color} />}
      {project ? <Link to={`/p/${project.slug}`} onClick={() => mode === 'drawer' && setParam('task', null)} className="text-[13px] text-t2 no-underline">{project.name}</Link> : null}
      <span className="text-t3">/</span>
      <span className="grow font-mono text-[12.5px] text-t3">{d?.code ?? code}</span>
      <button type="button" className={HEAD_BTN} aria-label="Копировать ссылку" title={copied ? 'Скопировано' : 'Копировать ссылку'}
        onClick={() => navigator.clipboard?.writeText(`${location.origin}/t/${d?.code ?? code}`).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500) })}>
        <Icon name={copied ? 'check' : 'link'} size={16} sw={1.8} />
      </button>
      {mode === 'drawer' && (
        <button type="button" className={HEAD_BTN} aria-label="Открыть на всю страницу" onClick={() => navigate(`/t/${d?.code ?? code}`)}>
          <Icon name="expand" size={16} sw={1.8} />
        </button>
      )}
      {d && !d.deleted_at && (
        <button type="button" className={HEAD_BTN} aria-label="Удалить в корзину" disabled={del.isPending} onClick={() => del.mutate()}><Icon name="trash" size={16} sw={1.8} /></button>
      )}
      <button type="button" className={HEAD_BTN} aria-label="Закрыть" onClick={onClose}><Icon name="x" size={16} sw={2} /></button>
    </div>
  )

  if (q.isPending) return <>{head}<SkeletonRows n={6} /></>
  if (q.isError) {
    return <>{head}{q.error.message.includes('not found') || q.error.message.includes('не найд')
      ? <EmptyState icon="doc" title="Задача не найдена" hint={`Нет задачи ${code}. Её могли очистить из корзины.`} />
      : <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}</>
  }
  const readOnly = !!d!.deleted_at
  return (
    <>
      {head}
      {readOnly && (
        <div className="flex items-center gap-2 bg-rds px-4 py-2.5 text-[13px] font-medium text-rd" role="status">
          <Icon name="trash" size={15} />В корзине — редактирование отключено
          <span className="grow" />
          <button type="button" className={BTN} disabled={restore.isPending} onClick={() => restore.mutate()}>Восстановить</button>
        </div>
      )}
      <div className={'flex flex-col gap-[26px] px-9 pt-[26px] pb-10 max-[640px]:px-4 max-[640px]:pt-5 max-[640px]:pb-8' + (mode === 'page' ? ' mx-auto w-full max-w-[860px]' : '')}>
        <Title d={d!} readOnly={readOnly} onSave={(title) => patch.mutate({ title })} />
        {err && <div className={ERR} role="alert">{err}</div>}
        <Properties d={d!} statuses={meta?.statuses ?? []} labels={meta?.labels ?? []} fields={meta?.fields ?? []} readOnly={readOnly} onPatch={(b) => patch.mutate(b)} />
        <Description d={d!} readOnly={readOnly} onSave={(description_md) => patch.mutate({ description_md })} />
        {!d!.parent_id && <Subtasks d={d!} statuses={meta?.statuses ?? []} projectKey={project?.key} readOnly={readOnly} />}
        <Notes d={d!} readOnly={readOnly} />
        <History acts={d!.activity} />
      </div>
    </>
  )
}

const H1 = 'text-[27px] leading-[1.2] font-bold tracking-[-.02em] text-balance'

function Title({ d, readOnly, onSave }: { d: TaskDetail; readOnly: boolean; onSave: (t: string) => void }) {
  const [editing, setEditing] = useState(false)
  const [v, setV] = useState(d.title)
  if (editing) {
    const done = () => { setEditing(false); if (v.trim() && v.trim() !== d.title) onSave(v.trim()) }
    return (
      <h1 className={H1}><input className="w-full border-0 border-b-2 border-ac bg-transparent px-0 pt-0 pb-1 outline-0" autoFocus aria-label="Название задачи" value={v} onChange={(e) => setV(e.target.value)} onBlur={done}
        onKeyDown={(e) => {
          if (e.key === 'Enter') done()
          if (e.key === 'Escape') { e.stopPropagation(); setV(d.title); setEditing(false) }
        }} /></h1>
    )
  }
  return <h1 className={H1}><button type="button" className="cursor-text rounded-md p-0 text-left disabled:cursor-default" disabled={readOnly} title="Нажмите, чтобы изменить" onClick={() => { setV(d.title); setEditing(true) }}>{d.title}</button></h1>
}

const DATEIN = 'h-7 rounded-lg border border-ln bg-sf px-2 text-[12.5px] text-tx focus:border-ac focus:outline-0'

function Properties({ d, statuses, labels, fields, readOnly, onPatch }: {
  d: TaskDetail; statuses: Status[]; labels: { id: number; name: string; color: string }[]; fields: Field[]; readOnly: boolean
  onPatch: (b: Record<string, unknown>) => void
}) {
  const st = statuses.find((s) => s.key === d.status)
  const pr = PRIORITIES[d.priority]
  const area = labels.find((l) => l.name === d.area)
  const [newArea, setNewArea] = useState('')
  const menu = (label: ReactNode, items: (close: () => void) => ReactNode) =>
    readOnly ? <>{label}</> : <Menu className={PKBTN} label={label}>{items}</Menu>

  // Linked tasks: codes mentioned in the description, plus the parent.
  const mentioned = [...new Set([...(d.description_md.match(CODE) ?? []), ...(d.parent_code ? [d.parent_code] : [])])].filter((c) => c !== d.code).slice(0, 8)
  const linked = useQueries({ queries: mentioned.map((c) => ({ queryKey: ['task-mini', c], queryFn: () => api.task(c), retry: false })) })
  const links = linked.flatMap((r) => (r.data ? [r.data] : []))

  return (
    <div className="flex flex-col">
      <Prop icon="clock" label="Статус">
        {menu(<span className={CHIP} style={st ? statusChip(st.color) : undefined}><span className={DOT} style={{ background: st ? statusColor(st.color) : 'var(--bk)' }} />{d.status_name}</span>,
          (close) => statuses.map((s) => (
            <button key={s.id} type="button" className={MENUITEM} onClick={() => { onPatch({ status: s.key }); close() }}>
              <span className={DOT} style={{ background: statusColor(s.color) }} />{s.name}{s.key === d.status && <span className={CHK}><Icon name="check" size={14} sw={2.2} /></span>}
            </button>
          )))}
      </Prop>
      <Prop icon="flag" label="Приоритет">
        {menu(<span className={CHIP + ' ' + pr.cls}><span className="font-mono text-[12px] font-semibold">{d.priority}</span>{pr.name}</span>,
          (close) => Object.entries(PRIORITIES).map(([k, p]) => (
            <button key={k} type="button" className={MENUITEM} onClick={() => { onPatch({ priority: k }); close() }}><span className="font-mono">{k}</span>{p.name}{k === d.priority && <span className={CHK}><Icon name="check" size={14} sw={2.2} /></span>}</button>
          )))}
      </Prop>
      <Prop icon="tag" label="Область">
        {menu(d.area
          ? <span className={CHIP + ' bg-sk text-t2'}><span className={DOT} style={{ background: area?.color ?? 'var(--bk)' }} />{d.area}</span>
          : <span className={MUTED}>не задана</span>,
        (close) => (
          <>
            {labels.map((l) => (
              <button key={l.id} type="button" className={MENUITEM} onClick={() => { onPatch({ area: l.name }); close() }}><span className={DOT} style={{ background: l.color }} />{l.name}{l.name === d.area && <span className={CHK}><Icon name="check" size={14} sw={2.2} /></span>}</button>
            ))}
            {d.area && <button type="button" className={MENUITEM} onClick={() => { onPatch({ area: '' }); close() }}>Убрать область</button>}
            <form className="p-1" onSubmit={(e) => { e.preventDefault(); if (newArea.trim()) { onPatch({ area: newArea.trim() }); setNewArea(''); close() } }}>
              <input className="h-[30px] w-full rounded-lg border border-ln bg-sf px-2.5 text-[13.5px] text-tx focus:border-ac focus:outline-0" placeholder="Новая область" aria-label="Новая область" value={newArea} onChange={(e) => setNewArea(e.target.value)} />
            </form>
          </>
        ))}
      </Prop>
      <Prop icon="cal" label="Дата">
        {menu(d.planned_for ? <span className="text-[13.5px]">{dateLabel(d.planned_for)}</span> : <span className={MUTED}>не задана</span>,
          (close) => (
            <>
              {[todayStr(), addDays(todayStr(), 1)].map((day) => <button key={day} type="button" className={MENUITEM} onClick={() => { onPatch({ planned_for: day }); close() }}>{fmtDay(day)}</button>)}
              <button type="button" className={MENUITEM} onClick={() => { onPatch({ planned_for: '' }); close() }}>Без даты</button>
              <div className="p-1">
                <input className={DATEIN} type="date" aria-label="Выбрать дату" defaultValue={d.planned_for ?? ''}
                  onChange={(e) => { if (e.target.value) { onPatch({ planned_for: isoDay(new Date(e.target.value + 'T12:00:00')) }); close() } }} />
              </div>
            </>
          ))}
      </Prop>
      {links.length > 0 && (
        <Prop icon="link" label="Связано">
          <span className="flex flex-wrap gap-1.5">
            {links.map((t) => <Link key={t.id} className="inline-flex min-h-[26px] items-center gap-1.5 rounded-[7px] border border-ln bg-sf2 px-[9px] text-[12.5px] text-act no-underline hover:bg-sk" to={hrefWith('task', t.code)}><span className="font-mono text-[11.5px]">{t.code}</span>{t.title}</Link>)}
          </span>
        </Prop>
      )}
      <Prop icon="user" label="Создал">
        <span className="inline-flex items-center gap-2 text-[13.5px]">
          {d.created_by === 'claude' ? <ClaudeBadge /> : <b className="font-semibold">Denis</b>}
          <span className="text-t3">{d.created_by === 'claude' ? 'через MCP · ' : ''}{fmtStamp(d.created_at)}</span>
        </span>
      </Prop>
      {fields.map((f) => <FieldProp key={f.id} f={f} d={d} readOnly={readOnly} onPatch={onPatch} />)}
    </div>
  )
}

function FieldProp({ f, d, readOnly, onPatch }: { f: Field; d: TaskDetail; readOnly: boolean; onPatch: (b: Record<string, unknown>) => void }) {
  const cur = d.custom[String(f.id)]
  const set = (v: string | number | null) => onPatch({ custom: { [f.name]: v } })
  let editor: ReactNode
  if (f.type === 'select') {
    editor = readOnly ? <>{cur ?? <span className={MUTED}>—</span>}</> : (
      <Menu className={PKBTN} label={cur ? <span className={CHIP + ' bg-sk text-t2'}>{String(cur)}</span> : <span className={MUTED}>не задано</span>}>
        {(close) => (
          <>
            {f.options.map((o) => <button key={o} type="button" className={MENUITEM} onClick={() => { set(o); close() }}>{o}{o === cur && <span className={CHK}><Icon name="check" size={14} sw={2.2} /></span>}</button>)}
            {cur !== undefined && <button type="button" className={MENUITEM} onClick={() => { set(null); close() }}>Очистить</button>}
          </>
        )}
      </Menu>
    )
  } else {
    editor = (
      <input className={DATEIN + ' w-[220px]'} type={f.type === 'number' ? 'number' : 'url'} aria-label={f.name} disabled={readOnly}
        placeholder={f.type === 'url' ? 'https://…' : '—'} defaultValue={cur === undefined ? '' : String(cur)} key={String(cur)}
        onBlur={(e) => { if (e.target.value !== (cur === undefined ? '' : String(cur))) set(e.target.value === '' ? null : e.target.value) }}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
    )
  }
  return <Prop icon={f.type === 'url' ? 'link' : f.type === 'number' ? 'bars' : 'tag'} label={f.name}>{editor}</Prop>
}

const SEG = 'inline-flex gap-0.5 rounded-lg border border-ln p-0.5 text-[12.5px]'
const SEGBTN = 'rounded-md px-2.5 py-[3px] text-t3 aria-pressed:bg-sk aria-pressed:font-semibold aria-pressed:text-tx disabled:cursor-default disabled:opacity-50'

function Description({ d, readOnly, onSave }: { d: TaskDetail; readOnly: boolean; onSave: (md: string) => void }) {
  const [edit, setEdit] = useState(false)
  const [draft, setDraft] = useState(d.description_md)
  const view = (
    <div className={MD_PROSE}
      onClick={(e) => {
        const el = e.target as HTMLElement
        if (el instanceof HTMLInputElement && el.type === 'checkbox' && el.dataset.idx !== undefined) {
          if (readOnly) return e.preventDefault()
          onSave(toggleCheckbox(d.description_md, Number(el.dataset.idx)))
        }
      }}
      dangerouslySetInnerHTML={{ __html: renderMarkdown(d.description_md) }} />
  )
  return (
    <section className={PSEC}>
      <div className={SEC}>
        <h2 className={'grow ' + SEC_H}>Описание</h2>
        <span className={SEG} role="group" aria-label="Режим описания">
          <button type="button" className={SEGBTN} aria-pressed={!edit} onClick={() => { if (edit && draft !== d.description_md) onSave(draft); setEdit(false) }}>Просмотр</button>
          <button type="button" className={SEGBTN} aria-pressed={edit} disabled={readOnly} onClick={() => { setDraft(d.description_md); setEdit(true) }}>Markdown</button>
        </span>
      </div>
      {edit ? (
        <textarea className="w-full resize-y rounded-[10px] border border-ln bg-sf2 px-3.5 py-3 font-mono text-[13px] leading-[1.6] text-tx focus:border-ac focus:outline-0" autoFocus aria-label="Описание в markdown" value={draft} rows={Math.max(8, draft.split('\n').length + 1)}
          onChange={(e) => setDraft(e.target.value)} onBlur={() => draft !== d.description_md && onSave(draft)} placeholder="Markdown: **жирный**, - [ ] чекбокс, [ссылка](https://…)" />
      ) : d.description_md.trim() ? view : (
        <p className={MUTED}>Описания нет. Нажмите «Markdown», чтобы добавить.</p>
      )}
    </section>
  )
}

function Subtasks({ d, statuses, projectKey, readOnly }: { d: TaskDetail; statuses: Status[]; projectKey?: string; readOnly: boolean }) {
  const qc = useQueryClient()
  const [title, setTitle] = useState('')
  const done = d.subtasks.filter((s) => s.status_category === 'done').length
  const toggle = useMutation({
    mutationFn: (s: TaskDetail['subtasks'][number]) => {
      const target = s.status_category === 'done'
        ? statuses.find((x) => x.key === 'todo') ?? statuses.find((x) => x.category === 'todo')
        : statuses.find((x) => x.category === 'done')
      return api.updateTask(s.code, { status: target?.key })
    },
    onSettled: () => qc.invalidateQueries(),
  })
  const add = useMutation({
    mutationFn: (t: string) => api.createTask({ project: projectKey!, title: t, parent: d.code }),
    onSuccess: () => { setTitle(''); qc.invalidateQueries() },
  })
  return (
    <section className={PSEC}>
      <div className={SEC}>
        <h2 className={SEC_H}>Подзадачи</h2>
        <span className="grow font-mono text-[12px] tabular-nums text-t3">{done} / {d.subtasks.length}</span>
        {d.subtasks.length > 0 && <span className="block h-1.5 w-[110px] overflow-hidden rounded-[3px] bg-sk" role="img" aria-label={`Готово ${done} из ${d.subtasks.length}`}><span className="block h-full bg-dn" style={{ width: `${(done / d.subtasks.length) * 100}%` }} /></span>}
      </div>
      <div>
        {d.subtasks.map((s) => {
          const on = s.status_category === 'done'
          return (
            <div className="flex min-h-9 items-center gap-2.5 border-b border-ln text-[14px]" key={s.id}>
              <button type="button" className="grid size-[17px] shrink-0 place-items-center rounded-[5px] border-[1.5px] border-ln2 bg-transparent p-0 text-white aria-checked:border-dn aria-checked:bg-dn" role="checkbox" aria-checked={on} aria-label={s.title} disabled={readOnly} onClick={() => toggle.mutate(s)}>
                {on && <Icon name="check" size={11} sw={3.4} />}
              </button>
              <Link to={hrefWith('task', s.code)} className={'grow ' + (on ? 'text-t3 line-through' : 'no-underline')}>{s.title}</Link>
              <span className={MONO_T3}>{s.code}</span>
            </div>
          )
        })}
        {!readOnly && (
          <label className="flex h-9 items-center gap-2 text-t3 focus-within:rounded-md focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-ac">
            <Icon name="plus" size={14} sw={2} />
            <input className="flex-1 border-0 bg-transparent text-[13.5px] text-tx outline-0" placeholder="Добавить подзадачу" aria-label="Добавить подзадачу" value={title} onChange={(e) => setTitle(e.target.value)} disabled={!projectKey}
              onKeyDown={(e) => e.key === 'Enter' && title.trim() && !add.isPending && add.mutate(title.trim())} />
          </label>
        )}
      </div>
      {add.isError && <div className={ERR} role="alert">{add.error.message}</div>}
    </section>
  )
}

function Notes({ d, readOnly }: { d: TaskDetail; readOnly: boolean }) {
  const qc = useQueryClient()
  const [text, setText] = useState('')
  const add = useMutation({
    mutationFn: (b: string) => api.addNote(d.code, b),
    onSuccess: () => { setText(''); qc.invalidateQueries() },
  })
  const send = () => text.trim() && !add.isPending && add.mutate(text.trim())
  const end = useRef<HTMLDivElement>(null)
  const count = useRef(d.notes.length)
  useEffect(() => {
    if (d.notes.length > count.current) end.current?.scrollIntoView({ block: 'nearest' })
    count.current = d.notes.length
  }, [d.notes.length])
  return (
    <section className={PSEC}>
      <div className={SEC}><h2 className={'grow ' + SEC_H}>Заметки</h2><span className="font-mono text-[12px] text-t3">{d.notes.length}</span></div>
      <div className="flex flex-col gap-3.5">
        {d.notes.map((n) => (
          <div key={n.id} className="flex gap-3">
            <span className={'grid size-7 shrink-0 place-items-center rounded-full ' + (n.author === 'claude' ? 'bg-acs text-act' : 'bg-sk text-t2')}><Icon name={n.author === 'claude' ? 'spark' : 'user'} size={14} sw={2} /></span>
            <div className="flex min-w-0 flex-[1_1_0] flex-col gap-[3px]">
              <div className="flex items-baseline gap-2">
                <b className="text-[13.5px] font-semibold">{n.author === 'claude' ? 'claude' : 'Denis'}</b>
                <span className={MONO_T3}>{fmtStamp(n.created_at)}</span>
              </div>
              <div className={MD_NOTE} dangerouslySetInnerHTML={{ __html: renderMarkdown(n.body_md) }} />
            </div>
          </div>
        ))}
        <div ref={end} />
        {!readOnly && (
          <label className="flex items-end gap-2.5 rounded-[10px] border border-ln bg-sf2 py-2 pr-2 pl-3.5 focus-within:border-ac">
            <textarea className="flex-1 resize-none border-0 bg-transparent text-[14px] leading-[1.5] text-tx outline-0" rows={2} placeholder="Добавить заметку" aria-label="Добавить заметку" value={text} onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => (e.metaKey || e.ctrlKey) && e.key === 'Enter' && send()} />
            <button type="button" className={BTN_PRI} disabled={!text.trim() || add.isPending} onClick={send}>Отправить<span className="rounded-[5px] border border-current bg-transparent px-[5px] py-px font-mono text-[11px] leading-[normal] font-medium text-inkt opacity-70">⌘↵</span></button>
          </label>
        )}
        {add.isError && <div className={ERR} role="alert">{add.error.message}</div>}
      </div>
    </section>
  )
}

function History({ acts }: { acts: Activity[] }) {
  const [all, setAll] = useState(false)
  const shown = all ? acts : acts.slice(0, 5)
  return (
    <section className={PSEC}>
      <div className={SEC}>
        <h2 className={'grow ' + SEC_H}>История</h2>
        {acts.length > 5 && <button type="button" className="p-0 text-[12.5px] text-t3 hover:text-tx" onClick={() => setAll(!all)}>{all ? 'свернуть' : 'показать всё'}</button>}
      </div>
      {shown.length === 0 && <p className={MUTED}>Изменений пока нет.</p>}
      {shown.map((a) => (
        <div className="flex items-baseline gap-2.5 py-[5px] text-[12.5px] text-t2" key={a.id}>
          <span className={MONO_T3 + ' w-[92px] shrink-0'}>{fmtStamp(a.at)}</span>
          <span><b className="font-semibold text-tx">{a.actor === 'claude' ? 'claude' : 'Denis'}</b> {histText(a)}</span>
        </div>
      ))}
    </section>
  )
}
