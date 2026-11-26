import { createFileRoute } from '@tanstack/react-router'
import { Attention } from '../Attention'

export const Route = createFileRoute('/_shell/attention')({ component: Attention })
