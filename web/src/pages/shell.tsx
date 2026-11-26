import { createFileRoute } from '@tanstack/react-router'
import { App } from '../App'
import { WorkspaceGate } from '../Workspaces'

export const Route = createFileRoute('/_shell')({ component: () => <WorkspaceGate><App /></WorkspaceGate> })
