/// <reference lib="webworker" />

const CACHE = 'sodapop-v4'
const MEDIA_CACHE = 'sodapop-media-v1'
const MEDIA_LIMIT = 240
const MEDIA_TRIM = 40
const SHELL = [
  '/',
  '/static/app.css',
  '/static/offline.html',
  '/static/icon-192.png',
  '/static/icon-512.png',
]

const worker = self as unknown as ServiceWorkerGlobalScope

async function storeMedia(request: Request, response: Response): Promise<void> {
  try {
    const cache = await caches.open(MEDIA_CACHE)
    await cache.put(request, response)
    const keys = await cache.keys()
    if (keys.length > MEDIA_LIMIT) {
      const removeCount = Math.max(MEDIA_TRIM, keys.length - MEDIA_LIMIT)
      await Promise.all(keys.slice(0, removeCount).map((key) => cache.delete(key)))
    }
  } catch {
    // A failed cache write must never break media loading.
  }
}

async function handleMedia(request: Request): Promise<Response> {
  let cached: Response | undefined
  try {
    cached = await caches.match(request, { cacheName: MEDIA_CACHE })
  } catch {
    cached = undefined
  }

  if (cached) {
    // Stale-while-revalidate: serve instantly, refresh in the background.
    void fetch(request)
      .then((response) => {
        if (response.ok && response.type === 'basic') return storeMedia(request, response)
        return undefined
      })
      .catch(() => {})
    return cached
  }

  const response = await fetch(request)
  if (response.ok && response.type === 'basic') {
    // Clone synchronously before the response body is handed back.
    const copy = response.clone()
    void storeMedia(request, copy)
  }
  return response
}

async function handleLogout(request: Request): Promise<Response> {
  const response = await fetch(request)
  if (response.ok || (response.status >= 300 && response.status < 400)) {
    try {
      await caches.delete(MEDIA_CACHE)
    } catch {
      // Sign-out must succeed even if the cache cannot be cleared.
    }
  }
  return response
}

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
    (async () => {
      const keys = await caches.keys()
      await Promise.all(
        keys.filter((key) => key !== CACHE && key !== MEDIA_CACHE).map((key) => caches.delete(key)),
      )
      // A new shell version means a new session surface: drop private media.
      await caches.delete(MEDIA_CACHE)
      await worker.clients.claim()
    })(),
  )
})

worker.addEventListener('fetch', (event) => {
  const request = event.request
  const url = new URL(request.url)
  if (url.origin !== worker.location.origin) return

  if (request.method !== 'GET') {
    if (request.method === 'POST' && url.pathname === '/logout') {
      event.respondWith(handleLogout(request))
    }
    return
  }

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

  if (url.pathname.startsWith('/media/')) {
    event.respondWith(handleMedia(request))
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
