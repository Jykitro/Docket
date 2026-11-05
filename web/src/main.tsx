// Fonts are bundled (no CDN): the app never talks to an outside host.
import '@fontsource/onest/latin-400.css'
import '@fontsource/onest/latin-500.css'
import '@fontsource/onest/latin-600.css'
import '@fontsource/onest/latin-700.css'
import '@fontsource/onest/cyrillic-400.css'
import '@fontsource/onest/cyrillic-500.css'
import '@fontsource/onest/cyrillic-600.css'
import '@fontsource/onest/cyrillic-700.css'
import '@fontsource/jetbrains-mono/latin-400.css'
import '@fontsource/jetbrains-mono/latin-500.css'
import '@fontsource/jetbrains-mono/cyrillic-400.css'
import '@fontsource/jetbrains-mono/cyrillic-500.css'
import './styles.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'

const client = new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 5_000 } } })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
