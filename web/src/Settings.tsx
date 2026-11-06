// Project settings: every control saves at once (no "Save" button).
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { ApiError, type Field, type Label, type Project, type Status, type View, api, statusColor, useProjects } from './api'
import { Icon, PROJECT_COLORS, PROJECT_ICONS, ProjectTile } from './icons'
import { Link, navigate } from './router'
import { ConfirmButton, Dialog, EmptyState, ErrorState, Menu, Skeleton, Toggle, MENUITEM } from './ui'

const CATEGORIES: [Status['category'], string][] = [['todo', 'К работе'], ['doing', 'В работе'], ['done', 'Готово']]
const STATUS_COLORS = ['bk', 'td', 'dg', 'dn', '#8B6CFF', '#E56B9B', '#2EB5A3', '#C0265F']
const LABEL_COLORS = ['#8B6CFF', '#4C8DF6', '#E5A23A', '#E56B9B', '#8A93A3', '#2EB5A3', '#C0265F', '#17784C']
const FIELD_TYPES: [Field['type'], string, string][] = [['number', 'Число', 'bars'], ['url', 'Ссылка', 'link'], ['select', 'Выбор', 'tag']]
const SECTIONS: [string, string][] = [['general', 'Основное'], ['statuses', 'Статусы'], ['labels', 'Метки'], ['fields', 'Поля'], ['views', 'Виды'], ['data', 'Данные']]

const BTN_LOOK = 'items-center gap-2 whitespace-nowrap rounded-md border font-medium no-underline disabled:cursor-default disabled:opacity-50'
const BTN = 'inline-flex h-9 px-3 text-sm border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const BTN_PRI = 'inline-flex h-9 px-3 text-sm border-ink bg-ink text-inkt hover:opacity-90 ' + BTN_LOOK
const BTN_SM = 'inline-flex h-[30px] px-2.5 text-[13px] border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const BTN_SM_PRI = 'inline-flex h-[30px] px-2.5 text-[13px] border-ink bg-ink text-inkt hover:opacity-90 ' + BTN_LOOK
const BTN_SQ = 'inline-flex size-[34px] justify-center p-0 text-sm border-ln bg-sf text-tx hover:bg-sk ' + BTN_LOOK
const BTN_GHOST = 'inline-flex size-[30px] justify-center p-0 text-sm border-transparent bg-transparent text-t3 hover:bg-sk hover:text-tx ' + BTN_LOOK
const BTN_DNG = 'inline-flex h-9 px-3 text-sm border-rd bg-sf text-rd hover:bg-sk ' + BTN_LOOK
const CHOICE = 'inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-[12.5px] '
const choice = (on: boolean) => CHOICE + (on ? 'border-transparent bg-acs font-medium text-act' : 'border-ln bg-sf text-t2 hover:bg-sk')
const CONTENT = 'flex flex-wrap items-start gap-6 px-8 pt-6 pb-8 max-[640px]:px-4'
const COL_PANEL = 'flex min-w-0 flex-[999_1_520px] flex-col gap-[18px] rounded-xl border border-ln bg-sf'
const ERR = 'text-[13px] text-rd'
const PB = 'flex flex-col gap-[18px] px-5 pt-4 pb-5'
const LB = 'flex min-w-0 flex-col gap-1.5 text-[12.5px] font-medium text-t2'
const HINT = 'text-[12px] font-normal text-t3'
const IN = 'w-full rounded-md border border-ln bg-sf px-3 text-[14px] text-tx focus:border-ac focus:outline-0'
// Small inputs inside .tr rows: on phones they stretch (the old `.tr .in.sm` rule used !important to beat inline styles).
const IN_TR = IN + ' h-8 max-[640px]:max-w-none! max-[640px]:flex-[1_1_120px]!'
const TR = 'flex min-h-[46px] flex-wrap items-center gap-3 border-t border-ln px-5 py-1 max-[640px]:gap-2 max-[640px]:px-3 max-[640px]:py-1.5'
const TLABEL = 'text-[12.5px] text-t3 max-[640px]:hidden'
const ADDLINE = 'flex min-h-10 w-full items-center gap-2 border-t border-ln bg-transparent px-4 py-1 text-left text-[13px] text-t3 hover:bg-sf2 hover:text-tx'
const PKBTN = 'inline-flex cursor-pointer rounded-[7px] bg-transparent p-0 hover:opacity-[.85]'
const SELBTN = 'inline-flex h-[30px] items-center justify-between gap-2 rounded-[7px] border border-ln bg-sf px-2.5 text-[13px] text-t2'
const SWATCH = 'grid size-[26px] place-items-center rounded-full border-0 p-0 text-white'
const RING = '[box-shadow:0_0_0_2px_var(--sf),0_0_0_4px_var(--tx)]'
const CHIPS = 'flex flex-wrap gap-1.5'

