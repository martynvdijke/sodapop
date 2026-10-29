/* Live unread notification badge over Server-Sent Events.
 *
 * The server pushes the unread count whenever it changes; this module only
 * updates the server-rendered badge so the first paint stays correct without
 * JavaScript. EventSource reconnects are managed here with exponential backoff
 * because the server closes the stream every 30 minutes on purpose.
 */

const STREAM_URL = '/notifications/stream'
const BASE_DELAY = 1000
const MAX_DELAY = 30000

let initialized = false
let source: EventSource | null = null
let retryTimer: number | null = null
let attempts = 0
let paused = false

function updateBadge(count: number): void {
  const badges = document.querySelectorAll<HTMLElement>('[data-unread-count]')
  for (const badge of badges) {
    badge.textContent = count > 0 ? String(count) : ''
    badge.hidden = count === 0
    const link = badge.closest<HTMLElement>('.nav-notif')
    if (!link) continue
    link.classList.toggle('has-unread', count > 0)

    // Keep the accessible name built from the link's own visible text plus this
    // screen-reader suffix. Setting aria-label here would replace "Notifications"
    // and fail the label-content-name-mismatch accessibility rule.
    let label = link.querySelector<HTMLElement>('[data-unread-label]')
    if (!label) {
      label = document.createElement('span')
      label.className = 'sr-only'
      label.dataset.unreadLabel = ''
      link.appendChild(label)
    }
    label.textContent = count > 0 ? `, ${count} unread` : ''
    label.hidden = count === 0
  }
}

function onUnread(event: Event): void {
  const data = (event as MessageEvent<unknown>).data
  if (typeof data !== 'string') return
  try {
    const parsed: unknown = JSON.parse(data)
    if (typeof parsed !== 'object' || parsed === null) return
    const count = (parsed as { count?: unknown }).count
    if (typeof count !== 'number' || !Number.isFinite(count) || count < 0) return
    updateBadge(Math.floor(count))
  } catch {
    // Ignore malformed frames; the next poll will resend the real count.
  }
}

function closeSource(): void {
  if (!source) return
  source.close()
  source = null
}

function clearRetry(): void {
  if (retryTimer === null) return
  window.clearTimeout(retryTimer)
  retryTimer = null
}

function scheduleReconnect(): void {
  if (paused || retryTimer !== null) return
  const delay = Math.min(BASE_DELAY * 2 ** attempts, MAX_DELAY)
  attempts += 1
  retryTimer = window.setTimeout(() => {
    retryTimer = null
    connect()
  }, delay)
}

function connect(): void {
  if (paused || source) return
  const next = new EventSource(STREAM_URL)
  source = next

  next.addEventListener('open', () => {
    attempts = 0
  })

  next.addEventListener('unread', onUnread)

  // The server sends a named close event before ending a long-lived stream.
  next.addEventListener('close', () => {
    if (source === next) closeSource()
    scheduleReconnect()
  })

  // EventSource retries automatically, which would fight our backoff. Close
  // first, then reconnect on our own schedule.
  next.addEventListener('error', () => {
    if (source === next) closeSource()
    scheduleReconnect()
  })
}

function pause(): void {
  paused = true
  clearRetry()
  closeSource()
}

function resume(): void {
  paused = false
  attempts = 0
  connect()
}

function init(): void {
  if (initialized) return
  if (!document.querySelector('[data-unread-count]')) return
  initialized = true

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      pause()
    } else {
      resume()
    }
  })

  if (document.hidden) {
    paused = true
    return
  }
  connect()
}

init()
