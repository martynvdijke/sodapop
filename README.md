# Sodapop

Untappd, but for soda. Scan a can or bottle, log where you found it, rate it in quarter-stars, see what your friends are drinking, and build a shared catalog of soft drinks — craft sodas, colas, ginger beers, root beers, sparkling waters, energy drinks, iced teas, and kombuchas.

Self-hosted, single Go binary + SQLite (Ent ORM + versioned migrations), server-rendered HTML with a TypeScript PWA and a native Flutter Android companion app.

## v1.11 highlights

Shipped on top of v1.10. The OpenAPI surface is `1.11.0`.

- **Share cards:** `/passport` and `/you` can mint a revocable capability link that renders a public page with Open Graph metadata and a generated 1200×630 PNG summary. Cards are server-rendered from the same passport and streak data, links never appear for anyone else, revocation is immediate for the HTML page, and the PNG is cached publicly for five minutes.
- **Taste-twin guided queue:** `/taste` gains "Drink this next", up to five friend-derived pours with reasons such as "3 taste twins rated it 16+", "New style for you" or "Above your average rating". Candidates respect check-in visibility and friendship, and the queue is derived per request rather than stored.
- **Live notification badge:** the signed-in web client keeps its unread count current through `GET /notifications/stream`, a Server-Sent Events stream that sends the current count immediately, re-checks every 15 seconds, and closes after 30 minutes so the browser reconnects. The Flutter client still refreshes on demand.
- **Delivery history:** `/admin/deliveries` lists outbox rows with status filters, attempt counts, last error and timestamps, and lets an admin retry an abandoned delivery without resetting rows that are still in flight.
- **Native Android push (FCM):** optional and configuration-driven. `POST`/`DELETE /api/v1/me/push/devices` register a device token; when `FCM_SERVICE_ACCOUNT_JSON` and a project id are set, notification fan-out queues one `fcm` delivery per device and the worker exchanges the service-account key for an OAuth2 token before calling FCM HTTP v1. With no credentials the path is a silent no-op, and tokens rejected as unregistered are pruned. The Android app registers when Firebase values are supplied at build time and skips Firebase entirely otherwise.
- **Upgrade notes:** migrations `0012_share_links.sql` and `0013_delivery_history.sql` are additive; `push_devices` is Ent-managed. FCM credentials are optional and should be supplied through the environment or admin settings, never committed.

## v1.10 highlights

Shipped on top of v1.9. The OpenAPI surface is `1.10.0`.

- **Badge levels and new families:** badge rules accept one to three ascending thresholds per counter, and awarding keeps the highest level. New seeded badges cover brands, countries, venue kinds, friends and streaks, and the admin editor round-trips multi-threshold rules.
- **Streaks:** personal stats gain current and longest streak, counted from distinct UTC check-in days, and `GET /api/v1/stats/me` exposes both.
- **Venue offers:** an admin can mark a drink as on offer at a venue from `/admin/venues/{id}`, and the venue page surfaces the flag.
- **Personal activity timeline:** `/you` shows the viewer's own recent activity, backed by `GET /api/v1/me/activity`. There is still no global feed.
- **Analytics allowlist:** browser events are restricted to a fixed, sanitized set of names and properties, and nothing fires when Umami is disabled.

## v1.2 highlights

Shipped on top of v1.1. See [docs/product.md](docs/product.md) for the product surface and [docs/architecture.md](docs/architecture.md) for routes and access rules.

