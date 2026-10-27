# Sodapop — Roadmap

Twelve phases, dependency-ordered. Estimates assume one experienced Go developer working alone, part-time at ~30 h/week. Each phase ends with a demoable slice, tests, and a green CI run — not a branch full of scaffolding.

Status: P0–P11 shipped for v1.0. v1.1–v1.5 have shipped on top of that baseline, including the v1.3 Phase B–D surfaces, the v1.4 Phase E work and the v1.5 Google Play release. The v1.6 post-interactions slice is recorded below. The build lists below remain the original plan; work delivered inside a phase is recorded here rather than by adding new phases.

| Shipped surface | Phase |
|---|---|
| `/scan?gtin=`, native `BarcodeDetector` + self-hosted ZXing-C++ wasm, wasm CSP/MIME | P3 |
| Catalog browse `/drinks`, `/brands`, `/brands/{slug}`, `/styles`, `/styles/{slug}` with filters, sorts, 24/page | P2 |
| Drink lifecycle `limited`/`seasonal`/`discontinued`, `/drops`, admin status action, wishlist notification | P2 |
| Notification centre `/notifications`, unread nav badge, per-item and mark-all read, notifications API | P5 |
| Check-in detail `/checkins/{id}` with toast and soft-delete, plus `GET /api/v1/checkins/{id}` | P4/P5 |
| Settings `/settings` covering profile, preferences, notifications, privacy, security and data; password reset end to end | P1/P9 |
| Opt-in public profiles `/u/{handle}` with friend-aware visibility and indexability rules | P7/P9 |
| Insights `/insights` and `GET /api/v1/stats/me/insights` with `range=7`, `30` or `90` | P4 |
| Home Assistant inbound webhook and `/integrations` token management; TRMNL dispatch; `push_enabled` gating | P9/P10 |
| Comments and venue photos/menus, with block enforcement and `/settings/blocked` | v1.1/v1.2 |
| `/compare`, drink galleries, AI draft sessions, report admin notes, first-run `/setup` and `/admin/settings` | v1.2 |
| Lists, mixes, taste, badges/passport and saved-venue HTML and API surfaces | v1.2 |
| Flutter social screens: check-in comments, user profile follow/block, notifications, lists, saved venues | v1.2 |
| Flutter insights, taste, passport/badges, mixes and venue search/detail; photo upload and offline photo parity | v1.2 |
| Post interactions: saved check-ins and `/saved`, `@mentions` with notifications, comment pagination, comment/gallery report controls | v1.6 |

## Shipped in v1.4–v1.6

Phase E is shipped as of v1.4.0, so there is no open phase: durable delivery (`delivery_outbox` plus migration `0008_delivery_outbox.sql`), digest scheduler catch-up with a single-instance lease, the OTel follow-up metrics and DB spans, unified search that still caps at 10 results with no totals, and the `registration_open` admin toggle all landed. The v1.6 post-interactions slice shipped saved check-ins (`/saved`, `/api/v1/me/saved`), `@handle` mentions with mention notifications and deep links, keyset comment pagination (50 default, 100 maximum, no total), and report controls for comments and drink gallery images.

## Next

- Nearby feed: still deferred because location data stays venue-only and any geo feed needs a privacy review first.
- Backup/restore job: the admin backup path does not use `VACUUM INTO` yet, so there is no consistent online snapshot.
- Starter catalog seed: first-run usefulness still depends on the community or OFF lookups.
- Admin badge management: badge definitions are seeded, not editable in the admin UI.
- Public read-only API: deliberately deferred to keep `/api/v1` friend-scoped and non-enumerable.
- PWA offline photos: the web offline queue covers check-in fields, not photo bytes.
- Mobile parity gaps: video capture, AI drafting, push registration and list CRUD are still web-only.


## Dependency graph

```
P0 Foundations
 ├── P1 Auth + OIDC + email ────────┐
 ├── P2 Catalog ── P3 Barcode ──────┤
 │                                  ├── P4 Check-ins ── P5 Social ── P7 Lists/badges/profile
 └── P6 Venues ─────────────────────┘        │                 │
                                             ├── P8 OpenCode AI ("opencode2")
                                             ├── P9 PWA/offline/analytics
                                             ├── P10 Flutter Android app (mobile/)
                                             └── P11 Hardening/launch
```

P1 and P2 can run in parallel once P0 lands. P3 needs P2's catalog and a barcode table but not P1's email gate. P4 needs P1 (identity) and P2/P3 (drink resolution). P6 is independent of P4 but its venue check-in needs P4. P8 (OpenCode AI) plugs into P2 (drink drafting) and P4 (tasting-note polish & recommendations). P9 and P10 build against the stable `/api/v1` surface from P1–P8.

