# Sodapop — Product spec

Status: draft for review. Companion docs: [architecture.md](architecture.md), [data-model.md](data-model.md), [design-direction.md](design-direction.md), [roadmap.md](roadmap.md).

## 1. What we are building

A barcode-first check-in app for soda. Untappd's loop — scan, rate, log where you found it, see what friends are drinking — applied to soft drinks: craft bottlings, regional brands, fountain pours, sparkling water, energy drinks, iced tea, kombucha.

Self-hosted, single binary, SQLite. No cloud account, no feed algorithm, no ads.

**Audience:** people who seek out regional sodas and want a record; travellers who find a new can in a hotel minibar or a corner shop; friends who compare scores; the kind of person who already keeps a spreadsheet.

**Product principles**

1. **Scanning is faster than typing.** A returning barcode resolves in under 300 ms from local data.
2. **Every unknown soda can be added in under 30 seconds** without leaving the check-in flow.
3. **The catalog is shared, the log is personal.** Community edits improve the catalog; nobody edits your check-ins.
4. **Ratings are explicit.** Quarter stars, a number always shown, no red-for-bad colouring.
5. **Quiet social.** Chronological feed, mutual friends, one-tap toast. No streaks pressure, no engagement bait.
6. **Works offline at the edge.** Scan a cached drink and queue the check-in when the signal drops.

## 2. Scope

### v1 (must ship)
Barcode scanning with manual fallback · shared drink catalog with styles and flavours · check-ins with quarter-star ratings, serving, venue, photo, notes · search and filters · friends and follows · chronological feed with toasts · venues including hotels, shops, cafés, vending · personal stats · lists and wishlist · badges · Authelia OIDC login + local email verification & password reset · OpenCode ("opencode2") AI drink drafting, tasting-note polish & recommendations · admin moderation & AI endpoint settings · Umami analytics · OTel traces/metrics/logs · PWA install + offline check-in queue · Flutter Android companion app (`mobile/`).

### v1.1 (fast follow)
Comments on check-ins · user-submitted venue photos and menus · weekly email digest · web push · CSV/JSON export · public profile pages with SEO metadata · drink comparison view.

### Explicit non-goals
Native iOS app · real-time chat · ActivityPub federation · paid business accounts · advertising · video · multi-tenant hosting · Postgres.

## 3. Domain vocabulary

| Term | Meaning |
|---|---|
| **Drink** | A canonical product: brand + name + style. One drink can have many barcodes (pack sizes, regions, relabels). |
| **Brand** | Producer or label. Not a venue. |
| **Style** | Two-level taxonomy: parent family (e.g. Cola) and substyle (e.g. Diet cola). |
| **Flavour** | Taste descriptor with intensity 1–5: sweet, sour, bitter, citrus, fruity, herbal, spicy, creamy, floral, caramel. |
| **Venue** | A physical place you can drink or buy: hotel, bar, restaurant, café, shop, festival, vending machine, other. |
| **Check-in** | One user's log of one drink at one time, optionally at a venue, optionally with a photo. |
| **Toast** | A like on a check-in. One per user. |
| **Unique** | Distinct drinks a user has checked in. Repeat check-ins are allowed and counted separately. |

Rating: 0–5 in 0.25 steps, stored as integer 0–20. `0` = unrated. A check-in without a rating is valid.

## 4. Soda taxonomy (seed)

Parent families with substyles:

- **Cola** — regular, diet/zero, cherry, vanilla, flavoured
- **Pepper-style** — regular, diet
- **Lemon-lime** — regular, diet, caffeinated
- **Orange** — regular, diet, blood orange, mandarin
- **Grape** — regular, diet
- **Cherry & berry** — cherry, strawberry, raspberry, mixed berry
- **Tropical** — pineapple, mango, passionfruit, guava, punch
- **Root beer & sarsaparilla** — root beer, sarsaparilla, birch beer
- **Cream soda** — classic, vanilla, diet
- **Ginger** — ginger ale (golden/dry), ginger beer (brewed), diet
- **Botanical & herbal** — gentian, spiced, other
- **Mixers** — tonic, club soda, bitter lemon, lemonade
- **Sparkling water** — plain, flavoured, mineral
- **Still** — iced tea, sweet tea, lemonade, juice drink, ade
- **Functional** — energy, sports, enhanced water, CBD-free functional
- **Fermented** — kombucha, water kefir
- **Other** — malta, hard soda (alcohol), limited editions

