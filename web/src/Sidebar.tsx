// Sidebar in the shadcn-admin layout: workspace switcher on top, labelled nav groups, account menu at the bottom.
// On /settings/* the nav groups give way to the settings sections.
// `rail` is the collapsed icon-only mode (desktop); on narrow screens it is an off-canvas drawer.
import { WorkspaceSwitcher } from './Workspaces'
import { SettingsNav, UserMenu } from './SettingsPage'
import { type ReactNode } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { type Project, type View, api, useAttention, useProjects } from './api'
import { Icon, ProjectTile } from './icons'
import { PlandilloLogo } from './PlandilloLogo'
import { tr } from './i18n'
import { Link } from './router'
import { cap, kbd, useFocusTrap } from './ui'
import { workspaceId } from './workspacePath'

const nv = 'flex h-8 items-center gap-2 rounded-md px-2 text-sm no-underline w-full text-left text-tx hover:bg-sk '
const nvOn = nv + 'bg-sk font-medium'
const nvOff = nv
const nvName = 'grow min-w-0 overflow-hidden text-ellipsis whitespace-nowrap'
const nvN = 'text-xs font-medium text-t2 tabular-nums'
const nvAlert = 'rounded-full bg-rds px-1.5 text-xs font-semibold text-rd tabular-nums'

type NavItem = { to: string; label: string; icon: string; n?: number; on: boolean; dot?: string; alert?: boolean }

function Group({ label, rail, action, children }: { label: string; rail: boolean; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      {!rail && <div className="flex h-8 items-center px-2"><span className={cap + ' grow'}>{label}</span>{action}</div>}
      {children}
    </div>
  )
}

