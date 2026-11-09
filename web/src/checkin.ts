/* Check-in composer enhancements: a click-gestured location fix, nearby
   venue ranking, friend filtering and a local photo preview. Every control
   degrades to a working form without this file. Coordinates stay in hidden
   fields for this page only: they are never put in the URL, logged or sent to
   analytics before the member saves the check-in. */

const LOCATION_UNSUPPORTED =
  'This browser cannot share a location. Try a different browser.'
const LOCATION_READY = 'Location attached. Nearby venues are listed first.'
const LOCATION_KEPT =
  'Location ready. Could not update it, so the last location still works for this page.'
const LOCATION_CLEARED = 'Location cleared.'
const VENUE_UNAVAILABLE = 'Could not load venues. The list still works.'

type VenueOption = {
  id: number
  name: string
  city?: string
  distance?: string
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

function venueLabel(venue: VenueOption): string {
  return [venue.name, venue.city, venue.distance].filter(Boolean).join(' · ')
}

function initCheckin(): void {
  const form = document.querySelector<HTMLFormElement>('form[data-checkin-form]')
  if (!form) return

  initFriends(form)

  const locateButton = form.querySelector<HTMLButtonElement>('[data-locate]')
  const clearButton = form.querySelector<HTMLButtonElement>('[data-clear-location]')
  const latField = form.querySelector<HTMLInputElement>('[data-lat]')
  const lngField = form.querySelector<HTMLInputElement>('[data-lng]')
  const locationStatus = form.querySelector<HTMLElement>('[data-location-status]')
  const venueSelect = form.querySelector<HTMLSelectElement>('[data-venue-select]')
  const venueQuery = form.querySelector<HTMLInputElement>('[data-venue-query]')
  const venueStatus = form.querySelector<HTMLElement>('[data-venue-status]')
  const venueForm = document.querySelector<HTMLFormElement>('[data-venue-search]')

  const atHomeField = form.querySelector<HTMLInputElement>('[data-at-home]')
  const modeRadios = Array.from(form.querySelectorAll<HTMLInputElement>('input[name="where_mode"]'))
  const panels = Array.from(form.querySelectorAll<HTMLElement>('[data-where-panel]'))
  const nearbyShow = form.querySelector<HTMLButtonElement>('[data-nearby-show]')
  const nearbyExternal = form.querySelector<HTMLElement>('[data-nearby-external]')
  const nearbyResults = form.querySelector<HTMLUListElement>('[data-nearby-results]')
  const nearbyStatus = form.querySelector<HTMLElement>('[data-nearby-status]')
  const externalSelected = form.querySelector<HTMLElement>('[data-external-selected]')
  const externalFields = {
    provider: form.querySelector<HTMLInputElement>('[data-external-provider]'),
    id: form.querySelector<HTMLInputElement>('[data-external-id]'),
    kind: form.querySelector<HTMLInputElement>('[data-external-kind]'),
    city: form.querySelector<HTMLInputElement>('[data-external-city]'),
    country: form.querySelector<HTMLInputElement>('[data-external-country]'),
  }
  const newName = form.querySelector<HTMLInputElement>('[data-new-venue-name]')
  const newCity = form.querySelector<HTMLInputElement>('[data-new-venue-city]')
  const newCountry = form.querySelector<HTMLInputElement>('[data-new-venue-country]')

  let venueRequest = 0
  let searchTimer: number | undefined
  let externalRequest = 0

  function currentMode(): string {
    return modeRadios.find((radio) => radio.checked)?.value ?? 'search'
  }

  function setLocationStatus(message: string, ready: boolean): void {
    if (locationStatus) locationStatus.textContent = message
    locationStatus?.closest('[data-where-panel]')?.classList.toggle('is-ready', ready)
  }

  function hasCoordinates(): boolean {
    return Boolean(latField?.value && lngField?.value)
  }

  function syncClearButton(): void {
    if (clearButton) clearButton.hidden = !hasCoordinates()
    if (nearbyShow) nearbyShow.hidden = !hasCoordinates()
  }

  function setCoordinates(latitude: number, longitude: number): void {
    // Same three-decimal precision the nearby screen shares.
    if (latField) latField.value = latitude.toFixed(3)
    if (lngField) lngField.value = longitude.toFixed(3)
    syncClearButton()
  }

  function clearExternal(): void {
    for (const field of Object.values(externalFields)) if (field) field.value = ''
    if (externalSelected) {
      externalSelected.hidden = true
      externalSelected.textContent = ''
    }
    if (nearbyExternal) nearbyExternal.hidden = true
    if (nearbyResults) nearbyResults.replaceChildren()
    if (nearbyStatus) nearbyStatus.textContent = ''
  }

  function setMode(mode: string): void {
    for (const panel of panels) panel.hidden = panel.dataset.wherePanel !== mode
    if (atHomeField) atHomeField.value = mode === 'home' ? '1' : ''
    if (mode === 'home') {
      if (venueSelect) venueSelect.value = ''
      clearExternal()
      if (newName) newName.value = ''
    }
    if (mode !== 'nearby') clearExternal()
  }

  function renderVenues(venues: VenueOption[]): void {
    if (!venueSelect) return
    const selected = venueSelect.value
    const previous =
      selected && Array.from(venueSelect.options).find((option) => option.value === selected)

    venueSelect.replaceChildren()
    const none = document.createElement('option')
    none.value = ''
    none.textContent = 'No venue'
    venueSelect.append(none)

    for (const venue of venues) {
      const option = document.createElement('option')
      option.value = String(venue.id)
      option.textContent = venueLabel(venue)
      venueSelect.append(option)
    }

    if (!selected) return
    if (Array.from(venueSelect.options).some((option) => option.value === selected)) {
      venueSelect.value = selected
      return
    }
    // Keep a venue the member already picked even when the new search misses
    // it, rather than silently falling back to "No venue".
    if (previous) {
      const kept = document.createElement('option')
      kept.value = previous.value
      kept.textContent = previous.textContent ?? previous.value
      venueSelect.insertBefore(kept, none.nextSibling)
      venueSelect.value = selected
    }
  }

  async function loadVenues(): Promise<void> {
    if (!venueSelect) return
    const query = venueQuery?.value.trim() ?? ''
    if (query.length === 1) return

    const request = ++venueRequest
    const params = new URLSearchParams()
    if (query.length >= 2) params.set('q', query)
    if (hasCoordinates() && latField && lngField) {
      params.set('lat', latField.value)
      params.set('lng', lngField.value)
    }

    try {
      const response = await fetch(`/checkins/venues?${params.toString()}`, {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      })
      if (!response.ok || request !== venueRequest) return
      const body = (await response.json()) as { venues?: VenueOption[] }
      if (request !== venueRequest) return
      renderVenues(body.venues ?? [])
      if (venueStatus) {
        const count = body.venues?.length ?? 0
        if (query.length >= 2) {
          venueStatus.textContent = count
            ? `Showing ${count} venue${count === 1 ? '' : 's'} for “${query}”.`
            : `No venue matched “${query}”. Add it below.`
        } else if (hasCoordinates()) {
          venueStatus.textContent = count
            ? 'Nearby venues first.'
            : 'No venues with coordinates nearby yet. Add one below.'
        } else {
          venueStatus.textContent = 'Recently added venues.'
        }
      }
    } catch {
      if (venueStatus) venueStatus.textContent = VENUE_UNAVAILABLE
    }
  }

  type ExternalPlace = {
    provider: string
    provider_id: string
    name: string
    kind?: string
    city?: string
    country?: string
    distance?: string
    lat: number
    lng: number
  }

  function renderExternal(places: ExternalPlace[]): void {
    if (!nearbyResults) return
    nearbyResults.replaceChildren()
    if (places.length === 0) {
      if (nearbyStatus) nearbyStatus.textContent = 'No nearby places found. You can still add one manually.'
      return
    }
    for (const place of places) {
      const item = document.createElement('li')
      const button = document.createElement('button')
      button.type = 'button'
      button.className = 'nearby-place'
      button.textContent = [place.name, place.city, place.distance].filter(Boolean).join(' · ')
      button.addEventListener('click', () => {
        if (externalFields.provider) externalFields.provider.value = place.provider
        if (externalFields.id) externalFields.id.value = place.provider_id
        if (externalFields.kind) externalFields.kind.value = place.kind ?? ''
        if (externalFields.city) externalFields.city.value = place.city ?? ''
        if (externalFields.country) externalFields.country.value = place.country ?? ''
        if (newName) newName.value = place.name
        if (newCity) newCity.value = place.city ?? ''
        if (newCountry) newCountry.value = place.country ?? ''
        if (latField) latField.value = place.lat.toFixed(3)
        if (lngField) lngField.value = place.lng.toFixed(3)
        if (venueSelect) venueSelect.value = ''
        if (externalSelected) {
          externalSelected.hidden = false
          externalSelected.textContent = `Selected: ${place.name} (from OpenStreetMap). It will be added when you save.`
        }
        syncClearButton()
      })
      item.append(button)
      nearbyResults.append(item)
    }
  }

  async function showNearby(): Promise<void> {
    if (!hasCoordinates() || !latField || !lngField) {
      if (nearbyStatus) nearbyStatus.textContent = 'Use my location first.'
      return
    }
    const request = ++externalRequest
    if (nearbyStatus) nearbyStatus.textContent = 'Looking for places…'
    try {
      const params = new URLSearchParams({ source: 'external', consent: '1', lat: latField.value, lng: lngField.value })
      const response = await fetch(`/checkins/venues?${params.toString()}`, {
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      })
      if (!response.ok || request !== externalRequest) {
        if (nearbyStatus) nearbyStatus.textContent = 'Nearby places are unavailable right now.'
        return
      }
      const body = (await response.json()) as { external?: ExternalPlace[]; external_status?: string }
      if (request !== externalRequest) return
      if (nearbyExternal) nearbyExternal.hidden = false
      if (body.external_status && body.external_status !== 'ok') {
        if (nearbyStatus) {
          nearbyStatus.textContent =
            body.external_status === 'rate_limited'
              ? 'Too many nearby lookups. Try again in a minute.'
              : 'Nearby places are unavailable right now. You can still search or add a venue.'
        }
      }
      renderExternal(body.external ?? [])
    } catch {
      if (nearbyStatus) nearbyStatus.textContent = 'Nearby places are unavailable right now.'
    }
  }

  function locate(): void {
    if (!navigator.geolocation) {
      setLocationStatus(LOCATION_UNSUPPORTED, false)
      return
    }
    if (locateButton) locateButton.disabled = true
    setLocationStatus('Finding your location…', false)

    try {
      navigator.geolocation.getCurrentPosition(
        (position) => {
          setCoordinates(position.coords.latitude, position.coords.longitude)
          setLocationStatus(LOCATION_READY, true)
          if (locateButton) locateButton.disabled = false
          if (currentMode() === 'nearby') {
            if (nearbyShow) nearbyShow.hidden = false
          } else {
            void loadVenues()
          }
        },
        (error) => {
          if (locateButton) locateButton.disabled = false
          setLocationStatus(hasCoordinates() ? LOCATION_KEPT : locationError(error), hasCoordinates())
        },
        { enableHighAccuracy: false, timeout: 10_000, maximumAge: 0 },
      )
    } catch {
      if (locateButton) locateButton.disabled = false
      setLocationStatus(LOCATION_UNSUPPORTED, false)
    }
  }

  locateButton?.addEventListener('click', locate)

  clearButton?.addEventListener('click', () => {
    if (latField) latField.value = ''
    if (lngField) lngField.value = ''
    syncClearButton()
    setLocationStatus(LOCATION_CLEARED, false)
    clearExternal()
    if (currentMode() !== 'nearby') void loadVenues()
  })

  nearbyShow?.addEventListener('click', () => void showNearby())

  for (const radio of modeRadios) {
    radio.addEventListener('change', () => {
      setMode(currentMode())
      if (currentMode() === 'search' && hasCoordinates()) void loadVenues()
    })
  }

  venueForm?.addEventListener('submit', (event) => {
    event.preventDefault()
    void loadVenues()
  })

  venueQuery?.addEventListener('input', () => {
    window.clearTimeout(searchTimer)
    searchTimer = window.setTimeout(() => void loadVenues(), 300)
  })

  initPhoto(form)
  syncClearButton()
  setMode(currentMode())
}

function initFriends(form: HTMLFormElement): void {
  const picker = form.querySelector<HTMLElement>('[data-friend-picker]')
  if (!picker) return
  const filter = picker.querySelector<HTMLInputElement>('[data-friend-filter]')
  const checks = Array.from(picker.querySelectorAll<HTMLInputElement>('[data-friend-checkbox]'))
  if (checks.length === 0) return
  const count = picker.querySelector<HTMLElement>('[data-friend-count]')
  const limit = Number.parseInt(form.dataset.friendLimit ?? '10', 10) || 10

  function refresh(): void {
    let selected = 0
    for (const checkbox of checks) {
      if (checkbox.checked) selected += 1
      checkbox.closest('label')?.classList.toggle('is-selected', checkbox.checked)
    }
    for (const checkbox of checks) {
      if (!checkbox.checked) checkbox.disabled = selected >= limit
    }
    if (count) {
      count.textContent = selected
        ? `${selected} friend${selected === 1 ? '' : 's'} selected.`
        : ''
    }
  }

  filter?.addEventListener('input', () => {
    const term = filter.value.trim().toLowerCase()
    for (const checkbox of checks) {
      const label = checkbox.closest<HTMLElement>('label')
      const name = label?.dataset.name?.toLowerCase() ?? ''
      const match = !term || name.includes(term)
      checkbox.closest('li')?.toggleAttribute('hidden', !match)
    }
  })

  for (const checkbox of checks) checkbox.addEventListener('change', refresh)
  refresh()
}

function initPhoto(form: HTMLFormElement): void {
  const input = form.querySelector<HTMLInputElement>('[data-photo-input]')
  const preview = form.querySelector<HTMLElement>('[data-photo-preview]')
  const image = form.querySelector<HTMLImageElement>('[data-photo-preview-img]')
  const remove = form.querySelector<HTMLButtonElement>('[data-photo-clear]')
  if (!input || !preview || !image) return

  let objectURL: string | null = null

  function clear(): void {
    if (objectURL) {
      URL.revokeObjectURL(objectURL)
      objectURL = null
    }
    image?.removeAttribute('src')
    preview?.setAttribute('hidden', '')
  }

  input.addEventListener('change', () => {
    clear()
    const file = input.files?.[0]
    if (!file || !file.type.startsWith('image/')) return
    objectURL = URL.createObjectURL(file)
    image.src = objectURL
    preview.hidden = false
  })

  remove?.addEventListener('click', () => {
    input.value = ''
    clear()
  })
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initCheckin, { once: true })
} else {
  initCheckin()
}

export {}
