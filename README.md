# Sodapop

Untappd, but for soda. Scan a can or bottle, log where you found it, rate it in quarter-stars, see what your friends are drinking, and build a shared catalog of soft drinks — craft sodas, colas, ginger beers, root beers, sparkling waters, energy drinks, iced teas, and kombuchas.

Self-hosted, single Go binary + SQLite (Ent ORM + versioned migrations), server-rendered HTML with a TypeScript PWA and a native Flutter Android companion app.

## Planning & Specification Docs

| Document | Covers |
|---|---|
| [docs/product.md](docs/product.md) | Positioning, soda taxonomy, user flows for scanning, check-ins, notifications, settings, profiles, insights and drops, barcode & PWA requirements, OIDC, OpenCode AI, integrations, Android app, acceptance criteria |
| [docs/architecture.md](docs/architecture.md) | Go 1.27 + Ent ORM + SQLite migrations, HTTP surface, indexability rules, Home Assistant webhook auth, Authelia OIDC + local email verification, OFF v3.6, OpenCode ("opencode2") AI, OTel, Umami, Docker/CI |
| [docs/data-model.md](docs/data-model.md) | Ent ORM schemas (`ent/schema/`), versioned migration registry (`internal/db/migrations`), `schema_consistency_test.go`, and full resulting SQLite DDL + FTS5 + views |
| [docs/design-direction.md](docs/design-direction.md) | Fresh lime visual system (`#B4E32C` fill, `#16241A` forest ink, `#F2F4E6` cream / `#0C130E` dark), typography, components, scanner overlay, accessibility |
| [docs/mobile.md](docs/mobile.md) | Flutter Android app (`mobile/`): zero-default server onboarding, Authelia OIDC WebView + `sd_` token exchange, `mobile_scanner` ML Kit, offline check-in outbox, APK CI/release |
| [docs/roadmap.md](docs/roadmap.md) | Dependency graph and phases `P0`–`P11` with build lists, exit criteria, and definition of done |

## Highlights

- **Check-ins that keep the moment:** JPEG/PNG/GIF/WebP photos are re-encoded to stripped, resized JPEG with thumbnails; MP4/WebM/QuickTime clips are stored as-is.
- **Taste Twin:** friends with similar palates, scored from overlapping quarter-star ratings.
- **Mix Lab:** publish soda mixes with up to six components and parts, then log them like any other drink.
- **Soda Passport:** styles, venue kinds and countries collected, with recent pours.
- **Badges, lists and Wishlist:** idempotent badge evaluation, curated lists, and a system Wishlist.
- **PWA with a real build:** TypeScript sources compile through Vite to hashed assets; the Go binary serves the manifest-resolved bundle, service worker, offline queue and Web Push.
- **Integrations without noise:** weekly email digest, Web Push, Home Assistant webhook, Matrix room notifications, TRMNL/RSS feed, and CSV/JSON import/export. No Discord.
- **Home Assistant inbound:** `POST /hooks/ha/{token}` accepts a machine check-in by GTIN or slug with an optional quarter-star rating, note, venue and serving; tokens are minted and revoked at `/integrations`, stored hashed, one active per user, and rate limited. `GET /api/v1/integrations/home-assistant` reports wiring state for a sensor.
- **Notifications:** `/notifications` lists friend requests, accepts, toasts, badges and system messages with an unread badge in the nav, per-item and mark-all read, plus `GET /api/v1/notifications` and `POST /api/v1/notifications/read`.
- **Settings that cover the account:** profile and avatar, theme, metric/imperial units, caffeine bedtime, notification channels, default visibility, opt-in public profile, analytics opt-out that removes the Umami script, password change, session list and revoke, sign out everywhere, API tokens, JSON export and soft-delete account closure.
- **Check-in detail:** `/checkins/{id}` and `GET /api/v1/checkins/{id}` render one pour with owner/friend/anonymous visibility, 404 for private rows, and toast or soft-delete actions.
- **Catalog browse:** `/drinks`, `/brands`, `/brands/{slug}`, `/styles` and `/styles/{slug}` with style (including children), brand, flavour, caffeine-free, sugar-free and lifecycle status filters, name/rated/newest sorts, 24 per page. HTML only; there is no collection API.
- **Drops:** drink statuses `limited`, `seasonal` and `discontinued` with `limited_until` and `discontinued_at`, a `/drops` page, an admin status action, and one idempotent wishlist notification when a wishlisted soda is discontinued.
- **Public profiles and insights:** opt-in `/u/{handle}` pages with friend-aware check-in visibility, and `/insights` with sugar/caffeine/calorie totals derived from per-100 ml facts and logged volume, a zero-filled daily series, caffeine clearance at a 5 h half-life and a late-caffeine flag against your bedtime.

- **Barcode-first check-ins:** native `BarcodeDetector` where available plus a self-hosted ZXing-C++ WebAssembly fallback (`barcode-detector`, `zxing_reader.wasm`) on iOS Safari and Firefox, and native CameraX/ML Kit (`mobile_scanner`) on Android; `GET /scan?gtin=` resolves a code, the scanner decodes at about 10 fps with torch when supported and releases the camera on close or tab hide; GTIN-14 normalization; local cache → Open Food Facts v3.6 (ODbL-isolated) → 20-second unknown-soda creation flow.
- **Soda-native taxonomy:** 17 parent style families with substyles, intensity-scored flavour tags, sweetener/acid/carbonation descriptors, caffeine/sugar/calorie facts per 100 ml, and packaging presets.
- **Venues that fit soda:** hotels (minibar & lobby fridge finds), corner shops, cafés, bars, restaurants, festivals, and vending machines, with Nominatim address lookup and 150 m deduplication.
- **Quiet social:** mutual friends, one-way follows, chronological keyset feed, one-tap toast with 5 s undo, personal stats, custom lists, Wishlist, and retroactive-safe badges.
- **Authelia OIDC + verified local accounts:** OIDC Authorization Code + PKCE S256 with RS256 JWKS verification and `admins` group sync, alongside local email+password signup with mandatory email verification (`ALLOW_PASSWORD_FALLBACK`).
- **OpenCode ("opencode2") AI assistant:** optional admin-configured OpenCode Zen (`https://opencode.ai/zen/go/v1`) or OpenAI-compatible endpoints (`ai_endpoints` with AES-GCM encrypted keys, `NormalizeAIBaseURL`, honest `User-Agent: sodapop/<version>` and `x-opencode-session` headers) for conversational/vision drink drafting, check-in tasting-note polish, and Soda Sommelier recommendations.
- **Observability & privacy-first analytics:** OpenTelemetry (`v1.47.0`) traces, metrics (`/metrics`), and correlated JSON logs via OTLP, plus optional self-hosted Umami analytics with an event allowlist and per-user opt-out.

## Stack

- **Backend:** Go 1.27, stdlib `net/http`, `html/template`, pure-Go SQLite (`modernc.org/sqlite`, WAL, FTS5), **Ent ORM (`entgo.io/ent v0.14.6`)** + versioned migration registry (`internal/db/migrations`)
- **Web / PWA:** Vite 8, TypeScript 7, hashed assets served by Go, service worker + offline check-in queue
- **Android:** Flutter (`mobile/`, Material 3, Riverpod, GoRouter, Dio, `mobile_scanner`, `webview_flutter`, `flutter_secure_storage`)
- **Observability & Analytics:** OpenTelemetry (`otelhttp`, `otelslog`, OTLP/Prometheus) + Umami

## License

[MIT](LICENSE)