/** Runs a change, refreshes every query afterwards and keeps the last error for display. */
function useAction() {
  const qc = useQueryClient()
  const [err, setErr] = useState('')
  const run = async (fn: () => Promise<unknown>): Promise<boolean> => {
    try {
      await fn()
      setErr('')
      return true
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      qc.invalidateQueries()
    }
  }
  return { run, err, setErr }
}

function Panel({ id, title, hint, children, flush }: { id: string; title: string; hint: string; children: ReactNode; flush?: boolean }) {
  return (
    <section className="overflow-hidden rounded-xl border border-ln bg-sf" id={'s-' + id}>
      <div className={'flex flex-col gap-[3px] px-5 pt-4 ' + (flush ? 'pb-3' : 'pb-1')}><h2 className="text-[16px] font-semibold tracking-[-.01em]">{title}</h2><p className="text-[13px] leading-[1.5] text-t3">{hint}</p></div>
      {children}
    </section>
  )
}

export function Settings({ slug }: { slug: string }) {
  const projects = useProjects()
  const project = projects.data?.find((p) => p.slug === slug)
  if (projects.isError) return <div className={CONTENT}><div className={COL_PANEL}><ErrorState message={projects.error.message} onRetry={() => projects.refetch()} /></div></div>
  if (!project) {
    return projects.isPending
      ? <div className={CONTENT}><Skeleton w="40%" h={28} /></div>
      : <div className={CONTENT}><div className={COL_PANEL}><EmptyState icon="board" title="Проект не найден" hint={`Нет проекта «${slug}».`} /></div></div>
  }
  return <ProjectSettings key={project.id} project={project} />
}

function ProjectSettings({ project }: { project: Project }) {
  const { run, err } = useAction()
  const [active, setActive] = useState('general')
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const io = new IntersectionObserver((es) => {
      const top = es.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0]
      if (top) setActive(top.target.id.slice(2))
    }, { rootMargin: '-10% 0px -70% 0px' })
    root.current?.querySelectorAll('section[id^="s-"]').forEach((el) => io.observe(el))
    return () => io.disconnect()
  }, [])

  return (
    <>
      <header className="flex flex-wrap items-center gap-3.5 px-8 pt-[22px] pb-4">
        <Link to={`/p/${project.slug}`} className={BTN_SQ} aria-label="Назад к доске"><Icon name="chevL" size={16} sw={2} /></Link>
        <div className="flex min-w-0 flex-[1_1_260px] flex-col gap-0.5">
          <span className="text-[13px] text-t3">{project.name}{project.archived_at && ' · в архиве'}</span>
          <h1 className="text-2xl font-bold tracking-tight">Настройки проекта</h1>
        </div>
        <span className="inline-flex items-center gap-1.5 text-[12.5px] text-dnt"><Icon name="check" size={14} sw={2.2} />Изменения сохраняются сразу</span>
      </header>
      <div className="flex flex-wrap items-start gap-7 px-8 pt-6 pb-10 max-[760px]:px-4 max-[760px]:py-4" ref={root}>
        <nav className="sticky top-4 flex flex-[0_0_200px] flex-col gap-0.5 max-[760px]:static max-[760px]:basis-full max-[760px]:flex-row max-[760px]:flex-wrap" aria-label="Разделы настроек">
          {SECTIONS.map(([id, label]) => (
            <button key={id} type="button" className="flex h-8 w-full items-center gap-2.5 rounded-md bg-transparent px-2.5 text-left text-sm text-t2 hover:bg-sk aria-[current=true]:bg-sk aria-[current=true]:font-semibold aria-[current=true]:text-tx max-[760px]:w-auto" aria-current={active === id ? 'true' : undefined}
              onClick={() => { setActive(id); document.getElementById('s-' + id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }}>{label}</button>
          ))}
        </nav>
        <div className="flex min-w-0 max-w-[880px] flex-[999_1_520px] flex-col gap-5">
          {err && <div className={ERR} role="alert">{err}</div>}
          <General project={project} run={run} />
          <StatusesSection project={project} run={run} />
          <LabelsSection project={project} run={run} />
          <FieldsSection project={project} run={run} />
          <ViewsSection project={project} run={run} />
          <DataSection project={project} run={run} />
        </div>
      </div>
    </>
  )
}

