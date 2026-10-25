# Change: Add Sodapop v1 (Untappd for Soda)

## Why

There is no self-hosted, barcode-first check-in community dedicated to soft drinks (craft sodas, colas, ginger beers, root beers, sparkling waters, energy drinks, iced teas, and kombuchas). Existing tools are blogs, retailers, or unstructured forums. Sodapop brings the Untappd check-in loop — scan a barcode, rate in quarter-stars, log the venue (including hotels and vending machines), toast friends' check-ins, and earn badges — to a single-binary Go + SQLite stack with a fresh lime PWA and a native Flutter Android companion app.

## What Changes

- **Data Layer (Ent ORM + SQLite Migrations)**: `modernc.org/sqlite` (WAL) managed by **Ent ORM (`entgo.io/ent v0.14.6`)** (`ent/schema/*.go` + `Client.Schema.Create`) paired with a **versioned transactional migration registry (`internal/db/migrations/registry.go`, `schema_migrations`)** for FTS5 (`drinks_fts`), SQL views (`drink_ratings`), partial unique indexes, and data backfills, guarded by `internal/db/schema_consistency_test.go`.
- **Auth, OIDC & Email Verification**: First-class Authelia OIDC Authorization Code + PKCE S256 (`/auth/oidc/login|callback|logout`, `/api/v1/auth/oidc/status`, RS256 JWKS verification, `admins` group → `admin` role sync) plus local email+password signup (`bcrypt`, `ALLOW_PASSWORD_FALLBACK`) with mandatory email verification (`email_verification_tokens`, read-only gate until verified), DB sessions (`sd_session`), CSRF, and `sd_` personal/mobile API tokens.
- **Catalog, Soda Taxonomy & Barcode Scanner**: 17 parent style families + substyles, intensity-scored flavour tags, soda descriptors (`sweetener`, `carbonation`, `acid`, caffeine/sugar/calories per 100 ml), GTIN-14 normalization + GS1 check digit, browser `BarcodeDetector` + mandatory `barcode-detector` (ZXing-C++ WebAssembly worker) fallback, Open Food Facts v3.6 resolver with `15 req/min` rate-limit guard and ODbL isolation (`off_products`), and unknown-soda provisional creation with soft-key dedupe.
- **Check-ins, Social, Venues, Badges & Lists**: Quarter-star ratings (`0..20` integer steps), idempotency keys, WebP/JPEG photo pipeline, mutual friendships + one-way follows, chronological keyset feed with one-tap Toast (and 5 s undo), venues (`hotel`, `bar`, `restaurant`, `cafe`, `shop`, `festival`, `vending`, `other`) with Nominatim proxy and 150 m dedupe, idempotent badge evaluation, and custom lists + Wishlist.
- **OpenCode ("opencode2") AI Subsystem (`internal/ai`)**: Ported from `dnd/ai` — admin-managed OpenAI-compatible / OpenCode Zen (`https://opencode.ai/zen/go/v1`) endpoints in `ai_endpoints` with AES-GCM encrypted API keys, `NormalizeAIBaseURL`, honest `User-Agent: sodapop/<version>` + `x-opencode-session` continuity, operation timeouts (`30s/60s/120s/180s`), `finish_reason == "length"` truncation guard, conversational/vision drink drafting, check-in tasting-note polish, and Soda Sommelier recommendations.
- **Observability (OTel) & Analytics (Umami)**: OpenTelemetry (`v1.47.0` core, `otelhttp v0.72.0`, `otelslog v0.21.0`, explicit env parsing for sampler/propagators/kill-switch, Prometheus `/metrics`, collector `0.163.0` compose profile) and self-hosted Umami script injection with allowlisted events and per-user opt-out.
- **Fresh Lime Design System & PWA**: Warm carbonated-cream (`#F2F4E6`) and dark (`#0C130E`) canvases, key-lime primary fill (`#B4E32C`) with deep forest ink (`#16241A`), tangerine rating accent (`#E85D26`), `BricolageGrotesque` variable typography, 5-slot bottom nav with lifted Scan button, service worker, and IndexedDB offline check-in queue.
- **Flutter Android App (`mobile/`)**: Native Android companion app (`Riverpod`, `GoRouter`, `Dio`, `mobile_scanner` ML Kit barcode scanner, zero hardcoded server URL, Authelia OIDC WebView login + native cookie capture + `sd_` token exchange, offline check-in outbox, vector adaptive/monochrome launcher icons, `.github/workflows/flutter-android.yaml` CI, and semantic-release APK attachment).

## Impact

- **Specs affected**:
  - `openspec/specs/data-model-and-migrations/spec.md`
  - `openspec/specs/authentication-and-oidc/spec.md`
  - `openspec/specs/catalog-and-barcodes/spec.md`
  - `openspec/specs/checkins-social-and-venues/spec.md`
  - `openspec/specs/ai-opencode-subsystem/spec.md`
  - `openspec/specs/observability-and-analytics/spec.md`
  - `openspec/specs/design-and-pwa/spec.md`
  - `openspec/specs/flutter-android-app/spec.md`
- **Documentation**: `docs/architecture.md`, `docs/data-model.md`, `docs/design-direction.md`, `docs/product.md`, `docs/mobile.md`, `docs/roadmap.md`.
