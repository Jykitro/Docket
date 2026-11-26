// Fonts are bundled (no CDN): the app never talks to an outside host.
// 300 and 800 serve the lighter and bolder text weights in Appearance; a file loads only when a page uses it.
import '@fontsource/inter/latin-300.css'
import '@fontsource/inter/latin-400.css'
import '@fontsource/inter/latin-500.css'
import '@fontsource/inter/latin-600.css'
import '@fontsource/inter/latin-700.css'
import '@fontsource/inter/latin-800.css'
import '@fontsource/inter/cyrillic-300.css'
import '@fontsource/inter/cyrillic-400.css'
import '@fontsource/inter/cyrillic-500.css'
import '@fontsource/inter/cyrillic-600.css'
import '@fontsource/inter/cyrillic-700.css'
import '@fontsource/inter/cyrillic-800.css'
import '@fontsource/jetbrains-mono/latin-300.css'
import '@fontsource/jetbrains-mono/latin-400.css'
import '@fontsource/jetbrains-mono/latin-500.css'
import '@fontsource/jetbrains-mono/latin-600.css'
import '@fontsource/jetbrains-mono/latin-700.css'
import '@fontsource/jetbrains-mono/latin-800.css'
import '@fontsource/jetbrains-mono/cyrillic-300.css'
import '@fontsource/jetbrains-mono/cyrillic-400.css'
import '@fontsource/jetbrains-mono/cyrillic-500.css'
import '@fontsource/jetbrains-mono/cyrillic-600.css'
import '@fontsource/jetbrains-mono/cyrillic-700.css'
import '@fontsource/jetbrains-mono/cyrillic-800.css'
import './styles.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryClient } from '@tanstack/react-query'
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client'
import { RouterProvider } from '@tanstack/react-router'
import { CollectionLimit, PagingRequests } from './Paging'
import { workspaceSlug } from './workspacePath'
import { hashKey } from '@tanstack/react-query'
import { adoptRestoredMe, errorKind, offlineStamp } from './api'
import { OFFLINE_MAX_AGE, persistOptions, wipeOfflineCache } from './offlineCache'
import { reportUnsaved, startConnectionWatch } from './connection'
import { router } from './router'

startConnectionWatch()
// Another sign-in's leftovers (a different user on this device) go before anything is restored.
void wipeOfflineCache(offlineStamp())

const client = new QueryClient({
  mutationCache: new MutationCache({ onError: e => { if (errorKind(e) === 'offline') reportUnsaved() } }),
  defaultOptions: { mutations: { networkMode: 'always' }, queries: { gcTime: OFFLINE_MAX_AGE, queryKeyHashFn: key => hashKey([workspaceSlug, ...key]), retry: (failures, error) => failures < 1 && !(error instanceof CollectionLimit) && ['offline', 'server'].includes(errorKind(error)), staleTime: 5_000 } } })

if (import.meta.env.PROD && window.isSecureContext && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/sw.js').catch(err => console.warn('Plandillo service worker', err))
  })
}

// After a deploy an open tab (an installed PWA above all) asks for page chunks that no longer exist: reload onto the new build.
window.addEventListener('vite:preloadError', () => location.reload())

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PersistQueryClientProvider client={client} persistOptions={persistOptions(() => `${offlineStamp()}:${workspaceSlug}`)} onSuccess={() => adoptRestoredMe(client.getQueryData(['me']))}>
      <RouterProvider router={router} />
      <PagingRequests />
    </PersistQueryClientProvider>
  </StrictMode>,
)