type Run = (fn: () => Promise<unknown>) => Promise<boolean>

function General({ project, run }: { project: Project; run: Run }) {
  const [name, setName] = useState(project.name)
  const [key, setKey] = useState(project.key)
  const [desc, setDesc] = useState(project.description)
  useEffect(() => setName(project.name), [project.name])
  useEffect(() => setKey(project.key), [project.key])
  useEffect(() => setDesc(project.description), [project.description])
  const patch = (b: Record<string, unknown>) => run(() => api.updateProject(project.id, b))
  return (
    <Panel id="general" title="Основное" hint="Иконка и цвет видны в боковой панели, на карточках «Сегодня» и в поиске.">
      <div className={PB}>
        <div className="flex flex-wrap items-center gap-4 rounded-[10px] border border-ln bg-sf2 px-4 py-3.5">
          <ProjectTile icon={project.icon} color={project.color} size={52} radius={14} />
          <span className="flex flex-[1_1_200px] flex-col gap-0.5">
            <span className="text-[18px] font-bold tracking-[-.01em]">{project.name}</span>
            <span className="font-mono text-[12px] text-t3">{project.key} · {project.open} открытых</span>
          </span>
          <span className="flex h-8 min-w-[200px] items-center gap-2.5 rounded-md bg-sk px-2 text-sm font-semibold text-tx">
            <ProjectTile icon={project.icon} color={project.color} /><span className="min-w-0 grow truncate">{project.name}</span><span className="font-mono text-[11.5px] text-t3">{project.open}</span>
          </span>
        </div>
        <div className={LB}>Иконка
          <div className="flex flex-wrap gap-2" role="group" aria-label="Иконка проекта">
            {PROJECT_ICONS.map((i) => (
              <button key={i} type="button" className="grid size-[38px] place-items-center rounded-[10px] border border-ln bg-sf p-0 text-t2 aria-pressed:border-ac aria-pressed:bg-ac aria-pressed:text-inkt" aria-label={i} aria-pressed={i === project.icon} onClick={() => patch({ icon: i })}><Icon name={i} size={18} /></button>
            ))}
          </div>
        </div>
        <div className={LB}>Цвет
          <div className="flex flex-wrap gap-3 px-1 py-[3px]" role="group" aria-label="Цвет проекта">
            {PROJECT_COLORS.map((c) => (
              <button key={c} type="button" className={SWATCH + (c.toLowerCase() === project.color.toLowerCase() ? ' ' + RING : '')} style={{ background: c }} aria-label={c} aria-pressed={c.toLowerCase() === project.color.toLowerCase()} onClick={() => patch({ color: c })}>
                {c.toLowerCase() === project.color.toLowerCase() && <Icon name="check" size={13} sw={3} />}
              </button>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4">
          <label className={LB}>Название
            <input className={IN + ' h-9'} value={name} onChange={(e) => setName(e.target.value)}
              onBlur={() => name.trim() && name.trim() !== project.name ? patch({ name: name.trim() }).then((ok) => !ok && setName(project.name)) : setName(project.name)}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
          </label>
          <label className={LB}>Ключ задач
            <input className={IN + ' h-9 font-mono'} maxLength={5} value={key} onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''))}
              onBlur={() => key !== project.key ? patch({ key }).then((ok) => !ok && setKey(project.key)) : undefined}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
            <span className={HINT}>Префикс ID: {project.key}-12. Меняется для всех задач сразу.</span>
            {key !== project.key && <span className="text-[12px] font-normal text-dgt">Старые ссылки с прежним ключом {project.key} перестанут работать.</span>}
          </label>
        </div>
        <label className={LB}>Описание
          <textarea className={IN + ' h-auto resize-y py-[9px] leading-[1.5]'} rows={2} value={desc} onChange={(e) => setDesc(e.target.value)}
            onBlur={() => desc !== project.description && patch({ description: desc })} />
        </label>
      </div>
    </Panel>
  )
}

