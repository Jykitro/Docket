// Placeholders are replaced at build time by the precacheList plugin in vite.config.ts.
const BUILD = '__PLANDILLO_BUILD__'
const PRECACHE = __PLANDILLO_PRECACHE__
const cacheName = `plandillo-${BUILD}`
const index = '/index.html'

self.addEventListener('install', event => {
  event.waitUntil(caches.open(cacheName).then(cache => cache.addAll(PRECACHE.map(u => new Request(u, { cache: 'reload' })))).then(() => self.skipWaiting()))
})

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const name of await caches.keys()) {
      if ((name.startsWith('plandillo-') || name.startsWith('docket-offline-')) && name !== cacheName) await caches.delete(name)
    }
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', event => {
  const request = event.request
  const url = new URL(request.url)
  if (request.method !== 'GET' || url.origin !== self.location.origin) return
  if (/^\/(api|mcp|healthz)(\/|$)/.test(url.pathname)) return
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(caches.match(request).then(hit => hit || fetch(request)))
  } else if (request.mode === 'navigate') {
    event.respondWith(fetch(request).catch(async () => (await caches.match(index)) || Response.error()))
  }
})
