// Project screen: board (columns by status, drag-and-drop) and list tabs, filters, grouping, sorting, saved views.
import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCorners, useDroppable, useSensor, useSensors,
  type DragEndEvent, type DragStartEvent,
} from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import {
  type Category, type Field, type Label, type Meta, type Project, type Status, type Task, type View, type ViewFilter,
  api, fmtDay, statusColor, todayStr, useProjects, useProjectsMeta, useWho,
} from './api'
import { Icon, ProjectTile } from './icons'
import { tr, useLang } from './i18n'
import { useFresh } from './live'
import { Link, hrefWith, navigate, setParam, useRoute } from './router'
import { AreaDot, ClaudeBadge, EmptyState, ErrorState, Menu, PriorityChip, Skeleton } from './ui'
import { Wiki } from './Wiki'

type Group = 'status' | 'priority' | 'area'
type Sort = 'manual' | 'priority' | 'date'
type Layout = 'board' | 'list'
type Col = { key: string; name: string; color: string; category?: Category; tasks: Task[]; total: number; canAdd: boolean }

const PRIO = { P0: 0, P1: 1, P2: 2 }
const GROUPS: [Group, string][] = [['status', 'Статус'], ['priority', 'Приоритет'], ['area', 'Область']]
const SORTS: [Sort, string][] = [['manual', 'вручную'], ['priority', 'приоритет'], ['date', 'дата']]
const DATE_OPTS: [string, string][] = [['today', 'Сегодня'], ['overdue', 'Просрочено'], ['none', 'Без даты']]
const DONE_LIMIT = 20

// Shared utility strings (full literals so Tailwind can see them).
const BTN = 'inline-flex items-center gap-2 rounded-md border text-sm font-medium no-underline whitespace-nowrap cursor-pointer disabled:opacity-50 disabled:cursor-default'
const BTN_PLAIN = BTN + ' px-3 border-ln bg-sf text-tx hover:bg-sk'
const BTN_PRI = BTN + ' px-3 border-ink bg-ink text-inkt hover:opacity-90'
const KBD_PRI = 'font-mono text-[11px] font-medium leading-[normal] border border-current rounded-[5px] px-[5px] py-px bg-transparent text-inkt opacity-70'
const FC = 'inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full border text-[12.5px] whitespace-nowrap'
const FC_OFF = ' border-ln bg-sf text-t2'
const FC_ON = ' border-transparent bg-acs text-act font-medium'
const FC_BTN = FC + ' cursor-pointer hover:bg-sk'
const POP_HEAD = 'px-2 pt-1.5 pb-1 text-xs font-medium text-t3'
const POP_ITEM = 'flex items-center gap-2 w-full h-[30px] px-2 border-0 rounded-md bg-transparent text-[13px] text-left cursor-pointer hover:bg-sk'
const TAB = 'inline-flex items-center gap-2 h-[38px] px-0.5 mr-[18px] border-b-2 rounded-none bg-transparent no-underline font-medium text-sm cursor-pointer hover:text-tx'
const MC = 'inline-flex items-center gap-[5px] text-[12px]'
const COLBOX = 'flex flex-[1_0_270px] min-w-[270px] flex-col gap-2.5 rounded-[10px]'
const ADDROW = 'flex items-center gap-2 w-full h-9 px-2.5 rounded-md border-0 bg-transparent text-t3 text-[13px] text-left cursor-pointer hover:bg-sk hover:text-tx'
const CONTENT = 'flex flex-wrap items-start gap-6 px-8 pt-6 pb-8 max-[640px]:px-4'
const COL_MAIN = 'flex flex-[999_1_520px] flex-col gap-[18px] min-w-0 bg-sf border border-ln rounded-xl'
const TH = 'text-left px-3 py-2 border-b border-ln'
const TD = 'px-3 py-2.5 border-t border-ln align-middle group-hover:bg-sf2 group-hover:cursor-pointer'

// ---- filtering, sorting, grouping ----

