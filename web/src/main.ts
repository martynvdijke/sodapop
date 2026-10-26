/* Sodapop progressive enhancement: service worker, offline check-ins,
   optional AI polishing and Web Push. Core pages work without this file. */

import { initInstall } from './install'
import { announce, isAnnouncing } from './notice'
import { loadPhoto, prunePhotos, removePhoto, savePhoto } from './photo-queue'
import { initScanner } from './scanner'
import { initShare } from './share'

const QUEUE_KEY = 'sodapop.offline.checkins.v1'

type QueuedCheckin = { data: Record<string, string>; at: number; photo?: boolean }

const SAVED_MESSAGE = 'Saved on this device. It will sync when you are back online.'
const PHOTO_SKIPPED_MESSAGE = 'Photo skipped — low storage'
const AUTH_LOST_MESSAGE = 'Sign in to sync your saved check-ins.'
const SYNC_FAILED_MESSAGE = 'A saved check-in could not sync.'

function readQueue(): QueuedCheckin[] {
  try {
    const raw = localStorage.getItem(QUEUE_KEY)
    return raw ? (JSON.parse(raw) as QueuedCheckin[]) : []
  } catch {
    return []
  }
}

function writeQueue(queue: QueuedCheckin[]): void {
  try {
    localStorage.setItem(QUEUE_KEY, JSON.stringify(queue))
  } catch {
    /* storage may be unavailable in private mode */
  }
  renderPending()
}

function renderPending(): void {
  const count = readQueue().length
  document.querySelectorAll<HTMLElement>('[data-pending-count]').forEach((el) => {
    el.textContent = count ? String(count) : ''
    el.hidden = count === 0
  })
  const status = document.getElementById('offline-status')
  if (status && !isAnnouncing()) status.hidden = navigator.onLine && count === 0
}

function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

async function queueForm(form: HTMLFormElement): Promise<void> {
  const data: Record<string, string> = {}
  const files: File[] = []
  new FormData(form).forEach((value, key) => {
    if (typeof value === 'string') {
      data[key] = value
    } else if (value instanceof File && value.size > 0) {
      files.push(value)
    }
  })
  const key = data.idempotency_key || newIdempotencyKey()
  data.idempotency_key = key

  // The text fields always queue; the photo is best-effort.
  const photo = files[0]
  const stored = photo ? await savePhoto(key, photo, photo.name || 'photo') : false
  if (photo && !stored) announce(PHOTO_SKIPPED_MESSAGE, { sticky: true })

  const queue = readQueue()
  queue.push({ data, at: Date.now(), photo: stored })
  writeQueue(queue)
  if (!photo || stored) announce(SAVED_MESSAGE, { sticky: true })
}

function isPermanentFailure(response: Response): boolean {
  return (
    response.status >= 400 &&
    response.status < 500 &&
    response.status !== 408 &&
    response.status !== 429
  )
}

/** A followed redirect to the login page means the session expired. */
function isAuthLoss(response: Response): boolean {
  if (!response.url) return false
  try {
    const finalUrl = new URL(response.url, location.href)
    return (
      finalUrl.origin === location.origin &&
      (finalUrl.pathname === '/login' || finalUrl.pathname === '/login/')
    )
  } catch {
    return false
  }
}

async function submitQueued(item: QueuedCheckin): Promise<Response> {
  const key = item.data.idempotency_key || ''
  const photo = item.photo && key ? await loadPhoto(key) : null
  if (!photo) {
    return fetch('/checkins', {
      method: 'POST',
      credentials: 'same-origin',
      redirect: 'follow',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams(item.data),
    })
  }
  const body = new FormData()
  for (const [name, value] of Object.entries(item.data)) body.append(name, value)
  body.append('media', photo.blob, photo.name || 'photo')
  return fetch('/checkins', {
    method: 'POST',
    credentials: 'same-origin',
    redirect: 'follow',
    body,
  })
}

function queueKeys(queue: QueuedCheckin[]): string[] {
  const keys: string[] = []
  for (const item of queue) {
    const key = item.data.idempotency_key
    if (key) keys.push(key)
  }
  return keys
}

function extractCsrfToken(html: string): string | null {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const input = doc.querySelector<HTMLInputElement>('input[name="csrf_token"]')
  if (input?.value) return input.value
  const meta =
    doc.querySelector<HTMLMetaElement>('meta[name="csrf-token"]') ??
    doc.querySelector<HTMLMetaElement>('meta[name="csrf_token"]')
  return meta?.content || null
}

/** Persist a refreshed token for an item that stays queued after a retry. */
function persistCsrfToken(key: string, token: string): void {
  if (!key) return
  const queue = readQueue()
  let changed = false
  for (const entry of queue) {
    if (
      (entry.data.idempotency_key || '') === key &&
      entry.data.csrf_token !== token
    ) {
      entry.data.csrf_token = token
      changed = true
    }
  }
  if (changed) writeQueue(queue)
}

