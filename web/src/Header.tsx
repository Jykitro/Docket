// Top bar in the shadcn-admin layout: sidebar trigger, breadcrumbs, then MCP status, theme switch and profile on the right.
// Search lives in the sidebar; on narrow screens (sidebar hidden) the top bar keeps a search icon.
import { useQuery } from '@tanstack/react-query'
import { api, mcpHealthLabel } from './api'
import { THEMES, UserMenu } from './SettingsPage'
import { Icon } from './icons'
import { tr } from './i18n'
import { Link } from './router'
import { type ThemePref } from './theme'
import { MENUITEM, Menu } from './ui'
import { useSpaces } from './Workspaces'
import { workspaceId } from './workspacePath'

const ghost = 'inline-grid size-8 place-items-center rounded-md bg-transparent p-0 text-tx hover:bg-sk'

export function Header({ onToggle, sidebarExpanded, onSearch, crumbs, theme, resolved, onTheme }: {
  onToggle: () => void; sidebarExpanded: boolean; onSearch: () => void; crumbs: string[]; theme: ThemePref; resolved: 'light' | 'dark'; onTheme: (t: ThemePref) => void
}) {
  const space = useSpaces().data?.find((s) => s.id === workspaceId)?.name
  const mcp = useQuery({ queryKey: ['mcp-status'], queryFn: api.mcpStatus })
  // Green only when an agent has actually connected, as in mcpHealthLabel; "ready to connect" stays neutral.
  const ok = !mcp.isError && mcp.data?.check && mcp.data.online && (mcp.data.last_call_at || mcp.data.token_last_used_at)
  const bad = mcp.isError || (mcp.data?.check && !mcp.data.online)
  const pill = ok ? 'bg-dns text-dnt' : bad ? 'bg-rds text-rd' : 'bg-sk text-t2'
  const label = mcpHealthLabel(mcp.data, mcp.isError)
  const path = space ? [space, ...crumbs] : crumbs
  return (
    <header className="sticky top-0 z-30 flex h-16 shrink-0 items-center gap-3 border-b border-ln bg-bg/80 px-4 backdrop-blur-lg sm:gap-4">
      <button type="button" className="inline-grid size-7 shrink-0 place-items-center rounded-md border border-ln bg-sf p-0 text-tx shadow-xs hover:bg-sk max-md:scale-125" aria-label={tr('Боковая панель')} aria-controls="app-sidebar" aria-expanded={sidebarExpanded} onClick={onToggle}>
        <Icon name="panelL" size={16} />
      </button>
      <span className="h-6 w-px shrink-0 bg-ln" role="separator" aria-orientation="vertical" />
      <nav aria-label={tr('Путь')} className="min-w-0">
        <ol className="flex min-w-0 items-center gap-2 text-sm text-t3">
          {path.map((c, i) => {
            const last = i === path.length - 1
            return (
              <li key={i} className={'flex min-w-0 items-center gap-2 ' + (last ? '' : 'max-sm:hidden')}>
                {i > 0 && <span aria-hidden="true" className="max-sm:hidden">/</span>}
                <span className={'truncate ' + (last ? 'font-medium text-tx' : '')} aria-current={last ? 'page' : undefined}>{c}</span>
              </li>
            )
          })}
        </ol>
      </nav>
      <div className="ml-auto flex shrink-0 items-center gap-2">
        <Link to="/integrations" className={'inline-flex h-7 items-center gap-2 rounded-full px-2.5 text-xs font-medium whitespace-nowrap no-underline ' + pill} title={label}>
          <span className="size-[7px] shrink-0 rounded-full bg-current" aria-hidden="true" />
          <span className="max-sm:sr-only">{label}</span>
        </Link>
        <button type="button" className={ghost + ' min-[900px]:hidden'} aria-label={tr('Поиск')} onClick={onSearch}><Icon name="search" size={17} /></button>
        <Menu right ariaLabel={tr('Тема')} className={ghost} label={<Icon name={resolved === 'dark' ? 'moon' : 'sun'} size={17} />}>
          {(close) => THEMES.map((t) => (
            <button key={t.id} type="button" className={MENUITEM} onClick={() => { onTheme(t.id); close() }}>
              <Icon name={t.icon} size={15} />{tr(t.label)}{theme === t.id && <span className="ml-auto"><Icon name="check" size={14} sw={2.2} /></span>}
            </button>
          ))}
        </Menu>
        <UserMenu variant="header" />
      </div>
    </header>
  )
}