function matches(t: Task, f: ViewFilter): boolean {
  const today = todayStr()
  if (f.q && !(t.title + ' ' + t.code).toLowerCase().includes(f.q.toLowerCase())) return false
  if (f.priority?.length && !f.priority.includes(t.priority)) return false
  if (f.area?.length && !f.area.includes(t.area)) return false
  if (f.author?.length && !f.author.includes(t.created_by)) return false
  if (f.date?.length) {
    const ok = f.date.some((d) =>
      d === 'today' ? t.planned_for === today : d === 'overdue' ? !!t.planned_for && t.planned_for < today && t.status_category !== 'done' : !t.planned_for)
    if (!ok) return false
  }
  for (const [id, vals] of Object.entries(f.fields ?? {})) {
    if (vals.length && !vals.includes(String(t.custom[id] ?? ''))) return false
  }
  return true
}

function sorted(ts: Task[], sort: Sort): Task[] {
  const out = [...ts]
  if (sort === 'priority') out.sort((a, b) => PRIO[a.priority] - PRIO[b.priority] || a.position - b.position)
  else if (sort === 'date') out.sort((a, b) => (a.planned_for ?? '9999').localeCompare(b.planned_for ?? '9999') || a.position - b.position)
  else out.sort((a, b) => a.position - b.position)
  return out
}

function buildCols(tasks: Task[], total: Task[], group: Group, sort: Sort, meta: Meta, expanded: Set<string>): Col[] {
  const cols: Col[] = []
  const add = (c: Omit<Col, 'tasks' | 'total'>, pick: (t: Task) => boolean) => {
    let ts = tasks.filter(pick)
    const all = total.filter(pick).length
    if (c.category === 'done') {
      ts = [...ts].sort((a, b) => (b.done_at ?? '').localeCompare(a.done_at ?? ''))
      if (!expanded.has(c.key)) ts = ts.slice(0, DONE_LIMIT)
    } else ts = sorted(ts, sort)
    cols.push({ ...c, tasks: ts, total: all })
  }
  if (group === 'status') {
    for (const s of meta.statuses.filter((s) => s.show_on_board))
      add({ key: s.key, name: s.name, color: statusColor(s.color), category: s.category, canAdd: s.category !== 'done' }, (t) => t.status === s.key)
  } else if (group === 'priority') {
    for (const p of ['P0', 'P1', 'P2']) add({ key: p, name: p, color: p === 'P0' ? 'var(--rd)' : p === 'P1' ? 'var(--t2)' : 'var(--bk)', canAdd: false }, (t) => t.priority === p)
  } else {
    for (const l of meta.labels) add({ key: l.name, name: l.name, color: l.color, canAdd: false }, (t) => t.area === l.name)
    add({ key: '', name: tr('Без области'), color: 'var(--bk)', canAdd: false }, (t) => !t.area)
  }
  return cols
}

// ---- cards ----

function TaskCard({ t, labels, fields, overlay }: { t: Task; labels: Label[]; fields: Field[]; overlay?: boolean }) {
  const sortable = useSortable({ id: t.id, disabled: overlay })
  const fresh = useFresh(t.id)
  const done = t.status_category === 'done'
  const today = t.planned_for === todayStr()
  const late = !!t.planned_for && t.planned_for < todayStr() && !done
  if (!overlay && sortable.isDragging)
    return <div ref={sortable.setNodeRef} style={{ transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }} className="h-[92px] border-[1.5px] border-dashed border-ac rounded-[10px] bg-acs opacity-60" />
  return (
    <Link
      to={hrefWith('task', t.code)} draggable={false}
      ref={overlay ? undefined : sortable.setNodeRef}
      style={overlay ? undefined : { transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }}
      className={'flex flex-col gap-2.5 w-full p-3 rounded-[10px] border bg-sf text-left no-underline touch-none ' + (overlay
        ? 'border-ac shadow-pop rotate-[1.8deg] cursor-grabbing'
        : 'border-ln cursor-grab hover:border-ln2' + (fresh ? ' shadow-[inset_0_0_0_2px_var(--ac)]' : ''))}
      {...(overlay ? {} : sortable.attributes)} {...(overlay ? {} : sortable.listeners)}
    >
      <div className="flex items-center gap-2">
        {done && <span className="inline-flex text-dn"><Icon name="check" size={15} sw={2.4} /></span>}
        <span className="font-mono grow text-[11.5px] text-t3">{t.code}</span>
        {t.created_by === 'claude' && <ClaudeBadge />}
        <PriorityChip p={t.priority} />
      </div>
      <div className={'text-[14px] font-medium leading-[1.35] ' + (done ? 'text-t2' : '')}>{t.title}</div>
      <div className="flex flex-wrap items-center gap-3">
        <AreaDot area={t.area} labels={labels} />
        {t.planned_for && (
          <span className={MC + (today ? ' text-act' : late ? ' text-rd' : ' text-t3')}>
            <Icon name="cal" size={13} sw={1.9} />{fmtDay(t.planned_for)}
          </span>
        )}
        {t.subtask_count > 0 && <span className={MC + ' text-t3 tabular-nums'}><Icon name="subtask" size={13} sw={1.9} />{t.subtask_done}/{t.subtask_count}</span>}
        {t.description_md && <span className={MC + ' text-t3'} title={tr('Есть описание')}><Icon name="doc" size={13} sw={1.9} /></span>}
        {fields.filter((f) => f.show_on_card && t.custom[String(f.id)] !== undefined).map((f) => (
          <span key={f.id} className={MC + ' text-t3'}>{f.name}: {String(t.custom[String(f.id)])}</span>
        ))}
      </div>
    </Link>
  )
}

