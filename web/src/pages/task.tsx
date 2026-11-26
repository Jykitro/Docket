import { createFileRoute } from '@tanstack/react-router'
import { TaskPanel } from '../TaskPanel'

export const Route = createFileRoute('/_shell/t/$code')({ component: Page })

function Page() {
  const { code } = Route.useParams()
  return <TaskPanel key={code} code={code} mode="page" />
}
