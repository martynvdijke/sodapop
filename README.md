# Sodapop

Untappd, but for soda. Scan a can or bottle, log where you found it, rate it in quarter-stars, see what your friends are drinking, and build a shared catalog of soft drinks — craft sodas, colas, ginger beers, root beers, sparkling waters, energy drinks, iced teas, and kombuchas.

Self-hosted, single Go binary + SQLite (Ent ORM + versioned migrations), server-rendered HTML with a TypeScript PWA and a native Flutter Android companion app.

## Planning & Specification Docs

| Document | Covers |
|---|---|
| [docs/product.md](docs/product.md) | Positioning, soda taxonomy, user flows, barcode & PWA requirements, OIDC, OpenCode AI, Android app, acceptance criteria |
| [docs/architecture.md](docs/architecture.md) | Go 1.27 + Ent ORM + SQLite migrations, HTTP surface, Authelia OIDC + local email verification, OFF v3.6, OpenCode ("opencode2") AI, OTel, Umami, Docker/CI |
| [docs/data-model.md](docs/data-model.md) | Ent ORM schemas (`ent/schema/`), versioned migration registry (`internal/db/migrations`), `schema_consistency_test.go`, and full resulting SQLite DDL + FTS5 + views |
| [docs/design-direction.md](docs/design-direction.md) | Fresh lime visual system (`#B4E32C` fill, `#16241A` forest ink, `#F2F4E6` cream / `#0C130E` dark), typography, components, scanner overlay, accessibility |
| [docs/mobile.md](docs/mobile.md) | Flutter Android app (`mobile/`): zero-default server onboarding, Authelia OIDC WebView + `sd_` token exchange, `mobile_scanner` ML Kit, offline check-in outbox, APK CI/release |
| [docs/roadmap.md](docs/roadmap.md) | Dependency graph and phases `P0`–`P11` with build lists, exit criteria, and definition of done |

## Highlights

- **Barcode-first check-ins:** native `BarcodeDetector` where available plus a Web Worker ZXing-C++ WebAssembly fallback (`barcode-detector`) on iOS Safari and Firefox, and native CameraX/ML Kit (`mobile_scanner`) on Android; GTIN-14 normalization; local cache → Open Food Facts v3.6 (ODbL-isolated) → 20-second unknown-soda creation flow.
- **Soda-native taxonomy:** 17 parent style families with substyles, intensity-scored flavour tags, sweetener/acid/carbonation descriptors, caffeine/sugar/calorie facts per 100 ml, and packaging presets.
- **Venues that fit soda:** hotels (minibar & lobby fridge finds), corner shops, cafés, bars, restaurants, festivals, and vending machines, with Nominatim address lookup and 150 m deduplication.
- **Quiet social:** mutual friends, one-way follows, chronological keyset feed, one-tap toast with 5 s undo, personal stats, custom lists, Wishlist, and retroactive-safe badges.
- **Authelia OIDC + verified local accounts:** OIDC Authorization Code + PKCE S256 with RS256 JWKS verification and `admins` group sync, alongside local email+password signup with mandatory email verification (`ALLOW_PASSWORD_FALLBACK`).
- **OpenCode ("opencode2") AI assistant:** optional admin-configured OpenCode Zen (`https://opencode.ai/zen/go/v1`) or OpenAI-compatible endpoints (`ai_endpoints` with AES-GCM encrypted keys, `NormalizeAIBaseURL`, honest `User-Agent: sodapop/<version>` and `x-opencode-session` headers) for conversational/vision drink drafting, check-in tasting-note polish, and Soda Sommelier recommendations.
- **Observability & privacy-first analytics:** OpenTelemetry (`v1.47.0`) traces, metrics (`/metrics`), and correlated JSON logs via OTLP, plus optional self-hosted Umami analytics with an event allowlist and per-user opt-out.

## Stack

- **Backend:** Go 1.27, stdlib `net/http`, `html/template`, pure-Go SQLite (`modernc.org/sqlite`, WAL, FTS5), **Ent ORM (`entgo.io/ent v0.14.6`)** + versioned migration registry (`internal/db/migrations`)
- **Web / PWA:** Vite 8, TypeScript 7, Tailwind 4, service worker + IndexedDB offline check-in queue
- **Android:** Flutter (`mobile/`, Material 3, Riverpod, GoRouter, Dio, `mobile_scanner`, `webview_flutter`, `flutter_secure_storage`)
- **Observability & Analytics:** OpenTelemetry (`otelhttp`, `otelslog`, OTLP/Prometheus) + Umami

## License

[MIT](LICENSE)
