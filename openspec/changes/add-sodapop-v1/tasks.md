# Tasks: Add Sodapop v1

## Phase 0: Foundations, Ent ORM & Migrations (`P0`)
- [ ] 0.1 Initialize `go.mod` (`module sodapop`, Go 1.27, `modernc.org/sqlite`, `entgo.io/ent v0.14.6`, `go.opentelemetry.io/otel v1.47.0`) and `package.json` (Vite 8, TypeScript 7, Tailwind 4, Vitest, Playwright).
- [ ] 0.2 Define canonical Ent schemas in `ent/schema/*.go` for all domains (`User`, `UserIdentity`, `Session`, `APIToken`, `EmailVerificationToken`, `PasswordResetToken`, `EmailOutbox`, `UserSetting`, `LoginAttempt`, `Media`, `Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkFlavour`, `DrinkAlias`, `DrinkImage`, `OffProduct`, `CatalogRevision`, `Venue`, `VenueDrink`, `VenueName`, `VenueSave`, `Checkin`, `CheckinLike`, `Follow`, `Friendship`, `Block`, `Activity`, `Notification`, `Badge`, `UserBadge`, `List`, `ListItem`, `Report`, `AdminNote`, `AppSetting`, `AIEndpoint`, `AIDraftSession`) and run `go generate ./ent`.
- [ ] 0.3 Implement `internal/db`: connection pragmas (`WAL`, `busy_timeout(10000)`, `foreign_keys(ON)`, `synchronous(NORMAL)`, `cache_size`, `mmap_size`), shared `*sql.DB` + `ent.Client`, `Client.Schema.Create(ctx)`, versioned transactional migration registry (`internal/db/migrations/registry.go` + `0001`..`0010` for `drinks_fts`, `drink_ratings` view, and partial indexes), `TraceQueryEx`, and `TestSchemaConsistency` in `internal/db/schema_consistency_test.go`.
- [ ] 0.4 Implement `internal/telemetry` (explicit `OTEL_*` env parsing, `otelhttp`, `otelslog` fanout, `/metrics`, `/healthz`, `/readyz`) and `internal/web` (stdlib mux, security headers, Vite manifest loader, lime design tokens from `docs/design-direction.md`).
- [ ] 0.5 Configure 3-stage `Dockerfile`, `docker-compose.yml` (with `observability` collector profile), `.github/workflows/ci.yaml`, and `AGENTS.md`.

## Phase 1: Auth, Authelia OIDC & Email Verification (`P1`)
- [ ] 1.1 Implement local signup/login (`bcrypt` cost 12, handle validation, rate limits), DB-backed `sd_session` cookies, per-session CSRF + `Origin` middleware, and `sd_` personal API tokens.
- [ ] 1.2 Implement `internal/email` (`net/smtp` sender + `email_outbox` worker + dev file sink) and 24h single-use `email_verification_tokens` + 1h `password_reset_tokens`, enforcing the read-only gate (`403 email_not_verified`) on unverified local accounts.
- [ ] 1.3 Implement Authelia OIDC (`OIDC_ENABLED`, `.well-known/openid-configuration` discovery, PKCE S256, HMAC state/nonce cookie, RS256 JWKS verification, `email_verified=true` requirement, `admins` group → `admin` role sync, `ALLOWED_EMAILS`/`ALLOWED_DOMAINS`, `ALLOW_PASSWORD_FALLBACK`, `/auth/oidc/login|callback|logout`, `/api/v1/auth/oidc/status`, and `POST /api/v1/auth/mobile-token`).

## Phase 2 & 3: Catalog, Soda Taxonomy, Barcode Scanner & Open Food Facts (`P2`–`P3`)
- [ ] 2.1 Implement `cmd/seed` for the 17 parent style families, substyles, and flavours; implement transactional `drinks_fts` synchronization on `Drink`/`Brand`/`DrinkAlias` writes.
- [ ] 2.2 Implement catalog pages (`/drinks/{slug}`, `/search`) and capped `/api/v1/search` plus `/api/v1/drinks/{slug}` (no collection listing, no totals) with `catalog_revisions` auditing.
- [ ] 3.1 Build the web barcode scanner overlay (`BarcodeDetector` primary + lazy Web Worker `barcode-detector` ZXing-C++ wasm fallback, 8–12 fps ROI decode, torch capability detection, manual GTIN entry, GTIN-14 normalization, GS1 check-digit validation).
- [ ] 3.2 Implement `GET /api/v1/barcodes/{gtin}`, `internal/catalog/off.go` (Open Food Facts v3.6 client, custom `User-Agent`, `1 req/s` spacing, `15 req/min` guard, `off_products` ODbL isolation, image proxy), `cmd/fakeoff`, and the unknown-soda provisional create + soft-key dedupe flow.

