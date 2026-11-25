import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Project, useProfile, useProjects } from './api'
import { Attention } from './Attention'
import { Board } from './Board'
import { Mcp } from './Mcp'
import { Onboarding, isOnboarded } from './Onboarding'
import { Settings } from './Settings'
import { SettingsPage } from './SettingsPage'
import { NewProjectDialog, NewTaskDialog } from './Dialogs'
import { useLive } from './live'
import { tr, useLang } from './i18n'
import { Header } from './Header'
import { useRoute } from './router'
import { SearchDialog } from './Search'
import { Sidebar } from './Sidebar'
import { TaskPanel } from './TaskPanel'
import { useTheme } from './theme'
import { Today } from './Today'
import { Trash } from './Trash'
import { ErrorPage } from './ui'

type NewTaskOpts = { project?: Project; date?: string; status?: string; title?: string }

const WIDE = '(min-width: 900px)'

/** Sidebar state: an icon rail on desktop (remembered), an off-canvas drawer on narrow screens that closes on navigation. */
function useSidebar(closeOn: string) {
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem('docket-sidebar') === 'rail' } catch { return false }
  })
  const [wide, setWide] = useState(() => matchMedia(WIDE).matches)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const mq = matchMedia(WIDE)
    const on = () => { setWide(mq.matches); setOpen(false) }
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  useEffect(() => setOpen(false), [closeOn])
  const toggle = () => {
    if (!wide) return setOpen(!open)
    setCollapsed(!collapsed)
    try { localStorage.setItem('docket-sidebar', collapsed ? 'open' : 'rail') } catch { /* storage can be blocked: the choice just is not remembered */ }
  }
  return { rail: wide && collapsed, expanded: wide ? !collapsed : open, open, close: () => setOpen(false), toggle }
}

export function App() {
  const { path, params } = useRoute()
  const qc = useQueryClient()
  useLive(qc)
  useLang() // re-render the whole app when the language changes
  const theme = useTheme()
  const sidebar = useSidebar(path + '?' + params.toString())
  const projects = useProjects()
  const active = projects.data?.filter((p) => !p.archived_at)
  const [newTask, setNewTask] = useState<NewTaskOpts | null>(null)
  const [newProject, setNewProject] = useState(false)
  const [search, setSearch] = useState(false)
  // Onboarding opens for a workspace with no name yet, or with no projects that has not finished it in this browser.
  // It stays up (latched) until it finishes, even though its own steps fill the name and add a project.
  const profile = useProfile()
  const [setup, setSetup] = useState<'open' | 'done' | null>(null)
  if (setup === null && profile.isSuccess && projects.isSuccess && (!profile.data.name || (!projects.data.length && !isOnboarded()))) setSetup('open')

  // C opens "new task" unless the user is typing or a dialog is open; Cmd/Ctrl+K opens search.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        if (!document.querySelector('[role="dialog"]')) setSearch(true)
        return
      }
      if (e.key.toLowerCase() !== 'c' || e.metaKey || e.ctrlKey || e.altKey) return
      const el = e.target as HTMLElement
      if (el.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]:not([data-drawer])')) return
      e.preventDefault()
      if (active?.length) setNewTask({})
      else setNewProject(true)
    }
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [active?.length])

  const openNewTask = (o: NewTaskOpts) => (active?.length ? setNewTask(o) : setNewProject(true))

  let page
  const proj = path.match(/^\/p\/([^/]+)(?:\/(settings)|\/(wiki)(?:\/(\d+))?)?$/)
  const taskPage = path.match(/^\/t\/([^/]+)$/)
  const settingsPage = path.match(/^\/settings(?:\/([a-z]+))?\/?$/)
  const section = settingsPage ? settingsPage[1] ?? 'profile' : null
  if (path === '/') page = <Today onNewTask={openNewTask} />
  else if (path === '/attention') page = <Attention />
  else if (proj && !proj[2]) page = <Board slug={proj[1]} onNewTask={openNewTask} wiki={proj[3] ? { id: proj[4] ? Number(proj[4]) : undefined } : undefined} />
  else if (proj) page = <Settings slug={proj[1]} />
  else if (taskPage) page = <TaskPanel key={taskPage[1]} code={taskPage[1]} mode="page" />
  else if (path === '/integrations' || path === '/mcp') page = <Mcp /> // /mcp: old bookmarks
  else if (path === '/trash') page = <Trash />
  else if (section) page = <SettingsPage section={section} theme={theme.pref} onTheme={theme.set} />
  else page = <ErrorPage kind="not_found" title={tr('Страница не найдена')} hint={tr('По адресу {path} ничего нет. Возможно, ссылка устарела или страницу удалили.', { path })} />

  const task = params.get('task')
  const current = proj ? projects.data?.find((p) => p.slug === proj[1]) : undefined
  const heading = path === '/attention' ? tr('Требует внимания') : path === '/' ? tr('Сегодня') : path === '/integrations' || path === '/mcp' ? tr('Интеграции') : path === '/trash' ? tr('Корзина') : section ? tr('Настройки') : taskPage ? taskPage[1].toUpperCase()
    : proj ? (current ? current.name + (proj[2] ? `: ${tr('настройки')}` : proj[3] ? `: ${tr('вики')}` : '') : tr('Проект')) : 'Plandillo'
  const crumbs = proj ? [current?.name ?? tr('Проект'), proj[2] ? tr('Настройки') : proj[3] ? tr('Вики') : tr('Доска')] : [heading]
  useEffect(() => { document.title = task ? `${task.toUpperCase()} · ${heading} · Plandillo` : heading === 'Plandillo' ? 'Plandillo' : `${heading} · Plandillo` }, [heading, task])
  if (profile.isPending || projects.isPending) return <div className="min-h-screen bg-bg" data-theme={theme.resolved} /> // avoid flashing the app before onboarding
  if (setup === 'open') return <div className="min-h-screen bg-bg text-tx" data-theme={theme.resolved}><Onboarding hasProjects={!!active?.length} onDone={() => setSetup('done')} /></div>
  return (
    <div className="min-h-screen flex items-stretch bg-bg text-tx" data-theme={theme.resolved}>
      <Sidebar path={path} view={params.get('view')} overdue={path === '/' && params.get('focus') === 'overdue'} settings={section} rail={sidebar.rail} mobileOpen={sidebar.open} onClose={sidebar.close}
        onNewProject={() => { sidebar.close(); setNewProject(true) }} onSearch={() => { sidebar.close(); setSearch(true) }} />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header onToggle={sidebar.toggle} sidebarExpanded={sidebar.expanded} onSearch={() => setSearch(true)} crumbs={crumbs} theme={theme.pref} resolved={theme.resolved} onTheme={theme.set} />
        <main className="flex min-w-0 flex-1 flex-col">{page}</main>
      </div>
      {task && <TaskPanel key={task} code={task} mode="drawer" />}
      {search && projects.data && (
        <SearchDialog projects={projects.data} current={current} onClose={() => setSearch(false)} onCreate={(title) => openNewTask({ title, project: current })} />
      )}
      {newTask && active && <NewTaskDialog projects={active} {...newTask} onClose={() => setNewTask(null)} />}
      {newProject && <NewProjectDialog onClose={() => setNewProject(false)} />}
    </div>
  )
}
