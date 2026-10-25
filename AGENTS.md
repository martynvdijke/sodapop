# AGENTS.md

House rules for working in Sodapop. Read this before changing code.

## What this is

Sodapop is a self-hosted, barcode-first check-in community for soft drinks. Go 1.27 + SQLite backend, server-rendered HTML, a TypeScript-free PWA (CSS + service-worker ready), `/api/v1` for the Flutter Android app (`mobile/`), OpenTelemetry, and optional Umami analytics.

## Commands

```sh
task generate        # regenerate Ent code after changing ent/schema
task build           # go build ./cmd/server
task run             # run against ./data/sodapop.db
task seed            # seed styles, flavours, badges; SEED_DEMO=1 for demo drinks
task test            # go test ./...
task fmt             # gofmt -w ./cmd ./internal
task vet             # go vet ./...
task e2e             # Playwright (needs chromium: npx playwright install chromium)
task ci              # fmt + vet + test + e2e
task mobile-analyze  # flutter analyze in mobile/
task mobile-test     # flutter test in mobile/
task mobile-apk      # release APK (needs JDK 17+)
```

Not using Task? The underlying commands are in `Taskfile.yml`. Always run `go test ./...` and `CI=1 npx playwright test` before proposing a commit.

## Architecture rules

- **Ent is the schema.** Add or change fields in `ent/schema/*.go`, then `task generate`. Never hand-edit `ent/` generated files.
- **Migrations are versioned.** `Client.Schema.Create` runs first; `internal/db/migrations/*.sql` then adds FTS5 tables, views, partial indexes, seeds, and backfills. New migrations are append-only, numbered, and transactional. `TestSchemaConsistency` fails CI when a table exists in SQLite but not in Ent or the raw-only allowlist.
- **SQLite settings are deliberate:** WAL, `busy_timeout(10000)`, foreign keys on. Keep writes short; use `db.WithTx`/Ent transactions for multi-table writes.
- **Thin handlers.** Business logic lives in `internal/{catalog,checkins,social,venues,auth,...}`. Handlers parse input, call a service, map errors.
- **Server-rendered first.** Use `html/template` with `base.html` + pages + partials and progressive enhancement. No SPA router, no client-side data fetching for core pages.
- **The API is friend-scoped and personal, never a catalog dump.** `/api/v1` requires a Bearer `sd_` token. Do not add collection endpoints that list all drinks, brands, venues, users, or check-ins. Search is capped (10) and returns no total counts. Cross-user reads return 404 unless the viewer is a friend and the row is visible. Never add a global feed.
- **Swagger is public, data is not.** `GET /api/docs/` and `GET /api/openapi.json` are unauthenticated; every data route stays behind auth. Update `internal/api/openapi.json` whenever a route or schema changes.
- **Auth:** Bearer tokens are hashed at rest and scope-checked. Session cookies are `HttpOnly`, `Secure` in production, `SameSite=Lax`; every mutation needs a CSRF token. Local accounts must verify email before writing. OIDC maps the Authelia `admins` group to the admin role.
- **Ratings are integers 0–20** (quarter stars). Never store floats. The average is hidden below 3 raters.
- **Barcodes are GTIN-14.** Normalize on ingress and validate the GS1 check digit. Open Food Facts data stays in `off_products` with ODbL attribution; never merge it wholesale into Sodapop-authored rows.
- **Parameterized SQL only.** Whitelist sort columns. Scope every user-owned query by `user_id`.

## Data and security gotchas

- User-owned rows use random 16-byte hex string IDs; shared catalog rows use autoincrement integers. Timestamps are RFC 3339 UTC strings.
- Soft-delete check-ins with `deleted_at`; never hard-delete user content without an explicit admin action.
- Secrets come from env; `SESSION_SECRET` is mandatory in production. Never log tokens, cookies, passwords, or SMTP credentials.
- Rate-limit auth, search, barcode resolution, and creation endpoints. Return generic auth errors to avoid account enumeration.
- The scanner requires HTTPS (or localhost). Camera permission is requested only from a user gesture; stop tracks when the sheet closes.

## Testing

- Table-driven Go tests with `httptest`; use a fresh in-memory SQLite per test.
- Playwright tests live in `e2e/` and run serially. Keep the API anti-enumeration test passing — it is a product requirement, not a nicety.
- axe checks are part of E2E. Fix serious/critical violations rather than excluding rules.
- Mobile: `flutter analyze` must be clean and `flutter test` green.

## Commits and releases

- Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`). `feat` bumps minor; `fix` patches. Releases are cut by semantic-release on `main`, which also builds the Docker image and attaches the Android APK.
- Do not commit `data/`, `*.db`, `.env`, Playwright output, `node_modules/`, Flutter build output, or signing keys. `.gitignore` covers these.
- Keep `README.md`, `docs/`, and `openspec/specs/` in sync with behaviour. The OpenSpec change under `openspec/changes/add-sodapop-v1/` is the v1 plan.
