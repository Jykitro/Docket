import { Suspense, createContext, lazy, useContext, useEffect, useRef, useState } from 'react'
import { Outlet } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { type Project, useProfile, useProjects } from './api'
import { Onboarding, isOnboarded } from './Onboarding'
import { NewProjectDialog, NewTaskDialog } from './Dialogs'
import { useLive } from './live'
import { tr, useLang } from './i18n'
import { Header } from './Header'
import { navigate, setParam, useRoute } from './router'
import { SearchDialog } from './Search'
import { Sidebar } from './Sidebar'
import { lastProject, useTheme } from './theme'

// The drawer brings Markdown rendering with it, which the first page does not need.
const TaskPanel = lazy(() => import('./TaskPanel').then((m) => ({ default: m.TaskPanel })))

type NewTaskOpts = { project?: Project; date?: string; status?: string; title?: string }

// What pages need from the shell around them.
const ShellContext = createContext<{ openNewTask: (o: NewTaskOpts) => void; theme: ReturnType<typeof useTheme> } | null>(null)
export const useShell = () => useContext(ShellContext)!

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

  const proj = path.match(/^\/p\/([^/]+)(?:\/(settings)|\/(wiki)(?:\/(\d+))?)?$/)
  const taskPage = path.match(/^\/t\/([^/]+)$/)
  const settingsPage = path.match(/^\/settings(?:\/([a-z]+))?\/?$/)
  const section = settingsPage ? settingsPage[1] ?? 'profile' : null

  const task = params.get('task')
  const current = proj ? projects.data?.find((p) => p.slug === proj[1]) : undefined
  const heading = path === '/attention' ? tr('Требует внимания') : path === '/' ? tr('Сегодня') : path === '/integrations' || path === '/mcp' ? tr('Интеграции') : path === '/trash' ? tr('Корзина') : section ? tr('Настройки') : taskPage ? taskPage[1].toUpperCase()
    : proj ? (current ? current.name + (proj[2] ? `: ${tr('настройки')}` : proj[3] ? `: ${tr('вики')}` : '') : tr('Проект')) : 'Plandillo'
  const crumbs = proj ? [current?.name ?? tr('Проект'), proj[2] ? tr('Настройки') : proj[3] ? tr('Вики') : tr('Доска')] : [heading]
  useEffect(() => { if (proj) lastProject(proj[1]) }, [proj?.[1]])
  // Settings → Appearance → Start page: once per load, an app that opens on Today goes to the chosen page instead.
  const started = useRef(false)
  useEffect(() => {
    if (started.current || !projects.isSuccess) return
    started.current = true
    // Shortcuts from the installed app's icon menu (manifest.webmanifest): ?open=new-task or ?open=today.
    const open = params.get('open')
    if (open) {
      setParam('open', null)
      if (open === 'new-task') openNewTask({})
      return
    }
    if (path !== '/' || params.toString()) return
    const start = document.documentElement.dataset.start
    const last = lastProject()
    if (start === 'attention') navigate('/attention', true)
    else if (start === 'last' && last && active?.some((p) => p.slug === last)) navigate('/p/' + last, true)
  }, [projects.isSuccess])
  useEffect(() => { document.title = task ? `${task.toUpperCase()} · ${heading} · Plandillo` : heading === 'Plandillo' ? 'Plandillo' : `${heading} · Plandillo` }, [heading, task])
  if (profile.isPending || projects.isPending) return <div className="min-h-screen bg-bg" data-theme={theme.resolved} /> // avoid flashing the app before onboarding
  if (setup === 'open') return <div className="min-h-screen bg-bg text-tx" data-theme={theme.resolved}><Onboarding hasProjects={!!active?.length} onDone={() => setSetup('done')} /></div>
  return (
    <div className="min-h-screen flex items-stretch bg-bg text-tx" data-theme={theme.resolved}>
      <Sidebar path={path} view={params.get('view')} overdue={path === '/' && params.get('focus') === 'overdue'} settings={section} rail={sidebar.rail} mobileOpen={sidebar.open} onClose={sidebar.close}
        onNewProject={() => { sidebar.close(); setNewProject(true) }} onSearch={() => { sidebar.close(); setSearch(true) }} />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header onToggle={sidebar.toggle} sidebarExpanded={sidebar.expanded} onSearch={() => setSearch(true)} crumbs={crumbs} theme={theme.pref} resolved={theme.resolved} onTheme={theme.set} />
        <main className="flex min-w-0 flex-1 flex-col"><ShellContext value={{ openNewTask, theme }}><Outlet /></ShellContext></main>
      </div>
      {task && <Suspense fallback={null}><TaskPanel key={task} code={task} mode="drawer" /></Suspense>}
      {search && projects.data && (
        <SearchDialog projects={projects.data} current={current} onClose={() => setSearch(false)} onCreate={(title) => openNewTask({ title, project: current })} />
      )}
      {newTask && active && <NewTaskDialog projects={active} {...newTask} onClose={() => setNewTask(null)} />}
      {newProject && <NewProjectDialog onClose={() => setNewProject(false)} />}
    </div>
  )
}
