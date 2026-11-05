import { useQueries, useQuery } from '@tanstack/react-query'
import { type Project, type View, api, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { Link } from './router'
import { type ThemePref } from './theme'

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
    <aside className="side">
      <div className="brand">
        <span className="logo"><Icon name="logo" size={17} sw={2} /></span>
        <b>Docket</b>
        <span className="mono" style={{ fontSize: 11, color: 'var(--t3)' }}>:{location.port || '80'}</span>
      </div>
      <button type="button" className="searchbtn" onClick={onSearch}>
        <Icon name="search" size={15} /><span className="grow">Поиск</span><span className="kbd">⌘K</span>
      </button>
      <nav className="nvlist" aria-label="Разделы">
        {nav.map((n) => (
          <Link key={n.label} to={n.to} className={'nv' + (n.on ? ' on' : '')} aria-current={n.on ? 'page' : undefined}>
            <Icon name={n.icon} />
            <span className="name">{n.label}</span>
            {n.dot && <span className="dot" style={{ width: 7, height: 7, flexBasis: 7, background: n.dot }} role="img" aria-label={n.dot.includes('dn') ? 'онлайн' : 'недоступен'} />}
            {n.n !== undefined && n.n > 0 && <span className="n mono tabnum">{n.n}</span>}
          </Link>
        ))}
      </nav>
      <div className="nvlist">
        <div className="grouphead">
          <span className="cap grow">Проекты</span>
          <button type="button" className="iconbtn" aria-label="Новый проект" onClick={onNewProject}><Icon name="plus" size={15} sw={2} /></button>
        </div>
        {list.map((p) => {
          const on = path === `/p/${p.slug}` && !view
          return (
            <Link key={p.id} to={`/p/${p.slug}`} className={'nv' + (on ? ' on' : '')} aria-current={on ? 'page' : undefined}>
              <ProjectTile icon={p.icon} color={p.color} />
              <span className="name">{p.name}</span>
              <span className="n mono tabnum">{p.open}</span>
            </Link>
          )
        })}
        {archived.length > 0 && (
          <details className="archive">
            <summary className="nv"><Icon name="stack" /><span className="name">Архив</span><span className="n mono">{archived.length}</span></summary>
            {archived.map((p) => (
              <Link key={p.id} to={`/p/${p.slug}`} className={'nv' + (path.startsWith(`/p/${p.slug}`) ? ' on' : '')} style={{ opacity: 0.75 }}>
                <ProjectTile icon={p.icon} color={p.color} /><span className="name">{p.name}</span>
              </Link>
            ))}
          </details>
        )}
        {projects.isError && <button type="button" className="linkbtn" style={{ padding: '0 8px', textAlign: 'left', color: 'var(--rd)' }} onClick={() => projects.refetch()}>Не удалось загрузить проекты · Повторить</button>}
        {projects.isSuccess && list.length === 0 && <span style={{ padding: '0 8px', fontSize: 12.5, color: 'var(--t3)' }}>Проектов пока нет</span>}
      </div>
      {pinned.length > 0 && (
        <div className="nvlist">
          <div className="cap" style={{ padding: '0 8px 6px' }}>Виды</div>
          {pinned.map(({ v, p }) => (
            <Link key={v.id} to={`/p/${p.slug}?view=${v.id}`} className={'nv' + (view === String(v.id) && path === `/p/${p.slug}` ? ' on' : '')}>
              <Icon name="funnel" />
              <span className="name">{v.name}</span>
            </Link>
          ))}
        </div>
      )}
      <div className="userbar">
        <span className="avatar">D</span>
        <span style={{ flexGrow: 1, display: 'flex', flexDirection: 'column', lineHeight: 1.2 }}>
          <b style={{ fontWeight: 600, fontSize: 13 }}>Denis</b>
          <span style={{ fontSize: 11.5, color: 'var(--t3)' }}>локально · без входа</span>
        </span>
        <span className="themeseg" role="group" aria-label="Тема">
          {THEMES.map((t) => (
            <button key={t.id} type="button" className={theme === t.id ? 'on' : ''} aria-label={t.label} aria-pressed={theme === t.id} title={t.label} onClick={() => onTheme(t.id)}>
              <Icon name={t.icon} size={13} sw={2} />
            </button>
          ))}
        </span>
      </div>
    </aside>
  )
}
