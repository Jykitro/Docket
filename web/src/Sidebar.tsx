import { useQueries, useQuery } from '@tanstack/react-query'
import { type Project, type View, api, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { Link } from './router'
import { type ThemePref } from './theme'
import { cap, kbd } from './ui'

const nv = 'flex items-center gap-2.5 h-8 px-2 rounded-lg no-underline text-[13.5px] w-full text-left hover:bg-sk '
const nvOn = nv + 'bg-sk text-tx font-semibold'
const nvOff = nv + 'text-t2'
const nvName = 'grow min-w-0 overflow-hidden text-ellipsis whitespace-nowrap'
const nvN = 'text-[11.5px] text-t3 font-mono'
const nvlist = 'flex flex-col gap-0.5'

const THEMES: { id: ThemePref; icon: string; label: string }[] = [
  { id: 'light', icon: 'sun', label: 'Светлая тема' },
  { id: 'dark', icon: 'moon', label: 'Тёмная тема' },
  { id: 'system', icon: 'monitor', label: 'Как в системе' },
]

export function Sidebar({ path, view, theme, onTheme, onNewProject, onSearch }: {
  path: string; view: string | null; theme: ThemePref; onTheme: (t: ThemePref) => void; onNewProject: () => void; onSearch: () => void
}) {
  const projects = useProjects()
  const today = useQuery({ queryKey: ['today', 'now'], queryFn: () => api.today() })
  const trash = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const mcp = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  const all: Project[] = projects.data ?? []
  const list = all.filter((p) => !p.archived_at)
  const archived = all.filter((p) => p.archived_at)
  const trashP = useQuery({ queryKey: ['trash-projects'], queryFn: api.trashProjects })
  const views = useQueries({ queries: all.map((p) => ({ queryKey: ['views', p.id], queryFn: () => api.views(p.id) })) })
  const pinned: { v: View; p: Project }[] = all.flatMap((p, i) =>
    (views[i]?.data ?? []).filter((v) => v.pinned && v.project_id === p.id).map((v) => ({ v, p })))

  const nav = [
    { to: '/', label: 'Сегодня', icon: 'cal', n: today.data?.tasks.length, on: path === '/' },
    { to: '/?focus=overdue', label: 'Просрочено', icon: 'clock', n: today.data?.overdue.length, on: false },
    { to: '/mcp', label: 'MCP', icon: 'plug', dot: mcp.data ? (mcp.data.online ? 'var(--dn)' : 'var(--rd)') : undefined, on: path === '/mcp' },
    { to: '/trash', label: 'Корзина', icon: 'trash', n: (trash.data?.length ?? 0) + (trashP.data?.length ?? 0), on: path === '/trash' },
  ]

  return (
    <aside className="flex-[0_0_264px] max-w-full bg-sf border-r border-ln pt-4 px-3 pb-3.5 flex flex-col gap-[18px] max-[899px]:flex-[1_1_100%] max-[899px]:border-r-0 max-[899px]:border-b min-[900px]:sticky min-[900px]:top-0 min-[900px]:h-screen min-[900px]:overflow-y-auto">
      <div className="flex items-center gap-2.5 px-1.5 py-0.5">
        <span className="size-7 rounded-lg bg-ink text-inkt grid place-items-center"><Icon name="logo" size={17} sw={2} /></span>
        <b className="text-[16px] font-bold tracking-[-.01em] grow">Docket</b>
        <span className="font-mono text-[11px] text-t3">:{location.port || '80'}</span>
      </div>
      <button type="button" className="flex items-center gap-2 h-[34px] px-2.5 border border-ln rounded-lg bg-sf2 text-t3 text-[13px] text-left w-full hover:bg-sk" onClick={onSearch}>
        <Icon name="search" size={15} /><span className="grow">Поиск</span><span className={kbd}>⌘K</span>
      </button>
      <nav className={nvlist} aria-label="Разделы">
        {nav.map((n) => (
          <Link key={n.label} to={n.to} className={n.on ? nvOn : nvOff} aria-current={n.on ? 'page' : undefined}>
            <Icon name={n.icon} />
            <span className={nvName}>{n.label}</span>
            {n.dot && <span className="inline-block w-[7px] h-[7px] rounded-full flex-[0_0_7px]" style={{ background: n.dot }} role="img" aria-label={n.dot.includes('dn') ? 'онлайн' : 'недоступен'} />}
            {n.n !== undefined && n.n > 0 && <span className={nvN + ' tabular-nums'}>{n.n}</span>}
          </Link>
        ))}
      </nav>
      <div className={nvlist}>
        <div className="flex items-center px-2 pb-1.5">
          <span className={cap + ' grow'}>Проекты</span>
          <button type="button" className="inline-grid place-items-center size-6 rounded-md bg-transparent text-t3 p-0 hover:bg-sk hover:text-tx" aria-label="Новый проект" onClick={onNewProject}><Icon name="plus" size={15} sw={2} /></button>
        </div>
        {list.map((p) => {
          const on = path === `/p/${p.slug}` && !view
          return (
            <Link key={p.id} to={`/p/${p.slug}`} className={on ? nvOn : nvOff} aria-current={on ? 'page' : undefined}>
              <ProjectTile icon={p.icon} color={p.color} />
              <span className={nvName}>{p.name}</span>
              <span className={nvN + ' tabular-nums'}>{p.open}</span>
            </Link>
          )
        })}
        {archived.length > 0 && (
          <details className="group">
            <summary className={nvOff + ' list-none cursor-pointer group-open:text-tx [&::-webkit-details-marker]:hidden'}><Icon name="stack" /><span className={nvName}>Архив</span><span className={nvN}>{archived.length}</span></summary>
            {archived.map((p) => (
              <Link key={p.id} to={`/p/${p.slug}`} className={(path.startsWith(`/p/${p.slug}`) ? nvOn : nvOff) + ' opacity-75'}>
                <ProjectTile icon={p.icon} color={p.color} /><span className={nvName}>{p.name}</span>
              </Link>
            ))}
          </details>
        )}
        {projects.isError && <button type="button" className="py-0 px-2 bg-transparent text-[12.5px] text-rd text-left" onClick={() => projects.refetch()}>Не удалось загрузить проекты · Повторить</button>}
        {projects.isSuccess && list.length === 0 && <span className="px-2 text-[12.5px] text-t3">Проектов пока нет</span>}
      </div>
      {pinned.length > 0 && (
        <div className={nvlist}>
          <div className={cap + ' px-2 pb-1.5'}>Виды</div>
          {pinned.map(({ v, p }) => (
            <Link key={v.id} to={`/p/${p.slug}?view=${v.id}`} className={view === String(v.id) && path === `/p/${p.slug}` ? nvOn : nvOff}>
              <Icon name="funnel" />
              <span className={nvName}>{v.name}</span>
            </Link>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2.5 pt-2.5 px-1.5 border-t border-ln mt-auto">
        <span className="size-[26px] rounded-full bg-sk grid place-items-center font-semibold text-[12px]">D</span>
        <span className="grow flex flex-col leading-[1.2]">
          <b className="font-semibold text-[13px]">Denis</b>
          <span className="text-[11.5px] text-t3">локально · без входа</span>
        </span>
        <span className="inline-flex border border-ln rounded-lg p-0.5 gap-0.5" role="group" aria-label="Тема">
          {THEMES.map((t) => (
            <button key={t.id} type="button" className="w-6 h-[22px] rounded-md bg-transparent text-t3 grid place-items-center p-0 aria-pressed:bg-sk aria-pressed:text-tx" aria-label={t.label} aria-pressed={theme === t.id} title={t.label} onClick={() => onTheme(t.id)}>
              <Icon name={t.icon} size={13} sw={2} />
            </button>
          ))}
        </span>
      </div>
    </aside>
  )
}
