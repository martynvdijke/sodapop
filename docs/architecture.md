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
│   ├── seed/              # styles, flavours, starter catalog, demo users (via Ent client)
│   ├── restore/           # offline database restore (stop the server first)
│   └── fakeoff/           # fake Open Food Facts server for dev/e2e
├── ent/                   # generated Ent ORM client, predicates, ent/migrate/schema.go
│   └── schema/            # canonical Ent entity definitions (user.go, drink.go, aiendpoint.go, …)
├── internal/
│   ├── config/            # typed env config + Validate + redacted LogValue
│   ├── db/                # sql.DB + ent.Client init, pragmas, TraceQueryEx, schema_consistency_test.go
│   │   └── migrations/    # versioned SQL migrations (0001_auth_indexes … 0010_checkins_venue_index; registry in db/migrations.go)
│   ├── auth/              # OIDC (PKCE/JWKS), local signup/login, sessions, CSRF, tokens, verification
│   ├── account/           # shared whole-account export and soft-delete used by web and API
│   ├── backup/            # VACUUM INTO snapshots, integrity check, retention, scheduler lease
│   ├── adminnotes/        # internal report-scoped admin notes
│   ├── ai/                # OpenCode ("opencode2") client, endpoint crypto, URL normalization, drink drafting
│   ├── badges/            # badge catalogue, evaluation, passport support
│   ├── comments/          # check-in comments, visibility inheritance, block filter, rate limit
│   ├── email/             # SMTP sender, templates, outbox retry
│   ├── catalog/           # drinks, brands, styles, barcodes, OFF import, galleries, revisions
│   ├── checkins/          # check-in create/edit/delete, ratings, photos, lists
│   ├── mix/               # mix recipes stored as kind=mix drinks
│   ├── ratelimit/         # in-process token buckets
│   ├── settings/          # runtime setting keys, validation, immutable snapshot
│   ├── setup/             # first-run token, atomic first-admin creation
│   ├── secretbox/         # AES-GCM for runtime secrets
│   ├── social/            # friends, follows, blocks, activity feed, notifications
│   ├── taste/             # taste twins and recommendations
│   ├── venues/            # venues, venue check-ins, menus, venue media, geocoding
│   ├── users/             # profiles, settings, privacy
│   ├── media/             # photo/video upload, resize, cache, image proxy, visibility checks
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
1. **Ent (`entgo.io/ent v0.14.6`) is canonical for entity schema and CRUD:** `ent/schema/*.go` defines all domain entities (`User`, `UserIdentity`, `Session`, `APIToken`, `EmailVerificationToken`, `PasswordResetToken`, `EmailOutbox`, `UserSetting`, `Media`, `Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkFlavour`, `DrinkAlias`, `DrinkImage`, `OffProduct`, `CatalogRevision`, `Venue`, `VenueDrink`, `VenueName`, `VenueSave`, `VenueMedia`, `Checkin`, `CheckinLike`, `CheckinSave`, `Comment`, `Follow`, `Friendship`, `Block`, `Activity`, `Notification`, `Badge`, `UserBadge`, `List`, `ListItem`, `Report`, `AdminNote`, `AppSetting`, `RuntimeSetting`, `AIEndpoint`, `AIDraftSession`, `Integration`, `PushSubscription`, `InboundToken`). `go generate ./ent` produces `ent/migrate/schema.go` (`migrate.Tables`).
2. **Shared connection pool:** `internal/db.Init(dbPath)` opens `modernc.org/sqlite` with connection-level `_pragma` params (`busy_timeout(10000)`, `synchronous(NORMAL)`, `cache_size(-64000)`, `temp_store(MEMORY)`, `mmap_size`, `foreign_keys(ON)`), runs `PRAGMA journal_mode=WAL` and `PRAGMA auto_vacuum=INCREMENTAL`, and wraps the same `*sql.DB` with `entsql.OpenDB(dialect.SQLite, DB)` → `ent.NewClient(ent.Driver(drv))`.
3. **Two-stage deterministic migration pipeline on boot:**
   - **Stage 1 — Ent schema migration (`Client.Schema.Create(ctx)`):** idempotently creates/updates all Ent-managed tables, columns, foreign keys, and indexes from `ent/migrate/schema.go`.
   - **Stage 2 — Versioned migration registry (`internal/db/migrations.go`):** runs ordered, transactional migrations recorded in `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)` for:
     - SQLite features Ent cannot express natively: standalone FTS5 virtual tables (`drinks_fts`), SQLite views (`drink_ratings`), and partial indexes (`WHERE is_primary = 1`, `WHERE off_id IS NOT NULL`, `WHERE deleted_at IS NULL`, `WHERE read_at IS NULL`);
     - Forward data migrations, backfills, and safe column/table transitions across releases.
4. **Permitted raw SQL scope:** entity CRUD goes through `db.Client.<Entity>`. Raw parameterized SQL on `db.DB` is permitted only for FTS5 `MATCH ?` queries, read-heavy projections (`drink_ratings`, Bayesian top lists, keyset feed joins), and `VACUUM INTO` snapshots, with request-path queries wrapped in `db.TraceQueryEx` and prompt `defer rows.Close()`.
5. **Mechanical consistency guard (`internal/db/schema_consistency_test.go`):** CI runs `TestSchemaConsistency` against an initialized `:memory:` DB and fails if any live SQLite table is missing from both `ent/migrate.Tables` and the explicit `rawOnlyTables` allowlist (`drinks_fts*` + `schema_migrations`).

## 3. HTTP surface

Server-rendered pages plus a Bearer-authenticated JSON API. No SPA, no client-side router.

