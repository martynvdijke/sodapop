/* Nearby: ask for a location only from a click, keep it in the form's hidden
   fields for this page, and send it to /nearby. Submit and paging stay on the
   page, so the coordinates never need storage to survive a reload. They are
   never logged, put in the URL, or sent to analytics. */

const NETWORK_ERROR =
  'Could not load nearby pours. Check your connection and try again.'
const SESSION_LOST = 'Your session expired. Reload the page and sign in again.'
const LOCATION_UNSUPPORTED =
  'This browser cannot share a location. Try a different browser.'
const LOCATION_MISSING = 'Set your location first, then show nearby pours.'
const LAST_LOCATION =
  'Location ready. Could not update it, so the last location still works for this page.'

type StatusTone = 'idle' | 'busy' | 'ready' | 'error'

function isLoginRedirect(response: Response): boolean {
  if (!response.redirected) return false
  try {
    return new URL(response.url, location.href).pathname === '/login'
  } catch {
    return false
  }
}

function relativeLabel(date: Date): string {
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour
  const elapsed = Date.now() - date.getTime()

  if (elapsed < minute) return 'Just now'
  if (elapsed < hour) {
    const minutes = Math.floor(elapsed / minute)
    return `${minutes} min ago`
  }
  if (elapsed < day) {
    const hours = Math.floor(elapsed / hour)
    return `${hours} hour${hours === 1 ? '' : 's'} ago`
  }
  if (elapsed < 7 * day) {
    const days = Math.floor(elapsed / day)
    return `${days} day${days === 1 ? '' : 's'} ago`
  }

  const sameYear = date.getFullYear() === new Date().getFullYear()
  return date.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    ...(sameYear ? {} : { year: 'numeric' }),
  })
}

function enhanceTimes(root: ParentNode): void {
  root.querySelectorAll<HTMLTimeElement>('time[data-nearby-time]').forEach((node) => {
    const raw = node.getAttribute('datetime') || ''
    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return
    node.textContent = relativeLabel(date)
    node.title = date.toLocaleString(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short',
    })
  })
}

function locationError(error: GeolocationPositionError): string {
  switch (error.code) {
    case 1:
      return 'Location access was denied. Allow it for this site in your browser settings, then try again.'
    case 2:
      return 'Your location is unavailable right now. Try again in a moment.'
    case 3:
      return 'Finding your location took too long. Try again.'
    default:
      return 'Your location could not be found. Try again.'
  }
}

