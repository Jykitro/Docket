import { createFileRoute } from '@tanstack/react-router'
import { OAuthConsent } from '../OAuth'

export const Route = createFileRoute('/oauth/authorize')({ component: OAuthConsent })
