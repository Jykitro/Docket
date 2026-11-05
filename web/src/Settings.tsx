// Project settings: every control saves at once (no "Save" button).
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { ApiError, type Field, type Label, type Project, type Status, type View, api, statusColor, useProjects } from './api'
import { Icon, PROJECT_COLORS, PROJECT_ICONS, ProjectTile } from './icons'
import { Link, navigate } from './router'
import { ConfirmButton, Dialog, EmptyState, ErrorState, Menu, Skeleton, Toggle } from './ui'

const CATEGORIES: [Status['category'], string][] = [['todo', 'К работе'], ['doing', 'В работе'], ['done', 'Готово']]
const STATUS_COLORS = ['bk', 'td', 'dg', 'dn', '#8B6CFF', '#E56B9B', '#2EB5A3', '#C0265F']
const LABEL_COLORS = ['#8B6CFF', '#4C8DF6', '#E5A23A', '#E56B9B', '#8A93A3', '#2EB5A3', '#C0265F', '#17784C']
const FIELD_TYPES: [Field['type'], string, string][] = [['number', 'Число', 'bars'], ['url', 'Ссылка', 'link'], ['select', 'Выбор', 'tag']]
const SECTIONS: [string, string][] = [['general', 'Основное'], ['statuses', 'Статусы'], ['labels', 'Метки'], ['fields', 'Поля'], ['views', 'Виды'], ['data', 'Данные']]

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
    <section className="panel clip" id={'s-' + id}>
      <div className="ph" style={flush ? { paddingBottom: 12 } : undefined}><h2>{title}</h2><p>{hint}</p></div>
      {children}
    </section>
  )
}

