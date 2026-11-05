// Cmd/Ctrl+K search: tasks by title, tasks by description (with a snippet), projects and actions.
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { type Hit, type Project, api, statusChip } from './api'
import { Icon, ProjectTile } from './icons'
import { Link, hrefWith, navigate, setParam } from './router'
import { useFocusTrap } from './ui'

type Item =
  | { kind: 'task' | 'desc'; hit: Hit }
  | { kind: 'project'; project: Project }
  | { kind: 'create' }
  | { kind: 'mcp' }

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function Highlight({ text, q }: { text: string; q: string }) {
  const words = q.trim().split(/\s+/).filter(Boolean).map(escape)
  if (!words.length) return <>{text}</>
  return <>{text.split(new RegExp(`(${words.join('|')})`, 'giu')).map((p, i) => (i % 2 ? <mark key={i}>{p}</mark> : p))}</>
}

/** The server marks matches with [brackets]; turn them into <mark>. */
function Snippet({ s }: { s: string }) {
  return <>{s.replace(/[#*`>]/g, '').split(/(\[[^\]]*\])/).map((p, i) => (p.startsWith('[') && p.endsWith(']') ? <mark key={i}>{p.slice(1, -1)}</mark> : p))}</>
}

export function SearchDialog({ projects, current, onClose, onCreate }: {
  projects: Project[]; current?: Project; onClose: () => void; onCreate: (title: string) => void
}) {
  const trap = useFocusTrap<HTMLDivElement>()
  const [q, setQ] = useState('')
  const [dq, setDq] = useState('')
  const [scope, setScope] = useState<'all' | 'tasks' | 'projects'>('all')
  const [onlyCurrent, setOnlyCurrent] = useState(false)
  const [sel, setSel] = useState(0)
  useEffect(() => {
    const t = setTimeout(() => setDq(q.trim()), 150)
    return () => clearTimeout(t)
  }, [q])
  const res = useQuery({ queryKey: ['search', dq], queryFn: () => api.search(dq), enabled: dq !== '', placeholderData: keepPreviousData })
  const data = dq ? res.data : undefined

  const inProject = (h: Hit) => !onlyCurrent || !current || h.project_id === current.id
  const tasks = scope === 'projects' ? [] : (data?.groups.tasks ?? []).filter(inProject)
  const descs = scope === 'projects' ? [] : (data?.groups.descriptions ?? []).filter(inProject)
  const projs = scope === 'tasks' ? [] : (data?.groups.projects ?? [])
  const items: Item[] = [
    ...tasks.map((hit) => ({ kind: 'task', hit }) as Item),
    ...descs.map((hit) => ({ kind: 'desc', hit }) as Item),
    ...projs.map((project) => ({ kind: 'project', project }) as Item),
    ...(q.trim() ? [{ kind: 'create' } as Item] : []),
    { kind: 'mcp' },
  ]
  const total = tasks.length + descs.length + projs.length

  useEffect(() => setSel(0), [dq, scope, onlyCurrent])
  const listRef = useRef<HTMLDivElement>(null)
  useEffect(() => { listRef.current?.querySelector('.rs.on')?.scrollIntoView({ block: 'nearest' }) }, [sel])

  const open = (it: Item) => {
    onClose()
    if (it.kind === 'task' || it.kind === 'desc') setParam('task', it.hit.code, false)
    else if (it.kind === 'project') navigate(`/p/${it.project.slug}`)
    else if (it.kind === 'create') onCreate(q.trim())
    else navigate('/mcp')
  }
  const key = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onClose() }
    else if (e.key === 'ArrowDown') { e.preventDefault(); setSel((s) => Math.min(items.length - 1, s + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel((s) => Math.max(0, s - 1)) }
    else if (e.key === 'Enter') {
      e.preventDefault()
      const it = e.metaKey || e.ctrlKey ? items.find((i) => i.kind === 'create') : items[sel]
      if (it) open(it)
    }
  }

  let idx = -1
  const row = (it: Item, body: ReactNode, to: string) => {
    const i = ++idx
    return <Link key={i} to={to} className={'rs' + (i === sel ? ' on' : '')} onMouseMove={() => setSel(i)} onClick={(e) => { e.preventDefault(); open(it) }}>{body}</Link>
  }
  const hitRow = (it: Item & { hit: Hit }, snippet: boolean) => {
    const h = it.hit
    const p = projects.find((x) => x.id === h.project_id)
    return row(it, (
      <>
        {p && <ProjectTile icon={p.icon} color={p.color} size={22} radius={7} />}
        <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)', flex: '0 0 52px' }}>{h.code}</span>
        <span style={{ flex: '1 1 0', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <span style={{ fontWeight: 500, fontSize: 14, lineHeight: 1.35 }}>{snippet ? h.title : <Highlight text={h.title} q={q} />}</span>
          {snippet && h.snippet && <span style={{ fontSize: 12.5, color: 'var(--t3)' }}><Snippet s={h.snippet} /></span>}
        </span>
        <span className="chip sm" style={statusChip(h.status_color)}>{h.status_name}</span>
        {i0(it) === sel && <span className="kbd">↵</span>}
      </>
    ), hrefWith('task', h.code))
  }
  const i0 = (it: Item) => items.indexOf(it)

  return (
    <div className="overlay" style={{ paddingTop: 112 }} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="searchbox" role="dialog" aria-modal="true" aria-label="Поиск" onKeyDown={key} ref={trap} tabIndex={-1}>
        <div className="searchtop">
          <Icon name="search" size={19} sw={1.9} />
          <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Поиск по задачам и проектам" aria-label="Поиск по задачам, проектам и командам" />
          <button type="button" className="kbd" aria-label="Закрыть поиск" onClick={onClose}>esc</button>
        </div>
        <div className="chips" style={{ padding: '12px 16px 4px' }}>
          {([['all', 'Все', total], ['tasks', 'Задачи', (data?.counts.tasks ?? 0) + (data?.counts.descriptions ?? 0)], ['projects', 'Проекты', data?.counts.projects ?? 0]] as const).map(([k, label, n]) => (
            <button key={k} type="button" className={'sch' + (scope === k ? ' on' : '')} aria-pressed={scope === k} onClick={() => setScope(k)}>
              {label}<span className="mono" style={{ fontSize: 11.5, opacity: 0.7 }}>{n}</span>
            </button>
          ))}
          {current && (
            <button type="button" className={'sch' + (onlyCurrent ? ' on' : '')} aria-pressed={onlyCurrent} onClick={() => setOnlyCurrent(!onlyCurrent)}>Проект: {current.name}</button>
          )}
        </div>
        <div className="results" ref={listRef}>
          {res.isError && <div className="errline" style={{ padding: '8px 12px' }} role="alert">{res.error.message}</div>}
          {tasks.length > 0 && <div className="cap rhead">Задачи · по названию</div>}
          {items.filter((i): i is Item & { kind: 'task' } => i.kind === 'task').map((it) => hitRow(it, false))}
          {descs.length > 0 && <div className="cap rhead">В описаниях</div>}
          {items.filter((i): i is Item & { kind: 'desc' } => i.kind === 'desc').map((it) => hitRow(it, true))}
          {projs.length > 0 && <div className="cap rhead">Проекты</div>}
          {items.filter((i): i is Item & { kind: 'project' } => i.kind === 'project').map((it) => row(it, (
            <>
              <ProjectTile icon={it.project.icon} color={it.project.color} size={22} radius={7} />
              <span className="mono" style={{ fontSize: 11.5, color: 'var(--t3)', flex: '0 0 52px' }}>{it.project.key}</span>
              <span style={{ flex: 1, fontWeight: 500 }}><Highlight text={it.project.name} q={q} /></span>
            </>
          ), `/p/${it.project.slug}`))}
          {dq !== '' && !res.isFetching && total === 0 && <div className="cap rhead" style={{ textTransform: 'none', letterSpacing: 0, fontWeight: 400 }}>Ничего не найдено по «{dq}»</div>}
          {dq === '' && <div className="cap rhead" style={{ textTransform: 'none', letterSpacing: 0, fontWeight: 400 }}>Введите слово из названия или описания, либо код задачи, например TRK-07</div>}
          <div className="cap rhead">Действия</div>
          {items.filter((i) => i.kind === 'create').map((it) => row(it, (
            <>
              <span className="sqicon" style={{ background: 'var(--ink)', color: 'var(--inkt)' }}><Icon name="plus" size={13} sw={2.4} /></span>
              <span style={{ flex: 1, fontWeight: 500 }}>Создать задачу «{q.trim()}»</span><span className="kbd">⌘↵</span>
            </>
          ), '/'))}
          {items.filter((i) => i.kind === 'mcp').map((it) => row(it, (
            <>
              <span className="sqicon" style={{ background: 'var(--sk)', color: 'var(--t2)' }}><Icon name="plug" size={13} sw={2} /></span>
              <span style={{ flex: 1, fontWeight: 500 }}>Открыть MCP и журнал вызовов</span>
            </>
          ), '/mcp'))}
        </div>
        <div className="searchfoot">
          <span><span className="kbd">↑</span> <span className="kbd">↓</span> выбрать</span>
          <span><span className="kbd">↵</span> открыть</span>
          <span><span className="kbd">⌘↵</span> создать</span>
          <span className="grow" />
          <span className="mono" style={{ fontSize: 11.5 }}>{data ? `${total} ${total === 1 ? 'результат' : 'результатов'} · ${data.took_ms} мс` : ''}</span>
        </div>
      </div>
    </div>
  )
}
