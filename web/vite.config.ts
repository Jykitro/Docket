import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { routes } from './src/routes.ts'

// Every browser the UI supports loads woff2, so the .woff fallbacks in @fontsource CSS would only bloat the binary.
const woff2Only = {
  name: 'woff2-only',
  enforce: 'pre' as const,
  transform(code: string, id: string) {
    if (/\/@fontsource\/.+\.css$/.test(id)) return code.replace(/,\s*url\([^)]+\.woff\)\s*format\('woff'\)/g, '')
  },
}

// Fills the placeholders in sw.js with the build id and the files to precache, so the app opens offline.
const precacheList = {
  name: 'precache-list',
  apply: 'build' as const,
  writeBundle(options: { dir?: string }) {
    const dir = options.dir as string
    const assets = readdirSync(join(dir, 'assets')).sort()
    // Root static files (icons, logos, manifest) too, or the logo is a broken image offline.
    const root = readdirSync(dir).filter((f: string) => !f.startsWith('.') && f !== 'sw.js' && f !== 'index.html' && statSync(join(dir, f)).isFile()).sort()
    // The shell is cached as `/`: the Go file server may redirect /index.html, and a redirected response cannot answer a navigation.
    const list = ['/', ...root.map((f: string) => `/${f}`), ...assets.map((f: string) => `/assets/${f}`)]
    // The build id covers every precached byte: a change only in public/ or index.html must still install a new worker.
    const hash = createHash('sha256')
    for (const p of list) hash.update(p).update(readFileSync(join(dir, p === '/' ? 'index.html' : p.slice(1))))
    const build = hash.digest('hex').slice(0, 10)
    const file = join(dir, 'sw.js')
    let sw = readFileSync(file, 'utf8')
    for (const p of ['__PLANDILLO_BUILD__', '__PLANDILLO_PRECACHE__']) if (!sw.includes(p)) throw new Error(`sw.js lacks ${p}`)
    sw = sw.replace('__PLANDILLO_BUILD__', build).replace('__PLANDILLO_PRECACHE__', JSON.stringify(list))
    writeFileSync(file, sw)
  },
}

export default defineConfig({
  // The router plugin goes first: it writes src/routeTree.gen.ts and splits every page into its own chunk.
  plugins: [tanstackRouter({ target: 'react', routesDirectory: './src/pages', generatedRouteTree: './src/routeTree.gen.ts', virtualRouteConfig: routes, autoCodeSplitting: true }), woff2Only, react(), tailwindcss(), precacheList],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // `bun run dev` talks to a backend started with `go run ./cmd/docket` (or the container) on :7777.
  server: { proxy: { '^/(?:api/|mcp(?:[/?]|$)|\\.well-known/oauth-|oauth/(?:token|register)$)': { target: 'http://127.0.0.1:7777', changeOrigin: false } } },
})