// ---- statuses ----

function StatusRow({ s, run, onDelete }: { s: Status; run: Run; onDelete: (s: Status) => void }) {
  const sortable = useSortable({ id: s.id })
  const [name, setName] = useState(s.name)
  useEffect(() => setName(s.name), [s.name])
  const patch = (b: Record<string, unknown>) => run(() => api.updateStatus(s.id, b))
  return (
    <div className={TR + (sortable.isDragging ? ' bg-sf2 shadow-pop' : '')} ref={sortable.setNodeRef} style={{ transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }}>
      <button type="button" className="inline-grid h-7 w-5 cursor-grab touch-none place-items-center border-0 bg-transparent p-0 text-t3" aria-label={`Перетащить статус «${s.name}»`} {...sortable.attributes} {...sortable.listeners}><Icon name="list" size={16} /></button>
      <Menu className={PKBTN} ariaLabel={`Цвет статуса «${s.name}»`} label={<span className="inline-block size-3.5 shrink-0 rounded-full" style={{ background: statusColor(s.color) }} />}>
        {(close) => (
          <div className="flex w-[190px] flex-wrap gap-2 p-1.5">
            {STATUS_COLORS.map((c) => (
              <button key={c} type="button" className={'size-[26px] rounded-md p-0' + (c === s.color ? ' ' + RING : '')} style={{ background: statusColor(c) }} aria-label={c} onClick={() => { patch({ color: c }); close() }} />
            ))}
          </div>
        )}
      </Menu>
      <input className={IN_TR + ' max-w-[240px] flex-[1_1_140px]'} aria-label="Название статуса" value={name} onChange={(e) => setName(e.target.value)}
        onBlur={() => name.trim() && name.trim() !== s.name ? patch({ name: name.trim() }) : setName(s.name)}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      <Menu className={SELBTN + ' min-w-[136px]'} ariaLabel="Категория статуса" label={<>{CATEGORIES.find((c) => c[0] === s.category)![1]}<Icon name="chevD" size={13} sw={2} /></>}>
        {(close) => CATEGORIES.map(([k, n]) => (
          <button key={k} type="button" className={MENUITEM} onClick={() => { patch({ category: k }); close() }}>{n}{k === s.category && <span className="ml-auto text-act"><Icon name="check" size={14} sw={2.2} /></span>}</button>
        ))}
      </Menu>
      <span className="grow" />
      <span className="font-mono text-[12px] tabular-nums text-t3" title="Задач в статусе">{s.task_count}</span>
      <span className={TLABEL}>На доске</span>
      <Toggle on={s.show_on_board} label={`На доске: ${s.name}`} onChange={(v) => patch({ show_on_board: v })} />
      <button type="button" className={BTN_GHOST} aria-label={`Удалить статус «${s.name}»`} onClick={() => onDelete(s)}><Icon name="trash" size={16} sw={1.8} /></button>
    </div>
  )
}

