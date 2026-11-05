// Task panel: a right-hand drawer over any screen (?task=TRK-07) or a full page (/t/TRK-07).
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  type Activity, type Field, type Status, type TaskDetail, addDays, api, fmtDay, fmtStamp, isoDay, statusChip, statusColor, todayStr,
  useProjects, useProjectsMeta,
} from './api'
import { Icon, ProjectTile } from './icons'
import { renderMarkdown, toggleCheckbox } from './markdown'
import { Link, hrefWith, navigate, setParam } from './router'
import { ClaudeBadge, EmptyState, ErrorState, Menu, SkeletonRows, useFocusTrap } from './ui'

const PRIORITIES: Record<string, { name: string; style: React.CSSProperties }> = {
  P0: { name: 'Критично', style: { background: 'var(--rds)', color: 'var(--rd)' } },
  P1: { name: 'Обычно', style: { background: 'var(--sk)', color: 'var(--t2)' } },
  P2: { name: 'Низкий', style: { border: '1px solid var(--ln)', color: 'var(--t3)' } },
}
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
    <div className="pk">
      <span><Icon name={icon} size={15} sw={1.8} />{label}</span>
      <span style={{ minWidth: 0 }}>{children}</span>
    </div>
  )
}

export function TaskPanel({ code, mode }: { code: string; mode: 'drawer' | 'page' }) {
  const close = () => (mode === 'drawer' ? setParam('task', null) : navigate('/'))

  useEffect(() => {
    if (mode !== 'drawer') return
    const key = (e: KeyboardEvent) => {
      // Esc first closes an open menu or dialog on top of the panel.
      if (e.key === 'Escape' && !document.querySelector('.pop, [role="dialog"]:not([data-drawer])')) close()
    }
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  })

  const trap = useFocusTrap<HTMLElement>()
  const body = <PanelBody code={code} mode={mode} onClose={close} />
  if (mode === 'page') return <section className="taskpanel page" aria-label={`Задача ${code}`}>{body}</section>
  return (
    <div className="overlay drawer" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <section className="taskpanel" role="dialog" aria-label={`Задача ${code}`} data-drawer ref={trap} tabIndex={-1}>{body}</section>
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
    <div className="phead">
      {project && <ProjectTile icon={project.icon} color={project.color} />}
      {project ? <Link to={`/p/${project.slug}`} onClick={() => mode === 'drawer' && setParam('task', null)} style={{ fontSize: 13, color: 'var(--t2)', textDecoration: 'none' }}>{project.name}</Link> : null}
      <span style={{ color: 'var(--t3)' }}>/</span>
      <span className="mono grow" style={{ fontSize: 12.5, color: 'var(--t3)' }}>{d?.code ?? code}</span>
      <button type="button" className="btn sq" aria-label="Копировать ссылку" title={copied ? 'Скопировано' : 'Копировать ссылку'}
        onClick={() => navigator.clipboard?.writeText(`${location.origin}/t/${d?.code ?? code}`).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500) })}>
        <Icon name={copied ? 'check' : 'link'} size={16} sw={1.8} />
      </button>
      {mode === 'drawer' && (
        <button type="button" className="btn sq" aria-label="Открыть на всю страницу" onClick={() => navigate(`/t/${d?.code ?? code}`)}>
          <Icon name="expand" size={16} sw={1.8} />
        </button>
      )}
      {d && !d.deleted_at && (
        <button type="button" className="btn sq" aria-label="Удалить в корзину" disabled={del.isPending} onClick={() => del.mutate()}><Icon name="trash" size={16} sw={1.8} /></button>
      )}
      <button type="button" className="btn sq" aria-label="Закрыть" onClick={onClose}><Icon name="x" size={16} sw={2} /></button>
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
        <div className="trashbar" role="status">
          <Icon name="trash" size={15} />В корзине — редактирование отключено
          <span className="grow" />
          <button type="button" className="btn" disabled={restore.isPending} onClick={() => restore.mutate()}>Восстановить</button>
        </div>
      )}
      <div className="pbody">
        <Title d={d!} readOnly={readOnly} onSave={(title) => patch.mutate({ title })} />
        {err && <div className="errline" role="alert">{err}</div>}
        <Properties d={d!} statuses={meta?.statuses ?? []} labels={meta?.labels ?? []} fields={meta?.fields ?? []} readOnly={readOnly} onPatch={(b) => patch.mutate(b)} />
        <Description d={d!} readOnly={readOnly} onSave={(description_md) => patch.mutate({ description_md })} />
        {!d!.parent_id && <Subtasks d={d!} statuses={meta?.statuses ?? []} projectKey={project?.key} readOnly={readOnly} />}
        <Notes d={d!} readOnly={readOnly} />
        <History acts={d!.activity} />
      </div>
    </>
  )
}