export function Settings({ slug }: { slug: string }) {
  const projects = useProjects()
  const project = projects.data?.find((p) => p.slug === slug)
  if (projects.isError) return <div className="content"><div className="col-main panel"><ErrorState message={projects.error.message} onRetry={() => projects.refetch()} /></div></div>
  if (!project) {
    return projects.isPending
      ? <div className="content"><Skeleton w="40%" h={28} /></div>
      : <div className="content"><div className="col-main panel"><EmptyState icon="board" title="Проект не найден" hint={`Нет проекта «${slug}».`} /></div></div>
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
      <header className="pagehead" style={{ padding: '22px 32px 18px' }}>
        <Link to={`/p/${project.slug}`} className="btn sq" aria-label="Назад к доске"><Icon name="chevL" size={16} sw={2} /></Link>
        <div className="stack">
          <span className="sub" style={{ fontSize: 13 }}>{project.name}{project.archived_at && ' · в архиве'}</span>
          <h1 style={{ fontSize: 24 }}>Настройки проекта</h1>
        </div>
        <span style={{ fontSize: 12.5, color: 'var(--dnt)', display: 'inline-flex', alignItems: 'center', gap: 6 }}><Icon name="check" size={14} sw={2.2} />Изменения сохраняются сразу</span>
      </header>
      <div className="settings-grid" ref={root}>
        <nav className="settings-nav" aria-label="Разделы настроек">
          {SECTIONS.map(([id, label]) => (
            <button key={id} type="button" className={'sn' + (active === id ? ' on' : '')} aria-current={active === id ? 'true' : undefined}
              onClick={() => { setActive(id); document.getElementById('s-' + id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }}>{label}</button>
          ))}
        </nav>
        <div className="settings-col">
          {err && <div className="errline" role="alert">{err}</div>}
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
      <div className="pb">
        <div className="preview">
          <ProjectTile icon={project.icon} color={project.color} size={52} radius={14} />
          <span style={{ display: 'flex', flexDirection: 'column', gap: 2, flex: '1 1 200px' }}>
            <span style={{ fontSize: 18, fontWeight: 700, letterSpacing: '-.01em' }}>{project.name}</span>
            <span className="mono" style={{ fontSize: 12, color: 'var(--t3)' }}>{project.key} · {project.open} открытых</span>
          </span>
          <span className="nv on" style={{ width: 'auto', minWidth: 200, height: 32 }}>
            <ProjectTile icon={project.icon} color={project.color} /><span className="name">{project.name}</span><span className="n mono">{project.open}</span>
          </span>
        </div>
        <div className="lb">Иконка
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }} role="group" aria-label="Иконка проекта">
            {PROJECT_ICONS.map((i) => (
              <button key={i} type="button" className={'icopick' + (i === project.icon ? ' on' : '')} aria-label={i} aria-pressed={i === project.icon} onClick={() => patch({ icon: i })}><Icon name={i} size={18} /></button>
            ))}
          </div>
        </div>
        <div className="lb">Цвет
          <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', padding: '3px 4px' }} role="group" aria-label="Цвет проекта">
            {PROJECT_COLORS.map((c) => (
              <button key={c} type="button" className={'swatch' + (c.toLowerCase() === project.color.toLowerCase() ? ' on' : '')} style={{ background: c }} aria-label={c} aria-pressed={c.toLowerCase() === project.color.toLowerCase()} onClick={() => patch({ color: c })}>
                {c.toLowerCase() === project.color.toLowerCase() && <Icon name="check" size={13} sw={3} />}
              </button>
            ))}
          </div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 16 }}>
          <label className="lb">Название
            <input className="in" value={name} onChange={(e) => setName(e.target.value)}
              onBlur={() => name.trim() && name.trim() !== project.name ? patch({ name: name.trim() }).then((ok) => !ok && setName(project.name)) : setName(project.name)}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
          </label>
          <label className="lb">Ключ задач
            <input className="in mono" maxLength={5} value={key} onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ''))}
              onBlur={() => key !== project.key ? patch({ key }).then((ok) => !ok && setKey(project.key)) : undefined}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
            <span className="hint">Префикс ID: {project.key}-12. Меняется для всех задач сразу.</span>
            {key !== project.key && <span className="hint warn">Старые ссылки с прежним ключом {project.key} перестанут работать.</span>}
          </label>
        </div>
        <label className="lb">Описание
          <textarea className="in" rows={2} style={{ height: 'auto', padding: '9px 12px', resize: 'vertical', lineHeight: 1.5 }} value={desc} onChange={(e) => setDesc(e.target.value)}
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
    <div className={'tr' + (sortable.isDragging ? ' drag' : '')} ref={sortable.setNodeRef} style={{ transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }}>
      <button type="button" className="grip" aria-label={`Перетащить статус «${s.name}»`} {...sortable.attributes} {...sortable.listeners}><Icon name="list" size={16} /></button>
      <Menu className="pkbtn" ariaLabel={`Цвет статуса «${s.name}»`} label={<span className="dot" style={{ width: 14, height: 14, flexBasis: 14, background: statusColor(s.color) }} />}>
        {(close) => (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', padding: 6, width: 190 }}>
            {STATUS_COLORS.map((c) => (
              <button key={c} type="button" className={'swatch' + (c === s.color ? ' on' : '')} style={{ background: statusColor(c), width: 26, height: 26, padding: 0 }} aria-label={c} onClick={() => { patch({ color: c }); close() }} />
            ))}
          </div>
        )}
      </Menu>
      <input className="in sm" style={{ maxWidth: 240, flex: '1 1 140px' }} aria-label="Название статуса" value={name} onChange={(e) => setName(e.target.value)}
        onBlur={() => name.trim() && name.trim() !== s.name ? patch({ name: name.trim() }) : setName(s.name)}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      <Menu className="selbtn" ariaLabel="Категория статуса" label={<>{CATEGORIES.find((c) => c[0] === s.category)![1]}<Icon name="chevD" size={13} sw={2} /></>}>
        {(close) => CATEGORIES.map(([k, n]) => (
          <button key={k} type="button" onClick={() => { patch({ category: k }); close() }}>{n}{k === s.category && <span className="chk"><Icon name="check" size={14} sw={2.2} /></span>}</button>
        ))}
      </Menu>
      <span className="grow" />
      <span className="mono tabnum" style={{ fontSize: 12, color: 'var(--t3)' }} title="Задач в статусе">{s.task_count}</span>
      <span className="tlabel" style={{ fontSize: 12.5, color: 'var(--t3)' }}>На доске</span>
      <Toggle on={s.show_on_board} label={`На доске: ${s.name}`} onChange={(v) => patch({ show_on_board: v })} />
      <button type="button" className="btn ghost" aria-label={`Удалить статус «${s.name}»`} onClick={() => onDelete(s)}><Icon name="trash" size={16} sw={1.8} /></button>
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
      {q.isPending && <div className="tr"><Skeleton w="60%" /></div>}
      {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={list.map((s) => s.id)} strategy={verticalListSortingStrategy}>
          {list.map((s) => <StatusRow key={s.id} s={s} run={run} onDelete={del} />)}
        </SortableContext>
      </DndContext>
      {adding ? (
        <div className="tr">
          <input className="in sm" style={{ maxWidth: 280 }} autoFocus placeholder="Название нового статуса" aria-label="Название нового статуса" value={name} onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { setAdding(false); setName('') } }} />
          <button type="button" className="btn pri sm" disabled={!name.trim()} onClick={add}>Добавить</button>
          <button type="button" className="btn sm" onClick={() => { setAdding(false); setName('') }}>Отмена</button>
        </div>
      ) : (
        <button type="button" className="addline" onClick={() => setAdding(true)}><Icon name="plus" size={14} sw={2} />Добавить статус</button>
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
      <p style={{ margin: 0, color: 'var(--t2)' }}>В статусе есть задачи{status.task_count > 0 ? ` (${status.task_count})` : ' (в корзине)'}. Выберите, куда их перенести.</p>
      <div className="chips">
        {others.map((s) => (
          <button key={s.id} type="button" className={'choice' + (s.id === to ? ' on' : '')} onClick={() => setTo(s.id)}>
            <span className="dot" style={{ width: 8, height: 8, flexBasis: 8, background: statusColor(s.color) }} />{s.name}
          </button>
        ))}
      </div>
      <div className="foot">
        <button type="button" className="btn" onClick={onClose}>Отмена</button>
        <button type="button" className="btn pri" disabled={!to} onClick={() => to && onMove(to)}>Перенести и удалить</button>
      </div>
    </Dialog>
  )
}