function StatusesSection({ project, run }: { project: Project; run: Run }) {
  const q = useQuery({ queryKey: ['statuses', project.id], queryFn: () => api.statuses(project.id) })
  const [list, setList] = useState<Status[]>([])
  useEffect(() => { if (q.data) setList(q.data) }, [q.data])
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [moving, setMoving] = useState<Status | null>(null)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }))

  const onDragEnd = (e: DragEndEvent) => {
    if (!e.over || e.active.id === e.over.id) return
    const next = arrayMove(list, list.findIndex((s) => s.id === e.active.id), list.findIndex((s) => s.id === e.over!.id))
    setList(next)
    run(() => api.reorderStatuses(project.id, next.map((s) => s.id)))
  }
  const del = async (s: Status) => {
    try {
      await api.deleteStatus(s.id)
      run(async () => undefined)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'status_in_use') setMoving(s)
      else run(async () => { throw e })
    }
  }
  const add = async () => {
    if (!name.trim()) return
    if (await run(() => api.createStatus(project.id, { name: name.trim() }))) { setName(''); setAdding(false) }
  }
  return (
    <Panel id="statuses" title="Статусы" hint="Колонки доски. Категория определяет поведение: «Готово» закрывает задачу и ставит дату закрытия." flush>
      {q.isPending && <div className={TR}><Skeleton w="60%" /></div>}
      {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={list.map((s) => s.id)} strategy={verticalListSortingStrategy}>
          {list.map((s) => <StatusRow key={s.id} s={s} run={run} onDelete={del} />)}
        </SortableContext>
      </DndContext>
      {adding ? (
        <div className={TR}>
          <input className={IN_TR + ' max-w-[280px]'} autoFocus placeholder="Название нового статуса" aria-label="Название нового статуса" value={name} onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { setAdding(false); setName('') } }} />
          <button type="button" className={BTN_SM_PRI} disabled={!name.trim()} onClick={add}>Добавить</button>
          <button type="button" className={BTN_SM} onClick={() => { setAdding(false); setName('') }}>Отмена</button>
        </div>
      ) : (
        <button type="button" className={ADDLINE} onClick={() => setAdding(true)}><Icon name="plus" size={14} sw={2} />Добавить статус</button>
      )}
      {moving && <MoveDialog status={moving} others={list.filter((s) => s.id !== moving.id)} onClose={() => setMoving(null)}
        onMove={async (to) => { if (await run(() => api.deleteStatus(moving.id, to))) setMoving(null) }} />}
    </Panel>
  )
}

function MoveDialog({ status, others, onClose, onMove }: { status: Status; others: Status[]; onClose: () => void; onMove: (to: number) => void }) {
  const [to, setTo] = useState(others[0]?.id)
  return (
    <Dialog title={`Удалить статус «${status.name}»`} onClose={onClose}>
      <p className="text-t2">В статусе есть задачи{status.task_count > 0 ? ` (${status.task_count})` : ' (в корзине)'}. Выберите, куда их перенести.</p>
      <div className={CHIPS}>
        {others.map((s) => (
          <button key={s.id} type="button" className={choice(s.id === to)} onClick={() => setTo(s.id)}>
            <span className="inline-block size-2 shrink-0 rounded-full" style={{ background: statusColor(s.color) }} />{s.name}
          </button>
        ))}
      </div>
      <div className="flex items-center justify-end gap-2">
        <button type="button" className={BTN} onClick={onClose}>Отмена</button>
        <button type="button" className={BTN_PRI} disabled={!to} onClick={() => to && onMove(to)}>Перенести и удалить</button>
      </div>
    </Dialog>
  )
}

