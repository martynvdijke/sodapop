# Sodapop — Architecture

Status: draft for review. Product scope: [product.md](product.md). Data model: [data-model.md](data-model.md). Visual language: [design-direction.md](design-direction.md). Delivery: [roadmap.md](roadmap.md).

## 1. Decisions at a glance

| Area | Decision | Why |
|---|---|---|
| Backend | Go 1.27, `net/http` stdlib mux with method patterns | House style in `board-games`; no framework tax |
| Storage & ORM | SQLite (`modernc.org/sqlite`) + **Ent ORM (`entgo.io/ent v0.14.6`)** + versioned migrations | Same hybrid as `dnd`/`datey`: Ent for canonical schema & CRUD, versioned migrations for FTS5/views/partial indexes & data backfills |
| Templates | `html/template`, `base.html` + `{{define "content"}}`, `//go:embed` | Server-rendered, fast first paint on mobile |
| Assets | Vite 8 + TypeScript 7 + Tailwind 4, hashed manifest | Same pipeline as `board-games` |
| Client behaviour | TS islands + service worker PWA | Barcode camera, offline queue, install prompt |
| Mobile app | Flutter Android app in `mobile/` (`Riverpod`, `GoRouter`, `Dio`, `mobile_scanner`) | Same architecture as `board-games`/`dnd`; native ML Kit camera scan + Authelia OIDC |
| Auth | Authelia OIDC (PKCE S256) + local email/password (`bcrypt`, email verification, `ALLOW_PASSWORD_FALLBACK`) | First-class OIDC SSO from `dnd`/`board-games` plus verified local signup |
| Sessions | 32-byte opaque cookie (`sd_session`), SHA-256 at rest, per-session CSRF token | Existing house pattern across web and mobile WebView |
| AI ("opencode2") | `internal/ai` OpenCode Zen / OpenAI-compatible client, DB `ai_endpoints` (AES-GCM), `x-opencode-session` | Same subsystem as `dnd`: drink drafting, label extraction, tasting-note polish, recommendations |
| Catalog | Local `drinks` catalog seeded/upserted from Open Food Facts | Offline-first reads, community edits |
| Barcode | `BarcodeDetector` primary, `barcode-detector` (ZXing-C++ wasm) worker fallback, manual entry always | `BarcodeDetector` is not production-available on iOS Safari |
| Social | Explicit friend requests + follow graph, activity rows written in-tx | Untappd semantics, not a generic feed engine |
| Analytics | Umami script tag + allowlisted custom events, no PII | House pattern in 5 repos |
| Observability | OTel traces/metrics/logs via OTLP, env-only config, `/metrics` | Matches 14 existing repos |
| Deployment | Single non-root container, `/data` volume, compose with optional `observability` profile | Self-hosted default |

## 2. Repository layout

```
sodapop/
├── cmd/
│   ├── server/            # main: config, wiring, graceful shutdown
│   ├── seed/              # styles, sample drinks, demo users (via Ent client)
│   └── fakeoff/           # fake Open Food Facts server for dev/e2e
├── ent/                   # generated Ent ORM client, predicates, ent/migrate/schema.go
│   └── schema/            # canonical Ent entity definitions (user.go, drink.go, aiendpoint.go, …)
├── internal/
│   ├── config/            # typed env config + Validate + redacted LogValue
│   ├── db/                # sql.DB + ent.Client init, pragmas, TraceQueryEx, schema_consistency_test.go
│   │   └── migrations/    # versioned migration registry + SQL/Go migrations (0001_auth_indexes … 0005_composite_indexes)
│   ├── auth/              # OIDC (PKCE/JWKS), local signup/login, sessions, CSRF, tokens, verification
│   ├── ai/                # OpenCode ("opencode2") client, endpoint crypto, URL normalization, drink drafting
│   ├── email/             # SMTP sender, templates, outbox retry
│   ├── catalog/           # drinks, brands, styles, barcodes, OFF import
│   ├── checkins/          # check-in create/edit/delete, ratings, photos
│   ├── social/            # friends, follows, activity feed, notifications
│   ├── venues/            # venues, venue check-ins, menus, geocoding
│   ├── users/             # profiles, settings, privacy
│   ├── media/             # photo upload, resize, cache, image proxy
│   ├── web/               # router, middleware, render, assets, handlers
│   ├── api/               # /api/v1 JSON handlers + OpenAPI
│   ├── telemetry/         # OTel providers, slog bridge, metrics
│   └── analytics/         # Umami config + event allowlist
├── templates/             # base, pages, partials
├── web/                   # TS entrypoints, styles, manifest, sw.ts
├── mobile/                # Flutter Android app (Riverpod, GoRouter, Dio, mobile_scanner)
├── e2e/                   # Playwright specs + a11y
├── data/                  # runtime volume (gitignored)
├── docs/
├── Dockerfile
├── docker-compose.yml
├── .releaserc.yaml        # semantic-release + Android pubspec version sync
├── go.mod                 # module sodapop
├── AGENTS.md
└── CHANGELOG.md
```

Rules: handlers stay thin (parse → service → render); all business logic in `internal/<domain>`; every query scoped by `user_id` unless explicitly public; cross-user access returns 404, never 403.

