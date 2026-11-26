import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_shell/mcp')({ beforeLoad: () => { throw redirect({ to: '/integrations', replace: true }) } })
