import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { routes } from './src/routes.ts'

// Every browser the UI supports loads woff2, so the .woff fallbacks in @fontsource CSS would only bloat the binary.
const woff2Only = {
  name: 'woff2-only',
  enforce: 'pre' as const,
  transform(code: string, id: string) {
    if (/\/@fontsource\/.+\.css$/.test(id)) return code.replace(/,\s*url\([^)]+\.woff\)\s*format\('woff'\)/g, '')
  },
}

export default defineConfig({
  // The router plugin goes first: it writes src/routeTree.gen.ts and splits every page into its own chunk.
  plugins: [tanstackRouter({ target: 'react', routesDirectory: './src/pages', generatedRouteTree: './src/routeTree.gen.ts', virtualRouteConfig: routes, autoCodeSplitting: true }), woff2Only, react(), tailwindcss()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // `bun run dev` talks to a backend started with `go run ./cmd/docket` (or the container) on :7777.
  server: { proxy: { '^/(?:api/|mcp(?:[/?]|$)|\\.well-known/oauth-|oauth/(?:token|register)$)': { target: 'http://127.0.0.1:7777', changeOrigin: false } } },
})
