// Only the generic recovery document belongs in Cache Storage.
const cacheName = 'plandillo-offline-v1'
const offlinePage = '/offline.html'

self.addEventListener('install', event => {
  event.waitUntil(caches.open(cacheName).then(cache => cache.add(new Request(offlinePage, { cache: 'reload' }))))
})

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const name of await caches.keys()) {
      if ((name.startsWith('docket-offline-') || name.startsWith('plandillo-offline-')) && name !== cacheName) await caches.delete(name)
    }
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', event => {
  const request = event.request
  const url = new URL(request.url)
  if (request.method !== 'GET' || request.mode !== 'navigate' || url.origin !== self.location.origin) return
  if (/^\/(api|mcp|healthz)(\/|$)/.test(url.pathname)) return
  event.respondWith(fetch(request).catch(async () => {
    const cache = await caches.open(cacheName)
    return (await cache.match(offlinePage)) || Response.error()
  }))
})