function Title({ d, readOnly, onSave }: { d: TaskDetail; readOnly: boolean; onSave: (t: string) => void }) {
  const [editing, setEditing] = useState(false)
  const [v, setV] = useState(d.title)
  if (editing) {
    const done = () => { setEditing(false); if (v.trim() && v.trim() !== d.title) onSave(v.trim()) }
    return (
      <h1><input className="titleedit" autoFocus aria-label="Название задачи" value={v} onChange={(e) => setV(e.target.value)} onBlur={done}
        onKeyDown={(e) => {
          if (e.key === 'Enter') done()
          if (e.key === 'Escape') { e.stopPropagation(); setV(d.title); setEditing(false) }
        }} /></h1>
    )
  }
  return <h1><button type="button" className="titlebtn" disabled={readOnly} title="Нажмите, чтобы изменить" onClick={() => { setV(d.title); setEditing(true) }}>{d.title}</button></h1>
}

function Properties({ d, statuses, labels, fields, readOnly, onPatch }: {
  d: TaskDetail; statuses: Status[]; labels: { id: number; name: string; color: string }[]; fields: Field[]; readOnly: boolean
  onPatch: (b: Record<string, unknown>) => void
}) {
  const st = statuses.find((s) => s.key === d.status)
  const pr = PRIORITIES[d.priority]
  const area = labels.find((l) => l.name === d.area)
  const [newArea, setNewArea] = useState('')
  const menu = (label: ReactNode, items: (close: () => void) => ReactNode) =>
    readOnly ? <>{label}</> : <Menu className="pkbtn" label={label}>{items}</Menu>

  // Linked tasks: codes mentioned in the description, plus the parent.
  const mentioned = [...new Set([...(d.description_md.match(CODE) ?? []), ...(d.parent_code ? [d.parent_code] : [])])].filter((c) => c !== d.code).slice(0, 8)
  const linked = useQueries({ queries: mentioned.map((c) => ({ queryKey: ['task-mini', c], queryFn: () => api.task(c), retry: false })) })
  const links = linked.flatMap((r) => (r.data ? [r.data] : []))

  return (
    <div className="props">
      <Prop icon="clock" label="Статус">
        {menu(<span className="chip" style={st ? statusChip(st.color) : undefined}><span className="dot" style={{ background: st ? statusColor(st.color) : 'var(--bk)' }} />{d.status_name}</span>,
          (close) => statuses.map((s) => (
            <button key={s.id} type="button" onClick={() => { onPatch({ status: s.key }); close() }}>
              <span className="dot" style={{ background: statusColor(s.color) }} />{s.name}{s.key === d.status && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}
            </button>
          )))}
      </Prop>
      <Prop icon="flag" label="Приоритет">
        {menu(<span className="chip" style={pr.style}><span className="mono" style={{ fontWeight: 600, fontSize: 12 }}>{d.priority}</span>{pr.name}</span>,
          (close) => Object.entries(PRIORITIES).map(([k, p]) => (
            <button key={k} type="button" onClick={() => { onPatch({ priority: k }); close() }}><span className="mono">{k}</span>{p.name}{k === d.priority && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}</button>
          )))}
      </Prop>
      <Prop icon="tag" label="Область">
        {menu(d.area
          ? <span className="chip" style={{ background: 'var(--sk)', color: 'var(--t2)' }}><span className="dot" style={{ background: area?.color ?? 'var(--bk)' }} />{d.area}</span>
          : <span className="muted">не задана</span>,
        (close) => (
          <>
            {labels.map((l) => (
              <button key={l.id} type="button" onClick={() => { onPatch({ area: l.name }); close() }}><span className="dot" style={{ background: l.color }} />{l.name}{l.name === d.area && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}</button>
            ))}
            {d.area && <button type="button" onClick={() => { onPatch({ area: '' }); close() }}>Убрать область</button>}
            <form style={{ padding: 4 }} onSubmit={(e) => { e.preventDefault(); if (newArea.trim()) { onPatch({ area: newArea.trim() }); setNewArea(''); close() } }}>
              <input className="textin" style={{ height: 30 }} placeholder="Новая область" aria-label="Новая область" value={newArea} onChange={(e) => setNewArea(e.target.value)} />
            </form>
          </>
        ))}
      </Prop>
      <Prop icon="cal" label="Дата">
        {menu(d.planned_for ? <span style={{ fontSize: 13.5 }}>{dateLabel(d.planned_for)}</span> : <span className="muted">не задана</span>,
          (close) => (
            <>
              {[todayStr(), addDays(todayStr(), 1)].map((day) => <button key={day} type="button" onClick={() => { onPatch({ planned_for: day }); close() }}>{fmtDay(day)}</button>)}
              <button type="button" onClick={() => { onPatch({ planned_for: '' }); close() }}>Без даты</button>
              <div style={{ padding: 4 }}>
                <input className="dateinput" type="date" aria-label="Выбрать дату" defaultValue={d.planned_for ?? ''}
                  onChange={(e) => { if (e.target.value) { onPatch({ planned_for: isoDay(new Date(e.target.value + 'T12:00:00')) }); close() } }} />
              </div>
            </>
          ))}
      </Prop>
      {links.length > 0 && (
        <Prop icon="link" label="Связано">
          <span className="chips">
            {links.map((t) => <Link key={t.id} className="lk" to={hrefWith('task', t.code)}><span className="mono" style={{ fontSize: 11.5 }}>{t.code}</span>{t.title}</Link>)}
          </span>
        </Prop>
      )}
      <Prop icon="user" label="Создал">
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13.5 }}>
          {d.created_by === 'claude' ? <ClaudeBadge /> : <b style={{ fontWeight: 600 }}>Denis</b>}
          <span style={{ color: 'var(--t3)' }}>{d.created_by === 'claude' ? 'через MCP · ' : ''}{fmtStamp(d.created_at)}</span>
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
    editor = readOnly ? <>{cur ?? <span className="muted">—</span>}</> : (
      <Menu className="pkbtn" label={cur ? <span className="chip" style={{ background: 'var(--sk)', color: 'var(--t2)' }}>{String(cur)}</span> : <span className="muted">не задано</span>}>
        {(close) => (
          <>
            {f.options.map((o) => <button key={o} type="button" onClick={() => { set(o); close() }}>{o}{o === cur && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}</button>)}
            {cur !== undefined && <button type="button" onClick={() => { set(null); close() }}>Очистить</button>}
          </>
        )}
      </Menu>
    )
  } else {
    editor = (
      <input className="dateinput" style={{ width: 220 }} type={f.type === 'number' ? 'number' : 'url'} aria-label={f.name} disabled={readOnly}
        placeholder={f.type === 'url' ? 'https://…' : '—'} defaultValue={cur === undefined ? '' : String(cur)} key={String(cur)}
        onBlur={(e) => { if (e.target.value !== (cur === undefined ? '' : String(cur))) set(e.target.value === '' ? null : e.target.value) }}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
    )
  }
  return <Prop icon={f.type === 'url' ? 'link' : f.type === 'number' ? 'bars' : 'tag'} label={f.name}>{editor}</Prop>
}

