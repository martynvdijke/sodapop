# Mobile parity finish — implementation plan

**Date:** 2026-11-18
**Spec:** `docs/superpowers/specs/2026-11-18-mobile-parity-design.md` (commit 9737afb) — authoritative for behavior and constraints; this plan only sequences and delegates it.
**Lane:** 1 of 4 (mobile parity → offline storage upgrade → push alerts + @home shelf fix → per-taste ratings)
**Structure:** two releases. R1 = edit parity (pure Flutter). R2 = discovery surfaces (Flutter + one Go payload change).

## Task graph

```
R1:  A ──► B ──► C
        └──► D          A first; then B ∥ D; C after B (B and C both write checkin_detail_screen.dart)

R2:  E ─┐
      F ─┴─► G ∥ H ∥ I   E ∥ F first; G and I after F; H after F + E
```

## Global rules for every lane

- **Never stage the housekeeping docs diff** (`docs/architecture.md`, `docs/mobile.md`, `docs/product.md`, `docs/roadmap.md` — uncommitted by user choice). All commits are pathspec-limited.
- Per release gate: `task mobile-analyze` clean + `task mobile-test` green; R2 also `go test ./...`.
- No Ent schema, no migrations, no web-template changes anywhere in this lane.
- Lanes share the working tree but touch disjoint files; each lane runs its own checks at completion. A lane whose verification trips over another lane's transient state re-runs after that lane lands. The release gate is authoritative.
- Each lane = one bounded `@fixer` dispatch; no lane makes design decisions (spec resolves them).

## Release 1 — edit parity

### Task A — shared plumbing (blocks all R1 UI)

Files: `mobile/lib/src/api/models.dart`, `mobile/lib/src/api/api_client.dart`, new helper file under `mobile/lib/src/` following existing layout, unit tests under `mobile/test/`.

- `models.dart`: `CheckinDetail.priceCents` (int?) + `priceCurrency` (String?) (:711-770 region); `Comment.editedAt` (DateTime?) (:1049-1070 region); `Insights.spend` parsed from the existing insights GET response as a list of `{currency, totalCents, count}` (:1535-1580 region).
- `api_client.dart`: `updateCheckin` → `PATCH /api/v1/checkins/{id}` (fields per spec §1; null `rating`/`venue_id`/`price_cents` clears); `updateComment` → `PATCH /api/v1/comments/{id}`; `createCheckin` gains `priceCents`/`priceCurrency` params (:734-800 region).
- Helpers + unit tests: 24h edit window from `created_at` (unparsable ⇒ closed); price text → integer cents (major units, cap 100000); currency display formatting.

Done when: `flutter analyze` clean and the new unit tests pass.

### Task B — check-in edit + price UI (after A) — spec §1 + §3

Files: check-in sheet (`checkin_sheet.dart`), check-in detail screen (`checkin_detail_screen.dart`).

- Sheet edit mode: optional `existing` (`CheckinDetail`) constructor param; seed controllers/state in `initState`; branch `_save()` (:548-575) between `createCheckin`/`updateCheckin`; skip offline-outbox fallback in edit mode (:639-672); **hide the attachment section in edit mode**.
- Price section between note (:1127) and attachment (:1150): decimal field + curated ISO-4217 currency picker, last-used currency persisted locally (follow existing local-persistence pattern); blank in edit mode ⇒ `price_cents: null` (clears).
- Detail screen: popup gains **Edit** gated on the Task A window helper; price row shown when present; success → pop + refresh; `400 edit_window_closed` → "Check-ins can be edited for 24 hours."

Done when: analyze clean; existing sheet/detail tests still pass.

### Task C — comment edit (after B; same detail-screen file) — spec §2

- `_commentTile` action row (:869-899): **Edit** beside Delete, gated like `canDelete`.
- Prefilled compose dialog (existing dialog); PATCH via `updateComment`; success: in-place update + **(edited)** marker when `editedAt` is set.

Done when: analyze + tests green.

### Task D — insights spend (parallel with B) — spec §4

Files: insights screen only.

- **Spend card** from Task A's `Insights.spend`: one row per currency (code, total in major units, priced-check-in count), server order, quiet empty state. No new request.

Done when: analyze + tests green.

### R1 release gate