## Phase 4, 5, 6 & 7: Check-ins, Social, Venues, Badges & Lists (`P4`–`P7`)
- [ ] 4.1 Implement check-in creation/edit/soft-delete (`/checkins`, `/api/v1/checkins`) with quarter-star integer ratings (`0..20`), UUID `idempotency_key` deduplication, `internal/media` WebP/JPEG resize + EXIF stripping, `drink_ratings` view (`MIN_RATERS_FOR_AVG=3`), and Bayesian top-rated lists.
- [ ] 5.1 Implement `friendships` (`user_a < user_b`), one-way `follows`, `blocks`, `checkin_likes` (Toast with 5 s undo), keyset-paginated chronological feed (`Following`, `You`, `Nearby`), and in-app `notifications`.
- [ ] 6.1 Implement `venues` (`hotel`, `bar`, `restaurant`, `cafe`, `shop`, `festival`, `vending`, `other`), Nominatim geocoding proxy, 150 m + name/city deduplication, `venue_drinks` ("seen here" decay + "on offer"), and `venue_saves`.
- [ ] 7.1 Implement `lists` + system Wishlist, idempotent transactional `badges` evaluation + backfill command, user profile/stats pages, JSON data export, and 30-day soft-delete account purge.

## Phase 8: OpenCode ("opencode2") AI Subsystem (`P8`)
- [ ] 8.1 Implement `internal/ai`: `NormalizeAIBaseURL`, `AIRequestURL`, `ValidateAIBaseURL`, `SetAIProviderHeaders` (`User-Agent: sodapop/<version>`, `x-opencode-session` continuity), AES-GCM API key encryption from `SESSION_SECRET`, timeouts (`30s/60s/120s/180s`), token budget clamping, and `finish_reason == "length"` (`ai_output_truncated`) handling.
- [ ] 8.2 Implement `/admin/ai` and `/api/v1/admin/ai-endpoints` (CRUD, global `ai_enabled` toggle, and `/test` probe returning upstream HTTP status + 200-char preview).
- [ ] 8.3 Implement conversational & vision drink drafting (`/api/v1/ai/drafts`, `/messages`, `/commit`, `/revise`), check-in tasting-note polish (`POST /api/v1/ai/tasting-note`), and Soda Sommelier recommendations (`POST /api/v1/ai/recommend`).

## Phase 9, 10 & 11: PWA, Flutter Android App & Launch Hardening (`P9`–`P11`)
- [ ] 9.1 Implement `manifest.webmanifest`, service worker (`sw.ts`), IndexedDB offline check-in queue with idempotency replay, and allowlisted Umami analytics (`UMAMI_URL`, `UMAMI_WEBSITE_ID`, per-user `analytics_opt_out`).
- [ ] 10.1 Scaffold `mobile/` Flutter Android app (`pubspec.yaml`, Riverpod, GoRouter, Dio, `mobile_scanner`, `webview_flutter`, `flutter_secure_storage`, lime/forest Material 3 theme, `BricolageGrotesque` font, adaptive + monochrome launcher icons).
- [ ] 10.2 Implement Android zero-default server URL onboarding, `/api/v1/auth/oidc/status` probe, Authelia WebView login + `MainActivity.kt` `CookieManager` bridge → `POST /api/v1/auth/mobile-token`, native ML Kit barcode scanning, check-in sheet with AI note polish, and offline check-in outbox.
- [ ] 10.3 Add `.github/workflows/flutter-android.yaml`, `.releaserc.yaml`, and `.github/workflows/release.yaml` APK signing & release asset attachment; run full load, backup/restore (`VACUUM INTO`), Playwright, and axe accessibility verification.
