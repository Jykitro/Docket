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
  api, fmtDay, statusColor, todayStr, useProjects, useProjectsMeta,
} from './api'
import { Icon, ProjectTile } from './icons'
import { useFresh } from './live'
import { Link, hrefWith, navigate, setParam, useRoute } from './router'
import { AreaDot, ClaudeBadge, EmptyState, ErrorState, Menu, PriorityChip, Skeleton } from './ui'

type Group = 'status' | 'priority' | 'area'
type Sort = 'manual' | 'priority' | 'date'
type Layout = 'board' | 'list'
type Col = { key: string; name: string; color: string; category?: Category; tasks: Task[]; total: number; canAdd: boolean }

const PRIO = { P0: 0, P1: 1, P2: 2 }
const GROUPS: [Group, string][] = [['status', 'Статус'], ['priority', 'Приоритет'], ['area', 'Область']]
const SORTS: [Sort, string][] = [['manual', 'вручную'], ['priority', 'приоритет'], ['date', 'дата']]
const DATE_OPTS: [string, string][] = [['today', 'Сегодня'], ['overdue', 'Просрочено'], ['none', 'Без даты']]
const AUTHOR_OPTS: [string, string][] = [['claude', 'Claude'], ['denis', 'Denis']]
const DONE_LIMIT = 20

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
    add({ key: '', name: 'Без области', color: 'var(--bk)', canAdd: false }, (t) => !t.area)
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
    return <div ref={sortable.setNodeRef} style={{ transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }} className="ghost" />
  return (
    <Link
      to={hrefWith('task', t.code)} draggable={false}
      ref={overlay ? undefined : sortable.setNodeRef}
      style={overlay ? undefined : { transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }}
      className={'card' + (overlay ? ' drag' : '') + (fresh ? ' fresh' : '')}
      {...(overlay ? {} : sortable.attributes)} {...(overlay ? {} : sortable.listeners)}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        {done && <span style={{ color: 'var(--dn)', display: 'inline-flex' }}><Icon name="check" size={15} sw={2.4} /></span>}
        <span className="mono grow" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{t.code}</span>
        {t.created_by === 'claude' && <ClaudeBadge />}
        <PriorityChip p={t.priority} />
      </div>
      <div className={'t' + (done ? ' done' : '')}>{t.title}</div>
      <div className="meta">
        <AreaDot area={t.area} labels={labels} />
        {t.planned_for && (
          <span className="mc" style={{ color: today ? 'var(--act)' : late ? 'var(--rd)' : undefined }}>
            <Icon name="cal" size={13} sw={1.9} />{fmtDay(t.planned_for)}
          </span>
        )}
        {t.subtask_count > 0 && <span className="mc tabnum"><Icon name="subtask" size={13} sw={1.9} />{t.subtask_done}/{t.subtask_count}</span>}
        {t.description_md && <span className="mc" title="Есть описание"><Icon name="doc" size={13} sw={1.9} /></span>}
        {fields.filter((f) => f.show_on_card && t.custom[String(f.id)] !== undefined).map((f) => (
          <span key={f.id} className="mc">{f.name}: {String(t.custom[String(f.id)])}</span>
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
    <div className={'colbox' + (drop.isOver ? ' over' : '')} ref={drop.setNodeRef}>
      <div className="colhead">
        <span className="dot" style={{ width: 9, height: 9, flexBasis: 9, background: col.color }} />
        <b>{col.name}</b>
        <span className="mono grow tabnum" style={{ fontSize: 12, color: 'var(--t3)' }}>{col.total}</span>
        {col.canAdd && <button type="button" className="iconbtn" aria-label={`Добавить задачу в «${col.name}»`} onClick={onAdd}><Icon name="plus" size={15} sw={2} /></button>}
      </div>
      <SortableContext items={col.tasks.map((t) => t.id)} strategy={verticalListSortingStrategy}>
        <div className="colcards">
          {col.tasks.map((t) => <TaskCard key={t.id} t={t} labels={meta.labels} fields={meta.fields} />)}
        </div>
      </SortableContext>
      {col.category === 'done' && !expanded && col.total > DONE_LIMIT && (
        <button type="button" className="addrow" onClick={onExpand}>Показать все {col.total}</button>
      )}
      {col.canAdd && (adding ? (
        <input className="inlineadd" autoFocus placeholder="Название, Enter — сохранить" aria-label={`Новая задача в «${col.name}»`} value={title}
          onChange={(e) => setTitle(e.target.value)} onBlur={() => !title.trim() && onStopAdd()}
          onKeyDown={(e) => {
            if (e.key === 'Escape') onStopAdd()
            if (e.key === 'Enter' && title.trim()) {
              onCreate(title.trim())
              setTitle('')
            }
          }} />
      ) : (
        <button type="button" className="addrow" onClick={onAdd}><Icon name="plus" size={14} sw={2} />Добавить задачу</button>
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
  if (filter.priority?.length) chips.push({ label: `Приоритет: ${filter.priority.join(', ')}`, clear: () => setFilter({ ...filter, priority: undefined }) })
  if (filter.area?.length) chips.push({ label: `Область: ${filter.area.join(', ')}`, clear: () => setFilter({ ...filter, area: undefined }) })
  if (filter.date?.length) chips.push({ label: `Дата: ${filter.date.map((d) => DATE_OPTS.find((o) => o[0] === d)?.[1]).join(', ')}`, clear: () => setFilter({ ...filter, date: undefined }) })
  if (filter.author?.length) chips.push({ label: `Автор: ${filter.author.map((a) => AUTHOR_OPTS.find((o) => o[0] === a)?.[1]).join(', ')}`, clear: () => setFilter({ ...filter, author: undefined }) })
  for (const [id, vals] of Object.entries(filter.fields ?? {})) {
    const f = meta.fields.find((x) => String(x.id) === id)
    if (f) chips.push({ label: `${f.name}: ${vals.join(', ')}`, clear: () => { const fields = { ...filter.fields }; delete fields[id]; setFilter({ ...filter, fields }) } })
  }
  const item = (label: string, on: boolean, click: () => void, key?: string) => (
    <button key={key ?? label} type="button" role="menuitemcheckbox" aria-checked={on} onClick={click}>{label}{on && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}</button>
  )
  const [naming, setNaming] = useState('')
  const saveView = useMutation({
    mutationFn: (name: string) => api.createView({ project_id: project.id, name, layout, filter, group_by: group, sort }),
    onSuccess: (v) => { qc.invalidateQueries(); navigate(`/p/${project.slug}?view=${v.id}`) },
  })

  return (
    <div className="filterbar">
      <label className="qbox">
        <Icon name="search" size={14} />
        <input type="text" placeholder="Фильтр по названию" aria-label="Фильтр по названию" value={filter.q ?? ''}
          onChange={(e) => setFilter({ ...filter, q: e.target.value || undefined })} />
      </label>
      {chips.map((c) => (
        <span key={c.label} className="fc on">{c.label}
          <button type="button" className="x" aria-label={`Убрать фильтр: ${c.label}`} onClick={c.clear}><Icon name="x" size={12} sw={2.2} /></button>
        </span>
      ))}
      <Menu className="fc dashed" label={<><Icon name="plus" size={12} sw={2.2} />Фильтр</>}>
        {() => (
          <>
            <div className="head cap">Приоритет</div>
            {['P0', 'P1', 'P2'].map((p) => item(p, !!filter.priority?.includes(p), () => toggle('priority', p)))}
            {meta.labels.length > 0 && <div className="head cap">Область</div>}
            {meta.labels.map((l) => item(l.name, !!filter.area?.includes(l.name), () => toggle('area', l.name), 'a' + l.id))}
            <div className="head cap">Дата</div>
            {DATE_OPTS.map(([k, n]) => item(n, !!filter.date?.includes(k), () => toggle('date', k)))}
            <div className="head cap">Автор</div>
            {AUTHOR_OPTS.map(([k, n]) => item(n, !!filter.author?.includes(k), () => toggle('author', k)))}
            {meta.fields.filter((f) => f.type === 'select').map((f) => (
              <div key={f.id}>
                <div className="head cap">{f.name}</div>
                {f.options.map((o) => item(o, !!filter.fields?.[String(f.id)]?.includes(o), () => toggleField(f.id, o), `f${f.id}${o}`))}
              </div>
            ))}
          </>
        )}
      </Menu>
      {(chips.length > 0 || filter.q) && (
        <Menu className="fc" label={<><Icon name="funnel" size={12} sw={2} />Сохранить как вид</>}>
          {(close) => (
            <form style={{ display: 'flex', gap: 6, padding: 4 }} onSubmit={(e) => { e.preventDefault(); if (naming.trim()) { saveView.mutate(naming.trim()); setNaming(''); close() } }}>
              <input className="textin" style={{ height: 30 }} autoFocus placeholder="Название вида" aria-label="Название вида" value={naming} onChange={(e) => setNaming(e.target.value)} />
              <button type="submit" className="btn pri" style={{ height: 30 }}>OK</button>
            </form>
          )}
        </Menu>
      )}
      <span className="grow" />
      <Menu right label={<>Группа: {GROUPS.find((g) => g[0] === group)![1]}<Icon name="chevD" size={12} sw={2} /></>}>
        {(close) => GROUPS.map(([k, n]) => item(n, k === group, () => { setGroup(k); close() }))}
      </Menu>
      <Menu right label={<>Сортировка: {SORTS.find((s) => s[0] === sort)![1]}<Icon name="chevD" size={12} sw={2} /></>}>
        {(close) => SORTS.map(([k, n]) => item(n, k === sort, () => { setSort(k); close() }))}
      </Menu>
      <button type="button" className={'fc' + (filter.author?.includes('claude') ? ' on' : '')} aria-pressed={!!filter.author?.includes('claude')} onClick={() => toggle('author', 'claude')}>
        <Icon name="plug" size={13} sw={2} />Создано Claude: {claudeCount}
      </button>
    </div>
  )
}

// ---- list tab ----

function ListTable({ tasks, meta }: { tasks: Task[]; meta: Meta }) {
  const color = (s: Status | undefined) => (s ? statusColor(s.color) : 'var(--bk)')
  return (
    <div style={{ padding: '18px 28px 28px', overflowX: 'auto' }}>
      <table className="tbl panel" style={{ borderCollapse: 'separate', borderSpacing: 0 }}>
        <thead>
          <tr className="cap"><th>ID</th><th>Название</th><th>Статус</th><th>Приоритет</th><th>Область</th><th>Дата</th></tr>
        </thead>
        <tbody>
          {tasks.map((t) => {
            const st = meta.statuses.find((s) => s.key === t.status)
            return (
              <tr key={t.id} className="rowlink">
                <td className="mono" style={{ fontSize: 12, color: 'var(--t3)' }}><Link to={hrefWith('task', t.code)} style={{ textDecoration: 'none' }}>{t.code}</Link></td>
                <td className="t"><Link to={hrefWith('task', t.code)} style={{ textDecoration: 'none' }}>{t.title}</Link>{t.created_by === 'claude' && <> <ClaudeBadge /></>}</td>
                <td><span className="mc" style={{ color: 'var(--t2)', fontSize: 13 }}><span className="dot" style={{ background: color(st) }} />{t.status_name}</span></td>
                <td><PriorityChip p={t.priority} /></td>
                <td><AreaDot area={t.area} labels={meta.labels} /></td>
                <td className="tabnum" style={{ color: 'var(--t2)' }}>{t.planned_for ? fmtDay(t.planned_for) : '—'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

// ---- screen ----

export function Board({ slug, onNewTask }: { slug: string; onNewTask: (o: { project?: Project; status?: string }) => void }) {
  const projects = useProjects()
  const project = projects.data?.find((p) => p.slug === slug)
  if (projects.isError) return <div className="content"><div className="col-main panel"><ErrorState message={projects.error.message} onRetry={() => projects.refetch()} /></div></div>
  if (!project) {
    return projects.isPending
      ? <div className="content"><Skeleton w="40%" h={28} /></div>
      : <div className="content"><div className="col-main panel"><EmptyState icon="board" title="Проект не найден" hint={`Нет проекта «${slug}». Возможно, он в архиве.`} /></div></div>
  }
  return <ProjectBoard key={project.id} project={project} onNewTask={onNewTask} />
}

function ProjectBoard({ project, onNewTask }: { project: Project; onNewTask: (o: { project?: Project; status?: string }) => void }) {
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
  const cols = useMemo(() => buildCols(shown, tasks, group, sort, meta, expanded), [shown, tasks, group, sort, meta, expanded])
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
    <button type="button" key={label} className={'tab' + (on ? ' on' : '')} aria-current={on ? 'page' : undefined} onClick={click}>
      <Icon name={icon} size={15} />{label}
    </button>
  )
  const plain = (l: Layout) => { setParam('view', null); setLayout(l) }

  return (
    <>
      <header className="projhead">
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, flexWrap: 'wrap' }}>
          <ProjectTile icon={project.icon} color={project.color} size={40} radius={11} />
          <div className="stack" style={{ flexBasis: 280 }}>
            <h1>{project.name}</h1>
            <span className="sub" style={{ fontSize: 13 }}>
              {project.description && <>{project.description} · </>}ключ <span className="mono">{project.key}</span> · {project.open} открытых, показано {shown.length} из {tasks.length}
            </span>
          </div>
          <Link to={`/p/${project.slug}/settings`} className="btn" aria-label="Настройки проекта"><Icon name="sliders" size={16} />Настройки</Link>
          <button type="button" className="btn pri" onClick={() => onNewTask({ project })}><Icon name="plus" size={16} sw={2} />Новая задача<span className="kbd">C</span></button>
        </div>
        <nav className="tabs" aria-label="Виды проекта">
          {tab('Доска', 'board', !viewId && layout === 'board', () => plain('board'))}
          {tab('Список', 'list', !viewId && layout === 'list', () => plain('list'))}
          {(vq.data ?? []).filter((v: View) => v.project_id === pid).map((v) => (
            <Link key={v.id} to={`/p/${project.slug}?view=${v.id}`} className={'tab' + (viewId === String(v.id) ? ' on' : '')}><Icon name="funnel" size={14} />{v.name}</Link>
          ))}
        </nav>
      </header>

      <FilterBar filter={filter} setFilter={setFilter} group={group} setGroup={setGroup} sort={sort} setSort={setSort}
        meta={meta} tasks={tasks} project={project} layout={layout} />

      {tq.isError && <div className="content"><div className="col-main panel"><ErrorState message={tq.error.message} onRetry={() => tq.refetch()} /></div></div>}
      {tq.isPending && (
        <div className="boardwrap"><div className="cols">{[0, 1, 2, 3].map((i) => (
          <div key={i} className="colbox" aria-hidden="true"><Skeleton w="50%" h={18} /><Skeleton h={92} /><Skeleton h={92} /></div>
        ))}</div></div>
      )}
      {tq.isSuccess && tasks.length === 0 && (
        <div style={{ padding: '18px 28px 0' }}>
          <div className="panel"><EmptyState icon="board" title="В проекте пока нет задач"
            hint="Добавьте первую задачу в колонке ниже или попросите Claude создать их через MCP." /></div>
        </div>
      )}
      {tq.isSuccess && layout === 'board' && (
        <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragCancel={() => setDragId(null)}>
          <section className="boardwrap">
            <div className="cols">
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
