import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { routes } from './src/routes.ts'

export default defineConfig({
  // The router plugin goes first: it writes src/routeTree.gen.ts and splits every page into its own chunk.
  plugins: [tanstackRouter({ target: 'react', routesDirectory: './src/pages', generatedRouteTree: './src/routeTree.gen.ts', virtualRouteConfig: routes, autoCodeSplitting: true }), react(), tailwindcss()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // `bun run dev` talks to a backend started with `go run ./cmd/docket` (or the container) on :7777.
  server: { proxy: { '^/(?:api/|mcp(?:[/?]|$)|\\.well-known/oauth-|oauth/(?:token|register)$)': { target: 'http://127.0.0.1:7777', changeOrigin: false } } },
})
