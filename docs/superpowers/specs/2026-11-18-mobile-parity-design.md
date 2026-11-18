# Mobile parity finish — design

**Date:** 2026-11-18
**Lane:** 1 of 4 (mobile parity → offline storage upgrade → push alerts + @home shelf fix → per-taste ratings)
**Status:** approved in design dialogue; spec submitted for review

## Problem

`docs/roadmap.md` tracks seven parity gaps between the Android app and the web app:

1. check-in editing
2. comment editing
3. price/currency capture on check-ins
4. spend totals on insights
5. saved searches
6. barcode contribution
7. follower/following list screens

All seven are already served by `/api/v1` (verified against handlers, services and the Ent schema) — the gaps live entirely in `mobile/`. This lane closes them with client work plus one bounded backend addition decided during design: drink payload slug fields for the from-drink drop-alert prefill (§8).

## Release structure

Two releases, each landing independently on `main` (semantic-release cuts a version and attaches the APK as usual). `docs/roadmap.md` parity gaps are ticked per release as they ship. The pending housekeeping docs diff (4 modified files) stays a separate concern, uncommitted by this lane.

- **Release 1 — edit parity:** check-in edit, comment edit, price/currency, insights spend. Pure Flutter: no Go, Ent, migration or OpenAPI changes.
- **Release 2 — discovery surfaces:** saved searches, from-drink prefill, barcode contribute, follow lists. Flutter except the payload slug addition in §8 (Go + OpenAPI).

## Release 1 — edit parity

### §1 Check-in edit

- The detail-screen popup (currently Delete/Report only) gains **Edit**, shown only while the client-computed 24h window from `created_at` is open. The API exposes no `can_edit` flag; an unparsable `created_at` is treated as a closed window.
- Edit opens the existing `CheckinSheet` in edit mode: an optional `existing` (`CheckinDetail`) constructor parameter seeds the note/prep-note controllers and plain state fields in a new `initState`; `_save()` branches between `createCheckin` and a new `updateCheckin` (`PATCH /api/v1/checkins/{id}`). Fields accepted by PATCH: rating (null clears), serving, prep_method, prep_note, note, venue_id (null clears), visibility, price_cents (null clears), price_currency.
- PATCH carries no photo field, so the attachment section is **hidden in edit mode**; the existing photo stays. The offline-outbox fallback is **skipped in edit mode** (edits require connectivity; a failed PATCH surfaces its server error).
- Server responses: `400 edit_window_closed` → "Check-ins can be edited for 24 hours."; validation `400`s → field errors; foreign/deleted `404` → standard not-found handling.
- No edited marker for check-ins — the API exposes none and web shows none. On success: pop back and refresh the detail.

### §2 Comment edit

- Own comments gain **Edit** beside the existing Delete in `_commentTile`'s action row (`checkin_detail_screen.dart:815`, row at 869-899), gated by the same own-comment condition as `canDelete`. The non-author `404` therefore cannot occur from the UI.
- Edit opens the existing compose dialog prefilled; `PATCH /api/v1/comments/{id}` with body 1–1000 runes. Author-only, no time window.
- On success the comment updates in place and shows an **(edited)** marker from `edited_at` (new `Comment.editedAt` model field).

### §3 Price and currency on check-ins

- The sheet gains a **Price section** between the note and attachment sections (`checkin_sheet.dart` sections at 1740-1770; mirrors the web form, `templates/pages/checkin.html:168-182`): a decimal text field plus a currency picker (curated ISO-4217 set, defaulting to the last-used currency, persisted locally).
- The client always treats input as **major units** and converts to integer cents ("1.50" → 150), capped at the API's 100,000 (`price_cents` is nillable, 0–100000; currency is blank or 3 uppercase ASCII letters — `ent/schema/checkins.go:23-26`). The web's integer-means-raw-cents quirk does not travel to mobile. Blank = no price; in edit mode, blanking the row sends `price_cents: null` to clear.
- `createCheckin` gains `price_cents`/`price_currency` parameters. Check-in detail displays the price when present; the same row in edit mode updates or clears it.

### §4 Insights spend

- The insights screen already calls `GET /api/v1/stats/me/insights?range=7|30|90`; it ignores the `spend` array. Add a **Spend card**: one row per currency (currency code, total in major units, count of priced check-ins), in server order (total desc), with a quiet empty state when nothing is priced. No new request, no paging.

### §5 R1 validation and errors

Client-computed: 24h window math, cents conversion + cap, currency format, comment length. Server-handled: `edit_window_closed`, validation `400`s mapped to field errors, `404`s to standard handling. The four slices touch disjoint screens — no shared-state risk.

## Release 2 — discovery surfaces

### §6 Saved searches (drop alerts)

A saved search is a **standing alert subscription**, not a replayable query: the server evaluates stored filters when a drink drops into the catalog (`internal/catalog/revisions.go:398-420` → `internal/savedsearch/savedsearch.go:303`) and emits one `drop_alert` notification (24h debounce per search, one per drink per user). Web has no "run" action either — its page is a create form plus a list with Delete (`templates/pages/saved_searches.html`). Mobile matches that model.