```
GET  /                          feed (friends + own activity)
GET  /nearby                    location picker and nearby pours (signed in)
POST /nearby                    nearby search from a one-shot point (CSRF)
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
POST /checkins/{id}/save        save/bookmark a visible check-in
POST /checkins/{id}/unsave      remove the viewer's bookmark
GET  /saved                     the viewer's saved check-ins, newest save first
POST /checkins/{id}/comments    comment on a visible check-in
POST /comments/{id}/delete      soft-delete own comment (moderator may remove any)
POST /checkins/{id}/delete      soft-delete (owner only)
GET  /compare?drinks=           side-by-side comparison of 2–6 drinks (HTML only)
GET  /venues/{id}               venue page, menu/on-offer list, photos and menus
GET  /venues/saved              venues the member bookmarked
POST /venues/{id}/media         upload a venue photo or menu
POST /venues/media/{id}/delete  remove own venue media (moderator may remove any)
GET  /u/{handle}                profile, badges, top styles, lists, recent (opt-in public)
POST /u/{handle}/follow         follow
POST /u/{handle}/unfollow       unfollow
POST /u/{handle}/block          block (removes friendship and follows both ways)
POST /u/{handle}/unblock        remove only the caller's block
GET  /settings/blocked          blocked accounts + unblock
GET  /friends                   friends + requests
GET  /notifications             notification centre
GET  /notifications/stream      live unread badge over Server-Sent Events
POST /notifications/read        mark one or all read
GET  /settings                  profile, preferences, privacy, security, data
POST /settings/{profile,avatar,preferences,privacy,password,delete}
POST /settings/sessions/{id}/revoke   revoke one session
POST /settings/logout-all       revoke every session including this device
GET  /lists                     own lists including the wishlist
POST /lists                     create a list; add, edit, delete and remove items
GET  /lists/{id}                visibility-scoped list
GET  /taste                     taste twins and recommendations
GET  /passport                  style, venue-kind and country passport
GET  /share/{token}             public share page with OG metadata (no auth, noindex)
GET  /share/{token}/card.png   generated 1200x630 PNG preview (public, 5-minute cache)
POST /share                     mint a passport or streak share link (CSRF, 20/hour)
POST /share/{id}/revoke         revoke an own share link
GET  /mix  GET /mix/{slug}      mix list and detail
POST /mix                       publish a mix of 2–6 drinks
POST /drinks/{slug}/images      add a drink gallery image
POST /drinks/images/{id}/primary  set primary image (admin only)
POST /drinks/images/{id}/delete   remove a gallery image (admin only)
GET  /integrations              outbound integrations + Home Assistant tokens
POST /integrations/ha-token     mint a Home Assistant inbound token
POST /integrations/ha-token/{id}/revoke
POST /hooks/ha/{token}          machine check-in (capability token, no session/CSRF)
GET  /insights                  personal sugar, caffeine and calorie summary
GET  /forgot  GET/POST /reset/{token}  password reset (anti-enumerated)
GET/POST /setup                 first-run admin creation (404 while a live admin exists)
GET  /auth/oidc/login           start OIDC Authorization Code + PKCE S256
GET  /auth/oidc/callback        verify state/nonce/PKCE + JWKS ID token
POST /auth/oidc/logout          clear session + redirect to OIDC end-session
GET  /admin                     moderation overview (admin or moderator)
GET  /admin/settings            runtime settings panel (admin only)
POST /admin/settings/{general,email,email/test,analytics,access,ai,observability}
POST /admin/ai                  save an AI endpoint (admin or moderator)
POST /admin/ai/{id}/toggle      enable or disable an AI endpoint (admin or moderator)
POST /admin/ai/{id}/test        probe an AI endpoint (admin or moderator)
POST /admin/reports/{id}/{resolve,dismiss,remove,notes}   report actions (admin or moderator)
POST /admin/notes/{id}/delete   delete an internal note (admin or moderator)
POST /admin/drinks/{id}/status  lifecycle change (active/limited/seasonal/discontinued)
POST /admin/drinks/{id}/activate  return a drink to active
POST /admin/drinks/{id}/publish  publish or unpublish the public fact sheet (admin only)
GET  /admin/digest              weekly digest status (admin only)
POST /admin/digest              queue the digest now (admin or moderator)
GET  /admin/system              instance diagnostics and backup list (admin only)
GET  /admin/deliveries          outbox history with per-item retry (admin only)
POST /admin/deliveries/{id}/retry   requeue an abandoned delivery (admin only)
POST /admin/backup              create a snapshot now (admin only)
GET  /admin/badges              badge definitions with award counts (admin only)
POST /admin/badges              create a badge definition (admin only)
POST /admin/badges/{id}/edit    edit name/description/kind/rule; slug is immutable (admin only)
POST /admin/badges/{id}/delete  delete only when unawarded (admin only)
GET  /admin/venues              venue merge tool (admin only)
POST /admin/venues/merge        merge source venue into target (admin only)
GET  /healthz /readyz /metrics
GET  /api/openapi.json /api/docs
GET  /api/v1/auth/oidc/status   public OIDC discovery for web + Flutter onboarding
GET  /api/v1/public/drinks/{slug}   public read-only fact sheet (only published drinks; no list route)
GET  /api/v1/me/export          caller's JSON export as an attachment (read scope)
DELETE /api/v1/me               close the caller's account (write scope + verified email + handle confirm)
POST /api/v1/me/push/devices    register an Android FCM device token (write scope)
DELETE /api/v1/me/push/devices  unregister the caller's device token (write scope)
GET  /api/v1/feed/nearby        friend-scoped nearby feed (feed scope, ephemeral coordinates)
GET  /api/v1/notifications      personal notifications (friend-scoped, no firehose)
POST /api/v1/notifications/read mark one notification or all
GET  /api/v1/checkins/{id}      one check-in, visibility-scoped (404 when private)
GET  /api/v1/media/{id}         original photo/video, same visibility rules as the web
GET  /api/v1/media/{id}/thumb   thumbnail (falls back to the original for video)
GET  /api/v1/checkins/{id}/comments   comments on a visible check-in
POST /api/v1/checkins/{id}/comments   comment (write scope + verified email)
DELETE /api/v1/comments/{id}    delete own comment (moderator may remove any)
POST /api/v1/checkins/{id}/save       save/bookmark a visible check-in (204)
DELETE /api/v1/checkins/{id}/save     remove the bookmark (204, idempotent)
GET  /api/v1/me/saved           the caller's saved check-ins (newest save first, no total)
POST /api/v1/users/{handle}/follow    follow (404 for blocked/unknown)
DELETE /api/v1/users/{handle}/follow  unfollow
POST /api/v1/users/{handle}/block     block
DELETE /api/v1/users/{handle}/block   unblock
GET  /api/v1/me/lists           own lists including the wishlist
GET  /api/v1/lists/{id}         visibility-scoped list
GET  /api/v1/mix                own mixes
POST /api/v1/mix                create a mix (2–6 drinks)
GET  /api/v1/mix/{id}           mix by id or slug, visibility-scoped
GET  /api/v1/taste/twins        taste twins (accepted friends)
GET  /api/v1/taste/recommendations   drink recommendations
GET  /api/v1/badges             own badges and progress
GET  /api/v1/passport           own passport stats and badges
GET  /api/v1/venues/saved       saved venues
GET  /api/v1/venues/{id}/media  active venue photos and menus
POST /api/v1/venues/{id}/media  upload a venue photo or menu
DELETE /api/v1/venues/{id}/save   unsave a venue
DELETE /api/v1/venues/media/{id}  remove own venue media
GET  /api/v1/drinks/{slug}/images     one drink's gallery
POST /api/v1/drinks/{slug}/images     upload a gallery image
POST /api/v1/drinks/images/{id}/primary  set primary (admin)
DELETE /api/v1/drinks/images/{id}     delete image (admin)
GET  /api/v1/admin/reports/{id}/notes   internal report notes (admin)
POST /api/v1/admin/reports/{id}/notes   add an internal note (admin)
DELETE /api/v1/admin/notes/{id}         delete an internal note (admin)
GET  /api/v1/stats/me/insights  personal sugar/caffeine/calorie series (?range=7|30|90)
GET  /api/v1/integrations/home-assistant  integration wiring state for a sensor
POST /api/v1/ai/drafts          conversational drink/venue AI drafting ("opencode2")
POST /api/v1/ai/tasting-note    polish rough flavour impressions into tasting note
POST /api/v1/ai/recommend       personalized soda sommelier recommendations
/api/v1/...                     JSON mirror of the above + Flutter Android/CLI
```