### Data access seam: Ent ORM + versioned migrations (`ent/` + `internal/db/`)
Adopted directly from `dnd` (`dnd/docs/adr-data-access.md`, `dnd/db/db.go`, `dnd/db/migrations/registry.go`, `dnd/db/schema_consistency_test.go`):
1. **Ent (`entgo.io/ent v0.14.6`) is canonical for entity schema and CRUD:** `ent/schema/*.go` defines all domain entities (`User`, `UserIdentity`, `Session`, `APIToken`, `EmailVerificationToken`, `PasswordResetToken`, `EmailOutbox`, `UserSetting`, `Media`, `Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkFlavour`, `DrinkAlias`, `DrinkImage`, `OffProduct`, `CatalogRevision`, `Venue`, `VenueDrink`, `VenueName`, `VenueSave`, `Checkin`, `CheckinLike`, `Follow`, `Friendship`, `Block`, `Activity`, `Notification`, `Badge`, `UserBadge`, `List`, `ListItem`, `Report`, `AdminNote`, `AppSetting`, `AIEndpoint`, `AIDraftSession`, `Integration`, `PushSubscription`, `InboundToken`). `go generate ./ent` produces `ent/migrate/schema.go` (`migrate.Tables`).
2. **Shared connection pool:** `internal/db.Init(dbPath)` opens `modernc.org/sqlite` with connection-level `_pragma` params (`busy_timeout(10000)`, `synchronous(NORMAL)`, `cache_size(-64000)`, `temp_store(MEMORY)`, `mmap_size`, `foreign_keys(ON)`), runs `PRAGMA journal_mode=WAL` and `PRAGMA auto_vacuum=INCREMENTAL`, and wraps the same `*sql.DB` with `entsql.OpenDB(dialect.SQLite, DB)` → `ent.NewClient(ent.Driver(drv))`.
3. **Two-stage deterministic migration pipeline on boot:**
   - **Stage 1 — Ent schema migration (`Client.Schema.Create(ctx)`):** idempotently creates/updates all Ent-managed tables, columns, foreign keys, and indexes from `ent/migrate/schema.go`.
   - **Stage 2 — Versioned migration registry (`internal/db/migrations/registry.go`):** runs ordered, transactional migrations recorded in `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)` for:
     - SQLite features Ent cannot express natively: standalone FTS5 virtual tables (`drinks_fts`), SQLite views (`drink_ratings`), and partial indexes (`WHERE is_primary = 1`, `WHERE off_id IS NOT NULL`, `WHERE deleted_at IS NULL`, `WHERE read_at IS NULL`);
     - Forward data migrations, backfills, and safe column/table transitions across releases.
4. **Permitted raw SQL scope:** entity CRUD goes through `db.Client.<Entity>`. Raw parameterized SQL on `db.DB` is permitted only for FTS5 `MATCH ?` queries, read-heavy projections (`drink_ratings`, Bayesian top lists, keyset feed joins), and `VACUUM INTO` backups, always wrapped in `db.TraceQueryEx` and with prompt `defer rows.Close()`.
5. **Mechanical consistency guard (`internal/db/schema_consistency_test.go`):** CI runs `TestSchemaConsistency` against an initialized `:memory:` DB and fails if any live SQLite table is missing from both `ent/migrate.Tables` and the explicit `rawOnlyTables` allowlist (`drinks_fts*` + `schema_migrations`).

## 3. HTTP surface

Server-rendered pages plus a Bearer-authenticated JSON API. No SPA, no client-side router.

```
GET  /                          feed (friends + own activity)
GET  /scan?gtin=                resolve a scanned GTIN and open the check-in page
GET  /drinks                    catalog browse with filters and paging (HTML only)
GET  /drinks/{slug}             drink detail, ratings, where found
GET  /brands                    brand index
GET  /brands/{slug}             brand detail
GET  /styles                    style index
GET  /styles/{slug}             style landing page
GET  /drops                     limited, seasonal and discontinued sodas
GET  /checkins/new              check-in form for a drink (?slug=)
POST /checkins                  create check-in
GET  /checkins/{id}             check-in detail (owner, friend, or public)
POST /checkins/{id}/toast       toast or undo
POST /checkins/{id}/delete      soft-delete (owner only)
GET  /venues/{slug}             venue page + menu/on-offer list
GET  /u/{handle}                profile, badges, recent (opt-in public profiles)
GET  /friends                   friends + requests
GET  /notifications             notification centre
POST /notifications/read        mark one or all read
GET  /settings                  profile, preferences, privacy, security, data
POST /settings/{profile,avatar,preferences,privacy,password,delete}
POST /settings/sessions/{id}/revoke   revoke one session
POST /settings/logout-all       revoke every session including this device
GET  /integrations              outbound integrations + Home Assistant tokens
POST /integrations/ha-token     mint a Home Assistant inbound token
POST /integrations/ha-token/{id}/revoke
POST /hooks/ha/{token}          machine check-in (capability token, no session/CSRF)
GET  /insights                  personal sugar, caffeine and calorie summary
GET  /forgot  GET/POST /reset/{token}  password reset (anti-enumerated)
GET  /auth/oidc/login           start OIDC Authorization Code + PKCE S256
GET  /auth/oidc/callback        verify state/nonce/PKCE + JWKS ID token
POST /auth/oidc/logout          clear session + redirect to OIDC end-session
GET  /admin/{drinks,venues,users,reports,email,ai}   # role-gated
POST /admin/drinks/{id}/status  lifecycle change (active/limited/seasonal/discontinued)
GET  /healthz /readyz /metrics
GET  /api/openapi.json /api/docs
GET  /api/v1/auth/oidc/status   public OIDC discovery for web + Flutter onboarding
GET  /api/v1/notifications      personal notifications (friend-scoped, no firehose)
POST /api/v1/notifications/read mark one notification or all
GET  /api/v1/checkins/{id}      one check-in, visibility-scoped (404 when private)
GET  /api/v1/stats/me/insights  personal sugar/caffeine/calorie series (?range=7|30|90)
GET  /api/v1/integrations/home-assistant  integration wiring state for a sensor
POST /api/v1/ai/drafts          conversational drink/venue AI drafting ("opencode2")
POST /api/v1/ai/tasting-note    polish rough flavour impressions into tasting note
POST /api/v1/ai/recommend       personalized soda sommelier recommendations
/api/v1/...                     JSON mirror of the above + Flutter Android/CLI
```