## P0 — Foundations (1 week)

**Build**
- `go.mod` (module `sodapop`, Go 1.27, `modernc.org/sqlite`, `entgo.io/ent v0.14.6`), `cmd/server`, typed `internal/config` with `Validate()` and redacted `LogValue`.
- `ent/` + `internal/db`: open `modernc.org/sqlite` with WAL/busy_timeout/foreign_keys/synchronous/cache_size/mmap_size, attach Ent driver (`entsql.OpenDB(dialect.SQLite, DB)`), run `Client.Schema.Create(ctx)` + versioned migration registry (`internal/db/migrations.go` recording `schema_migrations`), `TraceQueryEx` OTel span + slow-query `EXPLAIN QUERY PLAN`, `OpenMemory` for tests, and `schema_consistency_test.go` mechanical guard.
- `internal/telemetry`: OTel providers, `OTEL_SDK_DISABLED` and sampler env handling, `otelhttp` handler, `otelslog` fanout, Prometheus exporter, graceful 10 s flush.
- `internal/web`: stdlib mux, middleware chain, render helper (`base.html` + partials), embedded assets with Vite manifest, security headers, `/healthz`, `/readyz`, `/metrics`.
- Design tokens: copy `docs/design-direction.md` §2.4 into `web/src/styles/tokens.css` verbatim; Tailwind theme maps 1:1 to tokens.
- Vite 8 + TS 7 + Tailwind 4 pipeline; `npm run build` before `go build`; hashed assets.
- Dockerfile (3-stage, non-root), compose with optional `observability` profile and collector config.
- CI: gofmt, vet, `go test -race`, typecheck, Vitest, Playwright smoke, actionlint/zizmor/pinact, Renovate, semantic-release.
- `AGENTS.md` capturing the rules from `architecture.md` §2 and the design doc handoff notes.

**Exit criteria**
- `docker compose up` serves a themed placeholder page with the lime palette in light and dark.
- `/healthz` 200, `/readyz` checks DB, `/metrics` exposes Go runtime metrics.
- With `OTEL_EXPORTER_OTLP_ENDPOINT` unset, no outbound telemetry and no startup error; with the collector profile, spans and logs arrive.
- `go test ./...` passes, CI green, image builds under 30 MB.

## P1 — Accounts, sessions, email verification (2 weeks)

**Build**
- `users`, `user_identities`, `sessions`, `api_tokens`, `email_verification_tokens`, `password_reset_tokens`, `email_outbox`, `user_settings`, `login_attempts`.
- Signup with handle uniqueness and reserved list, bcrypt cost 12, restricted session until verified.
- Verification token issuance, resend throttling, `/verify` handler, expiry and reuse errors.
- Login (email or handle), session rotation, logout, logout-everywhere, session list + revoke.
- Password reset with anti-enumeration and session invalidation.
- SMTP sender with direct TLS / STARTTLS / plain, HTML+text templates, dev mail sink in `data/mail/`.
- Email outbox worker with exponential backoff and `/admin/email` visibility.
- CSRF tokens per session and Origin checks; login/signup/reset rate limits.
- First-class Authelia OIDC (`OIDC_ENABLED`, `.well-known/openid-configuration` discovery, PKCE S256, HMAC-signed state/nonce cookie, RS256 JWKS verification, `email_verified=true` gate, auto-link/provision by email, `admins` group → `admin` role sync, `ALLOWED_EMAILS`/`ALLOWED_DOMAINS`, `ALLOW_PASSWORD_FALLBACK`, `/api/v1/auth/oidc/status`, and `/api/v1/auth/mobile-token`).
- Profile settings: display name, handle, bio, avatar upload stub, default visibility.

**Exit criteria**
- Signup → verify → login → logout works end-to-end against a local mail sink.
- OIDC login → callback against a mock OIDC/JWKS provider provisions a verified user, maps the `admins` group to `admin`, and issues `sd_session`.
- Unverified account is blocked from writes with `email_not_verified`; verified account is not.
- Reset token is single-use, expires in 1 h, and kills all sessions.
- CSRF rejection verified by a test that posts without a token.
- axe passes on login, signup, verify, reset pages.

## P2 — Catalog, styles, search (2 weeks)