// ---- labels ----

function LabelChip({ l, run }: { l: Label; run: Run }) {
  const [name, setName] = useState(l.name)
  useEffect(() => setName(l.name), [l.name])
  return (
    <Menu className="inline-flex h-[30px] items-center gap-2 rounded-md border border-ln bg-sf2 px-[11px] text-[13px]" ariaLabel={`Метка «${l.name}»`} label={<><span className="inline-block size-[9px] shrink-0 rounded-full" style={{ background: l.color }} />{l.name}<span className="font-mono text-[11.5px] text-t3">{l.task_count}</span></>}>
      {(close) => (
        <div className="flex w-[220px] flex-col gap-2 p-1.5">
          <input className={IN + ' h-8'} aria-label="Название метки" value={name} onChange={(e) => setName(e.target.value)}
            onBlur={() => name.trim() && name.trim() !== l.name && run(() => api.updateLabel(l.id, { name: name.trim() }))}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
          <div className="flex flex-wrap gap-2">
            {LABEL_COLORS.map((c) => (
              <button key={c} type="button" className={'size-6 rounded-md p-0' + (c.toLowerCase() === l.color.toLowerCase() ? ' ' + RING : '')} style={{ background: c }} aria-label={c}
                onClick={() => run(() => api.updateLabel(l.id, { color: c }))} />
            ))}
          </div>
          <button type="button" className={MENUITEM + ' text-rd'} onClick={() => { run(() => api.deleteLabel(l.id)); close() }}>
            <Icon name="trash" size={14} />Удалить{l.task_count ? ` (область очистится у ${l.task_count})` : ''}
          </button>
        </div>
      )}
    </Menu>
  )
}

function LabelsSection({ project, run }: { project: Project; run: Run }) {
  const q = useQuery({ queryKey: ['labels', project.id], queryFn: () => api.labels(project.id) })
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const add = async () => {
    if (!name.trim()) return
    if (await run(() => api.createLabel(project.id, { name: name.trim() }))) { setName(''); setAdding(false) }
  }
  return (
    <Panel id="labels" title="Метки областей" hint="Быстрая группировка и фильтр. Цвет метки виден точкой на карточке. Новая область у задачи создаёт метку сама.">
      <div className={PB}>
        <div className="flex flex-wrap gap-2">
          {q.data?.map((l) => <LabelChip key={l.id} l={l} run={run} />)}
          {adding ? (
            <input className={IN + ' h-8 w-[180px]'} autoFocus placeholder="Название метки" aria-label="Название новой метки" value={name} onChange={(e) => setName(e.target.value)}
              onBlur={() => !name.trim() && setAdding(false)} onKeyDown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { setAdding(false); setName('') } }} />
          ) : (
            <button type="button" className="inline-flex h-[30px] items-center gap-2 rounded-md border border-dashed border-ln bg-transparent px-[11px] text-[13px] text-t3" onClick={() => setAdding(true)}><Icon name="plus" size={13} sw={2} />Новая метка</button>
          )}
        </div>
        {q.isSuccess && q.data.length === 0 && !adding && <span className={HINT}>Меток пока нет. Они появятся, когда у задач будут области.</span>}
        {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
      </div>
    </Panel>
  )
}

// ---- custom fields ----