type CsrfRetry =
  | { status: 'response'; response: Response }
  | { status: 'auth' }
  | { status: 'network' }
  | { status: 'failed' }

/**
 * A 403 usually means the queued item carries a CSRF token from a session that
 * has since rotated. Re-prime once from `/` and replay with the same
 * idempotency key. Network failures stay transient; a token we cannot read is
 * a permanent failure for this item.
 */
async function retryWithFreshCsrf(
  item: QueuedCheckin,
  key: string,
): Promise<CsrfRetry> {
  let html: string
  try {
    const response = await fetch('/', {
      credentials: 'same-origin',
      redirect: 'follow',
    })
    if (isAuthLoss(response)) return { status: 'auth' }
    if (!response.ok) return { status: 'failed' }
    html = await response.text()
  } catch {
    return { status: 'network' }
  }

  const token = extractCsrfToken(html)
  if (!token) return { status: 'failed' }
  item.data.csrf_token = token
  persistCsrfToken(key, token)

  try {
    const response = await submitQueued(item)
    if (isAuthLoss(response)) return { status: 'auth' }
    return { status: 'response', response }
  } catch {
    return { status: 'network' }
  }
}

async function flushQueueOnce(): Promise<void> {
  const snapshot = readQueue()
  // Legacy or tampered entries may lack a key. Assign one before replay so a
  // completed item can actually be dequeued (and the server can dedupe it)
  // instead of being retried forever.
  let patched = false
  for (const entry of snapshot) {
    if (!entry.data.idempotency_key) {
      entry.data.idempotency_key = newIdempotencyKey()
      patched = true
    }
  }
  if (patched) writeQueue(snapshot)
  // Collect photos orphaned by interrupted saves or a lost queue entry.
  await prunePhotos(queueKeys(snapshot))
  if (!navigator.onLine || snapshot.length === 0) return

  // Only items completed in this flush are removed at the end. Re-reading the
  // queue then preserves anything queueForm added while submits were awaiting.
  const handled = new Set<string>()
  let authLost = false
  let dropped = false

  for (const item of snapshot) {
    const key = item.data.idempotency_key || ''
    if (key && handled.has(key)) continue
    try {
      let response = await submitQueued(item)
      if (isAuthLoss(response)) {
        // An expired session redirects the POST to /login and still answers
        // 200. Leave this item and everything after it queued.
        authLost = true
        break
      }
      if (response.status === 403) {
        const retry = await retryWithFreshCsrf(item, key)
        if (retry.status === 'auth') {
          authLost = true
          break
        }
        if (retry.status === 'network') {
          // Could not fetch a fresh token: retry later with item and photo.
          continue
        }
        if (retry.status === 'failed') {
          // Re-priming failed for a non-network reason; do not retry forever.
          if (item.photo && key) await removePhoto(key)
          if (key) handled.add(key)
          dropped = true
          continue
        }
        response = retry.response
        if (isAuthLoss(response)) {
          authLost = true
          break
        }
      }
      if (isPermanentFailure(response)) {
        // Replaying this item can never succeed; drop it and its photo instead
        // of retrying forever.
        if (item.photo && key) await removePhoto(key)
        if (key) handled.add(key)
        dropped = true
        continue
      }
      if (response.ok) {
        // The same idempotency key means the server produced exactly one
        // check-in, so the local photo copy is done.
        if (item.photo && key) await removePhoto(key)
        if (key) handled.add(key)
        continue
      }
      // 408, 429, 5xx and unexpected statuses: leave queued for retry.
    } catch {
      // Network failure: leave queued for retry.
    }
  }

  const next = readQueue().filter((entry) => {
    const key = entry.data.idempotency_key
    return !(key && handled.has(key))
  })
  writeQueue(next)
  // Successful replays leave completed photos behind; purge anything the
  // resulting queue no longer references.
  await prunePhotos(queueKeys(next))

  if (authLost) {
    announce(AUTH_LOST_MESSAGE, { sticky: true })
  } else if (dropped) {
    announce(SYNC_FAILED_MESSAGE, { sticky: true })
  } else if (next.length === 0 && snapshot.length > 0) {
    location.reload()
  }
}

let flushing = false

async function flushQueue(): Promise<void> {
  // A DOMContentLoaded flush and an online event can overlap; run one at a
  // time so an outcome is announced once per flush.
  if (flushing) return
  flushing = true
  try {
    await flushQueueOnce()
  } finally {
    flushing = false
  }
}