Descriptors stored on the drink: sweetener (`sugar`, `hfcs`, `aspartame`, `sucralose`, `stevia`, `monk fruit`, `allulose`, `mixed`), `is_diet`, `is_caffeine_free`, sugar g/100 ml, caffeine mg/100 ml, calories kcal/100 ml, carbonation (`still|light|standard|extra`), acid (`phosphoric|citric|malic|tartaric|none`), default packaging and volume. Nutrition is stated per 100 ml and per container; never framed as a warning.

Packaging/volume presets: can 355 ml (12 oz), mini can 222 ml, tall can 473 ml, glass bottle 355 ml, PET 500 ml / 591 ml / 1 L / 2 L, fountain, carton, other. Reference serving 360 ml.

## 5. Core flows

### 5.1 Signup and verification

1. `/signup`: email, handle, password, optional display name. Handle validation with inline availability check (debounced 400 ms).
2. On submit: create user (`email_verified_at NULL`), send verification email, log the user in with a restricted session.
3. Restricted session: browse, search, scan, view profiles. Blocked: check-in, follow, friend request, list create, venue submit, photo upload, report. A dismissible banner reads “Verify your email to start checking in” with a resend action.
4. `/verify?token=…`: marks verified, shows “Email verified”, routes to the first-scan screen if the user has no check-ins.
5. Resend: 3 per hour, 24-hour token TTL, previous tokens invalidated.
6. Expired or used token: explanatory page with “Send a new link”.
7. Password reset: anti-enumeration (“If that address has an account, a link is on its way”), 1-hour token, consumes all sessions on success.

**OIDC login (Authelia & standard providers):**
- When `OIDC_ENABLED=true`, `/login` and `/signup` display **“Sign in with Authelia”** as the primary action, initiating Authorization Code + PKCE S256 (`openid email profile groups`).
- On callback (`/auth/oidc/callback`), the server validates `state`, `nonce`, PKCE verifier, and the ID token RS256 signature against `.well-known/openid-configuration` JWKS, requires `email_verified=true`, enforces `ALLOWED_EMAILS`/`ALLOWED_DOMAINS`, links or auto-provisions the user with `email_verified_at` set, and promotes members of the `admins` group (plus the very first user on a fresh DB) to `admin`.
- Local email+password signup/login coexists when `ALLOW_PASSWORD_FALLBACK=true` (default `true` when `OIDC_ENABLED=false`; explicit break-glass toggle when `OIDC_ENABLED=true`).
- `GET /api/v1/auth/oidc/status` exposes OIDC discovery for the web login page and the Flutter Android onboarding flow.

### 5.2 Scan → resolve

Entry points: center nav scan button, header search → “Scan instead”, venue page “Check in here”.

1. Scanner opens as a full-screen dark overlay. Camera permission is requested at that moment, never on page load.
2. Native `BarcodeDetector` if available; otherwise dynamically import the `barcode-detector` ponyfill (ZXing-C++ compiled to WebAssembly, ~1 MiB, cached after first use) inside a worker. Decode frames at 8–12 fps, cropped to the viewfinder band.
3. Formats: EAN-13, EAN-8, UPC-A, UPC-E, Code 128. Normalize every result to GTIN-14; validate the GS1 check digit; UPC-A arrives as EAN-13 with a leading zero — strip/normalize deterministically.
4. Debounce identical codes for 1.5 s. Haptic tick on detection; freeze frame; announce via live region.
5. Resolution ladder, all server-side and local-cache-aware:
   - **Local hit** → product line + “Check in” (≤300 ms).
   - **OFF hit** → show imported fields, allow immediate check-in, mark `source=off`, unverified.
   - **Miss** → “No match for {code}” with “Add this soda” and “Scan again”.
6. No camera or denied permission: “Camera unavailable”, explanation, and a prominent “Enter barcode” button. Manual entry is never hidden.
7. Manual entry accepts 8–14 digits, validates the check digit, and follows the same ladder.
8. After 8 seconds without a decode, offer manual entry in place.

**Unknown barcode create flow** (prefilled): barcode (read-only), brand with typeahead + “new brand”, name, style (parent then substyle), volume, packaging, optional sugar/caffeine/photo. Creates a provisional drink (`verified=0`, `source=user`, `created_by`) and immediately opens the check-in sheet. If a soft-key match exists (normalized brand + name + volume + packaging, or trigram similarity), show “Is this the same soda?” with both records side by side before creating. Never auto-merge.