function FieldRow({ f, run }: { f: Field; run: Run }) {
  const [name, setName] = useState(f.name)
  const [opts, setOpts] = useState(f.options.join(', '))
  useEffect(() => setName(f.name), [f.name])
  useEffect(() => setOpts(f.options.join(', ')), [f.options])
  const t = FIELD_TYPES.find((x) => x[0] === f.type)!
  const patch = (b: Record<string, unknown>) => run(() => api.updateField(f.id, b))
  return (
    <div className={TR}>
      <span className="inline-flex text-t3"><Icon name={t[2]} size={16} /></span>
      <input className={IN_TR + ' max-w-[220px] flex-[1_1_140px]'} aria-label="Название поля" value={name} onChange={(e) => setName(e.target.value)}
        onBlur={() => name.trim() && name.trim() !== f.name ? patch({ name: name.trim() }) : setName(f.name)} onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      <span className={SELBTN + ' min-w-[100px]'} title="Тип нельзя изменить после создания">{t[1]}</span>
      {f.type === 'select' && (
        <input className={IN_TR + ' max-w-[320px] flex-[1_1_200px]'} aria-label="Варианты через запятую" placeholder="Варианты через запятую" value={opts} onChange={(e) => setOpts(e.target.value)}
          onBlur={() => opts !== f.options.join(', ') && patch({ options: opts.split(',').map((x) => x.trim()).filter(Boolean) }).then((ok) => !ok && setOpts(f.options.join(', ')))}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      )}
      <span className="grow" />
      <span className={TLABEL}>На карточке</span>
      <Toggle on={f.show_on_card} label={`На карточке: ${f.name}`} onChange={(v) => patch({ show_on_card: v })} />
      <ConfirmButton className={BTN_GHOST} label="" ariaLabel={`Удалить поле «${f.name}»`} armed="Удалить поле со значениями?" icon={<Icon name="trash" size={16} sw={1.8} />} onConfirm={() => run(() => api.deleteField(f.id))} />
    </div>
  )
}