Admin HTML access is split by role. `/admin` accepts admins and moderators and renders the moderation queue; the existing report, note, drink-status and manual digest actions use that same role gate. The drink publish action is admin-only. The operational pages `/admin/settings`, `/admin/system`, `/admin/badges`, `/admin/venues` and `/admin/digest` are full-admin only, as is `POST /admin/backup`; moderators receive 403. The admin nav is rendered on all six admin pages for admins, while the More menu only shows the Admin link for `role=admin`.

Public HTML surfaces are `/` and `/u/{handle}` when the owner opted into a public profile. Catalog browse (`/drinks`, `/brands`, `/styles`, `/drops` and the brand/style detail pages), search, drink pages, `/nearby`, notifications, settings and insights all require a signed-in member. Apart from the single public drink fact sheet, every `/api/v1` route remains Bearer-authenticated and friend-scoped, so crawlable HTML never widens the API boundary.

The `internal/api/openapi.json` surface is deliberately bounded. There is no collection endpoint for drinks, brands, venues, users, check-ins, comments or lists. Search is capped at 10 results with no total, one drink's gallery and one venue's media are bounded lists without totals, `GET /api/v1/me/lists` returns only the caller's lists, and mix/taste/badges/passport reads are personal or visibility-scoped. Cross-user reads that fail visibility or hit a block return `404`; only the owner's own resources are addressed by id. The OpenAPI contract test fails if a registered `/api/v1` route is missing from the spec or a documented data route is not registered.

Comment pagination is keyset-based, not offset-based: `GET /api/v1/checkins/{id}/comments` accepts `before` (the oldest comment already seen) and `limit` (50 default, 100 maximum), returns one page oldest-first, and reports `meta.has_more` plus `meta.next_before`. `meta.count` is the size of that page; there is no total. Bookmarks are personal and idempotent: `POST`/`DELETE /api/v1/checkins/{id}/save` return `204`, the check-in must be visible to the caller, and `GET /api/v1/me/saved` returns the caller's visible saved check-ins using the same check-in DTO with `saved: true`. Mention notifications are best-effort: a failure never fails the comment, and the service skips the commenter, the check-in owner (already notified as a comment), blocked pairs, and anyone who cannot see the parent check-in.

Nearby is friend-scoped, not a public location surface. `GET /api/v1/feed/nearby` requires feed scope and `lat`/`lng`, accepts `radius` from 0.1 to 25 km (default 2), `limit` in 1–50 (default 20, explicit out-of-range values are rejected rather than clamped) and an opaque `before` cursor, and is capped at 30 requests per minute per user with `429` and `Retry-After`. Items are the normal feed item plus `distance_m`, and `meta` carries `count`, `has_more` and `next_before`. The web and API request carries the point in the POST body or query string for that request only; it is snapped to three decimals before use, used only for a bounding-box prefilter and an in-Go haversine sort, and never stored, app-logged or echoed. An operator's reverse-proxy access log may still record the request URL, so deploy with `TRUSTED_PROXIES` and short access-log retention. Distances are bucketed to 100 m. The query excludes deleted, suspended and blocked authors, merged or unlocated venues, and caps the candidate set at 200, so `has_more` means more candidates exist rather than a known total. `/nearby` is the signed-in HTML equivalent with CSRF, a 30-per-minute-per-user limit, and progressive enhancement in `web/src/nearby.ts`; there is no anonymous or global nearby feed, and no user location columns exist.