Public HTML surfaces are `/` and `/u/{handle}` when the owner opted into a public profile. Catalog browse (`/drinks`, `/brands`, `/styles`, `/drops` and the brand/style detail pages), search, drink pages, notifications, settings and insights all require a signed-in member. Every `/api/v1` route remains Bearer-authenticated and friend-scoped, so crawlable HTML never widens the API boundary.

Middleware order: `requestID → otelhttp → securityHeaders → LoadSession → RequireCSRF → mux`; API mux: `AuthenticateBearer → rateLimit → mux`.

Security headers: CSP `default-src 'self'`, `img-src 'self' data: blob:`, `media-src 'self' blob:`, `worker-src 'self' blob:`, `frame-ancestors 'none'`, nosniff, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy: camera=(self)`, HSTS in production. The page CSP includes `script-src 'self' 'wasm-unsafe-eval'` so the ZXing-C++ WebAssembly reader can instantiate; the API CSP is separate and keeps `'unsafe-eval'` scoped to JSON responses. `.wasm` is registered as `application/wasm` in `internal/web/assets.go` so streaming instantiation and service-worker caching work. CSP must also allow the configured Umami origin.

**Indexability:** the middleware sets `X-Robots-Tag: noindex, nofollow` unless the path is `/` or starts with `/u/`. Catalog browse, drink pages and all member surfaces are `noindex`. A profile page is indexable only when `user_settings.public_profile` is on; the handler otherwise sets `X-Robots-Tag: noindex, nofollow` and supplies `index,follow` plus canonical/Open Graph metadata only for opted-in profiles. The API is deliberately excluded from this rule: crawlable HTML never exposes a collection endpoint, and `/api/v1` stays Bearer-authenticated and friend-scoped.

**Home Assistant inbound webhook:** `POST /hooks/ha/{token}` is mounted outside the session and CSRF middleware. The capability token in the path is the credential (a `Bearer` header is also accepted); it is looked up by SHA-256 hash, must be unrevoked, and is scoped to one user. Unknown, revoked and malformed tokens all return the same generic `401 {"error":"unauthorized"}`. The body is capped at 64 KiB, accepts `gtin` or `slug` but not both, plus optional `rating` (0–20), `note`, `venue_id` and `serving`; `Idempotency-Key` is honoured from the header or the JSON body. The route is limited to 10 requests per minute per token. Errors never echo the token, and it is never logged.

CSRF: token stored per session, accepted as `X-CSRF-Token` or `csrf_token` form field, constant-time compare plus `Origin` check against `BASE_URL`; Bearer requests exempt; GET/HEAD/OPTIONS exempt.

Rate limits (in-process token buckets, per IP + per user): login 5/60s, signup 3/h, verification resend 3/h, password reset 5/h, check-in create 60/min, API 120/min, Home Assistant inbound 10/min per token. Return 429 with `Retry-After`.

## 4. Authentication and accounts

### Local accounts (required for email verification)
- Signup: email + handle + password (bcrypt cost 12, min 10 chars). Handle `^[a-z0-9_]{3,24}$`, reserved list (`admin`, `api`, `settings`, `sodapop`, …).
- Email verification: 32-byte token, sent as `https://host/verify?token=…`, only SHA-256 stored, 24h TTL, single use, all older tokens invalidated on resend. Verification marks `users.email_verified_at`.
- Gate policy: unverified users **can** log in but are read-only — no check-ins, follows, venue submissions, or photo uploads. A persistent banner offers resend. Rationale: avoids dead-end signups and lets people browse immediately.
- Login: email or handle, generic error, bcrypt compare, session rotation on login, throttle keyed `IP|identifier`.
- Password reset: 1h single-use token, anti-enumeration response, consumes token, invalidates all sessions.
- Account deletion: soft-delete with 30-day purge job; anonymizes public activity.