async function subscribePush(button: HTMLElement): Promise<void> {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return
  const key = button.dataset.vapidKey || ''
  if (!key) {
    button.textContent = 'Push is not configured'
    return
  }
  button.setAttribute('disabled', 'true')
  try {
    const permission = await Notification.requestPermission()
    if (permission !== 'granted') {
      button.textContent = 'Notifications blocked'
      return
    }
    const registration = await navigator.serviceWorker.ready
    const padding = '='.repeat((4 - (key.length % 4)) % 4)
    const raw = atob((key + padding).replace(/-/g, '+').replace(/_/g, '/'))
    const applicationServerKey = Uint8Array.from(raw, (char) => char.charCodeAt(0))
    const subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey,
    })
    const json = subscription.toJSON()
    const response = await fetch('/push/subscribe', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': button.dataset.csrf || '',
      },
      body: JSON.stringify({
        endpoint: json.endpoint,
        p256dh: json.keys?.p256dh || "",
        auth: json.keys?.auth || "",
      }),
    })
    button.textContent = response.ok ? 'Push enabled' : 'Could not enable push'
  } catch {
    button.textContent = 'Could not enable push'
  } finally {
    button.removeAttribute('disabled')
  }
}

if (
  'serviceWorker' in navigator &&
  (location.protocol === 'https:' ||
    location.hostname === 'localhost' ||
    location.hostname === '127.0.0.1')
) {
  navigator.serviceWorker.register("/static/sw.js").catch(() => {});
}

document.addEventListener('submit', (event) => {
  const form = (event.target as HTMLElement | null)?.closest<HTMLFormElement>(
    'form[data-offline-checkin]',
  )
  if (!form || navigator.onLine) return
  event.preventDefault()
  // FormData is captured synchronously inside queueForm before it yields.
  void queueForm(form)
  form.reset()
})

document.addEventListener('click', async (event) => {
  const target = event.target as HTMLElement | null

  const pushButton = target?.closest<HTMLElement>('[data-push-subscribe]')
  if (pushButton) {
    event.preventDefault()
    await subscribePush(pushButton)
    return
  }

  const draftButton = target?.closest<HTMLButtonElement>('[data-ai-draft]')
  if (draftButton) {
    const form = draftButton.closest('.card')?.nextElementSibling as HTMLFormElement | null
    const notes = draftButton.closest('.card')?.querySelector<HTMLTextAreaElement>('textarea[name="ai_notes"]')
    const status = draftButton.closest('.card')?.querySelector<HTMLElement>('[data-ai-draft-status]')
    if (!form || !notes?.value.trim()) return
    draftButton.disabled = true
    if (status) status.textContent = 'Drafting…'
    try {
      const response = await fetch('/ai/draft', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': draftButton.dataset.csrf || '' },
        body: JSON.stringify({ notes: notes.value, barcode: (form.elements.namedItem('barcode') as HTMLInputElement | null)?.value || '' }),
      })
      if (response.ok) {
        const body = (await response.json()) as { draft?: Record<string, unknown>; message?: string }
        const draft = body.draft || {}
        for (const [key, value] of Object.entries(draft)) {
          if (value === null || value === undefined || Array.isArray(value) || typeof value === 'object') continue
          const field = form.elements.namedItem(key)
          if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement || field instanceof HTMLSelectElement) {
            const text = String(value)
            if (field instanceof HTMLSelectElement) {
              const match = Array.from(field.options).find((option) => option.value === text || option.text.toLowerCase() === text.toLowerCase())
              if (match) field.value = match.value
            } else {
              field.value = text
            }
          }
        }
        if (status) status.textContent = body.message || 'Draft ready. Review before saving.'
      } else if (status) {
        status.textContent = 'The assistant is not configured yet.'
      }
    } catch {
      if (status) status.textContent = 'Could not reach the assistant.'
    }
    draftButton.disabled = false
    return
  }

  const button = target?.closest<HTMLButtonElement>('[data-ai-polish]')
  if (!button) return
  const form = button.closest('form')
  const note = form?.querySelector<HTMLTextAreaElement>('textarea[name="note"]')
  if (!note || !note.value.trim()) return
  const csrf = form?.querySelector<HTMLInputElement>('input[name="csrf_token"]')
  button.disabled = true
  const old = button.textContent
  button.textContent = 'Polishing…'
  try {
    const response = await fetch('/ai/polish', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': csrf?.value || '',
      },
      body: JSON.stringify({ text: note.value }),
    })
    if (response.ok) {
      const body = (await response.json()) as { note?: string }
      if (body.note) note.value = body.note
    }
  } catch {
    /* keep the original note */
  }
  button.disabled = false
  button.textContent = old
})

window.addEventListener('online', () => {
  void flushQueue()
})

document.addEventListener('DOMContentLoaded', () => {
  renderPending()
  void flushQueue()
})

initScanner()
initShare()
initInstall()
