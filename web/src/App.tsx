import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Project, useProjects } from './api'
import { Board } from './Board'
import { Mcp } from './Mcp'
import { Settings } from './Settings'
import { NewProjectDialog, NewTaskDialog } from './Dialogs'
import { useLive } from './live'
import { Link, useRoute } from './router'
import { SearchDialog } from './Search'
import { Sidebar } from './Sidebar'
import { TaskPanel } from './TaskPanel'
import { useTheme } from './theme'
import { Today } from './Today'
import { Trash } from './Trash'
import { EmptyState } from './ui'

type NewTaskOpts = { project?: Project; date?: string; status?: string; title?: string }

function Soon({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="content">
      <div className="col-main panel"><EmptyState icon="spark" title={title} hint={hint} action={<Link to="/" className="btn">На главную</Link>} /></div>
    </div>
  )
}

export function App() {
  const { path, params } = useRoute()
  const qc = useQueryClient()
  useLive(qc)
  const theme = useTheme()
  const projects = useProjects()
  const active = projects.data?.filter((p) => !p.archived_at)
  const [newTask, setNewTask] = useState<NewTaskOpts | null>(null)
  const [newProject, setNewProject] = useState(false)
  const [search, setSearch] = useState(false)

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
  else page = <Soon title="Страница не найдена" hint={path} />

  const task = params.get('task')
  const current = proj ? projects.data?.find((p) => p.slug === proj[1]) : undefined
  const heading = path === '/' ? 'Сегодня' : path === '/mcp' ? 'MCP' : path === '/trash' ? 'Корзина' : taskPage ? taskPage[1].toUpperCase()
    : proj ? (current ? current.name + (proj[2] ? ': настройки' : '') : 'Проект') : 'Docket'
  useEffect(() => { document.title = task ? `${task.toUpperCase()} · ${heading} · Docket` : heading === 'Docket' ? 'Docket' : `${heading} · Docket` }, [heading, task])
  return (
    <div className="app" data-theme={theme.resolved}>
      <Sidebar path={path} view={params.get('view')} theme={theme.pref} onTheme={theme.set} onNewProject={() => setNewProject(true)} onSearch={() => setSearch(true)} />
      <main className="main">{page}</main>
      {task && <TaskPanel key={task} code={task} mode="drawer" />}
      {search && projects.data && (
        <SearchDialog projects={projects.data} current={current} onClose={() => setSearch(false)} onCreate={(title) => openNewTask({ title, project: current })} />
      )}
      {newTask && active && <NewTaskDialog projects={active} {...newTask} onClose={() => setNewTask(null)} />}
      {newProject && <NewProjectDialog onClose={() => setNewProject(false)} />}
    </div>
  )
}
