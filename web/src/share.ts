/* Share button: Web Share API when available, clipboard otherwise. */

import { announce } from './notice'

function isAbort(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'name' in error &&
    (error as { name?: unknown }).name === 'AbortError'
  )
}

function legacyCopy(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '0'
  area.style.left = '-9999px'
  document.body.append(area)
  area.select()
  let copied = false
  try {
    copied = document.execCommand('copy')
  } catch {
    copied = false
  }
  area.remove()
  return copied
}

async function copyLink(url: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(url)
      return true
    }
  } catch {
    /* fall through to the legacy path */
  }
  return legacyCopy(url)
}

/** Wire up every `[data-share]` button on the page. Safe when none exists. */
export function initShare(): void {
  document.addEventListener('click', async (event) => {
    const button = (event.target as HTMLElement | null)?.closest<HTMLElement>(
      '[data-share]',
    )
    if (!button) return
    event.preventDefault()

    const url = button.dataset.shareUrl || window.location.href
    const title = button.dataset.shareTitle || document.title
    const text = button.dataset.shareText || ''

    if (typeof navigator.share === 'function') {
      try {
        await navigator.share(text ? { title, text, url } : { title, url })
        return
      } catch (error) {
        // The user closed the sheet: stay quiet. Anything else falls back.
        if (isAbort(error)) return
      }
    }

    if (await copyLink(url)) {
      announce('Link copied')
    } else {
      announce('Could not copy the link')
    }
  })
}