Barcode resolution results are cached locally and server-side: positive 30 days, negative 7 days, keyed by GTIN.

### 5.3 Check-in

Single bottom sheet, one screen, no wizard.

| Field | Required | Behaviour |
|---|---|---|
| Drink | yes | Fixed from scan/search; “Change” reopens search |
| Rating | no | Quarter-star slider + tap; numeral always visible; “Not rated” default |
| Serving | no | Can / glass bottle / plastic bottle / fountain / carton / other |
| Volume | no | Prefilled from barcode/product; editable |
| Venue | no | Typeahead, “near me” sorted by distance; create-inline allowed |
| Date | no | Defaults now; backdating up to 30 days |
| Photo | no | One image, 10 MiB cap, camera or library |
| Notes | no | 2000 chars, plain text |
| Visibility | yes | Defaults to profile default: public / friends / private |
| Price | no | Optional, local currency |

Rules:
- Repeat check-ins of the same drink are allowed; the drink page shows “You: 3 check-ins · 2.7 average”.
- Posting writes the check-in, activity row, venue link, badge evaluation, and stats invalidation in one transaction.
- Duplicate submit protection via an idempotency key (client-generated UUID) so offline retries never double-post.
- Success: sheet closes, confirmation bar appears, and the feed row is inserted at the top.
- Editing a check-in updates `updated_at` and the activity row; editing a rating recomputes aggregates asynchronously (immediate on read via view).

### 5.4 Discover

- **Search** (`/search?q=`): FTS5 over drink name, brand, style, aliases; grouped results (Drinks, Brands, Venues, Users). Debounced 250 ms, keyboard-navigable, empty state offers “Add a soda”.
- **Browse** (`/drinks`): filters for style (parent/sub), flavour, brand, country, diet, caffeine-free, packaging, and minimum rating; sorts for name, newest, rating, check-in count. Filters live in the URL.
- **Drink page**: image, brand, style chips, flavour chips, nutrition facts, average rating with count (hidden below 3 ratings, “Not enough ratings yet”), your history, friend ratings, “Found at” venue list, similar drinks by style, actions “Check in” and “Add to list”.
- **Brand page**: drinks grid, country, description, top-rated.
- **Style page**: description, top drinks, your count, badge progress.

