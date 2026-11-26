import { createFileRoute } from '@tanstack/react-router'
import { Trash } from '../Trash'

export const Route = createFileRoute('/_shell/trash')({ component: Trash })
