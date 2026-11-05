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

const KBD = 'font-mono text-[11px] font-medium leading-[normal] text-t3 border border-ln rounded-[5px] px-[5px] py-px bg-sf'
const MARK = 'rounded-[3px] bg-acs text-act group-data-[on]:bg-sf'
const SCH = 'inline-flex items-center gap-1.5 h-7 px-[11px] rounded-full border text-[12.5px] cursor-pointer'
const RHEAD = 'px-3 pt-3 pb-1 text-[11px] text-t3'
const RHEAD_CAP = RHEAD + ' font-semibold tracking-[.06em] uppercase'
const SQ = 'grid place-items-center w-[22px] h-[22px] flex-[0_0_22px] rounded-[7px]'

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function Highlight({ text, q }: { text: string; q: string }) {
  const words = q.trim().split(/\s+/).filter(Boolean).map(escape)
  if (!words.length) return <>{text}</>
  return <>{text.split(new RegExp(`(${words.join('|')})`, 'giu')).map((p, i) => (i % 2 ? <mark key={i} className={MARK}>{p}</mark> : p))}</>
}

/** The server marks matches with [brackets]; turn them into <mark>. */
function Snippet({ s }: { s: string }) {
  return <>{s.replace(/[#*`>]/g, '').split(/(\[[^\]]*\])/).map((p, i) => (p.startsWith('[') && p.endsWith(']') ? <mark key={i} className={MARK}>{p.slice(1, -1)}</mark> : p))}</>
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
  useEffect(() => { listRef.current?.querySelector('[data-on]')?.scrollIntoView({ block: 'nearest' }) }, [sel])

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
    return <Link key={i} to={to} className="group flex items-center gap-3 min-h-[46px] px-3 py-1.5 rounded-[9px] no-underline data-[on]:bg-acs" data-on={i === sel ? '' : undefined} onMouseMove={() => setSel(i)} onClick={(e) => { e.preventDefault(); open(it) }}>{body}</Link>
  }
  const hitRow = (it: Item & { hit: Hit }, snippet: boolean) => {
    const h = it.hit
    const p = projects.find((x) => x.id === h.project_id)
    return row(it, (
      <>
        {p && <ProjectTile icon={p.icon} color={p.color} size={22} radius={7} />}
        <span className="font-mono text-[11.5px] text-t3 flex-[0_0_52px]">{h.code}</span>
        <span className="flex flex-[1_1_0] flex-col gap-0.5 min-w-0">
          <span className="text-[14px] font-medium leading-[1.35]">{snippet ? h.title : <Highlight text={h.title} q={q} />}</span>
          {snippet && h.snippet && <span className="text-[12.5px] text-t3"><Snippet s={h.snippet} /></span>}
        </span>
        <span className="inline-flex items-center gap-1.5 h-[22px] px-2 rounded-md text-[12px] font-medium" style={statusChip(h.status_color)}>{h.status_name}</span>
        {i0(it) === sel && <span className={KBD}>↵</span>}
      </>
    ), hrefWith('task', h.code))
  }
  const i0 = (it: Item) => items.indexOf(it)

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-[rgba(15,17,22,.45)] px-4 pt-[112px] pb-4" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="flex flex-col self-start w-[680px] max-w-full overflow-hidden bg-sf border border-ln rounded-2xl shadow-pop" role="dialog" aria-modal="true" aria-label="Поиск" onKeyDown={key} ref={trap} tabIndex={-1}>
        <div className="flex items-center gap-3 px-4 h-[58px] border-b border-ln text-t3">
          <Icon name="search" size={19} sw={1.9} />
          <input className="flex-1 min-w-0 border-0 bg-transparent outline-0 text-[17px] font-medium text-tx" autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Поиск по задачам и проектам" aria-label="Поиск по задачам, проектам и командам" />
          <button type="button" className={KBD + ' cursor-pointer'} aria-label="Закрыть поиск" onClick={onClose}>esc</button>
        </div>
        <div className="flex flex-wrap gap-1.5 px-4 pt-3 pb-1">
          {([['all', 'Все', total], ['tasks', 'Задачи', (data?.counts.tasks ?? 0) + (data?.counts.descriptions ?? 0)], ['projects', 'Проекты', data?.counts.projects ?? 0]] as const).map(([k, label, n]) => (
            <button key={k} type="button" className={SCH + (scope === k ? ' bg-ink border-ink text-inkt font-medium' : ' border-ln bg-sf text-t2')} aria-pressed={scope === k} onClick={() => setScope(k)}>
              {label}<span className="font-mono text-[11.5px] opacity-70">{n}</span>
            </button>
          ))}
          {current && (
            <button type="button" className={SCH + (onlyCurrent ? ' bg-ink border-ink text-inkt font-medium' : ' border-ln bg-sf text-t2')} aria-pressed={onlyCurrent} onClick={() => setOnlyCurrent(!onlyCurrent)}>Проект: {current.name}</button>
          )}
        </div>
        <div className="flex flex-col gap-0.5 max-h-[52vh] overflow-y-auto px-2 pt-2 pb-1.5" ref={listRef}>
          {res.isError && <div className="px-3 py-2 text-[13px] text-rd" role="alert">{res.error.message}</div>}
          {tasks.length > 0 && <div className={RHEAD_CAP}>Задачи · по названию</div>}
          {items.filter((i): i is Item & { kind: 'task' } => i.kind === 'task').map((it) => hitRow(it, false))}
          {descs.length > 0 && <div className={RHEAD_CAP}>В описаниях</div>}
          {items.filter((i): i is Item & { kind: 'desc' } => i.kind === 'desc').map((it) => hitRow(it, true))}
          {projs.length > 0 && <div className={RHEAD_CAP}>Проекты</div>}
          {items.filter((i): i is Item & { kind: 'project' } => i.kind === 'project').map((it) => row(it, (
            <>
              <ProjectTile icon={it.project.icon} color={it.project.color} size={22} radius={7} />
              <span className="font-mono text-[11.5px] text-t3 flex-[0_0_52px]">{it.project.key}</span>
              <span className="flex-1 font-medium"><Highlight text={it.project.name} q={q} /></span>
            </>
          ), `/p/${it.project.slug}`))}
          {dq !== '' && !res.isFetching && total === 0 && <div className={RHEAD}>Ничего не найдено по «{dq}»</div>}
          {dq === '' && <div className={RHEAD}>Введите слово из названия или описания, либо код задачи, например TRK-07</div>}
          <div className={RHEAD_CAP}>Действия</div>
          {items.filter((i) => i.kind === 'create').map((it) => row(it, (
            <>
              <span className={SQ + ' bg-ink text-inkt'}><Icon name="plus" size={13} sw={2.4} /></span>
              <span className="flex-1 font-medium">Создать задачу «{q.trim()}»</span><span className={KBD}>⌘↵</span>
            </>
          ), '/'))}
          {items.filter((i) => i.kind === 'mcp').map((it) => row(it, (
            <>
              <span className={SQ + ' bg-sk text-t2'}><Icon name="plug" size={13} sw={2} /></span>
              <span className="flex-1 font-medium">Открыть MCP и журнал вызовов</span>
            </>
          ), '/mcp'))}
        </div>
        <div className="flex flex-wrap items-center gap-4 px-4 py-2.5 border-t border-ln bg-sf2 text-[12px] text-t3">
          <span><span className={KBD}>↑</span> <span className={KBD}>↓</span> выбрать</span>
          <span><span className={KBD}>↵</span> открыть</span>
          <span><span className={KBD}>⌘↵</span> создать</span>
          <span className="grow" />
          <span className="font-mono text-[11.5px]">{data ? `${total} ${total === 1 ? 'результат' : 'результатов'} · ${data.took_ms} мс` : ''}</span>
        </div>
      </div>
    </div>
  )
}