**Build**
- `brands`, `styles`, `flavours`, `drinks`, `drink_barcodes`, `drink_aliases`, `drink_images`, `drink_flavours`, `catalog_revisions`, `drinks_fts`.
- Seed command: full style tree (17 families), flavour list, a small demo set for e2e only.
- Drink CRUD (moderator-gated for edits of verified rows), brand CRUD, style browsing.
- FTS5 index refreshed transactionally on every write; search endpoint with grouping and ranking (name > brand > alias > style).
- Drink page, brand page, style page with aggregates from `drink_ratings`.
- Filters and sorts with URL state; pagination.
- Image proxy + cache for OFF images; local uploads for community images later.
- Slug generation with collision handling; merged-drink redirects.

**Exit criteria**
- Searching “coke”, “coca”, “cola” and “café” (diacritics) returns the expected drink.
- Creating a drink with a duplicate slug returns a readable conflict, not a 500.
- FTS stays in sync after update, merge, and delete (test asserts row counts).
- Style pages render counts and top drinks from real data.
- Seeded database loads in < 2 s.

## P3 — Barcode scanning and Open Food Facts (2 weeks)

**Build**
- Scanner UI per design doc §5.4: overlay, viewfinder, torch (feature-detected), manual entry, permission states, success/no-match panels.
- Decoder: native `BarcodeDetector` first; lazy worker ponyfill (`barcode-detector` + zxing-wasm) fallback; ROI crop; 8–12 fps; stop on hide; release tracks.
- GTIN normalization to 14 digits, check-digit validation, UPC-A/UPC-E mapping.
- `GET /api/v1/barcodes/{gtin}` resolution ladder with local cache and TTLs (positive 30 d, negative 7 d).
- Open Food Facts client: v3.6 product endpoint, mandatory contact User-Agent, 1 req/s global spacing, 15/min budget guard, retry/backoff, 8 MiB cap, OTel span.
- ODbL boundary: `off_products` staging table plus `drinks.off_id`; attribution text on any OFF-derived page; no blending of OFF and user data within a row.
- Unknown-barcode create flow with soft-key dedupe prompt and provisional create.
- `cmd/fakeoff` server and fixtures for tests/e2e.

**Exit criteria**
- On a mid-range Android phone, a can barcode resolves in < 2.5 s median in good light.
- iOS Safari (no BarcodeDetector) uses the wasm worker and still resolves.
- Denied permission shows manual entry, and manual entry creates the same result as a scan.
- OFF outage returns a graceful “couldn't reach catalog” state and still allows creation.
- Rate-limit guard never exceeds 15 requests/minute in a test that hammers the resolver.

## P4 — Check-ins, ratings, photos, stats (2 weeks)

**Build**
- `media`, `checkins`, `drink_ratings` view, soft delete.
- Check-in sheet per design doc §5.3, with rating slider (quarter steps, keyboard, ARIA), serving, volume, venue stub, date, photo, notes, visibility.
- Server-side validation and idempotency keys; duplicate-submit protection; activity row in the same transaction.
- Photo pipeline: multipart cap, MIME sniff + re-decode, resize 1600 px, WebP/JPEG, EXIF strip, deterministic placeholder.
- Drink page personal history, friend ratings, average with `MIN_RATERS_FOR_AVG=3`, Bayesian top-rated list.
- Profile stats and personal stats page; streak computation; money spent when prices exist.
- Edit and delete flows with confirmation; recompute on read.

**Exit criteria**
- 0, 0.25, 4.25 and 5.0 ratings round-trip exactly through DB, API, and template.
- An unrated check-in is valid and excluded from averages.
- Private check-in is invisible to a friend via API and HTML (404 on direct access).
- Replaying the same idempotency key produces one row.
- Photo upload rejects a renamed `.exe` and a 12 MiB file, and strips EXIF.
- Check-in creation p95 < 250 ms locally with a warm DB.

## P5 — Friends, follows, feed, toasts (1.5 weeks)

**Build**
- `follows`, `friendships`, `blocks`, `activities`, `notifications`, `checkin_likes`.
- Friend request/accept/decline/cancel, follow/unfollow, block/unblock with cascade rules.
- Feed: own + friends + follows, visibility + block filtering, keyset pagination, tabs (Friends / Following; Nearby deferred from v1.2).
- Toast with optimistic UI, idempotent like, 5 s undo (deferred delete), notification batching.
- Notification centre and unread badge; mark read.
- User search and profile pages with privacy filtering.
- Activity writes for check-in, badge, friend, venue check-in, list, drink added.

**Exit criteria**
- Accepting a request makes both users' public check-ins visible to each other.
- Blocking hides content in both directions within the same request, including from search.
- Feed pagination has no duplicates or gaps with concurrent inserts (test with 200 rows and interleaved writes).
- Toast twice = one row; undo removes it and the notification.
- Private profile returns 404 to non-friends, not 403.

## P6 — Venues and locations (1.5 weeks)

