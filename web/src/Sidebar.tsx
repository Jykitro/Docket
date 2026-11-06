// Sidebar in the shadcn-admin layout: team block on top, labelled nav groups, user card at the bottom.
// `rail` is the collapsed icon-only mode (desktop); on narrow screens it is an off-canvas drawer.
import { type ReactNode } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { type Project, type View, api, useMe, useProfile, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { Link } from './router'
import { cap } from './ui'

const nv = 'flex h-8 items-center gap-2 rounded-md px-2 text-sm no-underline w-full text-left text-tx hover:bg-sk '
const nvOn = nv + 'bg-sk font-medium'
const nvOff = nv
const nvName = 'grow min-w-0 overflow-hidden text-ellipsis whitespace-nowrap'
const nvN = 'text-xs font-medium text-t2 tabular-nums'

function Group({ label, rail, action, children }: { label: string; rail: boolean; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      {!rail && <div className="flex h-8 items-center px-2"><span className={cap + ' grow'}>{label}</span>{action}</div>}
      {children}
    </div>
  )
}

export function Sidebar({ path, view, rail, mobileOpen, onClose, onNewProject, onSettings }: {
  path: string; view: string | null; rail: boolean; mobileOpen: boolean; onClose: () => void; onNewProject: () => void; onSettings: () => void
}) {
  const projects = useProjects()
  const name = useProfile().data?.name ?? ''
  const me = useMe().data
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
  // In the icon rail an item is just its icon (centred), with the label as a tooltip.
  const cls = (on: boolean) => (on ? nvOn : nvOff) + (rail ? ' justify-center px-0' : '')

  return (
    <>
      {mobileOpen && <div className="fixed inset-0 z-40 bg-black/50 min-[900px]:hidden" onClick={onClose} aria-hidden="true" />}
      {/* Floating variant (shadcn): the aside is a padded slot, the inner card floats with border, radius and shadow. */}
      <aside
        className={'z-50 shrink-0 p-2 transition-[width,translate] duration-200 '
          + 'max-[899px]:fixed max-[899px]:inset-y-0 max-[899px]:left-0 max-[899px]:w-68 min-[900px]:sticky min-[900px]:top-0 min-[900px]:h-screen '
          + (rail ? 'min-[900px]:w-18 ' : 'min-[900px]:w-68 ') + (mobileOpen ? '' : 'max-[899px]:-translate-x-full')}
        aria-label="Боковая панель"
      >
        <div className="flex h-full flex-col rounded-lg border border-ln bg-sf shadow-sm">
        <div className={'flex h-16 items-center gap-2 px-3 ' + (rail ? 'justify-center' : '')}>
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-ink text-inkt"><Icon name="logo" size={17} sw={2} /></span>
          {!rail && <span className="flex min-w-0 grow flex-col leading-tight"><b className="text-sm font-semibold">Docket</b><span className="font-mono text-xs text-t3">:{location.port || '80'}</span></span>}
        </div>

        <div className="flex min-h-0 grow flex-col gap-3 overflow-y-auto px-2 py-1">
          <Group label="Общее" rail={rail}>
            <nav className="flex flex-col gap-0.5" aria-label="Разделы">
              {nav.map((n) => (
                <Link key={n.label} to={n.to} className={cls(n.on)} aria-current={n.on ? 'page' : undefined} title={rail ? n.label : undefined}>
                  <Icon name={n.icon} />
                  {!rail && <span className={nvName}>{n.label}</span>}
                  {!rail && n.dot && <span className="inline-block size-[7px] shrink-0 rounded-full" style={{ background: n.dot }} role="img" aria-label={n.dot.includes('dn') ? 'онлайн' : 'недоступен'} />}
                  {!rail && n.n !== undefined && n.n > 0 && <span className={nvN}>{n.n}</span>}
                </Link>
              ))}
            </nav>
          </Group>

          <Group label="Проекты" rail={rail}
            action={<button type="button" className="inline-grid size-6 place-items-center rounded-md bg-transparent p-0 text-t3 hover:bg-sk hover:text-tx" aria-label="Новый проект" onClick={onNewProject}><Icon name="plus" size={15} sw={2} /></button>}>
            {list.map((p) => {
              const on = path === `/p/${p.slug}` && !view
              return (
                <Link key={p.id} to={`/p/${p.slug}`} className={cls(on)} aria-current={on ? 'page' : undefined} title={rail ? p.name : undefined}>
                  <ProjectTile icon={p.icon} color={p.color} />
                  {!rail && <><span className={nvName}>{p.name}</span><span className={nvN}>{p.open}</span></>}
                </Link>
              )
            })}
            {!rail && archived.length > 0 && (
              <details className="group">
                <summary className={nvOff + ' list-none cursor-pointer [&::-webkit-details-marker]:hidden'}><Icon name="stack" /><span className={nvName}>Архив</span><span className={nvN}>{archived.length}</span></summary>
                {archived.map((p) => (
                  <Link key={p.id} to={`/p/${p.slug}`} className={(path.startsWith(`/p/${p.slug}`) ? nvOn : nvOff) + ' opacity-75'}>
                    <ProjectTile icon={p.icon} color={p.color} /><span className={nvName}>{p.name}</span>
                  </Link>
                ))}
              </details>
            )}
            {!rail && projects.isError && <button type="button" className="px-2 py-0 text-left text-[12.5px] text-rd" onClick={() => projects.refetch()}>Не удалось загрузить проекты · Повторить</button>}
            {!rail && projects.isSuccess && list.length === 0 && <span className="px-2 text-[12.5px] text-t3">Проектов пока нет</span>}
          </Group>

          {pinned.length > 0 && (
            <Group label="Виды" rail={rail}>
              {pinned.map(({ v, p }) => (
                <Link key={v.id} to={`/p/${p.slug}?view=${v.id}`} className={cls(view === String(v.id) && path === `/p/${p.slug}`)} title={rail ? v.name : undefined}>
                  <Icon name="funnel" />
                  {!rail && <span className={nvName}>{v.name}</span>}
                </Link>
              ))}
            </Group>
          )}
        </div>

        <div className="p-2">
          <button type="button" className={'flex w-full items-center gap-2 rounded-md p-2 text-left hover:bg-sk ' + (rail ? 'justify-center px-0' : '')} aria-label="Настройки" title="Настройки: профиль, тема, платформа" onClick={onSettings}>
            <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-sk text-sm font-semibold">{name ? name.charAt(0).toUpperCase() : <Icon name="user" size={15} sw={2} />}</span>
            {!rail && (
              <>
                <span className="flex min-w-0 grow flex-col leading-tight">
                  <b className="truncate text-sm font-semibold">{name || 'Профиль'}</b>
                  <span className="truncate text-xs text-t3">{me?.mode === 'selfhost' ? (me.user?.is_admin ? 'администратор' : 'пользователь') : 'локально · без входа'}</span>
                </span>
                <span className="text-t3"><Icon name="chevUD" size={16} /></span>
              </>
            )}
          </button>
        </div>
        </div>
      </aside>
    </>
  )
}