- New **SavedSearchesScreen** reached from Settings: the saved-search list (name, filter pills, notify state, **Delete**) plus a create form with the fields mobile can populate without server-side lists: name (≤80 chars, auto-named when blank), query word (≤120), beverage type (dropdown hardcoded to the closed option set used by the web template), caffeine-free, sugar-free, and the notify-drop switch.
- Constraints: at least one filter is required (client-validated before submit); the 25-per-user cap surfaces as "delete one first" on `400 limit_reached`. Body fields per `internal/api/tier1_search.go:60-70`: `name, query, beverage_type, style_slug, brand_slug, flavour_slug, caffeine_free, sugar_free, notify_drops` (`notify_drops` omitted ⇒ true).
- No update endpoint exists; notify-drops is fixed at creation and there are no list-level toggles — same as web.

### §7 Quick-save from Discover

`DiscoverScreen` sends free-text `q` only (`api_client.dart:427-437`). When the search box has text, a **Save as drop alert** action creates a saved search capturing the query word (other fields empty — query alone satisfies the ≥1-filter rule). This is the creation path that lives where searching happens.

### §8 From-drink drop alert prefill (payload extension)

- Drink detail gains a body-level **"More like this — alert me when similar drinks drop"** action row after the flavours section. It opens the §6 create form prefilled with the drink's `beverage_type` (already on the mobile `Drink` model, `models.dart:195`) and its style, brand and first flavour slugs (`flavour_slugs[0]`; an empty list leaves the form's flavour field unset); the user can adjust anything before saving.
- Backend addition (the lane's only Go change): `catalog.DrinkDetail` and `drinkDTO` (`internal/api/handlers.go:1005-1017`) gain `style_slug`, `brand_slug` and a `flavour_slugs` list parallel to the existing `flavours` display names, `openapi.json`'s drink schema is updated, and a Go serialization test covers the new fields. Web needs nothing — server-rendered pages already see slugs internally.

### §9 Barcode contribute

- Drink detail gains a **Barcodes section** (after facts/aliases, before the gallery — `_body()` at `drink_detail_screen.dart:386-460`): the drink's barcode count (already on the `Drink` model, `models.dart:183,214,285`) plus an **Add barcode** action. No barcode list display — the API offers no per-drink code listing, and the count is the surface.
- The action opens a bottom sheet: GTIN digits only, client-side GS1 check-digit validation, symbology auto-derived from length (13 → EAN-13, 12 → UPC-A, 14 → GTIN-14, 8 → EAN-8), then `POST /api/v1/drinks/{slug}/barcodes` with `{gtin, symbology}`.
- Responses: **201** → "Barcode added" + count refresh; **200** → "Already on file"; **409 barcode_taken** → dialog naming the drink that holds it; **400 invalid_barcode** → field error; **429** → "try again in a minute" (server enforces 30/min + 20/hour; the client only surfaces it).

### §10 Follower/following lists

- The profile screen's follower/following counts (`user_profile_screen.dart:382`) become tappable, opening a new **people list screen**: rows of avatar, handle, display name; tap → that profile.
- `GET /api/v1/users/{handle}/followers|following` with keyset paging: `before` cursor from `next_cursor`, page size 20 (endpoint default; max 50), infinite scroll.
- Private, blocked or deleted targets return `404` → standard "profile unavailable" handling. The endpoint already sends `Cache-Control: private no-store`; nothing special client-side.

### §11 R2 validation and errors

Client-computed: GTIN length + check digit, ≥1-filter rule, name/query lengths, slug prefill from trusted payload fields (never client-slugified from names). Server-handled: `limit_reached`, `invalid_barcode`, `barcode_taken`, `429`s, `404`s.

## Client plumbing

- `models.dart` additions: price fields on `CheckinDetail` (:711-770), `editedAt` on `Comment` (:1049-1070), `spend` on `Insights` (:1535-1580), `SavedSearch`, `FollowUser`, slug fields on `Drink` for §8.
- `api_client.dart` additions: `updateCheckin`, `updateComment`, price parameters on `createCheckin` (:734-800), saved-search create/list/delete, `contributeBarcode`, followers/following methods.
- Go: the §8 payload fields only.

## Testing

- `task mobile-analyze` clean and `task mobile-test` green per release (AGENTS.md).
- New unit tests: 24h window computation, cents conversion + cap, GS1 check-digit validation, ≥1-filter rule, keyset paging logic. Widget tests where existing patterns already cover the touched screens.
- Go: serialization test for the new drink payload fields; `go test ./...` before any commit per AGENTS.md. Playwright is untouched (no web changes), CI runs it regardless.

## Out of scope

- Offline storage upgrade (design lane 2), push alerts and the @home shelf fix (lane 3), per-taste ratings (lane 4).
- Drink barcode **list** display (no API), saved-search update endpoint (delete + recreate), replaying saved searches (web has no run action), taxonomy-listing endpoints (none added — the §6 beverage-type list is hardcoded from the web template's closed set), any web UI change.
- The uncommitted housekeeping docs diff rides separately.
