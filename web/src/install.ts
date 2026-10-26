/* Install prompt card: shown until dismissed, then hidden for 90 days.
   Does not depend on `beforeinstallprompt`. */

const DISMISS_KEY = 'sodapop.installDismissedAt'
const DISMISS_MS = 90 * 24 * 60 * 60 * 1000

function dismissedRecently(): boolean {
  try {
    const raw = localStorage.getItem(DISMISS_KEY)
    if (!raw) return false
    const at = Number(raw)
    if (!Number.isFinite(at)) return false
    return Date.now() - at < DISMISS_MS
  } catch {
    return false
  }
}

/** Wire up the install card. Safe when the card is not on the page. */
export function initInstall(): void {
  const standalone = window.matchMedia('(display-mode: standalone)')
  const cards = (): HTMLElement[] =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-install-card]'))

  const apply = (): void => {
    const hide = standalone.matches || dismissedRecently()
    for (const card of cards()) card.hidden = hide
  }

  apply()

  if (typeof standalone.addEventListener === 'function') {
    standalone.addEventListener('change', apply)
  } else {
    standalone.addListener(apply)
  }
  window.addEventListener('pageshow', apply)

  document.addEventListener('click', (event) => {
    const dismiss = (event.target as HTMLElement | null)?.closest<HTMLElement>(
      '[data-install-dismiss]',
    )
    if (!dismiss) return
    event.preventDefault()
    try {
      localStorage.setItem(DISMISS_KEY, String(Date.now()))
    } catch {
      /* storage may be unavailable in private mode */
    }
    for (const card of cards()) card.hidden = true
  })
}