function Column({ col, meta, dnd, adding, onAdd, onStopAdd, onCreate, expanded, onExpand }: {
  col: Col; meta: Meta; dnd: boolean; adding: boolean; onAdd: () => void; onStopAdd: () => void
  onCreate: (title: string) => void; expanded: boolean; onExpand: () => void
}) {
  const drop = useDroppable({ id: 'col:' + col.key, disabled: !dnd })
  const [title, setTitle] = useState('')
  return (
    <div className={COLBOX + (drop.isOver ? ' bg-acs' : '')} ref={drop.setNodeRef}>
      <div className="flex items-center gap-2 px-1 h-[26px]">
        <span className="inline-block w-[9px] h-[9px] flex-[0_0_9px] rounded-full" style={{ background: col.color }} />
        <b className="font-semibold text-sm">{col.name}</b>
        <span className="font-mono grow tabular-nums text-[12px] text-t3">{col.total}</span>
        {col.canAdd && <button type="button" className="inline-grid place-items-center w-6 h-6 p-0 border-0 rounded-md bg-transparent text-t3 cursor-pointer hover:bg-sk hover:text-tx" aria-label={tr('Добавить задачу в «{name}»', { name: col.name })} onClick={onAdd}><Icon name="plus" size={15} sw={2} /></button>}
      </div>
      <SortableContext items={col.tasks.map((t) => t.id)} strategy={verticalListSortingStrategy}>
        <div className="flex flex-col gap-2.5 min-h-2">
          {col.tasks.map((t) => <TaskCard key={t.id} t={t} labels={meta.labels} fields={meta.fields} />)}
        </div>
      </SortableContext>
      {col.category === 'done' && !expanded && col.total > DONE_LIMIT && (
        <button type="button" className={ADDROW} onClick={onExpand}>{tr('Показать все {n}', { n: col.total })}</button>
      )}
      {col.canAdd && (adding ? (
        <input className="w-full px-2.5 py-2 rounded-[10px] border border-ac bg-sf text-[14px] outline-0" autoFocus placeholder={tr('Название, Enter — сохранить')} aria-label={tr('Новая задача в «{name}»', { name: col.name })} value={title}
          onChange={(e) => setTitle(e.target.value)} onBlur={() => !title.trim() && onStopAdd()}
          onKeyDown={(e) => {
            if (e.key === 'Escape') onStopAdd()
            if (e.key === 'Enter' && title.trim()) {
              onCreate(title.trim())
              setTitle('')
            }
          }} />
      ) : (
        <button type="button" className={ADDROW} onClick={onAdd}><Icon name="plus" size={14} sw={2} />{tr('Добавить задачу')}</button>
      ))}
    </div>
  )
}

// ---- filter bar ----