- **Comments on check-ins:** flat threads on any check-in the viewer can see, capped at 1,000 characters and 30 comments per user per hour. Comment visibility inherits the parent check-in, and blocked pairs are filtered from the thread.
- **Venue photos and menus:** verified members can attach up to 6 active photos or menus per venue (20 per venue) with an optional 80-character label; the uploader or a moderator can remove them.
- **Compare sodas:** `/compare` renders 2–6 drinks side by side, including nutrition per 100 ml, container, taxonomy and ratings once the three-rater threshold is met.
- **Richer public profiles:** opted-in `/u/{handle}` pages show earned badges, top styles and visible lists. Unearned badges and their progress stay private.
- **Blocks and safety:** block or unblock from a profile or `/settings/blocked`. Blocking removes friendships and follows in both directions and hides both users from feeds, profiles, comments, toasts and notifications; API list reads also return 404. Cross-user reads stay `404`, never `403`.
- **Drink image galleries:** verified members can add attributed images through the same 12 MiB image pipeline; admins set the primary image and remove images. Open Food Facts images remain proxied and attributed under the existing ODbL boundary.
- **AI draft sessions:** conversational drink drafting persists turns in `ai_draft_sessions` with message, revise and commit endpoints, reusing the same `x-opencode-session` across the conversation.
- **Catalog audit and admin notes:** reports carry internal admin notes, catalog writes keep their revision trail, and runtime settings changes are audited.
- **First-run setup and runtime settings:** a fresh instance prints a one-time `setup required:` token, `/setup` creates the first verified admin, and `/admin/settings` overlays audited runtime values with `default|env|db` source badges.
- **Expanded API and Android:** OpenAPI `1.2.0` added comments, media, lists, mixes, taste, badges/passport, saved venues, follows/blocks and admin notes. The Android app covers check-in detail and comments, other-user profiles with follow/block, notification actions, lists, saved venues, venue search/detail, insights, taste, passport/badges and mixes; video clips still open in the web app.

## v1.9 highlights

Shipped on top of v1.8. The OpenAPI surface is `1.9.0`. This release adds account lifecycle endpoints, a privacy-reviewed nearby feed across web, API and Android, an opt-in public drink fact sheet, and social push coverage.

- **Account lifecycle API:** `GET /api/v1/me/export` returns the same JSON export as `/export.json` as a raw attachment, and `DELETE /api/v1/me` closes the account after echoing the handle in `{"confirm":"<handle>"}`. Deletion soft-deletes the account, removes sessions and revokes API and inbound tokens. The last live admin cannot be deleted (`409 last_admin`), and there is no purge job or handle release.
- **Nearby feed:** signed-in members can search for visible pours around a one-shot location on `/nearby`, or call `GET /api/v1/feed/nearby` with latitude, longitude and a 0.1–25 km radius. Coordinates are used only for the request, snapped before querying, never stored or echoed, and distances are returned in 100 m buckets. The feed respects friendship, follow, visibility and block rules; there is no global or anonymous nearby feed.
- **Public read-only drink API:** an admin can publish a drink, after which `GET /api/v1/public/drinks/{slug}` serves a minimal catalog fact sheet without authentication. Unpublished, provisional, merged, discontinued and unknown slugs all return the same `404`, there is no collection or search route, and ratings appear only once three people have rated the drink.
- **Push coverage:** browser push subscription now enables `push_enabled`, so delivery is not silently blocked. Friend requests, accepts, toasts, comments and mentions dispatch through the same integration/Web Push path; badges and system notices stay in-app. Native Android push remains deferred.
- **Android Nearby and account controls:** the app adds a Nearby screen from Discover, with coarse or fine one-shot location, radius chips and distance buckets, plus account export and closure in Settings. Admin surfaces and native push are still not in the app.

## v1.8 highlights

Shipped on top of v1.7. The OpenAPI surface is `1.6.1`; most of this release closes the Android parity gap and adds the APIs that made it possible.

