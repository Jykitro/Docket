import { createFileRoute } from '@tanstack/react-router'
import { Mcp } from '../Mcp'

export const Route = createFileRoute('/_shell/integrations')({ component: Mcp })
