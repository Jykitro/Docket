import { Outlet, createRootRoute } from '@tanstack/react-router'
import { Gate } from '../Auth'
import { ConnectionGate } from '../Pwa'

export const Route = createRootRoute({ component: () => <ConnectionGate><Gate><Outlet /></Gate></ConnectionGate> })