Account lifecycle is owned by `internal/account` and shared by the web and API handlers. `GET /api/v1/me/export` requires read scope and writes the same JSON export as `/export.json` as an attachment with no `data` wrapper. `DELETE /api/v1/me` requires write scope and a verified email, takes `{"confirm":"<handle>"}`, and returns `400 invalid_request` when the confirmation is missing or wrong, `409 last_admin` for the sole live admin, and `204` on success. Deletion soft-deletes the user row, removes sessions, and revokes API and inbound integration tokens in one transaction. There is no purge job and handles stay reserved.

The one unauthenticated catalog route is `GET /api/v1/public/drinks/{slug}`. It is IP-limited to 30 requests per minute, answers only when `drinks.published` is true and the status is active, limited or seasonal and the row is not merged, and returns the same `404` for every other case, including malformed slugs. The DTO is a deliberately narrow fact sheet without internal ids, source, creator, barcodes, aliases or media ids; ratings appear only at three or more raters. Responses set `Cache-Control: public, max-age=300`. There is no collection, search or list counterpart; admins toggle publication with `POST /admin/drinks/{id}/publish`. OpenAPI `info.version` is `1.9.0`.

Middleware order: `requestID → otelhttp → securityHeaders → LoadSession → RequireCSRF → mux`; API mux: `AuthenticateBearer → rateLimit → mux`.

Security headers: CSP `default-src 'self'`, `img-src 'self' data: blob:`, `media-src 'self' blob:`, `worker-src 'self' blob:`, `frame-ancestors 'none'`, nosniff, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy: camera=(self), geolocation=(self)`, HSTS in production. The page CSP includes `script-src 'self' 'wasm-unsafe-eval'` so the ZXing-C++ WebAssembly reader can instantiate; the API CSP is separate and keeps `'unsafe-eval'` scoped to JSON responses. `.wasm` is registered as `application/wasm` in `internal/web/assets.go` so streaming instantiation and service-worker caching work. CSP must also allow the configured Umami origin.

**Indexability:** the middleware sets `X-Robots-Tag: noindex, nofollow` unless the path is `/` or starts with `/u/`. Catalog browse, drink pages and all member surfaces are `noindex`. A profile page is indexable only when `user_settings.public_profile` is on; the handler otherwise sets `X-Robots-Tag: noindex, nofollow` and supplies `index,follow` plus canonical/Open Graph metadata only for opted-in profiles. The API is deliberately excluded from this rule: crawlable HTML never exposes a collection endpoint, and `/api/v1` stays Bearer-authenticated and friend-scoped.

**Home Assistant inbound webhook:** `POST /hooks/ha/{token}` is mounted outside the session and CSRF middleware. The capability token in the path is the credential (a `Bearer` header is also accepted); it is looked up by SHA-256 hash, must be unrevoked, and is scoped to one user. Unknown, revoked and malformed tokens all return the same generic `401 {"error":"unauthorized"}`. The body is capped at 64 KiB, accepts `gtin` or `slug` but not both, plus optional `rating` (0–20), `note`, `venue_id` and `serving`; `Idempotency-Key` is honoured from the header or the JSON body. The route is limited to 10 requests per minute per token. Errors never echo the token, and it is never logged.

CSRF: token stored per session, accepted as `X-CSRF-Token` or `csrf_token` form field, constant-time compare plus `Origin` check against `BASE_URL`; Bearer requests exempt; GET/HEAD/OPTIONS exempt.

Rate limits (in-process token buckets, per IP + per user): login 5/60s, signup 3/h, verification resend 3/h, password reset 5/h, check-in create 60/min, API 120/min, nearby feed 30/min per user, web nearby search 30/min per user, public drink fact sheet 30/min per IP, Home Assistant inbound 10/min per token. Return 429 with `Retry-After`.

## 4. Authentication and accounts

### Local accounts (required for email verification)
- Signup: email + handle + password (bcrypt cost 12, min 10 chars). Handle `^[a-z0-9_]{3,24}$`, reserved list (`admin`, `api`, `settings`, `sodapop`, …).
- Email verification: 32-byte token, sent as `https://host/verify?token=…`, only SHA-256 stored, 24h TTL, single use, all older tokens invalidated on resend. Verification marks `users.email_verified_at`.
- Gate policy: unverified users **can** log in but are read-only — no check-ins, follows, venue submissions, or photo uploads. A persistent banner offers resend. Rationale: avoids dead-end signups and lets people browse immediately.
- Login: email or handle, generic error, bcrypt compare, session rotation on login, throttle keyed `IP|identifier`.
- Password reset: 1h single-use token, anti-enumeration response, consumes token, invalidates all sessions.
- Account deletion: shared `internal/account` service with the web and API; soft-delete `status=deleted`/`deleted_at`, delete sessions, revoke API and inbound tokens in one transaction. The last live admin is refused with `last_admin`. There is no purge job and handles remain reserved.

### Sessions
32 random bytes → cookie value; store SHA-256 hex in `sessions(token_hash PRIMARY KEY, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)`. Cookie `sd_session`, `HttpOnly`, `Secure` when HTTPS, `SameSite=Lax`, TTL 720h sliding with 5-minute write throttle. Cleanup ticker every 6h. Logout deletes row; "log out everywhere" deletes all rows for user.

