import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Project, useProfile, useProjects } from './api'
import { AppSettings } from './AppSettings'
import { Board } from './Board'
import { Mcp } from './Mcp'
import { Onboarding } from './Onboarding'
import { Settings } from './Settings'
import { NewProjectDialog, NewTaskDialog } from './Dialogs'
import { useLive } from './live'
import { tr, useLang } from './i18n'
import { Header } from './Header'
import { Link, useRoute } from './router'
import { SearchDialog } from './Search'
import { Sidebar } from './Sidebar'
import { TaskPanel } from './TaskPanel'
import { useTheme } from './theme'
import { Today } from './Today'
import { Trash } from './Trash'
import { EmptyState, btn } from './ui'

type NewTaskOpts = { project?: Project; date?: string; status?: string; title?: string }

function Soon({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="px-8 pt-6 pb-8 flex gap-6 flex-wrap items-start max-[640px]:px-4">
      <div className="flex-[999_1_520px] min-w-0 flex flex-col gap-[18px] bg-sf border border-ln rounded-xl"><EmptyState icon="spark" title={title} hint={hint} action={<Link to="/" className={btn}>{tr('На главную')}</Link>} /></div>
    </div>
  )
}

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
    const on = () => setWide(mq.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  useEffect(() => setOpen(false), [closeOn])
  const toggle = () => {
    if (!wide) return setOpen(!open)
    setCollapsed(!collapsed)
    try { localStorage.setItem('docket-sidebar', collapsed ? 'open' : 'rail') } catch { /* storage can be blocked: the choice just is not remembered */ }
  }
  return { rail: wide && collapsed, open, close: () => setOpen(false), toggle }
}

export function App() {
  const { path, params } = useRoute()
  const qc = useQueryClient()
  useLive(qc)
  useLang() // re-render the whole app when the language changes
  const theme = useTheme()
  const sidebar = useSidebar(path + (params.get('view') ?? ''))
  const projects = useProjects()
  const active = projects.data?.filter((p) => !p.archived_at)
  const [newTask, setNewTask] = useState<NewTaskOpts | null>(null)
  const [newProject, setNewProject] = useState(false)
  const [search, setSearch] = useState(false)
  const [settings, setSettings] = useState(false)
  // Onboarding starts when the profile has no name and stays up (latched) until it finishes, even though
  // saving the name in step 1 makes the profile non-empty.
  const profile = useProfile()
  const [setup, setSetup] = useState(false)
  if (profile.isSuccess && !profile.data.name && !setup) setSetup(true)

  // C opens "new task" unless the user is typing or a dialog is open; Cmd/Ctrl+K opens search.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setSearch(true)
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
  const proj = path.match(/^\/p\/([^/]+)(\/settings)?$/)
  const taskPage = path.match(/^\/t\/([^/]+)$/)
  if (path === '/') page = <Today onNewTask={openNewTask} />
  else if (proj && !proj[2]) page = <Board slug={proj[1]} onNewTask={openNewTask} />
  else if (proj) page = <Settings slug={proj[1]} />
  else if (taskPage) page = <TaskPanel key={taskPage[1]} code={taskPage[1]} mode="page" />
  else if (path === '/mcp') page = <Mcp />
  else if (path === '/trash') page = <Trash />
  else page = <Soon title={tr('Страница не найдена')} hint={path} />

  const task = params.get('task')
  const current = proj ? projects.data?.find((p) => p.slug === proj[1]) : undefined
  const heading = path === '/' ? tr('Сегодня') : path === '/mcp' ? 'MCP' : path === '/trash' ? tr('Корзина') : taskPage ? taskPage[1].toUpperCase()
    : proj ? (current ? current.name + (proj[2] ? `: ${tr('настройки')}` : '') : tr('Проект')) : 'Docket'
  useEffect(() => { document.title = task ? `${task.toUpperCase()} · ${heading} · Docket` : heading === 'Docket' ? 'Docket' : `${heading} · Docket` }, [heading, task])
  if (profile.isPending) return <div className="min-h-screen bg-bg" data-theme={theme.resolved} /> // avoid flashing the app before onboarding
  if (setup) return <div className="min-h-screen bg-bg text-tx" data-theme={theme.resolved}><Onboarding hasProjects={!!active?.length} onDone={() => setSetup(false)} /></div>
  return (
    <div className="min-h-screen flex items-stretch bg-bg text-tx" data-theme={theme.resolved}>
      <Sidebar path={path} view={params.get('view')} rail={sidebar.rail} mobileOpen={sidebar.open} onClose={sidebar.close} onNewProject={() => setNewProject(true)} onSettings={() => setSettings(true)} />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header onToggle={sidebar.toggle} onSearch={() => setSearch(true)} theme={theme.pref} resolved={theme.resolved} onTheme={theme.set} onSettings={() => setSettings(true)} />
        <main className="flex min-w-0 flex-1 flex-col">{page}</main>
      </div>
      {task && <TaskPanel key={task} code={task} mode="drawer" />}
      {search && projects.data && (
        <SearchDialog projects={projects.data} current={current} onClose={() => setSearch(false)} onCreate={(title) => openNewTask({ title, project: current })} />
      )}
      {newTask && active && <NewTaskDialog projects={active} {...newTask} onClose={() => setNewTask(null)} />}
      {newProject && <NewProjectDialog onClose={() => setNewProject(false)} />}
      {settings && <AppSettings theme={theme.pref} onTheme={theme.set} onClose={() => setSettings(false)} />}
    </div>
  )
}