function Description({ d, readOnly, onSave }: { d: TaskDetail; readOnly: boolean; onSave: (md: string) => void }) {
  const [edit, setEdit] = useState(false)
  const [draft, setDraft] = useState(d.description_md)
  const view = (
    <div className="md"
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
    <section className="psec">
      <div className="sec">
        <h2 className="grow">Описание</h2>
        <span className="segmini" role="group" aria-label="Режим описания">
          <button type="button" className={!edit ? 'on' : ''} aria-pressed={!edit} onClick={() => { if (edit && draft !== d.description_md) onSave(draft); setEdit(false) }}>Просмотр</button>
          <button type="button" className={edit ? 'on' : ''} aria-pressed={edit} disabled={readOnly} onClick={() => { setDraft(d.description_md); setEdit(true) }}>Markdown</button>
        </span>
      </div>
      {edit ? (
        <textarea className="mdedit mono" autoFocus aria-label="Описание в markdown" value={draft} rows={Math.max(8, draft.split('\n').length + 1)}
          onChange={(e) => setDraft(e.target.value)} onBlur={() => draft !== d.description_md && onSave(draft)} placeholder="Markdown: **жирный**, - [ ] чекбокс, [ссылка](https://…)" />
      ) : d.description_md.trim() ? view : (
        <p className="muted" style={{ margin: 0 }}>Описания нет. Нажмите «Markdown», чтобы добавить.</p>
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
    <section className="psec">
      <div className="sec">
        <h2>Подзадачи</h2>
        <span className="mono grow tabnum" style={{ fontSize: 12, color: 'var(--t3)' }}>{done} / {d.subtasks.length}</span>
        {d.subtasks.length > 0 && <span className="bar" style={{ width: 110 }} role="img" aria-label={`Готово ${done} из ${d.subtasks.length}`}><span style={{ width: `${(done / d.subtasks.length) * 100}%` }} /></span>}
      </div>
      <div>
        {d.subtasks.map((s) => {
          const on = s.status_category === 'done'
          return (
            <div className="subrow" key={s.id}>
              <button type="button" className={'cb' + (on ? ' on' : '')} role="checkbox" aria-checked={on} aria-label={s.title} disabled={readOnly} onClick={() => toggle.mutate(s)}>
                {on && <Icon name="check" size={11} sw={3.4} />}
              </button>
              <Link to={hrefWith('task', s.code)} className={'grow' + (on ? ' donetxt' : '')} style={{ textDecoration: 'none' }}>{s.title}</Link>
              <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{s.code}</span>
            </div>
          )
        })}
        {!readOnly && (
          <label className="subadd">
            <Icon name="plus" size={14} sw={2} />
            <input placeholder="Добавить подзадачу" aria-label="Добавить подзадачу" value={title} onChange={(e) => setTitle(e.target.value)} disabled={!projectKey}
              onKeyDown={(e) => e.key === 'Enter' && title.trim() && !add.isPending && add.mutate(title.trim())} />
          </label>
        )}
      </div>
      {add.isError && <div className="errline" role="alert">{add.error.message}</div>}
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
    <section className="psec">
      <div className="sec"><h2 className="grow">Заметки</h2><span className="mono" style={{ fontSize: 12, color: 'var(--t3)' }}>{d.notes.length}</span></div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        {d.notes.map((n) => (
          <div key={n.id} style={{ display: 'flex', gap: 12 }}>
            <span className={'avatar28 ' + (n.author === 'claude' ? 'ai' : 'me')}><Icon name={n.author === 'claude' ? 'spark' : 'user'} size={14} sw={2} /></span>
            <div style={{ flex: '1 1 0', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 3 }}>
              <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
                <b style={{ fontWeight: 600, fontSize: 13.5 }}>{n.author === 'claude' ? 'claude' : 'Denis'}</b>
                <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{fmtStamp(n.created_at)}</span>
              </div>
              <div className="md note" dangerouslySetInnerHTML={{ __html: renderMarkdown(n.body_md) }} />
            </div>
          </div>
        ))}
        <div ref={end} />
        {!readOnly && (
          <label className="noteadd">
            <textarea rows={2} placeholder="Добавить заметку" aria-label="Добавить заметку" value={text} onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => (e.metaKey || e.ctrlKey) && e.key === 'Enter' && send()} />
            <button type="button" className="btn pri" disabled={!text.trim() || add.isPending} onClick={send}>Отправить<span className="kbd">⌘↵</span></button>
          </label>
        )}
        {add.isError && <div className="errline" role="alert">{add.error.message}</div>}
      </div>
    </section>
  )
}

function History({ acts }: { acts: Activity[] }) {
  const [all, setAll] = useState(false)
  const shown = all ? acts : acts.slice(0, 5)
  return (
    <section className="psec">
      <div className="sec">
        <h2 className="grow">История</h2>
        {acts.length > 5 && <button type="button" className="linkbtn" onClick={() => setAll(!all)}>{all ? 'свернуть' : 'показать всё'}</button>}
      </div>
      {shown.length === 0 && <p className="muted" style={{ margin: 0 }}>Изменений пока нет.</p>}
      {shown.map((a) => (
        <div className="hist" key={a.id}>
          <span className="mono">{fmtStamp(a.at)}</span>
          <span><b style={{ fontWeight: 600, color: 'var(--tx)' }}>{a.actor === 'claude' ? 'claude' : 'Denis'}</b> {histText(a)}</span>
        </div>
      ))}
    </section>
  )
}