### Sessions
32 random bytes → cookie value; store SHA-256 hex in `sessions(token_hash PRIMARY KEY, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)`. Cookie `sd_session`, `HttpOnly`, `Secure` when HTTPS, `SameSite=Lax`, TTL 720h sliding with 5-minute write throttle. Cleanup ticker every 6h. Logout deletes row; "log out everywhere" deletes all rows for user.

### OIDC authentication (Authelia & standard providers)
Modeled directly on `dnd` and `board-games`:
- **Protocol:** OpenID Connect Authorization Code flow with PKCE (`S256`) against `OIDC_ISSUER_URL` (e.g. `https://authelia.vandijke.xyz`), scopes `openid email profile groups`.
- **Discovery & verification:** discovers authorization, token, userinfo, end-session, and `jwks_uri` from `/.well-known/openid-configuration`. Callback validates HMAC-signed HttpOnly cookie `state`, `nonce`, and PKCE `code_verifier`, then verifies the ID token's RS256 signature against cached JWKS keys, `iss`, `aud` (`OIDC_CLIENT_ID`), and `exp`.
- **Account linking & provisioning:** requires `email_verified=true` from the provider. Looks up `(provider='oidc', subject=sub)` in `user_identities`; if absent, links to an existing `users.email` (case-insensitive) and stamps `email_verified_at` if unset, or auto-provisions a new verified user (deriving a unique handle from `preferred_username` or email local-part).
- **Allowlists & role mapping:** enforces `ALLOWED_EMAILS` and `ALLOWED_DOMAINS` when configured. The first user on a fresh database becomes `admin`; on every OIDC login, if the `groups` claim contains `admins`, the user's role is promoted to `admin`.
- **Coexistence with local accounts:** when `OIDC_ENABLED=true`, `/login` shows **"Sign in with Authelia"** as the primary action. Local email+password signup/login and email verification remain available when `ALLOW_PASSWORD_FALLBACK=true` (default `true` when `OIDC_ENABLED=false`; explicit break-glass toggle when `OIDC_ENABLED=true`).
- **Mobile & API discovery:** `GET /api/v1/auth/oidc/status` returns `{"enabled": true, "issuer": "...", "password_fallback": false}` so the Flutter Android onboarding screen can automatically present the Authelia WebView login flow.

### API tokens
Prefix `sd_`, 32-byte secret shown once, SHA-256 at rest, scopes `read`/`write`/`feed`, `revoked_at`, `last_used_at`. Used by the Flutter Android app, PWA offline sync, and CLI import. Authenticated WebView sessions can also mint a device-scoped `sd_` token via `POST /api/v1/auth/mobile-token` so the native Android app does not depend on WebView cookie lifetime.

### Public API, Swagger, and anti-scraping
Interactive documentation is served at `GET /api/docs/` (Scalar/Swagger UI) from the OpenAPI 3.1 document at `GET /api/openapi.json`. Both are public; every data endpoint under `/api/v1` requires a Bearer `sd_` token.

The API is deliberately **not a public catalog dump**. There is no endpoint that lists all drinks, brands, venues, users, or check-ins. Search returns at most 10 results and omits total counts, so paging through it cannot reconstruct the catalog. Reading another person's check-ins requires an accepted friendship, otherwise the server returns `404` rather than `403` to avoid confirming that a handle exists. Friend requests for unknown handles return the same `202` response. Authenticated users may only read their own data and that of accepted friends; there is no global feed. All mutation endpoints are rate limited, and the OpenAPI description states these constraints so integrators do not build scrapers against it.

## 5. Email