// ---- labels ----

function LabelChip({ l, run }: { l: Label; run: Run }) {
  const [name, setName] = useState(l.name)
  useEffect(() => setName(l.name), [l.name])
  return (
    <Menu className="tagc" ariaLabel={`Метка «${l.name}»`} label={<><span className="dot" style={{ width: 9, height: 9, flexBasis: 9, background: l.color }} />{l.name}<span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)' }}>{l.task_count}</span></>}>
      {(close) => (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 6, width: 220 }}>
          <input className="in sm" aria-label="Название метки" value={name} onChange={(e) => setName(e.target.value)}
            onBlur={() => name.trim() && name.trim() !== l.name && run(() => api.updateLabel(l.id, { name: name.trim() }))}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {LABEL_COLORS.map((c) => (
              <button key={c} type="button" className={'swatch' + (c.toLowerCase() === l.color.toLowerCase() ? ' on' : '')} style={{ background: c, width: 24, height: 24, padding: 0 }} aria-label={c}
                onClick={() => run(() => api.updateLabel(l.id, { color: c }))} />
            ))}
          </div>
          <button type="button" style={{ color: 'var(--rd)' }} onClick={() => { run(() => api.deleteLabel(l.id)); close() }}>
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
      <div className="pb">
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {q.data?.map((l) => <LabelChip key={l.id} l={l} run={run} />)}
          {adding ? (
            <input className="in sm" style={{ width: 180 }} autoFocus placeholder="Название метки" aria-label="Название новой метки" value={name} onChange={(e) => setName(e.target.value)}
              onBlur={() => !name.trim() && setAdding(false)} onKeyDown={(e) => { if (e.key === 'Enter') add(); if (e.key === 'Escape') { setAdding(false); setName('') } }} />
          ) : (
            <button type="button" className="tagc add" onClick={() => setAdding(true)}><Icon name="plus" size={13} sw={2} />Новая метка</button>
          )}
        </div>
        {q.isSuccess && q.data.length === 0 && !adding && <span className="hint">Меток пока нет. Они появятся, когда у задач будут области.</span>}
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
    <div className="tr">
      <span style={{ color: 'var(--t3)', display: 'inline-flex' }}><Icon name={t[2]} size={16} /></span>
      <input className="in sm" style={{ maxWidth: 220, flex: '1 1 140px' }} aria-label="Название поля" value={name} onChange={(e) => setName(e.target.value)}
        onBlur={() => name.trim() && name.trim() !== f.name ? patch({ name: name.trim() }) : setName(f.name)} onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      <span className="selbtn" style={{ minWidth: 100 }} title="Тип нельзя изменить после создания">{t[1]}</span>
      {f.type === 'select' && (
        <input className="in sm" style={{ flex: '1 1 200px', maxWidth: 320 }} aria-label="Варианты через запятую" placeholder="Варианты через запятую" value={opts} onChange={(e) => setOpts(e.target.value)}
          onBlur={() => opts !== f.options.join(', ') && patch({ options: opts.split(',').map((x) => x.trim()).filter(Boolean) }).then((ok) => !ok && setOpts(f.options.join(', ')))}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
      )}
      <span className="grow" />
      <span className="tlabel" style={{ fontSize: 12.5, color: 'var(--t3)' }}>На карточке</span>
      <Toggle on={f.show_on_card} label={`На карточке: ${f.name}`} onChange={(v) => patch({ show_on_card: v })} />
      <ConfirmButton className="btn ghost" label="" ariaLabel={`Удалить поле «${f.name}»`} armed="Удалить поле со значениями?" icon={<Icon name="trash" size={16} sw={1.8} />} onConfirm={() => run(() => api.deleteField(f.id))} />
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
      {q.isSuccess && q.data.length === 0 && !adding && <div className="tr" style={{ color: 'var(--t3)', fontSize: 13 }}>Полей пока нет.</div>}
      {adding ? (
        <div className="tr">
          <input className="in sm" style={{ maxWidth: 200 }} autoFocus placeholder="Название поля" aria-label="Название нового поля" value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add()} />
          <div className="chips" role="group" aria-label="Тип поля">
            {FIELD_TYPES.map(([k, n]) => <button key={k} type="button" className={'choice' + (k === type ? ' on' : '')} aria-pressed={k === type} onClick={() => setType(k)}>{n}</button>)}
          </div>
          {type === 'select' && <input className="in sm" style={{ maxWidth: 260 }} placeholder="Варианты через запятую" aria-label="Варианты через запятую" value={opts} onChange={(e) => setOpts(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add()} />}
          <button type="button" className="btn pri sm" disabled={!name.trim() || (type === 'select' && !opts.trim())} onClick={add}>Добавить</button>
          <button type="button" className="btn sm" onClick={reset}>Отмена</button>
        </div>
      ) : (
        <button type="button" className="addline" onClick={() => setAdding(true)}><Icon name="plus" size={14} sw={2} />Добавить поле</button>
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
    <div className="tr">
      <span style={{ color: 'var(--t3)', display: 'inline-flex' }}><Icon name="funnel" size={16} /></span>
      <span style={{ display: 'flex', flexDirection: 'column', flex: '1 1 200px', minWidth: 0, gap: 2 }}>
        <input className="in sm" style={{ maxWidth: 280 }} aria-label="Название вида" value={name} onChange={(e) => setName(e.target.value)}
          onBlur={() => name.trim() && name.trim() !== v.name ? run(() => api.updateView(v.id, { name: name.trim() })) : setName(v.name)} onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
        <span style={{ fontSize: 12.5, color: 'var(--t3)' }}>{describeView(v)}</span>
      </span>
      <span className="tlabel" style={{ fontSize: 12.5, color: 'var(--t3)' }}>В боковой панели</span>
      <Toggle on={v.pinned} label={`В боковой панели: ${v.name}`} onChange={(pinned) => run(() => api.updateView(v.id, { pinned }))} />
      <ConfirmButton className="btn ghost" label="" ariaLabel={`Удалить вид «${v.name}»`} armed="Удалить вид?" icon={<Icon name="trash" size={16} sw={1.8} />} onConfirm={() => run(() => api.deleteView(v.id))} />
    </div>
  )
}

function ViewsSection({ project, run }: { project: Project; run: Run }) {
  const q = useQuery({ queryKey: ['views', project.id], queryFn: () => api.views(project.id) })
  const mine = (q.data ?? []).filter((v) => v.project_id === project.id)
  return (
    <Panel id="views" title="Сохранённые виды" hint="Набор фильтров, группировки и сортировки. Закреплённые виды попадают в боковую панель." flush>
      {mine.map((v) => <ViewRow key={v.id} v={v} run={run} />)}
      {q.isSuccess && mine.length === 0 && <div className="tr" style={{ color: 'var(--t3)', fontSize: 13 }}>Видов пока нет. Настройте фильтры на доске и нажмите «Сохранить как вид».</div>}
      {q.isError && <ErrorState message={q.error.message} onRetry={() => q.refetch()} />}
    </Panel>
  )
}

// ---- data ----

function DataSection({ project, run }: { project: Project; run: Run }) {
  const archived = !!project.archived_at
  return (
    <Panel id="data" title="Данные и архив" hint="Удалённый проект уходит в корзину вместе с задачами. Вернуть его можно оттуда." flush>
      <div className="pb" style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 10, paddingTop: 6 }}>
        <a className="btn" href={`/api/projects/${project.id}/export`} download><Icon name="doc" size={15} />Экспорт проекта в JSON</a>
        <button type="button" className="btn" onClick={() => run(() => api.archiveProject(project.id, !archived)).then((ok) => ok && !archived && navigate('/'))}>
          <Icon name="stack" size={15} />{archived ? 'Вернуть из архива' : 'Архивировать'}
        </button>
        <span className="grow" />
        <ConfirmButton className="btn dng" label="Удалить в корзину" armed={`Удалить «${project.name}» вместе с задачами?`} icon={<Icon name="trash" size={15} />}
          onConfirm={() => run(() => api.deleteProject(project.id)).then((ok) => ok && navigate('/'))} />
      </div>
    </Panel>
  )
}