- **Native Android parity:** drink detail with per-100 ml facts, flavour intensity and a community gallery; native video playback for check-in clips; compare 2–6 sodas; a friends directory with requests; rich check-ins with serving, venue, visibility, a 1,000-character note and camera/gallery photo or short video; venue photo/menu upload; full list CRUD; passport stamps; AI drink drafting from notes or a barcode; saved posts; comment replies, mention links and pagination.
- **Mobile account controls:** a Settings screen for profile, theme, units, email/push preferences, public profile, analytics opt-out, caffeine bedtime, password change, session revoke, blocked people and account counts. Theme changes apply app-wide. Native registration, email-verification status/resend, and password-reset request complete the onboarding flow.
- **Offline reads:** the last friends feed and the last 30 viewed drinks with galleries are cached. When the network fails the app shows the cached copy with a refresh banner, and queued check-ins flush when connectivity returns or the app resumes.
- **API additions:** list create/update/delete and item add/remove, `GET /api/v1/compare`, passport stamps (`totals`, `styles`, `venue_kinds`, `countries`, `recent`), `GET/PATCH /api/v1/me/settings`, session list/revoke, password change, blocked people, public `auth/config`, registration, email verification and resend, password-reset request/confirm, and a viewer-specific `saved` flag on check-in detail.
- **Still web-only:** admin tools, account export/closure, Web Push registration, and the public read-only API. Nearby remains deferred pending privacy review.

## v1.7 highlights

Shipped on top of v1.6. The OpenAPI surface is unchanged at `1.3.0`; this release is operational tooling and HTML admin pages, with no new `/api/v1` routes.

- **Automatic backups and an offline restore:** `internal/backup` takes consistent snapshots with `VACUUM INTO`, writes `sodapop-YYYYMMDD-HHMMSS.db` into `${DATA_DIR}/backups` (default `data/backups`) with mode `0600`, verifies each with a read-only integrity check, and keeps the 14 newest. A snapshot runs at startup and then hourly behind a 24-hour gate and a `backup` lease in `scheduler_state`. Restore is deliberately offline: stop the server and run `go run ./cmd/restore -from <backup> -db <database> -yes`. There is no HTTP restore endpoint.
- **System pane:** `/admin/system` shows version, uptime, Go/OS/arch, database path and size, WAL size, migration version and name, the backup directory, snapshot count and last backup, outbox depth, table counts, scheduler leases and the backup list. `POST /admin/backup` creates a snapshot on demand.
- **Badge management:** `/admin/badges` lists every definition with its award count, creates badges, edits name/description/kind/rule (the slug is permanent) and deletes a definition only when nobody has earned it.
- **Venue merge:** `/admin/venues` searches by venue name, city or known alias, shows the source venue's aliases, and merges a source into a target with cycle, missing and invalid-id errors. The merge records a `venue` catalog revision with `merged_from`/`merged_into`.
- **Digest status:** `/admin/digest` is a read-only view of last run, the next Monday 09:00 UTC run, lease lock, recipient count and pending digest emails, alongside the existing CSRF-protected manual trigger.
- **Starter catalog:** `SEED_CATALOG=1` (`task seed:catalog`) idempotently adds 21 brands and 29 drinks by slug without barcodes or images; uncertain nutrition stays unset, and plastic containers use `pet`.
- **Admin navigation and access:** an admin nav now spans `/admin`, `/admin/settings`, `/admin/badges`, `/admin/venues`, `/admin/digest` and `/admin/system`, and the More menu exposes Admin only to `role=admin`. Moderators keep the `/admin` overview; the new pages are admin-only.

## v1.6 highlights

Shipped on top of v1.5. The OpenAPI surface is now `1.3.0`.

- **Saved check-ins:** bookmark any check-in you can see (including your own), idempotently, from the feed or detail page. `/saved` pages them newest-save-first, and `GET /api/v1/me/saved` returns the same list with `saved: true` and no totals. A bookmark never widens visibility.
- **@mentions:** `@handle` in a comment links to the profile and sends a best-effort mention notification that respects blocks and the parent check-in's visibility.
- **Comment pagination:** threads load oldest-first in keyset pages via `before`, 50 per page by default and 100 maximum, with `has_more`/`next_before` and no total, so long threads are not silently cut off.
- **Report controls:** comments and drink gallery images can be reported through the same moderation queue, and comment/mention notifications deep-link to `/checkins/{id}#comments`.

## Planning & Specification Docs