Average rating: plain mean across distinct users (each user's checks averaged to one value). Display only at ≥3 distinct raters (`MIN_RATERS_FOR_AVG`). Top-rated lists use a Bayesian score with prior mean 3.5 and weight 5, and require ≥3 raters.

### 5.5 Friends, follows, feed

- **Friend request** from a profile or search row. Pending → accepted. Blocking removes friendship and follows in both directions and hides content both ways.
- **Follow** is one-way and separate: following someone adds their public check-ins to the feed without a request.
- **Feed** (`/`): own activity plus accepted friends plus followed users, newest first, keyset paginated 20 at a time, `visibility` respected. Tabs: Following / You / Nearby (venue check-ins within 25 km, requires location permission on demand).
- **Toast**: one tap on the check-in icon; optimistic UI; undo within 5 s; the author gets a notification (in-app, batched per check-in).
- **Notifications**: friend request, friend accepted, toast on your check-in, badge earned, report outcome. Unread count in the nav; mark-all-read.
- No public like counts on profiles. Check-in detail shows who toasted.

### 5.6 Venues and locations

- **Venue kinds**: hotel, bar, restaurant, café, shop, festival, vending, other. Hotels matter because minibar and lobby-fridge finds are a real use case, not because of lodging features.
- **Venue page**: name, kind, address, map link (OSM), what's been checked in here, recent activity, average rating of drinks logged here, “Check in here”, save.
- **Create venue**: name, kind, address search (Nominatim proxy, optional), or “use my location”. Dedupe: normalized name + city exact match blocks creation and offers the existing venue; within 150 m and same kind shows “Is this the same place?”.
- **Menu / stock**: any user can attach a drink to a venue (“seen here”); admins can mark “on offer”. Each row stores `last_seen_at` so stale data decays out of the default view.
- **Venue check-in**: a check-in with `venue_id`. Untappd's “purchase location” distinction is simplified: one venue field, plus the venue kind tells you whether it was consumed or bought.
- **Verification**: admins can verify venues; verified venues show a subtle badge and can have a curated menu. No paid business tier.
- **Location data**: never store continuous location. Coordinates are stored only on venues, plus an optional one-shot “near me” query. No background geolocation.

### 5.7 Lists, wishlist, badges

- **Wishlist** is a system list per user, auto-created, cannot be deleted.
- Custom lists: title, description, visibility, ordered items (drinks), optional note per item.
- **Badges**: count milestones (first check-in, 10, 50, 100, 250, 500), style explorer (5/15/30 distinct styles), brand explorer (10/25/50 brands), country explorer (3/10/25 countries), venue kinds (hotel, vending, festival), streak (7/30/90 days), social (first friend, 10 friends), special (first soda added to catalog). Three levels per badge where it makes sense. Earned badges appear in the profile grid; locked ones show progress.
- Badge evaluation is idempotent and retroactive-safe: a backfill command can re-evaluate a user or all users.

### 5.8 Profile and stats

Public profile: avatar, handle, display name, bio, member since, counts, favourite style, recent check-ins, badges, lists (visibility-filtered), friends count.

Personal stats: total check-ins, unique drinks, distinct styles, distinct brands, distinct countries, average rating, top-rated drink, most-checked drink, current streak, longest streak, venues visited, money spent (if prices logged), monthly histogram.

### 5.9 Admin

Role-gated (`moderator`, `admin`):
- **Catalog**: search, edit, verify, merge duplicates (reassigns check-ins, lists, barcodes; keeps a redirect), delete (soft).
- **Reports**: queue for duplicate/wrong data/spam/abuse, resolve with reason, notify reporter.
- **Venues**: verify, edit, merge, close.
- **Users**: search, suspend, unsuspend, grant roles, revoke sessions.
- **Email**: outbox status, failed messages, retry.
- **AI ("opencode2")**: global `ai_enabled` toggle, OpenAI-compatible / OpenCode Zen (`https://opencode.ai/zen/go/v1`) endpoint CRUD (`text`, `vision`, `image`), AES-GCM encrypted API keys masked in UI, tag assignment (`draft`, `tasting_note`, `recommend`, `default`), `temperature`/`max_tokens`, and one-click **Test endpoint** probing upstream connectivity with actual status code and preview.
- **System**: version, DB size, backup now, job status, OTel status (enabled/endpoint), Umami config check.
- Every mutation writes `catalog_revisions` or an admin audit entry.

### 5.10 Settings

- **Profile**: display name, bio, avatar, country, default visibility.
- **Security**: change password, active sessions with revoke, API tokens (create with scopes, revoke), OIDC link/unlink.
- **Notifications**: email on friend requests, weekly digest opt-in, push (v1.1).
- **Appearance**: system/light/dark.
- **Privacy**: analytics opt-out, export my data (JSON), delete account.
- **Blocked users** list.

### 5.11 OpenCode ("opencode2") AI assistant

Ported from `dnd` (`internal/ai`), completely optional and hidden whenever `ai_enabled=0` or no enabled endpoint exists:
1. **Conversational & Vision Drink Drafting**: from the unknown-barcode create screen or `/admin/drinks`, users/moderators can open “Draft with AI”, paste rough tasting notes or label text (or attach a photo when a `vision` endpoint is enabled), and converse with the assistant to fill structured drink fields (`brand`, `name`, `style`, `flavours`, `sweetener`, `acid`, `carbonation`, nutrition per 100 ml, description). Multi-turn sessions preserve the same `x-opencode-session` header and `User-Agent: sodapop/<version>`.
2. **Tasting Note Polish**: inside the check-in sheet, a “Polish note” chip turns rough bullet points + selected flavour chips into a 1–2 sentence note matching Sodapop's voice (crisp, unpretentious, no exclamation marks, no sugar shaming). The user reviews and edits before submitting.
3. **Soda Sommelier (“What to try next”)**: on `/drinks` and the user profile, an optional card suggests 3 sodas from the catalog based on the user's top-rated styles, flavours, and sweetener preferences, with a one-sentence explanation for each.

### 5.12 Flutter Android app (`mobile/`)

Companion Android client modeled on `board-games/mobile` and `dnd/mobile` (full spec in [mobile.md](mobile.md)):
- **Onboarding**: starts with an empty server URL field (zero hardcoded default URL), validates and normalizes `http(s)` URLs, probes `GET /api/v1/auth/oidc/status`, and offers **“Sign in with Authelia”** (in-app WebView + native cookie capture + `sd_` device token exchange) alongside personal API token (`sd_…`) and optional password fallback.
- **Native barcode scanner**: uses `mobile_scanner` (CameraX + ML Kit) with hardware torch and pinch-to-zoom, normalizing scans to GTIN-14 and resolving against the same local cache → `/api/v1/barcodes/{gtin}` ladder.
- **Feature parity**: 5-slot bottom navigation (`Feed`, `Discover`, lifted lime `Scan` button, `Venues`, `Profile`), quarter-star check-in sheet with photo upload and AI tasting-note polish, offline check-in queue with idempotency keys, and lime/forest Material 3 theme (`#B4E32C` / `#16241A` / `#F2F4E6` / `#0C130E`).

## 6. Barcode scanning requirements

Platform reality that shapes the design:

- `BarcodeDetector` is available on Chrome/Edge (Android, ChromeOS, macOS) but **not on iOS Safari in production** and not on Firefox. iOS is the primary mobile browser share for this audience, so the WebAssembly fallback is mandatory, not optional.
- Requires a secure context (HTTPS or localhost) and a `Permissions-Policy: camera=(self)` header.
- Torch/flash and zoom are unavailable on iOS Safari; the UI must not depend on them. Feature-detect `getCapabilities().torch`.
- Any scan result is untrusted input: validate length and GS1 check digit before hitting the API.

Non-functional targets:
- Local-hit resolution to product line: p95 < 300 ms.
- First successful decode on a well-lit can: < 2.5 s median.
- Fallback bundle: lazy-loaded, ≤ 1.1 MiB gzipped wasm, cached by the service worker, never on the critical path.
- Battery: stop decoding when the tab is hidden or the overlay closes; release the camera track on `pagehide`.

## 7. PWA and offline

- `manifest.webmanifest`: name, short name, standalone display, lime theme colour (`#F2F4E6` light / `#0C130E` dark), 192/512 maskable icons, shortcuts (Scan, Feed, Discover).
- Service worker: precache app shell, tokens, fonts, icons; stale-while-revalidate for drink images; network-first for HTML; never cache API mutations.
- **Offline check-in queue**: IndexedDB queue of pending check-ins with client-generated idempotency keys, replayed on reconnect via the JSON API with a Bearer session token or a same-origin CSRF token. Conflict handling: if the drink was merged server-side, remap to the canonical drink and keep the check-in.
- Offline browse: last 100 viewed drinks and the user's own check-ins are available.
- Install prompt: custom, dismissible, shown after the second session, never on first load.

## 8. Email

| Template | Trigger | Contents |
|---|---|---|
| `verify_email` | Signup, resend | “Confirm your email”, 24 h link, expiry note, ignore-if-not-you |
| `reset_password` | Forgot password | 1 h link, “if you didn't ask, ignore this” |
| `friend_request` | Request received (opt-out) | Requester handle, link to accept |
| `weekly_digest` | Opt-in, Monday | Friends' top check-ins, your week, new badges |
| `report_resolved` | Moderation outcome | What changed, link |

All emails: plain-text alternative, no tracking pixels, List-Unsubscribe for digests, `From: Sodapop <noreply@…>`. Dev mode writes to `data/mail/` instead of SMTP.

## 9. Analytics events (Umami)

Allowlist only; no free text and no identifiers.

| Event | Data |
|---|---|
| `signup_started` | — |
| `email_verified` | — |
| `checkin_created` | `style`, `has_venue` (bool), `rated` (bool), `source` (`scan|search|venue|manual`) |
| `barcode_scan_started` | `context` (`nav|checkin|venue`) |
| `barcode_scan_result` | `result` (`local|off|miss|error`) |
| `drink_created` | `style`, `has_barcode` |
| `search_used` | `scope` (`drinks|venues|users`) |
| `venue_viewed` | `kind` |
| `friend_added` | — |
| `theme_changed` | `theme` |
| `list_created` | — |
| `badge_earned` | `badge_code` |

Rules: `UMAMI_URL`/`UMAMI_WEBSITE_ID` unset disables injection entirely; `analytics_opt_out` suppresses server-side; DNT respected; no event on admin pages; event names ≤ 50 chars.

## 10. Accessibility and content

- WCAG 2.2 AA. Lime is a fill, never text on cream. Contrast verified in the design doc.
- Full keyboard operation of the rating slider, scanner manual entry, filters, and sheets.
- Screen-reader announcements for scan results, form errors, and toast confirmations.
- Target size ≥ 44 px; sheet primary action 52 px.
- English only in v1; metric + imperial display toggle (ml/oz, g/oz) in settings; dates as “12 Mar 2026”.
- Tone: sentence case, verb-first buttons, no exclamation points, no sugar shaming, no mascots.

## 11. Acceptance criteria (v1)

**Auth & OIDC**
- Signup with a valid email sends exactly one verification email; the token works once and expires after 24 h.
- Unverified users cannot create check-ins, follow, or upload; the API returns 403 with `email_not_verified`; the UI shows the banner.
- Password reset invalidates all sessions and the old password fails.
- A user can revoke a single session and see it disappear from the list.
- With `OIDC_ENABLED=true`, `/auth/oidc/login` → `/auth/oidc/callback` verifies `state`, `nonce`, PKCE S256, and JWKS RS256 signature, requires `email_verified=true`, links or creates a verified user, and promotes `admins` group members to `admin`.
- `GET /api/v1/auth/oidc/status` returns OIDC configuration status without authentication.

**OpenCode ("opencode2") AI**
- Saving an endpoint with `/chat/completions` or `/images/generations` in `base_url` normalizes it to the base path (`https://opencode.ai/zen/go/v1`) and encrypts `api_key` at rest with AES-GCM.
- Every outbound AI request sets `User-Agent: sodapop/<version>` and a stable `x-opencode-session` header across multi-turn drink-drafting calls.
- Truncated (`finish_reason == "length"`) or empty AI responses return a clear `ai_output_truncated` error without persisting a broken assistant turn.

**Flutter Android (`mobile/`)**
- Onboarding starts with an empty server URL field, rejects invalid URLs, probes `/api/v1/auth/oidc/status`, and signs in via Authelia WebView or `sd_` token.
- Native `mobile_scanner` decodes EAN-13/UPC-A to GTIN-14 and opens the check-in sheet; offline check-ins replay idempotently on reconnect.

**Catalog and scanning**
- Scanning a barcode present locally resolves without a network round trip.
- An unknown barcode creates a provisional drink and a check-in in one flow, with the barcode attached.
- Scanning the same GTIN twice does not create a second drink.
- A UPC-A and its EAN-13 zero-prefixed form map to the same drink.
- Search finds a drink by brand, name, partial word, and diacritic-free spelling.

**Check-in**
- Rating 0, 4.25, and 5 persist exactly and render without layout shift.
- Private check-ins never appear in another user's feed, search, or profile counts.
- A repeated offline submit with the same idempotency key creates one check-in.
- Deleting a check-in removes it from the feed and recomputes the drink average.

**Social**
- Friend request → accept creates reciprocal visibility; blocking hides both directions immediately.
- Toast is idempotent per user/check-in and can be undone.
- Feed pagination returns no duplicates and no gaps under concurrent inserts.

**Venues**
- Creating a venue with an exact name+city match is blocked with a link to the existing venue.
- A check-in at a hotel appears on the hotel's page with kind `hotel`.

**Ops**
- `/healthz` returns 200 with no collector configured; `OTEL_*` unset means no outbound telemetry.
- `OTEL_EXPORTER_OTLP_ENDPOINT` points at the collector profile; traces, metrics, and logs arrive with `service.name=sodapop`.
- Nightly backup produces a restorable file.
- Umami unset means no script tag and no CSP entry.

## 12. Risks and open questions

| Risk | Mitigation |
|---|---|
| Open Food Facts coverage for soda is patchy (6.2k soft drinks, missing nutrition) | Local catalog is authoritative after first import; create flow is first-class; never block a check-in on OFF |
| iOS barcode performance in low light | Wasm fallback, ROI cropping, torch where supported, manual entry always visible |
| ODbL share-alike on OFF data | Keep OFF-sourced rows in separate tables/columns with attribution; do not blend with proprietary data; document the boundary |
| Catalog vandalism | Provisional entries, report queue, admin merge, revision history, rate limits on create |
| SQLite write contention from badge/stats writes | Single writer, WAL, short transactions; stats from views at read time |
| Self-hosted SMTP deliverability | Clear setup docs, test-email button, outbox visibility, optional relay |

Open: whether to seed a starter catalog (e.g. 500 common sodas) to make first-run useful; whether to support imperial units by locale; whether to expose a public read-only API.

## 13. Success signals

- First-run: signup → verified → first check-in completes in under 3 minutes.
- ≥ 60% of check-ins start from a scan.
- Barcode local-hit rate ≥ 70% after 30 days of use.
- Median check-in creation < 20 s.
- Zero P1 accessibility failures in axe on the top 10 pages.
