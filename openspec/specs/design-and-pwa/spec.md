# Fresh Lime Design System and PWA Offline Queue Specification

## Purpose

Defines Sodapop's fresh lime visual token system (`docs/design-direction.md`), accessibility rules, server-rendered + TypeScript islands architecture, and service-worker PWA offline check-in queue.

## Requirements

### Requirement: Fresh Lime Palette and Contrast Rules
The web and Android interfaces SHALL implement the design tokens defined in `docs/design-direction.md`.

#### Scenario: Lime is used as a fill with deep forest ink, never as text on light backgrounds
- **WHEN** primary interactive elements (such as the lifted Scan button, primary CTAs, and active chips) render in light mode (`#F2F4E6` background, `#FBFBF2` surface)
- **THEN** they use `#B4E32C` fill with `#16241A` text (`10.22:1` contrast)
- **AND** `#B4E32C` is never used as foreground text on cream or white surfaces
- **AND** quarter-star ratings use the tangerine accent (`#E85D26` fill / `#BC440F` text) and `tabular-nums`, displaying `"Not rated"` instead of `0.0`

### Requirement: Service Worker and IndexedDB Offline Check-in Queue
The PWA SHALL provide `manifest.webmanifest`, precache the app shell and fonts (`BricolageGrotesque`), cache the ZXing-C++ WebAssembly barcode reader after first scan, and queue offline check-ins in IndexedDB.

#### Scenario: Offline scan and check-in replay
- **WHEN** a user scans a locally cached barcode and submits a check-in while offline
- **THEN** the check-in is stored in IndexedDB with a client-generated UUID `idempotency_key` and shown with a pending badge
- **AND** when connectivity returns, the queue replays `POST /api/v1/checkins` and reconciles into a single server check-in