| Document | Covers |
|---|---|
| [docs/product.md](docs/product.md) | Positioning, soda taxonomy, user flows for scanning, check-ins, nearby pours, notifications, settings, account lifecycle, profiles, insights and drops, public drink facts, barcode & PWA requirements, OIDC, OpenCode AI, integrations, Android app, acceptance criteria |
| [docs/architecture.md](docs/architecture.md) | Go 1.27 + Ent ORM + SQLite migrations, HTTP surface including nearby, account lifecycle and the public drink route, indexability rules, Home Assistant webhook auth, Authelia OIDC + local email verification, OFF v3.6, OpenCode ("opencode2") AI, OTel, Umami, Docker/CI |
| [docs/data-model.md](docs/data-model.md) | Ent ORM schemas (`ent/schema/`), versioned migration registry (`internal/db/migrations.go` plus `internal/db/migrations/*.sql`, through `0010`), `schema_consistency_test.go`, and full resulting SQLite DDL + FTS5 + views |
| [docs/design-direction.md](docs/design-direction.md) | Fresh lime visual system (`#B4E32C` fill, `#16241A` forest ink, `#F2F4E6` cream / `#0C130E` dark), typography, components, scanner overlay, accessibility |
| [docs/mobile.md](docs/mobile.md) | Flutter Android app (`mobile/`): zero-default server onboarding, OIDC WebView + `sd_` token exchange, native registration and reset, `mobile_scanner` ML Kit, Nearby with one-shot location, account export/closure, full member parity, offline check-in outbox and read cache, APK CI/release |
| [docs/roadmap.md](docs/roadmap.md) | Dependency graph and phases `P0`–`P11` with build lists, exit criteria, shipped v1.1–v1.9 status, and definition of done |

## Highlights

- **Check-ins that keep the moment:** JPEG/PNG/GIF/WebP photos are re-encoded to stripped, resized JPEG with thumbnails; MP4/WebM/QuickTime clips are stored as-is.
- **Taste Twin:** friends with similar palates, scored from overlapping quarter-star ratings.
- **Mix Lab:** publish soda mixes with up to six components and parts, then log them like any other drink.
- **Soda Passport:** styles, venue kinds and countries collected, with recent pours.
- **Badges, lists and Wishlist:** idempotent badge evaluation, curated lists, and a system Wishlist.
- **PWA with a real build:** TypeScript sources compile through Vite to hashed assets; the Go binary serves the manifest-resolved bundle, service worker, offline queue and Web Push.
- **Integrations without noise:** weekly email digest, Web Push, Home Assistant webhook, Matrix room notifications, TRMNL/RSS feed, and CSV/JSON import/export. No Discord.
- **Home Assistant inbound:** `POST /hooks/ha/{token}` accepts a machine check-in by GTIN or slug with an optional quarter-star rating, note, venue and serving; tokens are minted and revoked at `/integrations`, stored hashed, one active per user, and rate limited. `GET /api/v1/integrations/home-assistant` reports wiring state for a sensor.
- **Notifications:** `/notifications` lists friend requests, accepts, toasts, comments, badges and system messages with an unread badge in the nav, per-item and mark-all read, plus `GET /api/v1/notifications` and `POST /api/v1/notifications/read`.
- **Settings that cover the account:** profile and avatar, theme, metric/imperial units, caffeine bedtime, notification channels, default visibility, opt-in public profile, analytics opt-out that removes the Umami script, password change, session list and revoke, sign out everywhere, API tokens, JSON export and soft-delete account closure.
- **Check-in detail:** `/checkins/{id}` and `GET /api/v1/checkins/{id}` render one pour with owner/friend/anonymous visibility, 404 for private rows, flat comments, and toast or soft-delete actions.
- **Catalog browse:** `/drinks`, `/brands`, `/brands/{slug}`, `/styles` and `/styles/{slug}` with style (including children), brand, flavour, caffeine-free, sugar-free and lifecycle status filters, name/rated/newest sorts, 24 per page. HTML only; there is no collection API.
- **Drops:** drink statuses `limited`, `seasonal` and `discontinued` with `limited_until` and `discontinued_at`, a `/drops` page, an admin status action, and one idempotent wishlist notification when a wishlisted soda is discontinued.
- **Public profiles and insights:** opt-in `/u/{handle}` pages with friend-aware check-in visibility, and `/insights` with sugar/caffeine/calorie totals derived from per-100 ml facts and logged volume, a zero-filled daily series, caffeine clearance at a 5 h half-life and a late-caffeine flag against your bedtime.

