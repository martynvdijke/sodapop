# Design: Sodapop v1 Architecture & Technical Decisions

## Context

Sodapop synthesizes the proven conventions across `/root/projects/board-games`, `/root/projects/dnd`, and `/root/projects/datey`:
- Go 1.27 stdlib `net/http` router, server-rendered `html/template` + Vite 8 / TypeScript 7 / Tailwind 4 islands, and `/api/v1` JSON endpoints from `board-games`.
- **Ent ORM (`entgo.io/ent v0.14.6`) + versioned SQLite migration registry + mechanical schema consistency guard (`schema_consistency_test.go`)**, **Authelia OIDC PKCE + JWKS**, **OpenCode ("opencode2") AI client (`internal/ai`)**, and **Flutter Android companion app (`mobile/`)** from `dnd` and `board-games`.

## Key Decisions

### 1. Ent ORM + Versioned SQLite Migrations (`ent/` + `internal/db/migrations/`)
- **Decision**: Use `entgo.io/ent v0.14.6` over `modernc.org/sqlite` as the canonical schema (`ent/schema/*.go` → `ent/migrate/schema.go`) and standard entity CRUD layer (`db.Client.<Entity>`), paired with an ordered transactional migration registry (`internal/db/migrations/registry.go`, recorded in `schema_migrations`) for SQLite-specific constructs that Ent cannot model directly (`drinks_fts` FTS5 virtual table, `drink_ratings` view, partial unique indexes) and forward data migrations.
- **Why**: Gives strongly-typed Go entity CRUD, automatic foreign-key/edge management, and declarative schema evolution via `Client.Schema.Create(ctx)` while preserving deterministic versioned SQL migrations for FTS5/views and enforcing zero drift via `TestSchemaConsistency`.

### 2. Dual Auth: First-Class Authelia OIDC + Verified Local Accounts
- **Decision**: Support OIDC Authorization Code + PKCE S256 (`OIDC_ENABLED`, `.well-known/openid-configuration`, RS256 JWKS verification, `email_verified=true` requirement, `admins` group → `admin` role sync) alongside local email+password accounts (`ALLOW_PASSWORD_FALLBACK`, `bcrypt` cost 12) where unverified local signups get a read-only session until `/verify?token=…` is completed.
- **Why**: Matches the homelab Authelia SSO workflow in `dnd` and `board-games` while fulfilling the requirement for self-contained email verification on standalone deployments.

### 3. Barcode Decoding & Open Food Facts ODbL Isolation
- **Decision**: Use native `BarcodeDetector` where available and lazy-load `barcode-detector` (ZXing-C++ WebAssembly in a Web Worker) on iOS Safari/Firefox; normalize all barcodes to 14-digit GTIN-14 with GS1 check-digit validation; isolate Open Food Facts v3.6 responses in `off_products` (`15 req/min` hard ceiling, custom `User-Agent`).
- **Why**: iOS Safari does not ship `BarcodeDetector` in production; UPC-A and EAN-13 representations must collide on a single key; ODbL 1.0 requires keeping OFF-sourced records separated and attributed.

### 4. OpenCode ("opencode2") AI Subsystem (`internal/ai`)
- **Decision**: Port `dnd/ai` directly: store endpoints in `ai_endpoints` with AES-GCM encrypted API keys derived from `SESSION_SECRET`, normalize base URLs (`NormalizeAIBaseURL` stripping `/chat/completions`, `/images/generations`, `/completions`), attach honest `User-Agent: sodapop/<version>` and multi-turn `x-opencode-session` headers (`SetAIProviderHeaders`), enforce `30s/60s/120s/180s` timeouts, and guard against `finish_reason == "length"` truncation.
- **Why**: Enables 1-tap drink drafting from rough notes/labels, check-in tasting-note polish, and Soda Sommelier recommendations against OpenCode Zen (`https://opencode.ai/zen/go/v1`) or any OpenAI-compatible provider.

### 5. Flutter Android App (`mobile/`)
- **Decision**: Ship a Flutter Android client in `mobile/` with zero hardcoded server URL, `/api/v1/auth/oidc/status` discovery, Authelia in-app WebView sign-in + native Kotlin `CookieManager` bridge → `POST /api/v1/auth/mobile-token` (`sd_` token), `mobile_scanner` CameraX/ML Kit barcode scanning, and an offline check-in queue.
- **Why**: Provides native Android camera performance, hardware torch/zoom, and seamless Authelia SSO while sharing `/api/v1` with the PWA.