export function Sidebar({ path, view, overdue, settings, rail, mobileOpen, onClose, onNewProject, onSearch }: {
  path: string; view: string | null; overdue: boolean; settings: string | null; rail: boolean; mobileOpen: boolean; onClose: () => void; onNewProject: () => void; onSearch: () => void
}) {
  const trap = useFocusTrap<HTMLElement>(mobileOpen)
  const projects = useProjects()
  const today = useQuery({ queryKey: ['today', 'now'], queryFn: () => api.today() })
  const attention = useAttention()
  const trash = useQuery({ queryKey: ['trash'], queryFn: api.trash })
  const mcp = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  // Same key as OAuthConnections on the Integrations page; selfhost only, elsewhere it fails and the count is just not shown.
  const apps = useQuery({ queryKey: ['oauth-connections', workspaceId], queryFn: () => api.oauthConnections(workspaceId), enabled: workspaceId > 0 })
  const all: Project[] = projects.data ?? []
  const list = all.filter((p) => !p.archived_at)
  const archived = all.filter((p) => p.archived_at)
  const trashP = useQuery({ queryKey: ['trash-projects'], queryFn: api.trashProjects })
  const views = useQueries({ queries: all.map((p) => ({ queryKey: ['views', p.id], queryFn: () => api.views(p.id) })) })
  const pinned: { v: View; p: Project }[] = all.flatMap((p, i) =>
    (views[i]?.data ?? []).filter((v) => v.pinned && v.project_id === p.id).map((v) => ({ v, p })))

  const nav = [
    { to: '/', label: tr('Сегодня'), icon: 'cal', n: today.data?.tasks.length, on: path === '/' && !overdue },
    { to: '/attention', label: tr('Требует внимания'), icon: 'flag', n: attention.data?.length, on: path === '/attention' },
    { to: '/?focus=overdue', label: tr('Просрочено'), icon: 'clock', n: today.data?.overdue.length, on: overdue, alert: true },
  ]
  const more = [
    { to: '/integrations', label: tr('Интеграции'), icon: 'plug', n: apps.data?.length, dot: mcp.data?.check && !mcp.isError ? (mcp.data.online ? 'var(--dn)' : 'var(--rd)') : undefined, on: path === '/integrations' || path === '/mcp' },
    { to: '/trash', label: tr('Корзина'), icon: 'trash', n: (trash.data?.length ?? 0) + (trashP.data?.length ?? 0), on: path === '/trash' },
  ]
  // In the icon rail an item is just its icon (centred), with the label as a tooltip.
  const cls = (on: boolean) => (on ? nvOn : nvOff) + (rail ? ' justify-center px-0' : '')
  const navLink = (n: NavItem) => (
    <Link key={n.to} to={n.to} className={cls(n.on)} aria-current={n.on ? 'page' : undefined} title={rail ? n.label : undefined}>
      <Icon name={n.icon} />
      {!rail && <span className={nvName}>{n.label}</span>}
      {!rail && n.dot && <span className="inline-block size-[7px] shrink-0 rounded-full" style={{ background: n.dot }} role="img" aria-label={n.dot.includes('dn') ? tr('онлайн') : tr('недоступен')} />}
      {!rail && n.n !== undefined && n.n > 0 && <span className={n.alert ? nvAlert : nvN}>{n.n}</span>}
    </Link>
  )

  return (
    <>
      {mobileOpen && <div className="fixed inset-0 z-40 bg-black/50 min-[900px]:hidden" onClick={onClose} aria-hidden="true" />}
      {/* Floating variant (shadcn): the aside is a padded slot, the inner card floats with border, radius and shadow. */}
      <aside
        className={'z-50 shrink-0 p-2 motion-safe:transition-[width,translate] motion-safe:duration-200 '
          + 'max-[900px]:fixed max-[900px]:inset-y-0 max-[900px]:left-0 max-[900px]:w-68 min-[900px]:sticky min-[900px]:top-0 min-[900px]:h-[calc(100vh/var(--zoom,1))] '
          + (rail ? 'min-[900px]:w-18 ' : 'min-[900px]:w-68 ') + (mobileOpen ? '' : 'max-[900px]:invisible max-[900px]:-translate-x-full')}
        id="app-sidebar" ref={trap} tabIndex={mobileOpen ? -1 : undefined} role={mobileOpen ? 'dialog' : undefined} aria-modal={mobileOpen || undefined}
        onKeyDown={e => { if (mobileOpen && e.key === 'Escape') { e.stopPropagation(); onClose() } }}
        onClick={e => { if (mobileOpen && (e.target as Element).closest('a[href]') && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) onClose() }}
        aria-label={tr('Боковая панель')}
      >
        <div className="flex h-full flex-col rounded-lg border border-ln bg-sf shadow-sm">
        <div className={'flex h-16 items-center gap-2 px-3 ' + (rail ? 'justify-center' : '')}>
          <PlandilloLogo compact={rail} />
          {mobileOpen && <button type="button" className="ml-auto grid size-8 place-items-center rounded-md text-t2 hover:bg-sk" aria-label={tr('Закрыть')} onClick={onClose}><Icon name="x" /></button>}
        </div>
        <div className={'px-2 pb-2 ' + (rail ? 'flex justify-center' : '')}><WorkspaceSwitcher rail={rail} /></div>

        <div className="flex min-h-0 grow flex-col gap-3 overflow-y-auto px-2 py-1">
          {settings !== null ? <SettingsNav section={settings} rail={rail} /> : <>
          <Group label={tr('Общее')} rail={rail}>
            <nav className="flex flex-col gap-0.5" aria-label={tr('Разделы')}>
              <button type="button" className={cls(false) + ' text-t2'} onClick={onSearch} aria-keyshortcuts="Meta+K Control+K" title={rail ? tr('Поиск') : undefined} aria-label={rail ? tr('Поиск') : undefined}>
                <Icon name="search" />
                {!rail && <><span className={nvName}>{tr('Поиск')}</span><span className={kbd}>⌘K</span></>}
              </button>
              {nav.map((n) => navLink(n))}
            </nav>
          </Group>

          <Group label={tr('Проекты')} rail={rail}
            action={<button type="button" className="inline-grid size-6 place-items-center rounded-md bg-transparent p-0 text-t3 hover:bg-sk hover:text-tx" aria-label={tr('Новый проект')} onClick={onNewProject}><Icon name="plus" size={15} sw={2} /></button>}>
            {list.map((p) => {
              const on = path === `/p/${p.slug}` && !view
              return (
                <Link key={p.id} to={`/p/${p.slug}`} className={cls(on)} aria-current={on ? 'page' : undefined} title={rail ? p.name : undefined}>
                  <ProjectTile project={p} />
                  {!rail && <><span className={nvName}>{p.name}</span><span className={nvN}>{p.open}</span></>}
                </Link>
              )
            })}
            {!rail && archived.length > 0 && (
              <details className="group">
                <summary className={nvOff + ' list-none cursor-pointer [&::-webkit-details-marker]:hidden'}><Icon name="stack" /><span className={nvName}>{tr('Архив')}</span><span className={nvN}>{archived.length}</span></summary>
                {archived.map((p) => (
                  <Link key={p.id} to={`/p/${p.slug}`} className={(path.startsWith(`/p/${p.slug}`) ? nvOn : nvOff) + ' opacity-75'}>
                    <ProjectTile project={p} /><span className={nvName}>{p.name}</span>
                  </Link>
                ))}
              </details>
            )}
            {!rail && projects.isError && <button type="button" className="px-2 py-0 text-left text-[12.5px] text-rd" onClick={() => projects.refetch()}>{tr('Не удалось загрузить проекты · Повторить')}</button>}
            {!rail && projects.isSuccess && list.length === 0 && <span className="px-2 text-[12.5px] text-t3">{tr('Проектов пока нет')}</span>}
          </Group>

          {pinned.length > 0 && (
            <Group label={tr('Виды')} rail={rail}>
              {pinned.map(({ v, p }) => (
                <Link key={v.id} to={`/p/${p.slug}?view=${v.id}`} className={cls(view === String(v.id) && path === `/p/${p.slug}`)} title={rail ? v.name : undefined}>
                  <Icon name="funnel" />
                  {!rail && <span className={nvName}>{v.name}</span>}
                </Link>
              ))}
            </Group>
          )}
          </>}
        </div>

        <div className="flex shrink-0 flex-col gap-1 p-2">
          {settings === null && <Group label={tr('Дополнительно')} rail={rail}>
            {more.map((n) => navLink(n))}
            <a href="https://github.com/huntly-digital/plandillo" target="_blank" rel="noopener noreferrer" className={cls(false)} title={rail ? 'GitHub' : undefined}>
              <Icon name="github" />
              {!rail && <span className={nvName}>GitHub</span>}
            </a>
            <a href="https://github.com/huntly-digital/plandillo/issues/new" target="_blank" rel="noopener noreferrer" className={cls(false)} title={rail ? tr('Сообщить о проблеме') : undefined}>
              <Icon name="flag" />
              {!rail && <span className={nvName}>{tr('Сообщить о проблеме')}</span>}
            </a>
          </Group>}
          <UserMenu variant="sidebar" rail={rail} />
        </div>
        </div>
      </aside>
    </>
  )
}