function FilterBar({ filter, setFilter, group, setGroup, sort, setSort, meta, tasks, project, layout }: {
  filter: ViewFilter; setFilter: (f: ViewFilter) => void; group: Group; setGroup: (g: Group) => void; sort: Sort; setSort: (s: Sort) => void
  meta: Meta; tasks: Task[]; project: Project; layout: Layout
}) {
  const qc = useQueryClient()
  const me = useWho()('denis')
  const AUTHOR_OPTS: [string, string][] = [['claude', 'Claude'], ['denis', me]]
  const claudeCount = tasks.filter((t) => t.created_by === 'claude').length
  const toggle = (kind: 'priority' | 'area' | 'date' | 'author', v: string) => {
    const cur = filter[kind] ?? []
    const next = cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v]
    setFilter({ ...filter, [kind]: next.length ? next : undefined })
  }
  const toggleField = (id: number, v: string) => {
    const key = String(id)
    const cur = filter.fields?.[key] ?? []
    const next = cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v]
    const fields = { ...filter.fields, [key]: next }
    if (!next.length) delete fields[key]
    setFilter({ ...filter, fields: Object.keys(fields).length ? fields : undefined })
  }
  const chips: { label: string; clear: () => void }[] = []
  if (filter.priority?.length) chips.push({ label: `${tr('Приоритет')}: ${filter.priority.join(', ')}`, clear: () => setFilter({ ...filter, priority: undefined }) })
  if (filter.area?.length) chips.push({ label: `${tr('Область')}: ${filter.area.join(', ')}`, clear: () => setFilter({ ...filter, area: undefined }) })
  if (filter.date?.length) chips.push({ label: `${tr('Дата')}: ${filter.date.map((d) => tr(DATE_OPTS.find((o) => o[0] === d)?.[1] ?? '')).join(', ')}`, clear: () => setFilter({ ...filter, date: undefined }) })
  if (filter.author?.length) chips.push({ label: `${tr('Автор')}: ${filter.author.map((a) => AUTHOR_OPTS.find((o) => o[0] === a)?.[1]).join(', ')}`, clear: () => setFilter({ ...filter, author: undefined }) })
  for (const [id, vals] of Object.entries(filter.fields ?? {})) {
    const f = meta.fields.find((x) => String(x.id) === id)
    if (f) chips.push({ label: `${f.name}: ${vals.join(', ')}`, clear: () => { const fields = { ...filter.fields }; delete fields[id]; setFilter({ ...filter, fields }) } })
  }
  const item = (label: string, on: boolean, click: () => void, key?: string) => (
    <button key={key ?? label} type="button" role="menuitemcheckbox" aria-checked={on} className={POP_ITEM} onClick={click}>{label}{on && <span className="ml-auto text-act"><Icon name="check" size={14} sw={2.2} /></span>}</button>
  )
  const [naming, setNaming] = useState('')
  const saveView = useMutation({
    mutationFn: (name: string) => api.createView({ project_id: project.id, name, layout, filter, group_by: group, sort }),
    onSuccess: (v) => { qc.invalidateQueries(); navigate(`/p/${project.slug}?view=${v.id}`) },
  })

  return (
    <div className="flex flex-wrap items-center gap-2 px-7 py-3.5 border-b border-ln">
      <label className="flex items-center gap-2 h-[30px] w-[220px] px-2.5 border border-ln rounded-md bg-sf text-t3 focus-within:border-ac focus-within:rounded-md focus-within:outline-2 focus-within:outline-ac focus-within:outline-offset-2">
        <Icon name="search" size={14} />
        <input className="w-full border-0 bg-transparent outline-0 text-[13px] text-tx" type="text" placeholder={tr('Фильтр по названию')} aria-label={tr('Фильтр по названию')} value={filter.q ?? ''}
          onChange={(e) => setFilter({ ...filter, q: e.target.value || undefined })} />
      </label>
      {chips.map((c) => (
        <span key={c.label} className={FC + FC_ON}>{c.label}
          <button type="button" className="inline-grid place-items-center cursor-pointer" aria-label={tr('Убрать фильтр: {name}', { name: c.label })} onClick={c.clear}><Icon name="x" size={12} sw={2.2} /></button>
        </span>
      ))}
      <Menu className={FC_BTN + FC_OFF + ' border-dashed'} label={<><Icon name="plus" size={12} sw={2.2} />{tr('Фильтр')}</>}>
        {() => (
          <>
            <div className={POP_HEAD}>{tr('Приоритет')}</div>
            {['P0', 'P1', 'P2'].map((p) => item(p, !!filter.priority?.includes(p), () => toggle('priority', p)))}
            {meta.labels.length > 0 && <div className={POP_HEAD}>{tr('Область')}</div>}
            {meta.labels.map((l) => item(l.name, !!filter.area?.includes(l.name), () => toggle('area', l.name), 'a' + l.id))}
            <div className={POP_HEAD}>{tr('Дата')}</div>
            {DATE_OPTS.map(([k, n]) => item(tr(n), !!filter.date?.includes(k), () => toggle('date', k)))}
            <div className={POP_HEAD}>{tr('Автор')}</div>
            {AUTHOR_OPTS.map(([k, n]) => item(n, !!filter.author?.includes(k), () => toggle('author', k)))}
            {meta.fields.filter((f) => f.type === 'select').map((f) => (
              <div key={f.id}>
                <div className={POP_HEAD}>{f.name}</div>
                {f.options.map((o) => item(o, !!filter.fields?.[String(f.id)]?.includes(o), () => toggleField(f.id, o), `f${f.id}${o}`))}
              </div>
            ))}
          </>
        )}
      </Menu>
      {(chips.length > 0 || filter.q) && (
        <Menu className={FC_BTN + FC_OFF} label={<><Icon name="funnel" size={12} sw={2} />{tr('Сохранить как вид')}</>}>
          {(close) => (
            <form className="flex gap-1.5 p-1" onSubmit={(e) => { e.preventDefault(); if (naming.trim()) { saveView.mutate(naming.trim()); setNaming(''); close() } }}>
              <input className="w-full h-[30px] px-2.5 border border-ln rounded-md bg-sf text-sm text-tx focus:border-ac focus:outline-0" autoFocus placeholder={tr('Название вида')} aria-label={tr('Название вида')} value={naming} onChange={(e) => setNaming(e.target.value)} />
              <button type="submit" className={BTN_PRI + ' h-[30px]'}>OK</button>
            </form>
          )}
        </Menu>
      )}
      <span className="grow" />
      <Menu right label={<>{tr('Группа')}: {tr(GROUPS.find((g) => g[0] === group)![1])}<Icon name="chevD" size={12} sw={2} /></>}>
        {(close) => GROUPS.map(([k, n]) => item(tr(n), k === group, () => { setGroup(k); close() }))}
      </Menu>
      <Menu right label={<>{tr('Сортировка')}: {tr(SORTS.find((s) => s[0] === sort)![1])}<Icon name="chevD" size={12} sw={2} /></>}>
        {(close) => SORTS.map(([k, n]) => item(tr(n), k === sort, () => { setSort(k); close() }))}
      </Menu>
      <button type="button" className={FC_BTN + (filter.author?.includes('claude') ? FC_ON : FC_OFF)} aria-pressed={!!filter.author?.includes('claude')} onClick={() => toggle('author', 'claude')}>
        <Icon name="plug" size={13} sw={2} />{tr('Создано Claude')}: {claudeCount}
      </button>
    </div>
  )
}

