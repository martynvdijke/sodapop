/* Analytics helper: the only path through which the page talks to Umami.
 *
 * Rules (mirrored in internal/analytics/events.go and docs/architecture.md §10):
 *   - event names come from the allowlist embedded in
 *     <script id="sodapop-analytics-events"> and are at most 50 characters;
 *   - string property values are truncated to 500 characters;
 *   - numbers are rounded to four decimal places;
 *   - free-text and identifying keys (email, handle, drink name, query, ...)
 *     are dropped before anything leaves the browser;
 *   - every call is a no-op unless Umami is present, which keeps the per-user
 *     opt-out working (the server removes the script and the allowlist node).
 *
 * Barcode scanning: web/src/scanner.ts does not dispatch custom events today,
 * so barcode_scan_started / barcode_scan_result are export-only. When the
 * scanner gains an event (for example 'sodapop:barcode-scan'), hook it here and
 * emit only a coarse result such as {result: 'found'|'not_found'} — never the
 * raw barcode or drink name.
 */

const MAX_NAME_LENGTH = 50
const MAX_STRING_LENGTH = 500
const DENIED_KEYS = new Set([
  'email',
  'handle',
  'username',
  'drink',
  'drink_name',
  'note',
  'text',
  'body',
  'comment',
  'query',
  'search',
  'password',
  'token',
])

let initialized = false
let allowlist: ReadonlySet<string> = new Set()

function readAllowlist(): ReadonlySet<string> {
  try {
    const el = document.getElementById('sodapop-analytics-events')
    if (!el || !el.textContent) return new Set()
    const parsed: unknown = JSON.parse(el.textContent)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((name): name is string => typeof name === 'string'))
  } catch {
    // Fail closed: an unreadable allowlist disables analytics.
    return new Set()
  }
}

function truncate(value: string): string {
  // Array.from splits by code point, matching rune counting on the server.
  if (value.length <= MAX_STRING_LENGTH) return value
  return Array.from(value).slice(0, MAX_STRING_LENGTH).join('')
}

function round(value: number): number {
  const rounded = Math.round(value * 1e4) / 1e4
  return rounded === 0 ? 0 : rounded
}

function sanitize(name: string, data?: Record<string, unknown>): Record<string, unknown> | null {
  if (!allowlist.has(name) || Array.from(name).length > MAX_NAME_LENGTH) return null
  const out: Record<string, unknown> = {}
  if (!data) return out
  for (const [rawKey, value] of Object.entries(data)) {
    const key = rawKey.trim()
    if (!key) continue
    if (DENIED_KEYS.has(key.toLowerCase())) continue
    if (typeof value === 'string') {
      out[key] = truncate(value)
    } else if (typeof value === 'number') {
      if (Number.isFinite(value)) out[key] = round(value)
    } else if (typeof value === 'boolean') {
      out[key] = value
    }
    // Everything else (objects, arrays, null, undefined) is dropped.
  }
  return out
}

type UmamiTracker = { track?: unknown }

/** Emit one allowlisted analytics event. Never throws and never blocks. */
export function track(name: string, data?: Record<string, unknown>): void {
  const props = sanitize(name, data)
  if (props === null) return
  try {
    const umami = (window as unknown as { umami?: UmamiTracker }).umami
    if (!umami || typeof umami.track !== 'function') return
    ;(umami.track as (event: string, payload: Record<string, unknown>) => void)(name, props)
  } catch {
    // Analytics must never break the page.
  }
}

function formPath(form: HTMLFormElement): string {
  const action = form.getAttribute('action')
  if (action === null) return ''
  try {
    return new URL(action, window.location.origin).pathname
  } catch {
    return action
  }
}

function onSubmit(event: Event): void {
  const target = event.target
  if (!(target instanceof HTMLFormElement)) return
  const path = formPath(target)
  if (path === '/search') {
    // No query text: only that a search happened.
    track('search_used')
  } else if (path === '/checkins') {
    track('checkin_created')
  }
}

function onChange(event: Event): void {
  const target = event.target
  if (!(target instanceof Element)) return
  if (target.matches('#theme-select, select[name="theme"], input[name="theme"]')) {
    track('theme_changed')
  }
}

function onReady(): void {
  const venueID =
    document.body?.dataset.venueId ??
    document.querySelector<HTMLElement>('[data-venue-id]')?.getAttribute('data-venue-id')
  if (venueID) track('venue_viewed', { venue_id: venueID })
}

function init(): void {
  if (initialized) return
  initialized = true
  const root = document.documentElement
  const alreadyInitialized = root.dataset.sodapopAnalytics === '1'
  root.dataset.sodapopAnalytics = '1'
  allowlist = readAllowlist()
  if (alreadyInitialized) return

  // Capture phase: the events fire before navigation or offline handling.
  document.addEventListener('submit', onSubmit, true)
  document.addEventListener('change', onChange, true)
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', onReady, { once: true })
  } else {
    onReady()
  }
}

init()