function FieldsSection({ project, run }: { project: Project; run: Run }) {
  const q = useQuery({ queryKey: ['fields', project.id], queryFn: () => api.fields(project.id) })
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [type, setType] = useState<Field['type']>('number')
  const [opts, setOpts] = useState('')
  const reset = () => { setAdding(false); setName(''); setOpts(''); setType('number') }
  const add = async () => {
    if (!name.trim()) return
    const options = opts.split(',').map((x) => x.trim()).filter(Boolean)
    if (await run(() => api.createField(project.id, { name: name.trim(), type, options }))) reset()
  }
  return (
    <Panel id="fields" title="Свои поля" hint="Дополнительные свойства задачи. Появляются в панели задачи и, по желанию, на карточке." flush>
      {q.data?.map((f) => <FieldRow key={f.id} f={f} run={run} />)}
      {q.isSuccess && q.data.length === 0 && !adding && <div className={TR + ' text-[13px] text-t3'}>Полей пока нет.</div>}
      {adding ? (
        <div className={TR}>
          <input className={IN_TR + ' max-w-[200px]'} autoFocus placeholder="Название поля" aria-label="Название нового поля" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add()} />
          <div className={CHIPS} role="group" aria-label="Тип поля">
            {FIELD_TYPES.map(([k, n]) => <button key={k} type="button" className={choice(k === type)} aria-pressed={k === type} onClick={() => setType(k)}>{n}</button>)}
          </div>
          {type === 'select' && <input className={IN_TR + ' max-w-[260px]'} placeholder="Варианты через запятую" aria-label="Варианты через запятую" value={opts} onChange={(e) => setOpts(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add()} />}
          <button type="button" className={BTN_SM_PRI} disabled={!name.trim() || (type === 'select' && !opts.trim())} onClick={add}>Добавить</button>
          <button type="button" className={BTN_SM} onClick={reset}>Отмена</button>
        </div>
      ) : (
        <button type="button" className={ADDLINE} onClick={() => setAdding(true)}><Icon name="plus" size={14} sw={2} />Добавить поле</button>
      )}
      {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
    </Panel>
  )
}

// ---- saved views ----

const DATE_NAMES: Record<string, string> = { today: 'сегодня', overdue: 'просрочено', none: 'без даты' }
const GROUP_NAMES: Record<string, string> = { status: 'статус', priority: 'приоритет', area: 'область' }
const SORT_NAMES: Record<string, string> = { manual: 'вручную', priority: 'приоритет', date: 'дата' }

function describeView(v: View): string {
  const f = v.filter ?? {}
  const parts: string[] = [v.layout === 'list' ? 'Список' : 'Доска']
  if (f.q) parts.push(`«${f.q}»`)
  if (f.priority?.length) parts.push(`приоритет ${f.priority.join('/')}`)
  if (f.area?.length) parts.push(`область ${f.area.join('/')}`)
  if (f.date?.length) parts.push(f.date.map((d) => DATE_NAMES[d] ?? d).join('/'))
  if (f.author?.length) parts.push(`создали: ${f.author.join('/')}`)
  parts.push(`группа: ${GROUP_NAMES[v.group_by] ?? v.group_by}`, `сортировка: ${SORT_NAMES[v.sort] ?? v.sort}`)
  return parts.join(' · ')
}

function ViewRow({ v, run }: { v: View; run: Run }) {
  const [name, setName] = useState(v.name)
  useEffect(() => setName(v.name), [v.name])
  return (
    <div className={TR}>
      <span className="inline-flex text-t3"><Icon name="funnel" size={16} /></span>
      <span className="flex min-w-0 flex-[1_1_200px] flex-col gap-0.5">
        <input className={IN_TR + ' max-w-[280px]'} aria-label="Название вида" value={name} onChange={(e) => setName(e.target.value)}
          onBlur={() => name.trim() && name.trim() !== v.name ? run(() => api.updateView(v.id, { name: name.trim() })) : setName(v.name)} onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
        <span className="text-[12.5px] text-t3">{describeView(v)}</span>
      </span>
      <span className={TLABEL}>В боковой панели</span>
      <Toggle on={v.pinned} label={`В боковой панели: ${v.name}`} onChange={(pinned) => run(() => api.updateView(v.id, { pinned }))} />
      <ConfirmButton className={BTN_GHOST} label="" ariaLabel={`Удалить вид «${v.name}»`} armed="Удалить вид?" icon={<Icon name="trash" size={16} sw={1.8} />} onConfirm={() => run(() => api.deleteView(v.id))} />
    </div>
  )
}

function ViewsSection({ project, run }: { project: Project; run: Run }) {
  const q = useQuery({ queryKey: ['views', project.id], queryFn: () => api.views(project.id) })
  const mine = (q.data ?? []).filter((v) => v.project_id === project.id)
  return (
    <Panel id="views" title="Сохранённые виды" hint="Набор фильтров, группировки и сортировки. Закреплённые виды попадают в боковую панель." flush>
      {mine.map((v) => <ViewRow key={v.id} v={v} run={run} />)}
      {q.isSuccess && mine.length === 0 && <div className={TR + ' text-[13px] text-t3'}>Видов пока нет. Настройте фильтры на доске и нажмите «Сохранить как вид».</div>}
      {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
    </Panel>
  )
}

// ---- data ----

function DataSection({ project, run }: { project: Project; run: Run }) {
  const archived = !!project.archived_at
  return (
    <Panel id="data" title="Данные и архив" hint="Удалённый проект уходит в корзину вместе с задачами. Вернуть его можно оттуда." flush>
      <div className="flex flex-wrap gap-2.5 px-5 pt-1.5 pb-5">
        <a className={BTN} href={`/api/projects/${project.id}/export`} download><Icon name="doc" size={15} />Экспорт проекта в JSON</a>
        <button type="button" className={BTN} onClick={() => run(() => api.archiveProject(project.id, !archived)).then((ok) => ok && !archived && navigate('/'))}>
          <Icon name="stack" size={15} />{archived ? 'Вернуть из архива' : 'Архивировать'}
        </button>
        <span className="grow" />
        <ConfirmButton className={BTN_DNG} label="Удалить в корзину" armed={`Удалить «${project.name}» вместе с задачами?`} icon={<Icon name="trash" size={15} />}
          onConfirm={() => run(() => api.deleteProject(project.id)).then((ok) => ok && navigate('/'))} />
      </div>
    </Panel>
  )
}

