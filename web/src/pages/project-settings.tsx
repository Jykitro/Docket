import { createFileRoute } from '@tanstack/react-router'
import { Settings } from '../Settings'

export const Route = createFileRoute('/_shell/p/$slug/settings')({ component: () => <Settings slug={Route.useParams().slug} /> })
