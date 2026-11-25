// Fonts are bundled (no CDN): the app never talks to an outside host.
import '@fontsource/inter/latin-400.css'
import '@fontsource/inter/latin-500.css'
import '@fontsource/inter/latin-600.css'
import '@fontsource/inter/latin-700.css'
import '@fontsource/inter/cyrillic-400.css'
import '@fontsource/inter/cyrillic-500.css'
import '@fontsource/inter/cyrillic-600.css'
import '@fontsource/inter/cyrillic-700.css'
import '@fontsource/jetbrains-mono/latin-400.css'
import '@fontsource/jetbrains-mono/latin-500.css'
import '@fontsource/jetbrains-mono/cyrillic-400.css'
import '@fontsource/jetbrains-mono/cyrillic-500.css'
import './styles.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'
import { CollectionLimit, PagingRequests } from './Paging'
import { ConnectionGate } from './Pwa'
import { WorkspaceGate } from './Workspaces'
import { workspaceId } from './workspacePath'
import { hashKey } from '@tanstack/react-query'
import { OAuthConsent } from './OAuth'
import { Gate } from './Auth'

const client = new QueryClient({ defaultOptions: { queries: { queryKeyHashFn: key => hashKey([workspaceId, ...key]), retry: (failures, error) => failures < 1 && !(error instanceof CollectionLimit), staleTime: 5_000 } } })

if (import.meta.env.PROD && window.isSecureContext && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/sw.js').catch(err => console.warn('Plandillo service worker', err))
  })
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <ConnectionGate><Gate>{location.pathname === '/oauth/authorize' ? <OAuthConsent /> : <WorkspaceGate><App /></WorkspaceGate>}</Gate></ConnectionGate>
      <PagingRequests />
    </QueryClientProvider>
  </StrictMode>,
)