**Build**
- `venues`, `venue_drinks`, `venue_names`, `venue_saves`; kind enum including `hotel` and `vending`.
- Venue page per design doc §5.8, venue search by name/city, map link (OSM), "near me" with one-shot geolocation.
- Create venue flow with address search (Nominatim proxy, cache, rate limit) and manual coordinates.
- Dedupe: exact name+city block, 150 m + same-kind prompt, merge tool for admins.
- Attach drink to venue, "seen here" with `last_seen_at` decay, admin "on offer".
- Venue check-in entry point and venue activity list; venue stats.
- Save venue.

**Exit criteria**
- Creating a duplicate venue is blocked and links to the existing one.
- Hotel check-ins appear on the hotel page and in the `hotel` venue-kind stats.
- Stale "seen here" rows fall out of the default view after the decay window.
- Nominatim failure degrades to manual address entry without blocking creation.
- No coordinates are stored for users; a test asserts the schema has no user location column.

## P7 — Lists, badges, profile polish (1.5 weeks)

**Build**
- `lists`, `list_items`, auto-created Wishlist, visibility, ordering, add-from-drink-page.
- `badges`, `user_badges` with the badge catalogue in seed data; idempotent evaluation in the check-in transaction; backfill command.
- Badge grid with earned/locked states and progress per design doc §5.7.
- Profile page: header, stats, favourite style, recent check-ins, badges, lists, friends.
- Data export (JSON) and account deletion (soft delete + 30-day purge job).
- Empty states across feed, search, badges, venues.

**Exit criteria**
- First check-in awards "First sip" exactly once, including under concurrent check-ins.
- Deleting a check-in does not revoke a badge already earned.
- Backfill on a seeded user reproduces the same badge set as live evaluation.
- Export contains the user's check-ins, lists, badges, and settings, and no other user's data.
- Deleted account's handle is freed after purge and check-ins display "deleted user".

## P8 — OpenCode ("opencode2") AI Subsystem (1.5 weeks)

**Build**
- Ent schemas `AppSetting`, `AIEndpoint`, `AIDraftSession` (Ent-managed; no separate SQL migration).
- `internal/ai`: OpenCode Zen (`https://opencode.ai/zen/go/v1`) and OpenAI-compatible HTTP client ported from `dnd/ai`:
  - `NormalizeAIBaseURL`, `AIRequestURL`, `ValidateAIBaseURL` (strips trailing `/`, `/chat/completions`, `/images/generations`, `/completions` on save and request construction).
  - `SetAIProviderHeaders`: honest `User-Agent: sodapop/<version>` and multi-turn `x-opencode-session` continuity (`16` random bytes hex generated on turn 1 and returned in responses).
  - AES-GCM encryption of `api_key` derived from `SESSION_SECRET` (`SHA-256("sodapop-ai-key:" + SESSION_SECRET)`), masked in all admin responses.
  - Explicit operation timeouts (`TestTimeout=30s`, `TextTimeout=60s`, `ImageTimeout=120s`, `DraftTimeout=180s`), token budget resolution (`endpoint.max_tokens` clamped `[256, 16384]`), and `finish_reason == "length"` / empty-content guard (`ai_output_truncated`).
- Admin UI & API (`/admin/ai`, `/api/v1/admin/ai-endpoints`, `/api/v1/admin/ai-endpoints/{id}/test` returning upstream HTTP status + 200-char body preview).
- Conversational & vision drink drafting (`/api/v1/ai/drafts`, `/api/v1/ai/drafts/{id}/messages`, `/api/v1/ai/drafts/{id}/commit`, `/api/v1/ai/drafts/revise`), tasting-note polish (`POST /api/v1/ai/tasting-note`), and Soda Sommelier recommendations (`POST /api/v1/ai/recommend`).

**Exit criteria**
- Saving `https://opencode.ai/zen/go/v1/chat/completions/` normalizes to `https://opencode.ai/zen/go/v1` and never doubles `/chat/completions`.
- `httptest` suite verifies `User-Agent: sodapop/<version>` and identical `x-opencode-session` across a 3-turn drink-drafting conversation.
- Truncated (`finish_reason: "length"`) upstream responses return `ai_output_truncated` and do not append a broken assistant message.
- With `ai_enabled=0`, all AI UI chips and endpoints gracefully disable.

## P9 — PWA, offline queue, analytics (1 week)

