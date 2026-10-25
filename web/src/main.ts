/* Sodapop progressive enhancement: service worker, offline check-ins,
   optional AI polishing and Web Push. Core pages work without this file. */

const QUEUE_KEY = 'sodapop.offline.checkins.v1'

type QueuedCheckin = { data: Record<string, string>; at: number }

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
  if (status) status.hidden = navigator.onLine && count === 0
}

function queueForm(form: HTMLFormElement): void {
  const data: Record<string, string> = {}
  new FormData(form).forEach((value, key) => {
    if (typeof value === 'string') data[key] = value
  })
  const queue = readQueue()
  queue.push({ data, at: Date.now() })
  writeQueue(queue)
}

async function flushQueue(): Promise<void> {
  if (!navigator.onLine) return
  const queue = readQueue()
  if (queue.length === 0) return
  const remaining: QueuedCheckin[] = []
  for (const item of queue) {
    try {
      const response = await fetch('/checkins', {
        method: 'POST',
        credentials: 'same-origin',
        redirect: 'follow',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams(item.data),
      })
      if (!response.ok) remaining.push(item)
    } catch {
      remaining.push(item)
    }
  }
  writeQueue(remaining)
  if (remaining.length === 0) location.reload()
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
  queueForm(form)
  const note = document.getElementById('offline-status')
  if (note) {
    note.hidden = false
    note.textContent = 'Saved on this device. It will sync when you are back online.'
  }
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