1. `task mobile-analyze` + `task mobile-test`.
2. Tick parity items 1-4 in `docs/roadmap.md`. **Staging wrinkle:** `docs/roadmap.md` carries the uncommitted housekeeping diff. Default: ask the user to land housekeeping first; otherwise stash-dance: `git stash push -- docs/roadmap.md` → tick → commit (pathspec) → `git stash pop` (keep both sides on any conflict).
3. Single release commit, pathspec-limited to `mobile/` + the roadmap tick: `feat(mobile): check-in and comment editing, prices and spend insights`.
4. semantic-release cuts the version + APK on `main` as usual.

## Release 2 — discovery surfaces

### Task E — drink payload slugs (Go; parallel with F) — spec §8 backend

Files: `catalog.DrinkDetail`, `drinkDTO` (`internal/api/handlers.go:1005-1017`), `internal/api/openapi.json`, new Go serialization test.

- Add `style_slug`, `brand_slug`, `flavour_slugs` (list parallel to the existing `flavours` display names).
- Update the openapi.json drink schema; serialization test covers the new fields.

Done when: `go test ./...` green. **No separate commit** — folds into the R2 release commit.

### Task F — R2 shared plumbing (blocks G/H/I)

Files: `models.dart`, `api_client.dart`, helpers + unit tests.

- `models.dart`: `SavedSearch`; `FollowUser`; `Drink.styleSlug`/`brandSlug`/`flavourSlugs` (:195 region).
- `api_client.dart`: saved-search create/list/delete (`POST|GET|DELETE /api/v1/me/searches`); `contributeBarcode(drinkSlug, gtin, symbology)` with 201/200/409/400/429 result mapping; `followers`/`following` methods with `before` cursor from `next_cursor`.
- Helpers + unit tests: GS1 check digit; length→symbology (8→ean8, 12→upca, 13→ean13, 14→gtin14); ≥1-filter rule.

Done when: analyze clean, unit tests pass.

### Task G — saved searches + quick-save (after F) — spec §6 + §7

- New `SavedSearchesScreen` from Settings: list (name, filter pills, notify state, Delete) + create form: name (≤80, auto-named blank), query word (≤120), beverage-type dropdown hardcoded from the web template's closed option set, caffeine-free, sugar-free, notify switch; `400 limit_reached` → "delete one first".
- `DiscoverScreen` quick-save ("Save as drop alert") when the search box has text — captures the query word as the sole filter.

Done when: analyze + tests green.

### Task H — drink detail: prefill + barcode (after F + E) — spec §8 UI + §9

- "More like this — alert me when similar drinks drop" action row after the flavours section → §6 create form prefilled with `beverage_type`, `style_slug`, `brand_slug`, `flavour_slugs[0]` (empty list ⇒ flavour field unset).
- **Barcodes section** after facts/aliases, before the gallery: the drink's `barcodeCount` + **Add barcode** bottom sheet — GTIN digits only, client check-digit validation, symbology by length; 201 → "Barcode added" + count refresh; 200 → "Already on file"; 409 → dialog naming the drink; 400 → field error; 429 → "try again in a minute".

Done when: analyze + tests green.

### Task I — follow lists (after F; parallel with G/H) — spec §10

- Profile follower/following counts (`user_profile_screen.dart:382`) become tappable → new people-list screen (avatar, handle, display name; tap → profile).
- Keyset paging: `before` from `next_cursor`, page size 20, infinite scroll; 404 → "profile unavailable".

Done when: analyze + tests green.

### R2 release gate

1. `go test ./...` (Task E) + `task mobile-analyze` + `task mobile-test`.
2. Tick parity items 5-7 in `docs/roadmap.md` — same staging mechanics as R1.
3. Single release commit, pathspec-limited to `mobile/`, `internal/`, `openapi.json` + roadmap tick: `feat(mobile): saved searches, barcode contribution, follow lists and drop-alert prefill`.
4. semantic-release cuts the version + APK on `main`.

## Out of scope (spec §Out of scope)

Offline storage upgrade (lane 2), push alerts + @home shelf fix (lane 3), per-taste ratings (lane 4); drink barcode list display, saved-search update endpoint, replaying saved searches, taxonomy-listing endpoints, any web UI change. The uncommitted housekeeping docs diff rides separately.