- **Barcode-first check-ins:** native `BarcodeDetector` where available plus a self-hosted ZXing-C++ WebAssembly fallback (`barcode-detector`, `zxing_reader.wasm`) on iOS Safari and Firefox, and native CameraX/ML Kit (`mobile_scanner`) on Android; `GET /scan?gtin=` resolves a code, the scanner decodes at about 10 fps with torch when supported and releases the camera on close or tab hide; GTIN-14 normalization; local cache → Open Food Facts v3.6 (ODbL-isolated) → 20-second unknown-soda creation flow.
- **Soda-native taxonomy:** 17 parent style families with substyles, intensity-scored flavour tags, sweetener/acid/carbonation descriptors, caffeine/sugar/calorie facts per 100 ml, and packaging presets.
- **Venues that fit soda:** hotels (minibar & lobby fridge finds), corner shops, cafés, bars, restaurants, festivals, and vending machines, with Nominatim address lookup and 150 m deduplication.
- **Quiet social:** mutual friends, one-way follows, chronological keyset feed, one-tap toast with 5 s undo, personal stats, custom lists, Wishlist, and retroactive-safe badges.
- **Authelia OIDC + verified local accounts:** OIDC Authorization Code + PKCE S256 with RS256 JWKS verification and `admins` group sync, alongside local email+password signup with mandatory email verification (`ALLOW_PASSWORD_FALLBACK`).
- **First-run setup without a default password:** a fresh instance prints a one-time `setup required:` token (or reads `SETUP_TOKEN`); `/setup` atomically creates the first verified admin, then closes after that admin exists and reopens only when no live admin remains (see the last-admin recovery note in `docs/architecture.md`). No seeded credentials.
- **Admin settings panel:** `/admin/settings` manages site name, SMTP, Umami, registration and password fallback, allowlists and AI, with masked secrets, `default|env|db` source badges and restart labels for startup-only OTel. Values apply from an immutable runtime snapshot; secrets are encrypted with `SESSION_SECRET`.
- **OpenCode ("opencode2") AI assistant:** optional admin-configured OpenCode Zen (`https://opencode.ai/zen/go/v1`) or OpenAI-compatible endpoints (`ai_endpoints` with AES-GCM encrypted keys, `NormalizeAIBaseURL`, honest `User-Agent: sodapop/<version>` and `x-opencode-session` headers) for conversational/vision drink drafting, check-in tasting-note polish, and Soda Sommelier recommendations.
- **Observability & privacy-first analytics:** OpenTelemetry (`v1.47.0`) traces, metrics (`/metrics`), and correlated JSON logs via OTLP, plus optional self-hosted Umami analytics with an event allowlist and per-user opt-out.

## Stack

- **Backend:** Go 1.27, stdlib `net/http`, `html/template`, pure-Go SQLite (`modernc.org/sqlite`, WAL, FTS5), **Ent ORM (`entgo.io/ent v0.14.6`)** + versioned migration registry (`internal/db/migrations`)
- **Web / PWA:** Vite 8, TypeScript 7, hashed assets served by Go, service worker + offline check-in queue
- **Android:** Flutter (`mobile/`, Material 3, Riverpod, GoRouter, Dio, `mobile_scanner`, `webview_flutter`, `flutter_secure_storage`)
- **Observability & Analytics:** OpenTelemetry (`otelhttp`, `otelslog`, OTLP/Prometheus) + Umami

## License

[MIT](LICENSE)
