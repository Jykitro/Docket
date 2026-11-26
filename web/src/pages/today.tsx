import { createFileRoute } from '@tanstack/react-router'
import { useShell } from '../App'
import { Today } from '../Today'

export const Route = createFileRoute('/_shell/')({ component: () => <Today onNewTask={useShell().openNewTask} /> })
