import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // `npm run dev` talks to a backend started with `go run ./cmd/docket` (or the container) on :7777.
  server: { proxy: { '/api': 'http://localhost:7777' } },
})
