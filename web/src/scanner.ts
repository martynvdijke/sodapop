/* Camera barcode scanner: native BarcodeDetector first, lazy ZXing/WASM
   ponyfill second. The manual-entry form works without any of this. */

const DESIRED_FORMATS = [
  'ean_13',
  'ean_8',
  'upc_a',
  'upc_e',
  'gtin_13',
  'gtin_14',
] as const

const COMMON_FORMATS = ['ean_13', 'ean_8', 'upc_a', 'upc_e'] as const

const FRAME_INTERVAL_MS = 100 // ~10 fps, within the 8–12 fps target
const POINT_MESSAGE = 'Point the camera at the barcode.'
const BLOCKED_MESSAGE = 'Camera access was blocked. Type the barcode instead.'
const INVALID_MESSAGE = 'That does not look like a barcode. Try again.'
const UNAVAILABLE_MESSAGE =
  'Camera scanning is not available in this browser. Type the barcode instead.'

type BarcodeDetectorInstance = {
  detect(source: CanvasImageSource): Promise<ReadonlyArray<{ rawValue: string }>>
}

type BarcodeDetectorCtor = {
  new (options?: { formats?: readonly string[] }): BarcodeDetectorInstance
  getSupportedFormats?: () => Promise<readonly string[]>
}

type ScannerElements = {
  root: HTMLElement
  openButton: HTMLElement | null
  closeButton: HTMLElement | null
  video: HTMLVideoElement | null
  status: HTMLElement | null
  torchButton: HTMLButtonElement | null
}

function setStatus(elements: ScannerElements, message: string): void {
  if (elements.status) elements.status.textContent = message
}

function isNotFound(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'name' in error &&
    (error as { name?: unknown }).name === 'NotFoundException'
  )
}

async function supportedFormats(ctor: BarcodeDetectorCtor): Promise<string[]> {
  try {
    const supported = await ctor.getSupportedFormats?.()
    if (supported && supported.length > 0) {
      const filtered = DESIRED_FORMATS.filter((format) =>
        supported.includes(format),
      )
      if (filtered.length > 0) return filtered
    }
  } catch {
    /* fall through to the desired list */
  }
  return [...DESIRED_FORMATS]
}

async function instantiateDetector(
  ctor: BarcodeDetectorCtor,
): Promise<BarcodeDetectorInstance> {
  const formats = await supportedFormats(ctor)
  try {
    return new ctor({ formats })
  } catch {
    // Some native implementations reject unknown or unsupported format names.
    return new ctor({ formats: [...COMMON_FORMATS] })
  }
}