### OIDC authentication (Authelia & standard providers)
Modeled directly on `dnd` and `board-games`:
- **Protocol:** OpenID Connect Authorization Code flow with PKCE (`S256`) against `OIDC_ISSUER_URL` (e.g. `https://authelia.vandijke.xyz`), scopes `openid email profile groups`.
- **Discovery & verification:** discovers authorization, token, userinfo, end-session, and `jwks_uri` from `/.well-known/openid-configuration`. Callback validates HMAC-signed HttpOnly cookie `state`, `nonce`, and PKCE `code_verifier`, then verifies the ID token's RS256 signature against cached JWKS keys, `iss`, `aud` (`OIDC_CLIENT_ID`), and `exp`.
- **Account linking & provisioning:** requires `email_verified=true` from the provider. Looks up `(provider='oidc', subject=sub)` in `user_identities`; if absent, links to an existing `users.email` (case-insensitive) and stamps `email_verified_at` if unset, or auto-provisions a new verified user (deriving a unique handle from `preferred_username` or email local-part).
- **Allowlists & role mapping:** enforces `ALLOWED_EMAILS` and `ALLOWED_DOMAINS` when configured. The first user on a fresh database becomes `admin`; on every OIDC login, if the `groups` claim contains `admins`, the user's role is promoted to `admin`.
- **Recovery when the last admin is demoted:** OIDC group mapping can remove the last admin. `/setup` reopens whenever there are zero live admins (`role=admin`, active, not deleted), even if the historical `setup_complete` row exists. The next request or restart installs `SETUP_TOKEN` (or generates and logs a one-time token), and setup can create a replacement admin. This is the supported recovery path rather than blocking external demotion.
- **Coexistence with local accounts:** when `OIDC_ENABLED=true`, `/login` shows **"Sign in with Authelia"** as the primary action. Local email+password signup/login and email verification remain available when `ALLOW_PASSWORD_FALLBACK=true` (default `true` when `OIDC_ENABLED=false`; explicit break-glass toggle when `OIDC_ENABLED=true`).
- **Mobile & API discovery:** `GET /api/v1/auth/oidc/status` returns `{"enabled": true, "issuer": "...", "password_fallback": false}` so the Flutter Android onboarding screen can automatically present the Authelia WebView login flow.

### API tokens
Prefix `sd_`, 32-byte secret shown once, SHA-256 at rest, scopes `read`/`write`/`feed`, `revoked_at`, `last_used_at`. Used by the Flutter Android app, PWA offline sync, and CLI import. Authenticated WebView sessions can also mint a device-scoped `sd_` token via `POST /api/v1/auth/mobile-token` so the native Android app does not depend on WebView cookie lifetime.

### Public API, Swagger, and anti-scraping
Interactive documentation is served at `GET /api/docs/` (Scalar/Swagger UI) from the OpenAPI 3.1 document at `GET /api/openapi.json`. Both are public. Every data endpoint under `/api/v1` requires a Bearer `sd_` token except the single opt-in public drink fact sheet at `GET /api/v1/public/drinks/{slug}`, which is rate limited by IP and configured with `security: []`.

The API is deliberately **not a public catalog dump**. There is no endpoint that lists all drinks, brands, venues, users, or check-ins. The only unauthenticated catalog route is one drink's opt-in fact sheet, and it has no collection or search counterpart; publishing is an explicit admin action per drink. Search returns at most 10 results and omits total counts, so paging through it cannot reconstruct the catalog. Reading another person's check-ins requires an accepted friendship, otherwise the server returns `404` rather than `403` to avoid confirming that a handle exists. Friend requests for unknown handles return the same `202` response. Authenticated users may only read their own data and that of accepted friends; there is no global feed. All mutation endpoints are rate limited, and the OpenAPI description states these constraints so integrators do not build scrapers against it.

## 5. Email

