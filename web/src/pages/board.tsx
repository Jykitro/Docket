import { createFileRoute, useMatch } from '@tanstack/react-router'
import { useShell } from '../App'
import { Board } from '../Board'

export const Route = createFileRoute('/_shell/p/$slug')({ component: Page })

function Page() {
  const { slug } = Route.useParams()
  const wiki = useMatch({ from: '/_shell/p/$slug/wiki/{-$pageId}', shouldThrow: false })
  const id = Number(wiki?.params.pageId)
  return <Board slug={slug} onNewTask={useShell().openNewTask} wiki={wiki ? { id: Number.isInteger(id) && id > 0 ? id : undefined } : undefined} />
}
