/* One shared announcement region for progressive enhancement. The banner in
   base.html is the only live region; this module never creates another one. */

const TRANSIENT_MS = 4000

let timer = 0

function region(): HTMLElement | null {
  return (
    document.querySelector<HTMLElement>('[data-announcer]') ??
    document.getElementById('offline-status')
  )
}

/** True while a transient message is still on screen. */
export function isAnnouncing(): boolean {
  return timer !== 0
}

/**
 * Show a message in the shared region. `sticky` leaves it in place until it is
 * replaced (used for the offline queue state).
 */
export function announce(
  message: string,
  options: { sticky?: boolean } = {},
): void {
  const el = region()
  if (!el) return
  el.textContent = message
  el.hidden = false
  if (timer !== 0) window.clearTimeout(timer)
  timer = 0
  if (options.sticky) return
  timer = window.setTimeout(() => {
    timer = 0
    if (el.textContent === message) el.hidden = true
  }, TRANSIENT_MS)
}
