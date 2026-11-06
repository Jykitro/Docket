import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // `bun run dev` talks to a backend started with `go run ./cmd/docket` (or the container) on :7777.
  server: { proxy: { '/api': 'http://localhost:7777' } },
})
