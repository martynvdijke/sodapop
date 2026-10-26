/// <reference lib="webworker" />

const CACHE = 'sodapop-v3'
const SHELL = [
  '/',
  '/static/app.css',
  '/static/offline.html',
  '/static/icon-192.png',
  '/static/icon-512.png',
]

const worker = self as unknown as ServiceWorkerGlobalScope

worker.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(SHELL))
      .then(() => worker.skipWaiting()),
  )
})

worker.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key))))
      .then(() => worker.clients.claim()),
  )
})

worker.addEventListener('fetch', (event) => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== worker.location.origin) return

  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((response) => {
          const copy = response.clone()
          void caches.open(CACHE).then((cache) => cache.put(request, copy)).catch(() => {})
          return response
        })
        .catch(async () => {
          const cached = await caches.match(request)
          if (cached) return cached
          const offline = await caches.match('/static/offline.html')
          return offline || new Response('Offline', { status: 503, headers: { 'Content-Type': 'text/plain' } })
        }),
    )
    return
  }

  event.respondWith(
    caches.match(request).then(
      (hit) =>
        hit ||
        fetch(request).then((response) => {
          if (response.ok && url.pathname.startsWith('/static/')) {
            const copy = response.clone()
            void caches.open(CACHE).then((cache) => cache.put(request, copy)).catch(() => {})
          }
          return response
        }),
    ),
  )
})

worker.addEventListener('push', (event) => {
  let payload: { title?: string; body?: string; url?: string } = {}
  try {
    payload = event.data ? (event.data.json() as typeof payload) : payload
  } catch {
    payload = { body: event.data?.text() || '' }
  }
  event.waitUntil(
    worker.registration.showNotification(payload.title || 'Sodapop', {
      body: payload.body || 'Something new from your friends.',
      icon: '/static/icon-192.png',
      badge: '/static/icon-192.png',
      data: { url: payload.url || '/' },
    }),
  )
})

worker.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = (event.notification.data as { url?: string } | undefined)?.url || '/'
  event.waitUntil(
    worker.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if ('focus' in client) {
          void client.navigate(target)
          return client.focus()
        }
      }
      return worker.clients.openWindow(target)
    }),
  )
})