function setupScanner(root: HTMLElement): void {
  const elements: ScannerElements = {
    root,
    openButton:
      root.querySelector<HTMLElement>('[data-scanner-open]') ??
      document.querySelector<HTMLElement>('[data-scanner-open]'),
    closeButton: root.querySelector<HTMLElement>('[data-scanner-close]'),
    video: root.querySelector<HTMLVideoElement>('video[data-scanner-video]'),
    status: root.querySelector<HTMLElement>('[data-scanner-status]'),
    torchButton: root.querySelector<HTMLButtonElement>('[data-scanner-torch]'),
  }

  if (!elements.openButton) return

  let stream: MediaStream | null = null
  let detector: BarcodeDetectorInstance | null = null
  let detectorPromise: Promise<BarcodeDetectorInstance> | null = null
  let frameRequest = 0
  let lastFrame = 0
  let active = false
  let torchOn = false
  let removalObserver: MutationObserver | null = null

  const video = elements.video
  if (video) {
    video.muted = true
    video.playsInline = true
  }
  if (elements.torchButton) elements.torchButton.hidden = true

  function stopTracks(): void {
    if (stream) {
      for (const track of stream.getTracks()) track.stop()
      stream = null
    }
    if (video) video.srcObject = null
  }

  function updateTorch(): void {
    const button = elements.torchButton
    if (!button) return
    const track = stream?.getVideoTracks()[0]
    const capabilities = track?.getCapabilities?.() as
      | (MediaTrackCapabilities & { torch?: boolean })
      | undefined
    if (capabilities?.torch) {
      button.hidden = false
      button.setAttribute('aria-pressed', torchOn ? 'true' : 'false')
    } else {
      button.hidden = true
    }
  }

  async function toggleTorch(): Promise<void> {
    const track = stream?.getVideoTracks()[0]
    if (!track) return
    const next = !torchOn
    try {
      await track.applyConstraints({
        advanced: [{ torch: next } as unknown as MediaTrackConstraintSet],
      })
      torchOn = next
      elements.torchButton?.setAttribute(
        'aria-pressed',
        torchOn ? 'true' : 'false',
      )
    } catch {
      /* torch is best-effort, never a hard requirement */
    }
  }

  function scheduleFrame(): void {
    if (!active || frameRequest !== 0) return
    frameRequest = window.requestAnimationFrame((now) => {
      frameRequest = 0
      void decodeFrame(now)
    })
  }

  async function decodeFrame(now: number): Promise<void> {
    if (!active) return
    if (now - lastFrame < FRAME_INTERVAL_MS) {
      scheduleFrame()
      return
    }
    lastFrame = now

    if (!detector || !video || video.readyState < 2) {
      scheduleFrame()
      return
    }

    try {
      const results = await detector.detect(video)
      if (!active) return
      const value = results[0]?.rawValue.trim()
      if (value) {
        finish(value)
        return
      }
    } catch (error) {
      if (!isNotFound(error)) setStatus(elements, INVALID_MESSAGE)
    }
    scheduleFrame()
  }

  function finish(rawValue: string): void {
    stop()
    window.location.assign('/scan?gtin=' + encodeURIComponent(rawValue))
  }

  function loadDetector(): Promise<BarcodeDetectorInstance> {
    if (detectorPromise) return detectorPromise
    detectorPromise = (async () => {
      const nativeCtor = (
        window as Window & { BarcodeDetector?: BarcodeDetectorCtor }
      ).BarcodeDetector
      if (nativeCtor) return instantiateDetector(nativeCtor)

      // Ponyfill entry: no global side effects, no polyfill of native.
      const module = await import('barcode-detector/ponyfill')
      // Serve the ZXing wasm from our own build instead of the default CDN so
      // the service worker can cache it and CSP only needs `self`.
      const wasm = await import('zxing-wasm/reader/zxing_reader.wasm?url')
      module.prepareZXingModule({
        overrides: {
          locateFile: (path: string) =>
            path.endsWith('.wasm') ? wasm.default : path,
        },
        fireImmediately: false,
      })
      const ponyfillCtor = module.BarcodeDetector as unknown as BarcodeDetectorCtor
      return instantiateDetector(ponyfillCtor)
    })().catch((error: unknown) => {
      detectorPromise = null
      throw error
    })
    return detectorPromise
  }

  function stop(options: { restoreFocus?: boolean } = {}): void {
    if (!active && !stream && root.hidden) return
    active = false
    if (frameRequest !== 0) {
      window.cancelAnimationFrame(frameRequest)
      frameRequest = 0
    }
    stopTracks()
    torchOn = false
    if (elements.torchButton) {
      elements.torchButton.hidden = true
      elements.torchButton.setAttribute('aria-pressed', 'false')
    }
    if (removalObserver) {
      removalObserver.disconnect()
      removalObserver = null
    }
    document.removeEventListener('keydown', onKeydown)
    elements.root.hidden = true
    if (options.restoreFocus !== false) elements.openButton?.focus()
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape') return
    event.preventDefault()
    stop()
  }

  function watchRemoval(): void {
    if (removalObserver) return
    removalObserver = new MutationObserver(() => {
      if (!root.isConnected) stop({ restoreFocus: false })
    })
    removalObserver.observe(document.documentElement, {
      childList: true,
      subtree: true,
    })
  }

  async function open(): Promise<void> {
    if (active) return
    active = true
    elements.root.hidden = false
    setStatus(elements, POINT_MESSAGE)
    if (elements.closeButton) {
      elements.closeButton.focus()
    } else {
      elements.root.tabIndex = -1
      elements.root.focus()
    }
    document.addEventListener('keydown', onKeydown)
    watchRemoval()

    if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
      setStatus(elements, UNAVAILABLE_MESSAGE)
      return
    }

    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: { facingMode: { ideal: 'environment' } },
      })
    } catch (error) {
      const denied =
        typeof error === 'object' &&
        error !== null &&
        'name' in error &&
        (error as { name?: unknown }).name === 'NotAllowedError'
      setStatus(elements, denied ? BLOCKED_MESSAGE : UNAVAILABLE_MESSAGE)
      return
    }

    if (!active) {
      stopTracks()
      return
    }

    if (video) {
      video.srcObject = stream
      try {
        await video.play()
      } catch {
        /* muted inline playback should succeed; decoding will retry frames */
      }
    }

    try {
      detector = await loadDetector()
    } catch {
      setStatus(elements, UNAVAILABLE_MESSAGE)
      stopTracks()
      return
    }
    if (!active) return

    updateTorch()
    lastFrame = 0
    scheduleFrame()
  }

  elements.openButton.addEventListener('click', (event) => {
    event.preventDefault()
    void open()
  })

  elements.closeButton?.addEventListener('click', (event) => {
    event.preventDefault()
    stop()
  })

  elements.torchButton?.addEventListener('click', (event) => {
    event.preventDefault()
    void toggleTorch()
  })

  window.addEventListener('pagehide', () => stop({ restoreFocus: false }))

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      if (frameRequest !== 0) {
        window.cancelAnimationFrame(frameRequest)
        frameRequest = 0
      }
    } else if (active && stream) {
      lastFrame = 0
      scheduleFrame()
    }
  })

  if (root.dataset.scannerAutostart === 'true') {
    // Never request camera permission on load. Reveal the overlay so the
    // start button (and manual entry) are available after the first gesture.
    root.hidden = false
    setStatus(elements, 'Tap the camera button to start.')
  }
}

/** Wire up every scanner overlay on the page. Safe when none exists. */
export function initScanner(): void {
  document
    .querySelectorAll<HTMLElement>('[data-scanner]')
    .forEach((root) => setupScanner(root))
}