Hand-rolled `net/smtp` sender (house style), supporting implicit TLS 465, STARTTLS, and plain, with `SMTP_HOST/PORT/USER/PASS`, `SMTP_FROM`, `SMTP_TLS`, 15s timeout. HTML + plain-text templates for: verify address, reset password, friend request (sent only when the recipient's `email_friend_req` setting is on), weekly digest (opt-in), venue/drink report outcome.

Delivery is queued in `email_outboxes(id, to_email, template, payload_json, attempts, next_attempt_at, sent_at, last_error)`. A worker ticks every 30s, with exponential backoff 1m→1h and max 8 attempts, then marks the row failed and records `last_error` for operators. This keeps signup fast and survives SMTP outages. Dev mode writes messages to `data/mail/` instead of sending.

## 6. Catalog and barcodes

`drinks` holds the canonical product; `drink_barcodes` maps many GTINs to one drink (multipacks, regional packaging); `brands`, `styles`, `drink_images` support it. Barcode is the primary key for scanning but never the identity of a drink.

`drinks.published` is a per-drink opt-in for the public fact sheet and defaults to false. Publication is a separate admin action from lifecycle status: the public route additionally requires a non-merged active, limited or seasonal row. The narrow `catalog.PublicDrink` DTO is assembled in `internal/catalog/public.go` and deliberately omits internal ids, source, creator, barcodes, aliases and media ids.

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
- Nearby: `social.Service.Nearby` reuses the feed visibility ladder against venue coordinates. It snaps the viewer point to three decimals, builds a bounding box for the SQL prefilter, computes haversine distance in Go, buckets it to 100 m and paginates by an opaque cursor over `(distance bucket, created_at, id)`. It excludes merged and zero-coordinate venues, deleted and suspended authors, and blocked pairs, and caps candidates at 200. The viewer point arrives in the web POST body or API query string for that request only and is never written to SQL, app logs or a response; an operator's reverse-proxy access log may record the URL, which is why deployment pairs `TRUSTED_PROXIES` with short log retention. Migration `0010_checkins_venue_index.sql` adds `idx_checkins_venue_created` on `checkins(venue_id, created_at) WHERE deleted_at IS NULL`; there are no user location columns, and a DB test enforces that.
- Privacy: `public | friends | private` per check-in and per profile default; private check-ins still count for personal stats, never appear in feeds.
- Notifications: in-app table with kinds `friend_request`, `friend_accept`, `toast`, `comment`, `mention`, `badge` and `system`. Comment and mention rows deep-link to `/checkins/{id}#comments`. The nav shows an unread count, `/notifications` lists the newest 50, and `POST /notifications/read` marks one item (with `id`) or all. `GET /api/v1/notifications` and `POST /api/v1/notifications/read` expose the same personal scope. Friend-request email is sent only when the target's `email_friend_req` setting is on. Mention delivery is best-effort and skipped for the commenter, the check-in owner, blocked pairs, and users who cannot see the parent check-in. `social.Service.OnNotify` fans out `friend_request`, `friend_accept`, `toast`, `comment` and `mention` to integrations and Web Push after the in-app row is saved; `badge` and `system` stay in-app. URLs are `/friends` for friend events, `/checkins/{id}` for toasts, and `/checkins/{id}#comments` for comments and mentions. Comment creation routes through `social.Notify`, so a comment push fires once. Subscribing a browser to push sets `push_enabled=true` (creating the preference row if absent); unsubscribing removes the device and leaves the preference unchanged. Native Android push uses FCM. An authenticated client registers a device token at `POST /api/v1/me/push/devices` (unique, user-scoped `push_devices` rows); when `FCM_SERVICE_ACCOUNT_JSON` and a project id are configured, notification fan-out enqueues one `fcm` delivery per registered device and the worker mints an access token from the service-account key (RS256 JWT assertion exchanged at the token endpoint, cached until shortly before expiry) before posting to FCM HTTP v1. Tokens rejected as `UNREGISTERED` or `SENDER_ID_MISMATCH` are deleted; with no credentials the path is a silent no-op. The signed-in web client keeps its nav badge current through `GET /notifications/stream`: a per-connection Server-Sent Events stream that sends the current unread count immediately, re-checks every 15 seconds, writes only on change, sends a comment heartbeat every 20 seconds and closes itself after 30 minutes so EventSource reconnects. The handler resolves the session itself and returns `401` instead of a login redirect, clears the server's 60-second write deadline, and disables proxy buffering; the Flutter client cannot use EventSource and still refreshes on demand.
- Taste is friend-scoped and visibility-safe. `internal/taste.Queue` returns up to five drinks that similar accepted friends rated highly and the viewer has not checked in, each with one to three reasons (twin rating, new style, new brand, above the viewer's average). Every cross-user check-in read filters `visibility IN ('public','friends')`, so a private friend rating can neither create a recommendation nor change a twin score; the queue is derived on request with no table, no catalog dump and no global ranking.
- Blocks are enforced on read, not only stored: feed queries exclude either direction, check-in detail returns 404, comments and toasts reject blocked pairs, notifications are dropped or hidden, profiles return 404, and API list reads return 404. Blocking also deletes the friendship row and both follow directions in one transaction; unblocking removes only the caller's block and does not restore them. There is no user-returning search surface to filter; catalog and venue search contain no user rows. `/settings/blocked` lists the accounts the viewer blocked and offers unblock.
- Check-in visibility is enforced by `checkins.Service.Get`: owner sees everything, a friend sees `public` and `friends`, an anonymous or non-friend viewer sees only `public`. Everything else returns `ErrNotFound`, which the web and API layers map to 404.
- Comments inherit the parent check-in's visibility. `comments.Service.ListPage` and `Create` call the same `checkins.Get`, so an invisible parent is a 404 for both; rows from blocked authors are filtered in SQL. Comments are flat, 1,000 characters, 30 per user per hour, soft-deleted by the author or a moderator, and read oldest-first in keyset pages (`before` cursor, 50 default, 100 maximum) with `has_more`/`next_before` and no total. `@handle` mentions render as links and send a best-effort `mention` notification when the mentioned user can see the parent and neither side has blocked the other. A comment notifies the check-in owner unless they wrote it or either side has blocked the other.
- Public profiles are opt-in through `user_settings.public_profile`. A deleted account is always hidden. When public, anonymous visitors see the profile and only `public` check-ins; a signed-in stranger sees the same; the owner and accepted friends see `public`, `friends` and `private` rows. When not public, anonymous visitors are redirected to `/login` and a signed-in non-friend gets a 404.
- Insights are self-only. Sugar, caffeine and calories are derived from the drink's per-100 ml facts multiplied by the logged volume; merged and provisional drinks are excluded from the top-drinks list but still counted in totals. The series is zero-filled for each of the 7, 30 or 90 calendar days, caffeine clearance halves the last dose every 5 hours until it falls below 20 mg, and `late_caffeine` is true when the last dose plus 8 hours lands after `caffeine_bedtime`.
- Outbound integrations are stored one row per user and kind (`home_assistant`, `matrix`, `trmnl`) with an optional secret and an enabled flag. Event dispatch is fire-and-forget: Home Assistant receives `{event,text,url}`, Matrix receives an `m.text` message, TRMNL receives a `merge_variables` JSON body, and push delivery is skipped unless `user_settings.push_enabled` is on. The shared HTTP client has a 10 s timeout and each dispatched event runs under a 5 s context. Integration failures are logged and never block the check-in write.

## 8. Media

Uploads via multipart. Images are capped at 12 MiB, MIME-sniffed and re-decoded (`image/jpeg|png|webp|gif`), resized to a 1600 px longest edge with a 480 px thumbnail, EXIF-stripped, and stored under `/data/media/<yy>/<mm>/<id>.jpg`. Short MP4/WebM/QuickTime clips are capped at 120 MiB, sniffed rather than trusted by extension, streamed to disk untouched, and have no poster or thumbnail without ffmpeg; the check-in detail page renders a `<video>` with a download fallback and the Android app links to the web app. There is no gallery transcoding, no standalone video hosting and no video feed. Served with private, long cache headers and content-addressed filenames. Check-in uploads follow the check-in rate limit; drink and venue galleries add a 10-per-hour upload cap per user.

`media.Service.CanView` is the single access check for the web `/media/{id}` routes and the Bearer-authenticated `/api/v1/media/{id}` and `/thumb` routes. The owner always sees their own row. Avatars, drink images and brand logos are public. A check-in photo or clip follows the parent check-in's `public`/`friends` visibility and stops being visible when the check-in is soft-deleted; a `friends` row requires an accepted friendship. Venue media is public while its `venue_media` row is active and the venue is not merged away; deleting the attachment revokes access while keeping the file. Orphan media rows are not public. Blocks are enforced on the feed, detail, comment, notification and list read paths; media access is derived from the owning row's visibility rather than a separate grant table. The API exposes the same check through the Bearer-authenticated `/api/v1/media/{id}` path, and the Flutter client has a helper (`ApiClient.mediaUrl`) for that authenticated path.

Drink galleries are stored in `drink_images` plus `media`; any verified member may add an image with attribution (new uploads are not auto-promoted), while setting the primary image and deleting an image are admin-only. Venue photos and menus are stored in `venue_media` (`kind` `photo|menu`, optional 80-character label) with a 6-per-uploader/venue and 20-per-venue active cap. Gallery reports reuse the shared `reports` table with `entity` `drink_image` or `venue_media`; admin removal soft-deletes the attachment (venue media) or deletes the image row (drink gallery). Open Food Facts images remain proxied and attributed separately under §6.

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

- **Stack:** Flutter (Dart `^3.9.0`, Material 3), `flutter_riverpod`, `dio`, `flutter_secure_storage`, `shared_preferences`, `webview_flutter`, `mobile_scanner` (native Android CameraX + ML Kit barcode decoding with hardware torch/zoom), `geolocator` for one-shot Nearby location, `image_picker`, `video_player`, `connectivity_plus`.
- **Zero hardcoded server URL:** the onboarding screen starts with an empty server URL field, validates `http(s)` origin format, persists the normalized URL in `flutter_secure_storage`, and blocks API traffic until configured.
- **Authentication:**
  - Probes `GET /api/v1/auth/config` and `GET /api/v1/auth/oidc/status` for the entered server URL.
  - When OIDC is enabled, displays **"Sign in with Authelia"** as the primary button, opens `/auth/oidc/login` in an in-app WebView after clearing stale cookies, captures the `sd_session` cookie via the native Android cookie channel (`xyz.vandijke.sodapop/cookies`), and exchanges it for a long-lived device `sd_` Bearer token (`POST /api/v1/auth/mobile-token`).
  - Also supports pasting a personal API token (`sd_…`), native registration with email verification and resend, native password-reset request, and email/password sign-in when `ALLOW_PASSWORD_FALLBACK=true`.
- **Parity & offline:** native bottom nav (`Friends`, `Discover`, lifted lime `Scan` button, `You`), instant ML Kit GTIN-14 barcode scan → check-in sheet, AI tasting-note polish and AI draft sessions, photo or short-video check-in (video is online-only), and a `shared_preferences` check-in outbox with idempotency keys (photos up to 2 MiB, dropped with a message above that). Rich check-ins carry serving, venue, visibility and a 1,000-character note. The member surface includes drink detail with nutrition/gallery/native video, compare, friends and requests, comments with mentions and pagination, saved posts, lists with CRUD, passport stamps, badges, taste, insights, mixes, venues with photo/menu upload, Nearby from Discover with one-shot coarse/fine location and 0.5–25 km radius chips, account export and closure, account settings, app-wide theme, session revoke and blocked people. Offline reads cache the last feed and 30 drinks; the outbox flushes on reconnect or resume. Every API call carries a W3C `traceparent`. Admin surfaces are not in the app; there is no background location. Push is opt-in: with `FCM_*` defines the app initialises Firebase from explicit options (no `google-services.json`), registers the FCM token after sign-in and deletes it on sign-out; without them the push code path never runs.
- **Branding:** vector adaptive launcher icon (`#B4E32C` lime fill + `#16241A` forest mark), Android 13+ monochrome themed icon, API 31+ splash screen.

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

Set `TRUSTED_PROXIES` to the reverse proxy's exact IPs or CIDRs when the app is behind a proxy. Without it, `/setup` treats every client as the proxy address, so one stranger can exhaust a shared bucket; configure it alongside proxy-aware limits so clients are distinguished consistently. With it, forwarded hops are walked from the right and trusted hops are skipped.

Secrets accept `_FILE` variants. Invalid config fails fast with a readable error; secrets never appear in logs.

### Runtime settings

`/admin/settings` overlays a small, audited set of operational values on top of env config. Precedence is **built-in default < env < database**, loaded once into an immutable snapshot so request paths never query settings tables. Database overrides are denied for bootstrap-only keys (`ENV`, `PORT`, `DATABASE_PATH`, `DATA_DIR`, `SESSION_SECRET`, `OTEL_*`, `EMAIL_DEV_DIR`, `SEED_DEMO`); OTel is shown read-only with a restart label. Runtime-overridable keys include `APP_NAME`, `BASE_URL`, SMTP host/port/username/password/from/TLS, `UMAMI_URL`, `UMAMI_WEBSITE_ID`, registration, password fallback, allowlists and AI enablement. The settings page is limited to full admins (`requireAdminStrict`); moderators keep their moderation tools but cannot change instance-wide values.

Secrets are stored only as AES-256-GCM ciphertext (`enc:v1:`) under a domain-separated key derived from `SESSION_SECRET`; the UI shows `set` plus the last four characters, a blank field preserves the stored value, and an explicit clear removes it. Changing `SESSION_SECRET` makes old ciphertext unreadable (the instance falls back to env/default and logs a warning). Each write is transactional and appends an `admin_notes` audit row containing key names only. First-run setup uses the same storage to record `setup_complete` and create the first verified admin atomically; the one-time token comes from `SETUP_TOKEN` or a generated value logged once on a fresh database.

Report-scoped internal notes use the same `admin_notes` table with `entity='system'` and the report id as `entity_id`; they are appended from `/admin/reports/{id}/notes` and removed from `/admin/notes/{id}/delete`. Non-admins get `404` on both the HTML and API surfaces. Catalog edits continue to append `catalog_revisions`, and migration `0009_audit_indexes.sql` adds the `(entity, created_at)` lookup indexes used by the admin queue and revision feed.

## 14. Ops

- Multi-stage Dockerfile: `node:24-alpine` → `golang:1.27-alpine` (`CGO_ENABLED=0`, `-trimpath -ldflags="-s -w -X sodapop/internal/ai.Version=${VERSION}"`) → `alpine:3.24`, non-root user, `/data` volume, `HEALTHCHECK` on `/healthz`.
- Compose: bind `127.0.0.1:6291`, `env_file: .env`, named volume, healthcheck, `stop_grace_period: 20s`, optional `observability` profile.
- Graceful shutdown: signal context → HTTP shutdown 15s → stop workers → flush OTel providers 10s → close DB.
- Backups: `internal/backup` takes a consistent snapshot with `VACUUM INTO` into `$DATA_DIR/backups` (default `data/backups`), named `sodapop-YYYYMMDD-HHMMSS.db` with a numeric suffix if two land in the same second. Each snapshot is chmod `0600`, opened read-only and required to return `ok` from `PRAGMA integrity_check`, and the newest 14 are retained. The worker runs at startup and then hourly, with a 24-hour gate and a `backup` lease row in `scheduler_state` (10-minute lock TTL); a failed snapshot is logged and never stops the server. `POST /admin/backup` creates and prunes on demand.
- Restore: intentionally offline. Stop the server, then run `go run ./cmd/restore -from <backup> -db <database> -yes`. The command verifies the snapshot's integrity and required tables, writes an atomic copy over the target, and removes stale `-wal`/`-shm` files. There is no HTTP restore route. Backups are standalone files outside SQLite.
- Starter catalog: `SEED_CATALOG=1` (Taskfile `task seed:catalog`) idempotently upserts 21 brands and 29 drinks by slug, with no barcodes or images and uncertain nutrition left at schema defaults.
- Jobs: session cleanup (6h), expired tokens (hourly), email outbox (30s), OFF retry queue, backup (startup + hourly, 24h gate), soft-delete purge (daily), badge evaluation after check-ins.
- Health: `/healthz` liveness, `/readyz` DB ping + migrations applied + media dir writable.

## 15. Testing

- Go: table-driven unit tests per package; handler tests with `httptest` and a real router; per-test in-memory SQLite (`file:test-<nanos>?mode=memory&cache=shared`). Seed helper builds admin + two friends.
- AI & OIDC: unit + `httptest` suites verifying `NormalizeAIBaseURL`, `SetAIProviderHeaders` (`User-Agent: sodapop/<version>`, `x-opencode-session` continuity), AES-GCM key encryption, truncated response handling, and OIDC PKCE/JWKS callback verification.
- Barcode: fixture GTINs (EAN-13, UPC-A, EAN-8), decoder output parsing, OFF payload fixtures, dedupe/merge cases.
- Frontend: Vitest for scanning state machine and offline queue; Playwright (serial workers) for signup→verify→scan→check-in; axe pass on all top-level pages.
- Mobile: `flutter analyze` and `flutter test` widget/unit suites covering URL validation, auth screens, OIDC status probe, GTIN-14 check digit, check-in queue serialization, read cache, theme preference, comments/mentions/pagination, profiles, friends, notifications, lists CRUD, compare, drink detail/galleries/video, insights, taste, passport stamps/badges, mixes, venues/media, settings and AI drafting.
- v1.2 contract tests: `internal/api/contract_test.go` asserts every registered `/api/v1` route appears in `openapi.json` and no documented data route is unregistered; `e2e/api-parity.spec.ts` covers the friend-scoped/no-totals/404 rules.
- Integration: `cmd/fakeoff` and a mail sink (files) so no network is required in CI.
- Coverage floors: Go ≥45% overall, handlers/middleware ≥60%; TS ≥50%.

## 16. CI & Release

- `.github/workflows/ci.yaml`: gofmt/vet/`go test -race` with coverage; `npm ci` + build + typecheck + Vitest; Playwright with artifacts; actionlint/zizmor/pinact.
- `.github/workflows/flutter-android.yaml`: `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with SHA-pinned actions and `continue-on-error: true`.
- `.github/workflows/release.yaml` + `.releaserc.yaml`: semantic-release on `main` (Conventional Commits) updating `CHANGELOG.md` and `mobile/pubspec.yaml`, publishing multi-arch GHCR/Docker Hub images, building & attaching the signed `sodapop-<version>-android.apk` asset to the GitHub Release (plus a signed App Bundle kept as a workflow artifact rather than a public download), and exporting CI OTel spans. `.github/workflows/play-store.yaml` then uploads that bundle to Google Play when `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON` is configured.

## 17. Deliberate non-goals for v1

- Native iOS app (iOS uses the installable PWA with WebAssembly barcode fallback; Android has both the PWA and the native Flutter app in `mobile/`).
- Federation/ActivityPub, real-time chat.
- Multi-region or Postgres; SQLite is the product.
- Server-side image CDN, a video platform/feed or stories. Short check-in clips are supported through the media service; there is no standalone video hosting, transcoding pipeline or algorithmic video feed.
- Paid business accounts or subscriptions.