Hand-rolled `net/smtp` sender (house style), supporting implicit TLS 465, STARTTLS, and plain, with `SMTP_HOST/PORT/USER/PASS`, `SMTP_FROM`, `SMTP_TLS`, 15s timeout. HTML + plain-text templates for: verify address, reset password, friend request (sent only when the recipient's `email_friend_req` setting is on), weekly digest (opt-in), venue/drink report outcome.

Delivery is queued in `email_outbox(id, to, template, payload_json, attempts, next_attempt_at, sent_at, last_error)`. A worker ticks every 30s, exponential backoff 1m→1h, max 8 attempts, then marks failed and surfaces in `/admin/email`. This keeps signup fast and survives SMTP outages. Dev mode writes messages to `data/mail/` instead of sending.

## 6. Catalog and barcodes

`drinks` holds the canonical product; `drink_barcodes` maps many GTINs to one drink (multipacks, regional packaging); `brands`, `styles`, `drink_images` support it. Barcode is the primary key for scanning but never the identity of a drink.

Client-side decoding:
- `BarcodeDetector` when `'BarcodeDetector' in globalThis` and `getSupportedFormats()` returns a usable subset. Reality: Chrome/Edge on Android and ChromeOS/macOS only; **not** iOS Safari, not Firefox. Hence the fallback is mandatory.
- Fallback: dynamically imported `barcode-detector` ponyfill (API-compatible over `zxing-wasm` reader, ~1 MiB wasm) with `zxing_reader.wasm` served from our own build rather than a CDN, cached by the service worker after first use. Never in the initial bundle.
- Decode at about 10 fps (`requestAnimationFrame` with a 100 ms frame interval) across EAN-13, EAN-8, UPC-A, UPC-E and GTIN-13/14. Torch/zoom only when `getCapabilities().torch` exists (not on iOS).
- `GET /scan?gtin=` normalizes the query value, validates the check digit, resolves the barcode and either opens the check-in page for the match or the pre-filled create form for a miss. An invalid GTIN returns 400.
- Stop the decode loop and every `MediaStreamTrack` when the overlay closes, the page is hidden, or `pagehide` fires. Camera permission is requested only from the open gesture.
- Normalize every result to GTIN-14, validate the GS1 check digit, dedupe identical codes for 1.5 s. UPC-A typically arrives as EAN-13 with a leading zero — normalize deterministically so both forms hit the same row.

Lookup ladder (server, with client cache):
1. local `drink_barcodes` — instant, offline-capable
2. Open Food Facts v3.6 `GET /api/v3.6/product/{barcode}.json?fields=…` when enabled → store in `off_products`, link `drinks.off_id`, `verified=0`
3. miss or OFF outage → pre-filled "create this soda" form (barcode, brand, name, style, volume, packaging)

Cache TTLs: positive 30 days, negative 7 days, keyed by GTIN. The client stores the same records in IndexedDB so a repeat scan works offline.

Merge rules: exact GTIN wins. A new GTIN with a matching normalized brand+name+volume+packaging suggests the existing drink ("Is this the same soda?"), never auto-merges. Conflicting data opens a moderation report. Community edits are audited in `catalog_revisions`.

Catalog browse (`BrowseDrinks`) is HTML-only. Filters are style (a parent slug also matches its children), brand slug, flavour slug, `caffeine_free=1`, `sugar_free=1`, and lifecycle `status`; sorts are `name`, `rated` and `newest` from a fixed SQL whitelist; the page size is 24 with one extra row fetched for `HasNext`. Merged and provisional rows are never browsable, and the default visibility set is `active`, `limited`, `seasonal`. There is no API endpoint that lists the catalog.

Drink lifecycle is `provisional | active | merged | limited | seasonal | discontinued`. `limited` and `seasonal` may carry `limited_until`; `discontinued` carries `discontinued_at`. The `/drops` page groups limited rows that have a date, seasonal rows, and discontinued rows. Admins change status with `POST /admin/drinks/{id}/status`; when an admin moves a drink to `discontinued` from another status, each distinct owner of a system wishlist containing it receives one `system` notification, idempotent per user and drink.

Open Food Facts client: mandatory contact User-Agent (`Sodapop/1.0 (contact@…)`), 15 s timeout, ≥1 req/s global spacing with a hard 15/min budget guard, retry 429/500/503 with `Retry-After` + jittered backoff, 8 MiB cap, OTel span per call.

Licensing boundary (non-negotiable): OFF data is ODbL 1.0 (contents DbCL, images CC BY-SA). OFF rows live in `off_products` and are surfaced read-only with attribution; user-created or edited rows never absorb OFF fields into the same record. OFF images are proxied and cached, never hot-linked, with attribution on the drink page. Export/share-alike obligations are documented in the deployment docs.

## 7. Social model

- Friendship is symmetric: `friendships(user_a, user_b, status, requested_by, created_at)` with `user_a < user_b`. Status `pending|accepted|declined`; blocking is a separate `blocks` row.
- Follow is one-way: `follows(follower_id, followee_id)`; friendship implies mutual follow for feed purposes but is stored separately.
- `activities` rows are written in the same transaction as the action: kinds `checkin`, `badge`, `list`, `venue`, `friend`. Columns: `id, user_id, kind, subject_id, visibility, created_at`.
- Feed query: own + accepted friends' + followed users' activities, `visibility IN ('public','friends')`, keyset paginated on `(created_at, id)`, 20 per page. Materialized fan-out is deliberately avoided; SQLite handles this at self-hosted scale.
- Privacy: `public | friends | private` per check-in and per profile default; private check-ins still count for personal stats, never appear in feeds.
- Notifications: in-app table with kinds `friend_request`, `friend_accept`, `toast`, `badge` and `system` (`comment` exists in the enum but has no producer yet). The nav shows an unread count, `/notifications` lists the newest 50, and `POST /notifications/read` marks one item (with `id`) or all. `GET /api/v1/notifications` and `POST /api/v1/notifications/read` expose the same personal scope. Friend-request email is sent only when the target's `email_friend_req` setting is on.
- Check-in visibility is enforced by `checkins.Service.Get`: owner sees everything, a friend sees `public` and `friends`, an anonymous or non-friend viewer sees only `public`. Everything else returns `ErrNotFound`, which the web and API layers map to 404.
- Public profiles are opt-in through `user_settings.public_profile`. A deleted account is always hidden. When public, anonymous visitors see the profile and only `public` check-ins; a signed-in stranger sees the same; the owner and accepted friends see `public`, `friends` and `private` rows. When not public, anonymous visitors are redirected to `/login` and a signed-in non-friend gets a 404.
- Insights are self-only. Sugar, caffeine and calories are derived from the drink's per-100 ml facts multiplied by the logged volume; merged and provisional drinks are excluded from the top-drinks list but still counted in totals. The series is zero-filled for each of the 7, 30 or 90 calendar days, caffeine clearance halves the last dose every 5 hours until it falls below 20 mg, and `late_caffeine` is true when the last dose plus 8 hours lands after `caffeine_bedtime`.
- Outbound integrations are stored one row per user and kind (`home_assistant`, `matrix`, `trmnl`) with an optional secret and an enabled flag. Event dispatch is fire-and-forget: Home Assistant receives `{event,text,url}`, Matrix receives an `m.text` message, TRMNL receives a `merge_variables` JSON body, and push delivery is skipped unless `user_settings.push_enabled` is on. The shared HTTP client has a 10 s timeout and each dispatched event runs under a 5 s context. Integration failures are logged and never block the check-in write.

## 8. Media

Uploads via multipart, 10 MiB cap, MIME sniffed and re-decoded (`image/jpeg|png|webp`), max dimension 1600px, re-encoded to WebP/JPEG, stored under `/data/media/<yy>/<mm>/<id>.webp` with a deterministic placeholder for failures. Served with long cache headers and content-addressed filenames. EXIF stripped. Rate limited per user (20/day). Optional S3-compatible backend behind an interface, not in v1.

## 9. Observability (OTel)

Dependencies: `go.opentelemetry.io/otel` v1.47.0 (`sdk`, `sdk/metric`, `sdk/log`, `otlptracehttp`, `otlpmetrichttp`, `otlploghttp`), `contrib/.../otelhttp` v0.72.0, `contrib/bridges/otelslog` v0.21.0, plus the Prometheus exporter. Core v1.47 requires Go ≥ 1.26; we are on 1.27.

- **Config is env-only in v1, but the Go SDK does not read most `OTEL_*` for you.** Only exporter endpoint/headers/timeout/compression/insecure are read by the exporter constructors. `OTEL_SDK_DISABLED`, `OTEL_TRACES_SAMPLER(_ARG)`, `OTEL_PROPAGATORS`, and `OTEL_BSP_*` must be parsed explicitly in `internal/config` and applied via `WithSampler`, `SetTextMapPropagator`, and batch options. A test asserts each one.
- Endpoints are full URLs (`http://otel-collector:4318`) set via `WithEndpointURL`; protocol is `http/protobuf`; local collector needs `OTEL_EXPORTER_OTLP_INSECURE=true` or TLS fails against localhost. Sampling defaults to `parentbased_traceidratio` 0.2 in production, 1.0 in dev.
- HTTP: `otelhttp.NewHandler` around the mux with a low-cardinality operation name per route (never raw paths, never `r.URL.Path`), `WithFilter` skipping `/healthz`, `/readyz`, `/metrics`. A middleware returns `X-Trace-ID` and logs `request_id`/`trace_id`.
- DB: manual `db.*` spans via `WithTx` plus a query-duration histogram and slow-query logging. There is no `otelsql` in contrib; `XSAM/otelsql` v0.44 exists but is out of scope for v1.
- Outbound: `otelhttp.NewTransport` on the Open Food Facts client, Nominatim proxy, and SMTP probe. Response bodies are always closed so client spans end.
- Logs: JSON `slog` to stdout **and** an OTel fanout handler, not a replacement. `otelslog` only attaches `trace_id`/`span_id` when handlers call `slog.InfoContext`/`ErrorContext`; a lint rule (or review checklist) enforces `*Context` inside request paths.
- Metrics: HTTP duration/count by route+status, check-in counter, barcode lookup outcomes (`local|off|miss|error`), feed query latency, email queue depth, catalog moderation backlog, plus `runtime` metrics. Prometheus at `/metrics`, OTLP metrics when configured.
- Compose: optional `observability` profile with `otel/opentelemetry-collector-contrib:0.163.0`, `memory_limiter` before `batch`, debug exporter by default and an optional `otlphttp` exporter for an external backend. No Grafana/Tempo bundled; app runs with no collector. Jaeger's native exporter is gone — export OTLP.
- Kill switch: `OTEL_SDK_DISABLED=true` short-circuits provider construction before any exporter is created; verified by a test that asserts no outbound connection attempt.

## 10. Analytics (Umami)

`UMAMI_URL` + `UMAMI_WEBSITE_ID`; when set, inject `<script defer src="{UMAMI_URL}/script.js" data-website-id="…">` (respect `TRACKER_SCRIPT_NAME` if the instance renames it) and add the origin to CSP. Events are allowlisted in `internal/analytics/events.go` and emitted from one TS helper: `checkin_created`, `barcode_scan_started`, `barcode_scan_result` (`local|off|miss|error`), `drink_created`, `search_used`, `venue_viewed`, `friend_added`, `list_created`, `badge_earned`, `theme_changed`. Umami event names are capped at 50 chars and string data at 500 chars; numbers are rounded to 4 decimals. No emails, handles, drink names, or free text. Set `data-do-not-track` and honour the per-user analytics opt-out server-side (no script tag at all). Self-hosted Umami is cookieless but still counts as analytics under GDPR — the privacy page states what is collected and why.

## 11. OpenCode ("opencode2") AI Subsystem (`internal/ai`)

Ported directly from `dnd` (`dnd/ai/client.go`, `dnd/ent/schema/aiendpoint.go`, and `dnd/openspec/specs/ai-*`):

- **Endpoints & storage:** admins configure one or more OpenAI-compatible endpoints in `ai_endpoints` (`type` IN `text|image|vision`, `base_url`, `encrypted_api_key`, `model`, `tags` JSON array, `enabled`, `temperature`, `max_tokens`, `image_size`), gated globally by `app_settings('ai_enabled')`. API keys are encrypted at rest with AES-GCM using a 32-byte key derived from `SESSION_SECRET` (`SHA-256("sodapop-ai-key:" + SESSION_SECRET)`), and masked (`sk-...xxxx`) in all admin responses.
- **Base URL normalization (`NormalizeAIBaseURL`, `AIRequestURL`, `ValidateAIBaseURL`):**
  - Built first-class for **OpenCode Zen** (`https://opencode.ai/zen/go/v1`) as well as OpenRouter, Ollama, or OpenAI.
  - Strips trailing slashes and accidental operation suffixes (`/chat/completions`, `/images/generations`, `/completions`) both when saving an endpoint and before constructing outbound request URLs so `/chat/completions` is never doubled. Rejects non-`http(s)` schemes with 400.
- **Honest provider attribution (`SetAIProviderHeaders`):**
  - Every outbound AI HTTP request (endpoint test, drink drafting, label vision extraction, tasting note polish, recommendations) sets:
    - `User-Agent: sodapop/<version>` (build-injected version, fallback `0.0.0-dev`; never `Go-http-client/1.1` and never a spoofed third-party SDK name).
    - `x-opencode-session: <session_id>` (forwards caller-supplied `session_id` / `x-opencode-session`, or generates 16 random bytes as 32-char hex on turn 1 and returns `session_id` in the JSON response so multi-turn conversations reuse the exact same session header for upstream sticky routing and prompt caching).
  - No artificial traffic-shape mimicry or pacing sleeps.
- **Timeouts, token budgets & diagnostics:**
  - Explicit `http.Client` timeouts per operation class: `TestTimeout = 30s`, `TextTimeout = 60s`, `ImageTimeout = 120s`, `DraftTimeout = 180s`.
  - Completion token budgets resolve from `endpoint.max_tokens` (clamped `[256, 16384]`, generous default `4096` for drafting when unset). If the upstream returns an empty content string or `finish_reason == "length"` before valid JSON is produced, the service returns an actionable error (`ai_output_truncated`) and never persists a broken assistant turn.
  - `POST /api/v1/admin/ai-endpoints/{id}/test` runs a live probe and surfaces the upstream HTTP status code and a 200-char response preview on failure.
- **Sodapop AI capabilities:**
  1. **Conversational & Vision Drink Drafting (`/api/v1/ai/drafts`, `/api/v1/ai/drafts/{id}/messages`, `/api/v1/ai/drafts/{id}/commit`, `/api/v1/ai/drafts/revise`):** users or moderators can paste rough notes, a barcode/OFF stub, or a can label photo (`vision` endpoint) and chat with the assistant to populate structured drink fields (`brand`, `name`, `style_slug`, `flavour_tags`, `sweetener`, `acid`, `carbonation`, `sugar_g_per_100ml`, `caffeine_mg_per_100ml`, `calories_kcal_per_100ml`, `description`). Responses follow the `dnd` envelope: `{"status": "chatting"|"ready", "message": "...", "draft": {...}, "session_id": "..."}`.
  2. **Tasting Note Polish (`POST /api/v1/ai/tasting-note`):** optional 1-tap helper inside the check-in sheet that turns quick flavour impressions + rating into a concise, unpretentious tasting note adhering to Sodapop's voice rules (sentence case, no exclamation marks, no sugar shaming).
  3. **Soda Sommelier Recommendations (`POST /api/v1/ai/recommend`):** suggests 3 catalog sodas to try next based on the user's highest-rated styles, flavours, and sweetener preferences.

## 12. Flutter Android Companion App (`mobile/`)

Matches `board-games/mobile` and `dnd/mobile` (documented in [mobile.md](mobile.md)):

- **Stack:** Flutter (Dart `^3.13.2`, Material 3), `flutter_riverpod`, `go_router`, `dio`, `flutter_secure_storage`, `shared_preferences`, `webview_flutter`, `mobile_scanner` (native Android CameraX + ML Kit barcode decoding with hardware torch/zoom), `cached_network_image`.
- **Zero hardcoded server URL:** the onboarding screen starts with an empty server URL field, validates `http(s)` origin format, persists the normalized URL in `flutter_secure_storage`, and blocks API traffic until configured.
- **Authentication:**
  - Probes `GET /api/v1/auth/oidc/status` for the entered server URL.
  - When OIDC is enabled, displays **"Sign in with Authelia"** as the primary button, opens `/auth/oidc/login` in an in-app WebView after clearing stale cookies, captures the `sd_session` cookie via the native Android cookie channel (`xyz.vandijke.sodapop/cookies`), and exchanges it for a long-lived device `sd_` Bearer token (`POST /api/v1/auth/mobile-token`).
  - Also supports pasting a personal API token (`sd_…`) or using email/password when `ALLOW_PASSWORD_FALLBACK=true`.
- **Parity & offline:** native bottom nav (`Feed`, `Discover`, lifted lime `Scan` button, `Venues`, `Profile`), instant ML Kit GTIN-14 barcode scan → check-in sheet, AI tasting-note polish & drink drafting, SQLite/shared-prefs offline check-in outbox with idempotency keys, OTLP/HTTP trace propagation (`traceparent`), and opt-out-aware Umami event ping.
- **Branding:** vector adaptive launcher icon (`#B4E32C` lime fill + `#16241A` forest mark), Android 13+ monochrome themed icon, API 31+ splash screen, self-hosted `BricolageGrotesque` font.

## 13. Configuration

Uppercase env, mostly unprefixed, loaded into a typed struct with `Validate()` and `LogValue()` redaction.

```
APP_NAME=sodapop  ENV=production  PORT=6291  BASE_URL=https://soda.example
DATABASE_PATH=/data/sodapop.db  SESSION_SECRET=…  SESSION_TTL=720h
SIGNUP_ENABLED=true  ALLOW_PASSWORD_FALLBACK=true  ALLOWED_EMAILS=  ALLOWED_DOMAINS=
SMTP_HOST SMTP_PORT SMTP_USER SMTP_PASS SMTP_FROM SMTP_TLS
OIDC_ENABLED OIDC_ISSUER_URL OIDC_CLIENT_ID OIDC_CLIENT_SECRET OIDC_CLIENT_SECRET_FILE OIDC_REDIRECT_URL OIDC_SCOPES="openid email profile groups"
AI_DEFAULT_BASE_URL=https://opencode.ai/zen/go/v1
OFF_ENABLED=true  OFF_USER_AGENT="Sodapop/1.0 (contact@example.com)"
UMAMI_URL UMAMI_WEBSITE_ID
OTEL_EXPORTER_OTLP_ENDPOINT OTEL_EXPORTER_OTLP_PROTOCOL OTEL_SERVICE_NAME=sodapop
TRUSTED_PROXIES METRICS_ENABLED LOG_LEVEL VERSION
```

Secrets accept `_FILE` variants. Invalid config fails fast with a readable error; secrets never appear in logs.

## 14. Ops

- Multi-stage Dockerfile: `node:24-alpine` → `golang:1.27-alpine` (`CGO_ENABLED=0`, `-trimpath -ldflags="-s -w -X sodapop/internal/ai.Version=${VERSION}"`) → `alpine:3.24`, non-root user, `/data` volume, `HEALTHCHECK` on `/healthz`.
- Compose: bind `127.0.0.1:6291`, `env_file: .env`, named volume, healthcheck, `stop_grace_period: 20s`, optional `observability` profile.
- Graceful shutdown: signal context → HTTP shutdown 15s → stop workers → flush OTel providers 10s → close DB.
- Backups: nightly `VACUUM INTO '/data/backups/sodapop-<ts>.db'`, retain 7 daily + 4 weekly; admin-triggered backup endpoint.
- Jobs: session cleanup (6h), expired tokens (hourly), email outbox (30s), OFF retry queue, backup (nightly), soft-delete purge (daily), badge evaluation after check-ins.
- Health: `/healthz` liveness, `/readyz` DB ping + migrations applied + media dir writable.

## 15. Testing

- Go: table-driven unit tests per package; handler tests with `httptest` and a real router; per-test in-memory SQLite (`file:test-<nanos>?mode=memory&cache=shared`). Seed helper builds admin + two friends.
- AI & OIDC: unit + `httptest` suites verifying `NormalizeAIBaseURL`, `SetAIProviderHeaders` (`User-Agent: sodapop/<version>`, `x-opencode-session` continuity), AES-GCM key encryption, truncated response handling, and OIDC PKCE/JWKS callback verification.
- Barcode: fixture GTINs (EAN-13, UPC-A, EAN-8), decoder output parsing, OFF payload fixtures, dedupe/merge cases.
- Frontend: Vitest for scanning state machine and offline queue; Playwright (serial workers) for signup→verify→scan→check-in; axe pass on all top-level pages.
- Mobile: `flutter analyze` and `flutter test` widget/unit suites covering URL validation, OIDC status probe, GTIN-14 check digit, and check-in queue serialization.
- Integration: `cmd/fakeoff` and a mail sink (files) so no network is required in CI.
- Coverage floors: Go ≥45% overall, handlers/middleware ≥60%; TS ≥50%.

## 16. CI & Release

- `.github/workflows/ci.yaml`: gofmt/vet/`go test -race` with coverage; `npm ci` + build + typecheck + Vitest; Playwright with artifacts; actionlint/zizmor/pinact.
- `.github/workflows/flutter-android.yaml`: `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with SHA-pinned actions and `continue-on-error: true`.
- `.github/workflows/release.yaml` + `.releaserc.yaml`: semantic-release on `main` (Conventional Commits) updating `CHANGELOG.md` and `mobile/pubspec.yaml`, publishing multi-arch GHCR/Docker Hub images, building & attaching `sodapop-<version>-android.apk` to the GitHub Release, and exporting CI OTel spans.

## 17. Deliberate non-goals for v1

- Native iOS app (iOS uses the installable PWA with WebAssembly barcode fallback; Android has both the PWA and the native Flutter app in `mobile/`).
- Federation/ActivityPub, real-time chat.
- Multi-region or Postgres; SQLite is the product.
- Server-side image CDN, video, stories.
- Paid business accounts or subscriptions.