// ---- list tab ----

function ListTable({ tasks, meta }: { tasks: Task[]; meta: Meta }) {
  const color = (s: Status | undefined) => (s ? statusColor(s.color) : 'var(--bk)')
  return (
    <div className="px-7 pt-[18px] pb-7 overflow-x-auto">
      <table className="w-full border-separate border-spacing-0 text-sm bg-sf border border-ln rounded-xl">
        <thead>
          <tr className="text-xs font-medium text-t3"><th className={TH}>ID</th><th className={TH}>{tr('Название')}</th><th className={TH}>{tr('Статус')}</th><th className={TH}>{tr('Приоритет')}</th><th className={TH}>{tr('Область')}</th><th className={TH}>{tr('Дата')}</th></tr>
        </thead>
        <tbody>
          {tasks.map((t) => {
            const st = meta.statuses.find((s) => s.key === t.status)
            return (
              <tr key={t.id} className="group">
                <td className={TD + ' font-mono text-[12px] text-t3 whitespace-nowrap'}><Link to={hrefWith('task', t.code)} className="no-underline">{t.code}</Link></td>
                <td className={TD + ' font-medium'}><Link to={hrefWith('task', t.code)} className="no-underline">{t.title}</Link>{t.created_by === 'claude' && <> <ClaudeBadge /></>}</td>
                <td className={TD + ' whitespace-nowrap'}><span className="inline-flex items-center gap-[5px] text-[13px] text-t2"><span className="inline-block w-2 h-2 flex-[0_0_8px] rounded-full" style={{ background: color(st) }} />{t.status_name}</span></td>
                <td className={TD + ' whitespace-nowrap'}><PriorityChip p={t.priority} /></td>
                <td className={TD + ' whitespace-nowrap'}><AreaDot area={t.area} labels={meta.labels} /></td>
                <td className={TD + ' tabular-nums text-t2 whitespace-nowrap'}>{t.planned_for ? fmtDay(t.planned_for) : '—'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

// ---- screen ----

export function Board({ slug, onNewTask, wiki }: { slug: string; onNewTask: (o: { project?: Project; status?: string }) => void; wiki?: { id?: number } }) {
  const projects = useProjects()
  const project = projects.data?.find((p) => p.slug === slug)
  if (projects.isError) return <div className={CONTENT}><div className={COL_MAIN}><ErrorState message={projects.error.message} onRetry={() => projects.refetch()} /></div></div>
  if (!project) {
    return projects.isPending
      ? <div className={CONTENT}><Skeleton w="40%" h={28} /></div>
      : <div className={CONTENT}><div className={COL_MAIN}><EmptyState icon="board" title={tr('Проект не найден')} hint={tr('Нет проекта «{slug}». Возможно, он в архиве.', { slug })} /></div></div>
  }
  return <ProjectBoard key={project.id} project={project} onNewTask={onNewTask} wiki={wiki} />
}

function ProjectBoard({ project, onNewTask, wiki }: { project: Project; onNewTask: (o: { project?: Project; status?: string }) => void; wiki?: { id?: number } }) {
  const qc = useQueryClient()
  const { params } = useRoute()
  const viewId = params.get('view')
  const pid = project.id
  const meta = useProjectsMeta([pid])[pid]
  const tq = useQuery({ queryKey: ['tasks', pid], queryFn: () => api.tasks(project.key) })
  const vq = useQuery({ queryKey: ['views', pid], queryFn: () => api.views(pid) })
  const tasks = tq.data ?? []

  const [layout, setLayout] = useState<Layout>('board')
  const [filter, setFilter] = useState<ViewFilter>({})
  const [group, setGroup] = useState<Group>('status')
  const [sort, setSort] = useState<Sort>('manual')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [adding, setAdding] = useState<string | null>(null)
  const [dragId, setDragId] = useState<number | null>(null)

  // Opening a saved view loads its settings once; later edits stay local until saved as a new view.
  const applied = useRef<string | null>(null)
  useEffect(() => {
    if (applied.current === viewId) return
    if (!viewId) { applied.current = null; return }
    const v = vq.data?.find((x) => String(x.id) === viewId)
    if (!v) return
    applied.current = viewId
    setLayout(v.layout); setFilter(v.filter ?? {}); setGroup(v.group_by as Group); setSort(v.sort as Sort)
  }, [viewId, vq.data])

  const shown = useMemo(() => tasks.filter((t) => matches(t, filter)), [tasks, filter])
  const lang = useLang() // buildCols translates the "no area" column name, so a language change must rebuild the columns
  const cols = useMemo(() => buildCols(shown, tasks, group, sort, meta, expanded), [shown, tasks, group, sort, meta, expanded, lang])
  const colOf = new Map<number, string>()
  cols.forEach((c) => c.tasks.forEach((t) => colOf.set(t.id, c.key)))
  const dnd = layout === 'board' && group === 'status'

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates, keyboardCodes: { start: ['Space'], cancel: ['Escape'], end: ['Space'] } }),
  )

  const move = useMutation({
    mutationFn: (v: { id: number; code: string; status: string; before?: number; after?: number }) =>
      api.moveTask(v.code, { status: v.status, before_id: v.before, after_id: v.after }),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: ['tasks', pid] })
      const prev = qc.getQueryData<Task[]>(['tasks', pid])
      if (prev) qc.setQueryData<Task[]>(['tasks', pid], optimisticMove(prev, v, meta.statuses))
      return { prev }
    },
    onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(['tasks', pid], ctx.prev),
    onSettled: () => qc.invalidateQueries(),
  })
  const create = useMutation({
    mutationFn: (v: { title: string; status: string }) => api.createTask({ project: project.key, title: v.title, status: v.status }),
    onSettled: () => qc.invalidateQueries(),
  })

  const onDragStart = (e: DragStartEvent) => setDragId(Number(e.active.id))
  const onDragEnd = (e: DragEndEvent) => {
    setDragId(null)
    const { active, over } = e
    if (!over) return
    const task = tasks.find((t) => t.id === Number(active.id))
    if (!task) return
    const overId = String(over.id)
    let targetKey: string, before: number | undefined, after: number | undefined
    if (overId.startsWith('col:')) {
      targetKey = overId.slice(4)
      const col = cols.find((c) => c.key === targetKey)?.tasks.filter((t) => t.id !== task.id) ?? []
      if (col.length) after = col[col.length - 1].id
    } else {
      const overTask = tasks.find((t) => t.id === Number(overId))
      if (!overTask || overTask.id === task.id) return
      targetKey = colOf.get(overTask.id) ?? overTask.status
      const rect = active.rect.current.translated
      const below = !!rect && rect.top + rect.height / 2 > over.rect.top + over.rect.height / 2
      if (below) after = overTask.id
      else before = overTask.id
    }
    if (sort !== 'manual') { before = undefined; after = undefined } // a sorted column has no manual order
    if (targetKey === task.status && before === undefined && after === undefined) return
    move.mutate({ id: task.id, code: task.code, status: targetKey, before, after })
  }

  const dragged = tasks.find((t) => t.id === dragId)
  const tab = (label: string, icon: string, on: boolean, click: () => void) => (
    <button type="button" key={label} className={TAB + (on ? ' text-tx border-b-ac' : ' text-t3 border-b-transparent')} aria-current={on ? 'page' : undefined} onClick={click}>
      <Icon name={icon} size={15} />{label}
    </button>
  )
  const plain = (l: Layout) => { setLayout(l); if (wiki) navigate(`/p/${project.slug}`); else setParam('view', null) }

  return (
    <>
      <header className="flex flex-col gap-3.5 px-7 pt-[22px] border-b border-ln">
        <div className="flex flex-wrap items-center gap-3.5">
          <ProjectTile icon={project.icon} color={project.color} size={40} radius={11} />
          <div className="flex flex-col gap-0.5 flex-[1_1_280px] min-w-0">
            <h1 className="text-2xl font-bold tracking-tight text-balance">{project.name}</h1>
            <span className="text-[13px] text-t3">
              {project.description && <>{project.description} · </>}{tr('ключ')} <span className="font-mono">{project.key}</span> · {tr('{open} открытых, показано {shown} из {total}', { open: project.open, shown: shown.length, total: tasks.length })}
            </span>
          </div>
          <Link to={`/p/${project.slug}/settings`} className={BTN_PLAIN + ' h-9'} aria-label={tr('Настройки проекта')}><Icon name="sliders" size={16} />{tr('Настройки')}</Link>
          <button type="button" className={BTN_PRI + ' h-9'} onClick={() => onNewTask({ project })}><Icon name="plus" size={16} sw={2} />{tr('Новая задача')}<span className={KBD_PRI}>C</span></button>
        </div>
        <nav className="flex flex-wrap items-end" aria-label={tr('Виды проекта')}>
          {tab(tr('Доска'), 'board', !wiki && !viewId && layout === 'board', () => plain('board'))}
          {tab(tr('Список'), 'list', !wiki && !viewId && layout === 'list', () => plain('list'))}
          {tab(tr('Вики'), 'doc', !!wiki, () => navigate(`/p/${project.slug}/wiki`))}
          {(vq.data ?? []).filter((v: View) => v.project_id === pid).map((v) => (
            <Link key={v.id} to={`/p/${project.slug}?view=${v.id}`} className={TAB + (viewId === String(v.id) ? ' text-tx border-b-ac' : ' text-t3 border-b-transparent')}><Icon name="funnel" size={14} />{v.name}</Link>
          ))}
        </nav>
      </header>

      {wiki ? <Wiki project={project} pageId={wiki.id} /> : <>
      <FilterBar filter={filter} setFilter={setFilter} group={group} setGroup={setGroup} sort={sort} setSort={setSort}
        meta={meta} tasks={tasks} project={project} layout={layout} />

      {tq.isError && <div className={CONTENT}><div className={COL_MAIN}><ErrorState message={tq.error.message} onRetry={() => tq.refetch()} /></div></div>}
      {tq.isPending && (
        <div className="grow overflow-x-auto px-7 pt-[18px] pb-7"><div className="flex items-start gap-4">{[0, 1, 2, 3].map((i) => (
          <div key={i} className={COLBOX} aria-hidden="true"><Skeleton w="50%" h={18} /><Skeleton h={92} /><Skeleton h={92} /></div>
        ))}</div></div>
      )}
      {tq.isSuccess && tasks.length === 0 && (
        <div className="px-7 pt-[18px]">
          <div className="bg-sf border border-ln rounded-xl"><EmptyState icon="board" title={tr('В проекте пока нет задач')}
            hint={tr('Добавьте первую задачу в колонке ниже или попросите Claude создать их через MCP.')} /></div>
        </div>
      )}
      {tq.isSuccess && layout === 'board' && (
        <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={() => setDragId(null)}>
          <section className="grow overflow-x-auto px-7 pt-[18px] pb-7">
            <div className="flex items-start gap-4">
              {cols.map((c) => (
                <Column key={c.key} col={c} meta={meta} dnd={dnd} adding={adding === c.key} expanded={expanded.has(c.key)}
                  onAdd={() => setAdding(c.key)} onStopAdd={() => setAdding(null)}
                  onExpand={() => setExpanded(new Set(expanded).add(c.key))}
                  onCreate={(title) => create.mutate({ title, status: c.key })} />
              ))}
            </div>
          </section>
          <DragOverlay>{dragged && <TaskCard t={dragged} labels={meta.labels} fields={meta.fields} overlay />}</DragOverlay>
        </DndContext>
      )}
      {tq.isSuccess && layout === 'list' && <ListTable tasks={cols.flatMap((c) => c.tasks)} meta={meta} />}
      </>}
    </>
  )
}

/** Mirrors the server's placement so a dropped card appears at once; the refetch afterwards is authoritative. */
function optimisticMove(tasks: Task[], v: { id: number; status: string; before?: number; after?: number }, statuses: Status[]): Task[] {
  const st = statuses.find((s) => s.key === v.status)
  if (!st) return tasks
  const col = tasks.filter((t) => t.status === v.status && t.id !== v.id).sort((a, b) => a.position - b.position)
  let idx = col.length
  if (v.before !== undefined) idx = Math.max(0, col.findIndex((t) => t.id === v.before))
  else if (v.after !== undefined) idx = col.findIndex((t) => t.id === v.after) + 1 || col.length
  const pos = col.length === 0 ? 1024 : idx === 0 ? col[0].position - 1024 : idx === col.length ? col[col.length - 1].position + 1024 : (col[idx - 1].position + col[idx].position) / 2
  const now = new Date().toISOString()
  return tasks.map((t) => t.id !== v.id ? t : {
    ...t, status: st.key, status_id: st.id, status_name: st.name, status_category: st.category, position: pos,
    done_at: st.category === 'done' ? t.done_at ?? now : null,
  })
}