function initNearby(): void {
  const form = document.querySelector<HTMLFormElement>('[data-nearby-form]')
  if (!form) return
  const nearbyForm: HTMLFormElement = form

  const latField = form.querySelector<HTMLInputElement>('input[name="lat"]')
  const lngField = form.querySelector<HTMLInputElement>('input[name="lng"]')
  const cursorField = form.querySelector<HTMLInputElement>('input[name="cursor"]')
  const radiusSelect = form.querySelector<HTMLSelectElement>('select[name="radius"]')
  const submitButton = form.querySelector<HTMLButtonElement>('[data-nearby-submit]')
  const locateButton = document.querySelector<HTMLButtonElement>('[data-nearby-locate]')
  const locateLabel = locateButton?.querySelector<HTMLElement>('[data-nearby-locate-label]') ?? null
  const status = document.querySelector<HTMLElement>('[data-nearby-status]')
  const statusText = document.querySelector<HTMLElement>('[data-nearby-status-text]')
  const errorBox = document.querySelector<HTMLElement>('[data-nearby-error]')
  const moreButton = document.querySelector<HTMLButtonElement>('[data-nearby-more]')
  const results = document.querySelector<HTMLElement>('[data-nearby-results]')
  const resultStatus = document.querySelector<HTMLElement>('[data-nearby-result-status]')
  const resultsHeading = document.querySelector<HTMLElement>('[data-nearby-results-heading]')

  let busy = false
  let locating = false

  function setStatus(message: string, tone: StatusTone): void {
    if (statusText) statusText.textContent = message
    if (!status) return
    status.classList.toggle('is-ready', tone === 'ready')
    status.classList.toggle('is-busy', tone === 'busy')
    status.classList.toggle('is-error', tone === 'error')
  }

  function setError(message: string | null): void {
    if (!errorBox) return
    errorBox.textContent = message ?? ''
    errorBox.hidden = message === null
  }

  function setBusy(value: boolean): void {
    busy = value
    nearbyForm.setAttribute('aria-busy', value ? 'true' : 'false')
    if (submitButton) submitButton.disabled = value
    if (moreButton) moreButton.disabled = value
    if (locateButton) locateButton.disabled = value
  }

  function hasCoordinates(): boolean {
    return Boolean(latField?.value && lngField?.value)
  }

  function resetPaging(): void {
    if (cursorField) cursorField.value = ''
    if (moreButton) moreButton.hidden = true
  }

  function setCoordinates(latitude: number, longitude: number): void {
    // The service snaps to three decimals before it uses the point. Sending the
    // same resolution avoids sharing more precision than the request needs.
    const lat = latitude.toFixed(3)
    const lng = longitude.toFixed(3)
    if (latField?.value !== lat || lngField?.value !== lng) resetPaging()
    if (latField) latField.value = lat
    if (lngField) lngField.value = lng
  }

  function located(): void {
    setError(null)
    setStatus('Location ready', 'ready')
    if (locateLabel) locateLabel.textContent = 'Update location'
  }

  function locate(): void {
    if (!navigator.geolocation) {
      setStatus(LOCATION_UNSUPPORTED, 'error')
      return
    }

    locating = true
    setError(null)
    setStatus('Finding your location…', 'busy')
    if (locateButton) locateButton.disabled = true
    if (submitButton) submitButton.disabled = true
    if (moreButton) moreButton.disabled = true

    try {
      navigator.geolocation.getCurrentPosition(
        (position) => {
          locating = false
          setCoordinates(position.coords.latitude, position.coords.longitude)
          located()
          if (locateButton) locateButton.disabled = busy
          if (submitButton) submitButton.disabled = busy
          if (moreButton) moreButton.disabled = busy
        },
        (error) => {
          locating = false
          if (locateButton) locateButton.disabled = busy
          if (submitButton) submitButton.disabled = busy
          if (moreButton) moreButton.disabled = busy
          if (hasCoordinates()) {
            setStatus(LAST_LOCATION, 'ready')
            return
          }
          setStatus(locationError(error), 'error')
        },
        { enableHighAccuracy: false, timeout: 10_000, maximumAge: 0 },
      )
    } catch {
      locating = false
      if (locateButton) locateButton.disabled = busy
      if (submitButton) submitButton.disabled = busy
      if (moreButton) moreButton.disabled = busy
      setStatus(LOCATION_UNSUPPORTED, 'error')
    }
  }

  async function load(append: boolean): Promise<void> {
    if (!hasCoordinates()) {
      setError(LOCATION_MISSING)
      setStatus('Location not set', 'idle')
      locateButton?.focus()
      return
    }

    setError(null)
    const previousCursor = cursorField?.value ?? ''
    if (!append && cursorField) cursorField.value = ''
    let succeeded = false
    setBusy(true)
    if (resultStatus) resultStatus.textContent = 'Finding nearby pours.'

    const formData = new FormData(nearbyForm)
    const body = new URLSearchParams()
    formData.forEach((value, key) => {
      if (typeof value === 'string') body.append(key, value)
    })

    try {
      const response = await fetch(nearbyForm.action, {
        method: 'POST',
        body,
        credentials: 'same-origin',
        headers: { Accept: 'text/html' },
      })

      if (response.status === 403 || isLoginRedirect(response)) {
        setError(SESSION_LOST)
        if (resultStatus) resultStatus.textContent = 'Nearby pours did not load.'
        return
      }
      if (!response.ok) throw new Error(`HTTP ${response.status}`)

      const doc = new DOMParser().parseFromString(await response.text(), 'text/html')
      const incomingResults = doc.querySelector<HTMLElement>('[data-nearby-results]')
      if (!incomingResults) throw new Error('missing results')

      const incomingError = doc.querySelector<HTMLElement>('[data-nearby-error]')
      const incomingMessage =
        incomingError && !incomingError.hidden ? (incomingError.textContent ?? '').trim() : ''
      if (incomingMessage) {
        setError(incomingMessage)
        if (resultStatus) resultStatus.textContent = 'Nearby pours did not load.'
        return
      }

      if (results) {
        if (append) {
          incomingResults.querySelectorAll<HTMLElement>('.nearby-card').forEach((card) => {
            results.append(card.cloneNode(true))
          })
        } else {
          results.innerHTML = incomingResults.innerHTML
        }
        enhanceTimes(results)
      }

      const incomingCursor = doc.querySelector<HTMLInputElement>('input[name="cursor"]')
      if (cursorField) cursorField.value = incomingCursor?.value ?? ''

      const incomingMore = doc.querySelector<HTMLElement>('[data-nearby-more]')
      if (moreButton) {
        moreButton.hidden = !incomingMore || incomingMore.hasAttribute('hidden')
      }
      succeeded = true

      const count = results?.querySelectorAll('.nearby-card').length ?? 0
      if (resultStatus) {
        resultStatus.textContent =
          count === 0
            ? 'No visible pours nearby.'
            : `Showing ${count} nearby pour${count === 1 ? '' : 's'}.`
      }

      if (!append) {
        located()
        resultsHeading?.focus()
      }
    } catch {
      setError(NETWORK_ERROR)
      if (resultStatus) resultStatus.textContent = 'Nearby pours did not load.'
    } finally {
      // A failed fresh search keeps the old results, so put their cursor back
      // rather than appending page one again from the More button.
      if (!append && !succeeded && cursorField) cursorField.value = previousCursor
      setBusy(false)
    }
  }

  form.addEventListener('submit', (event) => {
    event.preventDefault()
    if (busy || locating) return
    const submitter = (event as SubmitEvent).submitter
    const append = submitter instanceof HTMLElement && submitter.hasAttribute('data-nearby-more')
    void load(append)
  })

  locateButton?.addEventListener('click', locate)
  radiusSelect?.addEventListener('change', resetPaging)
  enhanceTimes(document)
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initNearby, { once: true })
} else {
  initNearby()
}