**Build**
- `manifest.webmanifest`, icons, shortcuts, theme-color metas, install prompt.
- Service worker: app shell precache, image stale-while-revalidate, HTML network-first, wasm reader cached after first scan, versioned cache cleanup.
- IndexedDB offline check-in queue with idempotency keys, replay on `online`, merge-remap handling, visible pending badge.
- Offline fallback page and offline drink detail for the last 100 viewed drinks.
- Umami injection with CSP origin, allowlisted events from `analytics/events.go`, TS helper, per-user opt-out server-side.
- Settings page completion: appearance, privacy, sessions, tokens, blocked users.

**Exit criteria**
- Airplane mode: scan a cached drink, queue a check-in, reload, reconnect → exactly one check-in.
- Service worker update does not serve a stale app shell after a version bump.
- Umami unset → no script tag, no CSP entry, no requests.
- Analytics opt-out → no script tag for that user, even when configured.
- Lighthouse PWA installable, performance ≥ 90 on the feed with a seeded DB.

## P10 — Flutter Android Companion App (`mobile/`) (2 weeks)

**Build**
- `mobile/` Flutter project (`pubspec.yaml`, Dart `^3.9.0`, Material 3, `flutter_riverpod`, `go_router`, `dio`, `flutter_secure_storage`, `shared_preferences`, `webview_flutter`, `mobile_scanner`, `image_picker`).
- Onboarding screen with **zero hardcoded default server URL**, URL validation/normalization, `GET /api/v1/auth/oidc/status` discovery, primary **“Sign in with Authelia”** button (in-app WebView `/auth/oidc/login`, native Android cookie capture channel `xyz.vandijke.sodapop/cookies`, `POST /api/v1/auth/mobile-token` exchange), plus `sd_` personal API token paste and optional password fallback.
- 4-slot bottom navigation shell (`Friends`, `Discover`, lifted lime `Scan` button, `You`) styled with Sodapop's lime/forest tokens (`#B4E32C` / `#16241A` / `#F2F4E6` / `#0C130E`).
- Native `mobile_scanner` CameraX + ML Kit barcode scanner with torch/zoom, GTIN-14 normalization, GS1 check-digit validation, local cache + `/api/v1/barcodes/{gtin}` resolution, and unknown-soda creation + AI drafting sheet.
- Quarter-star check-in bottom sheet with one-photo upload, AI tasting-note polish, and a `shared_preferences` offline outbox with idempotency keys.
- Native Android polish: vector adaptive launcher icon, Android 13 monochrome icon, API 31+ splash screen, `.github/workflows/flutter-android.yaml` CI, and `.releaserc.yaml` + `.github/workflows/release.yaml` APK release asset attachment.

**Exit criteria**
- Onboarding starts blank, rejects malformed URLs, detects Authelia OIDC, completes WebView sign-in, and stores the `sd_` token in secure storage.
- Native camera scan resolves a known EAN-13/UPC-A barcode in < 300 ms from cache and submits a check-in.
- Offline check-ins queued in Android airplane mode replay idempotently on reconnect.
- `flutter analyze` and `flutter test` pass in CI; release workflow builds `sodapop-<version>-android.apk`.

## P11 — Hardening and launch (1.5 weeks)

**Build**
- Rate limits across mutations; request size limits; per-IP and per-user buckets.
- Backup job (`VACUUM INTO`), restore runbook, retention, admin backup trigger.
- Structured audit logging for admin actions; secrets review; CSP tightening; dependency audit.
- Load pass on 10k drinks / 100k check-ins / 1k users: feed, search, drink page, scan resolution.
- Full Playwright suite + axe on every top-level page; reduced-motion and keyboard passes.
- Docs: deployment, configuration, backup/restore, Open Food Facts attribution, troubleshooting, API reference, mobile setup.
- Release: tagged version, CHANGELOG, GHCR image, Android APK, compose example, migration check from empty DB.

**Exit criteria**
- Restore from backup into a fresh volume reproduces all data and passes a smoke test.
- p95 feed and drink page < 150 ms with the load dataset.
- Zero high/critical vulnerabilities in `govulncheck` and the npm audit gate.
- A new self-hoster can go from `git clone` to a verified account in under 10 minutes following the docs.
- Rollback procedure documented and tested one version back.

## Cross-cutting definition of done (every phase)

- Tests: unit for logic, handler tests with a real router, migration test from empty DB, e2e for the new happy path.
- `gofmt`, `go vet`, `go test -race`, `npm run build`, typecheck, Vitest, Playwright all green in CI.
- No new lint suppressions without a comment.
- New config documented in `.env.example` and the config table.
- New user-facing strings reviewed against the voice rules; no placeholder copy.
- Accessibility: keyboard path and axe pass for new pages.
- Telemetry: new external calls wrapped with spans; new error paths logged with `slog.*Context`.
- Docs updated (`architecture.md`, `product.md`, `data-model.md` as applicable).
