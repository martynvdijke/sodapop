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

### v1 (shipped)
Barcode scanning with manual fallback · shared drink catalog with styles and flavours · catalog browse with filters and lifecycle statuses including drops · check-ins with quarter-star ratings, serving, venue, photo, notes · check-in detail with toasts · search and filters · friends and follows · chronological feed with toasts · in-app notifications with an unread badge · venues including hotels, shops, cafés, vending · personal stats and nutrition insights · lists and wishlist · badges · Authelia OIDC login + local email verification & password reset · account settings with profile, preferences, privacy, security and data controls · opt-in public profiles · Home Assistant inbound and outbound integrations · OpenCode ("opencode2") AI drink drafting, tasting-note polish & recommendations · admin moderation, drink lifecycle and AI endpoint settings · Umami analytics · OTel traces/metrics/logs · PWA install + offline check-in queue · Flutter Android companion app (`mobile/`).

### v1.1 (shipped)
Comments on check-ins · user-submitted venue photos and menus · weekly email digest · drink comparison view · richer public-profile metadata.

### v1.2 (shipped)
Blocks and a blocked-accounts settings page · drink image galleries · persisted AI draft sessions with message, revise and commit · catalog revisions and internal admin notes on reports · first-run `/setup` admin creation and the `/admin/settings` runtime panel · expanded `/api/v1` for comments, media, lists, mixes, taste, badges/passport, saved venues, follows and blocks · Android parity for check-in comments, profiles with follow/block, notifications, lists, saved venues, venues, insights, taste, passport/badges and mixes, including check-in photo upload.

### Explicit non-goals
Native iOS app · real-time chat · ActivityPub federation · paid business accounts · advertising · a video platform, standalone video hosting or an algorithmic video feed (short MP4/WebM check-in clips are supported) · multi-tenant hosting · Postgres.

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

Entry points: the scan tab in the bottom bar, the check-in page, and `GET /scan` directly.

1. The scanner asks for camera access only when the visitor opens it. The module binds to a `[data-scanner]` overlay and never requests permission on page load.
2. Native `BarcodeDetector` is used when the browser exposes it, with the supported subset of EAN-13, EAN-8, UPC-A, UPC-E, GTIN-13 and GTIN-14. Otherwise the page lazy-imports the `barcode-detector` ponyfill (ZXing-C++ compiled to WebAssembly) and points it at the self-hosted `zxing_reader.wasm`, so CSP only needs `self` and the service worker can cache the binary.
3. Frames are decoded at about 10 fps. The torch button appears only when `getCapabilities().torch` exists; it is hidden on iOS Safari.
4. A decode stops the loop and every camera track, then navigates to `/scan?gtin={code}`. Closing the overlay, hiding the tab or `pagehide` also stops the tracks.
5. `GET /scan?gtin=` normalizes the value to GTIN-14 and validates the GS1 check digit (invalid input returns 400). It then resolves the barcode:
   - **Local hit** → the check-in page for that drink.
   - **OFF hit** → the imported fields are used for the check-in and the row stays marked unverified.
   - **Miss** → the pre-filled “Add a soda” form with the barcode attached.
6. Manual GTIN entry stays available alongside the camera: `GET /scan?gtin=` accepts a typed code, so a blocked camera never blocks a check-in. The Flutter app uses its native ML Kit scanner for the same flow.
7. Camera-denied and unsupported-browser states keep the rest of the check-in page usable and name the reason.

**Unknown barcode create flow** (prefilled): barcode (read-only), brand with typeahead + “new brand”, name, style (parent then substyle), volume, packaging, optional sugar/caffeine/photo. Creates a provisional drink (`status=provisional`, `source=user`, `created_by`) and immediately opens the check-in sheet. If a soft-key match exists (normalized brand + name + volume + packaging), show “Is this the same soda?” with both records side by side before creating. Never auto-merge.

Barcode resolution results are cached locally and server-side: positive 30 days, negative 7 days, keyed by GTIN.

### 5.3 Check-in

Single bottom sheet, one screen, no wizard.

| Field | Required | Behaviour |
|---|---|---|
| Drink | yes | Fixed from scan/search; “Change” reopens search |
| Rating | no | Quarter-star choice; numeral always visible; “Not rated” default |
| Serving | no | Can / bottle / glass / fountain / other |
| Venue | no | Dropdown of known venues; no venue is fine |
| Photo or clip | no | One image or MP4/WebM/QuickTime clip |
| Notes | no | 1000 chars, plain text |
| Visibility | yes | Defaults to the profile default: public / friends / private |

Rules:
- Repeat check-ins of the same drink are allowed; the drink page shows “You: 3 check-ins · 2.7 average”.
- Posting writes the check-in, activity row, venue link, badge evaluation, and stats invalidation in one transaction.
- Duplicate submit protection via an idempotency key so offline retries never double-post.
- Success: the form returns to the feed and the new row appears at the top.
- Editing a check-in updates `updated_at` and the activity row; editing a rating recomputes aggregates asynchronously (immediate on read via view).

**Check-in detail** (`/checkins/{id}`) renders one check-in with author, drink, rating, note, venue, photo or clip, visibility, toast count and comments. The owner sees delete; a signed-in non-owner sees the toast and comment actions; an anonymous visitor sees a sign-in prompt. Visibility follows the same rule as the API: owner, then `public` for anyone, then `friends` for accepted friends, otherwise a 404. `GET /api/v1/checkins/{id}` returns the same record with the same 404 behaviour. Delete is a soft delete and returns to `/you`.

**Comments** are flat, 1,000-character messages on a check-in. A viewer can read or post only when the parent check-in is visible to them; the thread inherits that visibility and returns 404 for an invisible parent. Authors can delete their own comments; moderators can remove any. Comments are capped at 30 per user per hour and 200 per thread, newest kept. A comment on someone else's check-in notifies the owner and is skipped when either side has blocked the other.

### 5.4 Discover

- **Search** (`/search?q=`): FTS5 over drink name, brand, style, aliases; grouped results (Drinks, Brands, Venues) with no user directory. Debounced 250 ms, keyboard-navigable, empty state offers “Add a soda”. The API search is capped at 10 results and returns no total.
- **Browse** (`/drinks`): filters for style (a parent also matches its children), brand, flavour, caffeine-free, sugar-free, and lifecycle status (active, limited, seasonal, discontinued); sorts for name, rated and newest; 24 per page with a next-page link. Filters live in the URL and are plain HTML, with no collection API behind them. `/brands` and `/styles` index every brand and style family, and their detail pages list the drinks in each.
- **Drops** (`/drops`): limited editions with a `limited_until` date, seasonal sodas, and discontinued sodas with `discontinued_at`. Admins move a drink between active, limited, seasonal and discontinued from the drink page or admin view; wishlist owners get one in-app notification when a wishlisted soda is discontinued.
- **Compare** (`/compare?drinks=slug-a,slug-b`): a read-only side-by-side view of 2–6 drinks. It shows container, style, flavour, sweetener, carbonation, acid and nutrition per 100 ml, plus the average rating only when at least three distinct users have rated. Duplicate, unknown or merged drinks keep the user on the page with a clear message rather than a broken request. There is no comparison API or stored comparison.
- **Drink page**: image gallery, brand, style chips, flavour chips, nutrition facts, average rating with count (hidden below 3 ratings, “Not enough ratings yet”), your history, friend ratings, “Found at” venue list, similar drinks by style, actions “Check in”, “Add to list” and “Compare”.
- **Brand page**: drinks grid, country, description, top-rated.
- **Style page**: description, top drinks, your count, badge progress.

Average rating: plain mean across distinct users (each user's checks averaged to one value). Display only at ≥3 distinct raters (`MIN_RATERS_FOR_AVG`). Top-rated lists use a Bayesian score with prior mean 3.5 and weight 5, and require ≥3 raters.

### 5.5 Friends, follows, feed

- **Friend request** from a profile or search row. Pending → accepted. Blocking removes friendship and follows in both directions and hides content both ways.
- **Follow** is one-way and separate: following someone adds their public check-ins to the feed without a request.
- **Feed** (`/`): own activity plus accepted friends plus followed users, newest first, keyset paginated 20 at a time, `visibility` respected. Tabs: Friends / Following.
- **Toast**: one tap on the check-in icon; optimistic UI; undo while the page is open; the author gets a notification (in-app, and push when enabled).
- **Blocks**: block or unblock from a profile, and manage the list at `/settings/blocked`. Blocking ends an accepted or pending friendship and every follow in either direction in one transaction, then hides both accounts from each other's feed, profiles, comments, toasts and notifications. The API returns 404 for a blocked list; profiles return 404 rather than confirming an account, and a blocked follow or friend request is a silent no-op. Unblocking removes only the caller's block; it does not restore the old friendship or follows.
- **Notifications**: kinds are friend request, friend accepted, toast on your check-in, comment on your check-in, badge earned, and system (a wishlist drop, for example). The nav shows an unread count. `/notifications` lists the newest 50 with an actor, text, time and link; each row can be marked read, and “Mark all read” clears the rest. The same personal list is available at `GET /api/v1/notifications`, with `POST /api/v1/notifications/read` accepting an `id` or marking everything.
- No public like counts on profiles. Check-in detail shows the toast count and the viewer's own toast state.

### 5.6 Venues and locations

- **Venue kinds**: hotel, bar, restaurant, café, shop, festival, vending, other. Hotels matter because minibar and lobby-fridge finds are a real use case, not because of lodging features.
- **Venue page**: name, kind, address, map link (OSM), what's been checked in here, recent activity, average rating of drinks logged here, community photos and menus, “Check in here”, save.
- **Create venue**: name, kind, address search (Nominatim proxy, optional), or “use my location”. Dedupe: normalized name + city exact match blocks creation and offers the existing venue; within 150 m and same kind shows “Is this the same place?”.
- **Menu / stock**: any user can attach a drink to a venue (“seen here”); admins can mark “on offer”. Each row stores `last_seen_at` so stale data decays out of the default view.
- **Venue photos and menus**: verified members can add an image as a `photo` or `menu` with an optional 80-character label. Each uploader can keep 6 active rows at one venue and the venue can hold 20 in total. The uploader, a moderator or an admin can remove a row; removal is a soft delete of the attachment, and the file is kept. Images follow the same 12 MiB image pipeline as check-in photos (JPEG/PNG/WebP/GIF, EXIF stripped, resized). Menus are images, not documents.
- **Venue check-in**: a check-in with `venue_id`. Untappd's “purchase location” distinction is simplified: one venue field, plus the venue kind tells you whether it was consumed or bought.
- **Verification**: admins can verify venues; verified venues show a subtle badge and can have a curated menu. No paid business tier.
- **Location data**: never store continuous location. Coordinates are stored only on venues, plus an optional one-shot “near me” query. No background geolocation.

### 5.7 Lists, wishlist, badges

- **Wishlist** is a system list per user, auto-created, cannot be deleted.
- Custom lists: title, description, visibility, ordered items (drinks), optional note per item.
- **Badges**: count milestones (first check-in, 10, 50, 100, 250, 500), style explorer (5/15/30 distinct styles), brand explorer (10/25/50 brands), country explorer (3/10/25 countries), venue kinds (hotel, vending, festival), streak (7/30/90 days), social (first friend, 10 friends), special (first soda added to catalog). Three levels per badge where it makes sense. Earned badges appear in the profile grid; locked ones show progress.
- Badge evaluation is idempotent and retroactive-safe: a backfill command can re-evaluate a user or all users.

### 5.8 Profile, stats and insights

A profile is opt-in public (`user_settings.public_profile`). The page lives at `/u/{handle}` and shows avatar, handle, display name, bio, member since, counts, recent check-ins, earned badges, top styles, lists and friend state. Top styles rank the styles in the check-ins the viewer may see; unearned badge definitions and unearned progress are never shown on another user's profile.

Access rules:
- Account deleted → 404.
- Public profile → anyone, signed in or not, sees the profile and its `public` check-ins.
- Not public, anonymous visitor → redirect to `/login` with a return path.
- Not public, signed-in non-friend → 404, never 403, so handles cannot be enumerated.
- Owner and accepted friends see `public`, `friends` and `private` check-ins; lists follow their own visibility.

Only an opted-in public profile is indexable (`index,follow` with canonical and Open Graph metadata); everything else is `noindex,nofollow`. The API stays friend-scoped regardless: `GET /api/v1/users/{handle}` does not become a public profile endpoint.

Personal stats: total check-ins, unique drinks, average rating, toasts received and badges. The profile summary shows pours, styles, venues and badges; `/you` shows totals, badge progress and recent pours.

**Insights** (`/insights`) is self-only and reads the last 7, 30 or 90 calendar days:
- Sugar, caffeine and calories come from the drink's per-100 ml facts multiplied by the logged volume. Logged check-ins without nutrition facts contribute zero.
- The daily series is zero-filled so every day in the window is present with `sugar_g`, `caffeine_mg`, `calories_kcal` and `pours`.
- Caffeine clearance halves the last dose every 5 hours until it drops below 20 mg, and reports when that happens.
- With a `caffeine_bedtime` set, the page flags a late-caffeine day when the last dose plus 8 hours falls after bedtime.
- The top-drinks list excludes merged and provisional rows; health totals still include what was logged.
- The same data is available at `GET /api/v1/stats/me/insights?range=7|30|90`.

### 5.9 Admin

Role-gated (`moderator`, `admin`):
- **Catalog**: search, edit, verify, merge duplicates (reassigns check-ins, lists, barcodes; keeps a redirect), delete (soft).
- **Reports**: queue for duplicate/wrong data/spam/abuse, resolve with reason, notify reporter.
- **Venues**: verify, edit, merge, close.
- **Users**: search, suspend, unsuspend, grant roles, revoke sessions.
- **Email**: outbox status, failed messages, retry.
- **AI ("opencode2")**: global `ai_enabled` toggle, OpenAI-compatible / OpenCode Zen (`https://opencode.ai/zen/go/v1`) endpoint CRUD (`text`, `vision`, `image`), AES-GCM encrypted API keys masked in UI, tag assignment (`draft`, `tasting_note`, `recommend`, `default`), `temperature`/`max_tokens`, and one-click **Test endpoint** probing upstream connectivity with actual status code and preview.
- **System**: version, DB size, backup now, job status, OTel status (enabled/endpoint), Umami config check, and the `/admin/settings` runtime panel for site name, SMTP, analytics, registration, password fallback, allowlists and AI enablement.
- **Reports and notes**: the moderation queue resolves, dismisses or removes a report and can append an internal note. Report notes are admin-only; non-admins receive 404. Note author and timestamp are shown, and a note can be deleted by an admin.
- **First-run setup**: a fresh instance prints a one-time `setup required:` token (or reads `SETUP_TOKEN`); `/setup` creates the first verified admin atomically and then closes. It reopens only when no live admin remains, which is also the recovery path after an OIDC group demotion.
- Every mutation writes `catalog_revisions` or an admin audit entry. Runtime setting writes append an `admin_notes` row containing key names only, never secret values.

### 5.10 Settings

All account controls live on one page at `/settings`:

- **Profile**: display name, bio, avatar upload (JPEG, PNG, WebP or GIF, 5 MB cap). Handle and email are read-only here.
- **Preferences**: theme system/light/dark, units metric or imperial, and an optional caffeine bedtime (`HH:MM`) used by insights.
- **Notifications**: weekly email digest, friend-request email, and push (HTTPS and a supporting browser required).
- **Privacy**: default visibility for new check-ins, opt-in public profile, and an analytics opt-out that removes the Umami script entirely.
- **Security**: change password (current password required; the form asks for at least 10 characters and other sessions are revoked), API token management at `/settings/tokens`, active session list with per-session revoke, “Sign out everywhere”, and blocked accounts at `/settings/blocked` with unblock.
- **Data**: JSON export of the account's data and soft-delete account closure (type the handle to confirm). Deletion marks the row deleted, removes sessions and revokes tokens.
- **Password reset**: `/forgot` always shows the same anti-enumerated response, and `/reset/{token}` accepts a 1-hour, single-use link that revokes every session on success.

### 5.11 OpenCode ("opencode2") AI assistant

Ported from `dnd` (`internal/ai`), completely optional and hidden whenever `ai_enabled=0` or no enabled endpoint exists:
1. **Conversational & Vision Drink Drafting**: from the unknown-barcode create screen or `/admin/drinks`, users/moderators can open “Draft with AI”, paste rough tasting notes or label text (or attach a photo when a `vision` endpoint is enabled), and converse with the assistant to fill structured drink fields (`brand`, `name`, `style`, `flavours`, `sweetener`, `acid`, `carbonation`, nutrition per 100 ml, description). The conversation is stored in `ai_draft_sessions` and can be continued, revised or committed; each turn preserves the same `x-opencode-session` header and `User-Agent: sodapop/<version>`.
2. **Tasting Note Polish**: inside the check-in sheet, a “Polish note” chip turns rough bullet points + selected flavour chips into a 1–2 sentence note matching Sodapop's voice (crisp, unpretentious, no exclamation marks, no sugar shaming). The user reviews and edits before submitting.
3. **Soda Sommelier (“What to try next”)**: on `/drinks` and the user profile, an optional card suggests 3 sodas from the catalog based on the user's top-rated styles, flavours, and sweetener preferences, with a one-sentence explanation for each.

### 5.12 Flutter Android app (`mobile/`)

Companion Android client modeled on `board-games/mobile` and `dnd/mobile` (full spec in [mobile.md](mobile.md)):
- **Onboarding**: starts with an empty server URL field (zero hardcoded default URL), validates and normalizes `http(s)` URLs, probes `GET /api/v1/auth/oidc/status`, and offers **“Sign in with Authelia”** (in-app WebView + native cookie capture + `sd_` device token exchange) alongside personal API token (`sd_…`) and optional password fallback.
- **Native barcode scanner**: uses `mobile_scanner` (CameraX + ML Kit) with hardware torch and pinch-to-zoom, normalizing scans to GTIN-14 and resolving against the same local cache → `/api/v1/barcodes/{gtin}` ladder.
- **Feature parity**: 4-slot bottom navigation (`Friends`, lifted lime `Scan`, `Discover`, `You`), quarter-star check-in sheet with photo attachment, AI tasting-note polish and an offline check-in queue with idempotency keys (photos up to 2 MiB included), and lime/forest Material 3 theme (`#B4E32C` / `#16241A` / `#F2F4E6` / `#0C130E`). The v1.2 app adds check-in detail with comments, other-user profiles with follow and block, notification actions, lists, saved venues, venue search/detail, insights, taste twins, passport/badges and mixes. It has an authenticated media path (`ApiClient.mediaUrl` → `/api/v1/media/{id}`); short video clips are shown as a "watch on web" notice.

### 5.13 Integrations

Outbound integrations are configured per user at `/integrations`: Home Assistant webhook, Matrix room (`homeserver|room_id` plus a token) and TRMNL. A test button sends a sample event. TRMNL receives `merge_variables` JSON from the latest public or friends-visible check-in, and push delivery only happens when `push_enabled` is on.

**Home Assistant inbound** lets a motion sensor or NFC tag log a soda:
- Create a token at `/integrations` (one active token per user; creating a new one revokes the previous). Only metadata is listed afterwards; the token is stored as a SHA-256 hash.
- `POST /hooks/ha/{token}` with `gtin` or `slug`, plus optional `rating` (0–20), `note`, `venue_id` and `serving`. Send an `Idempotency-Key` header (or `idempotency_key` body field) so a retrying automation does not log twice.
- The route is rate limited to 10 requests per minute per token and returns a generic 401 for unknown, revoked or malformed tokens.
- `GET /api/v1/integrations/home-assistant` returns outbound wiring plus inbound token metadata for a sensor or diagnostic card.

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

**Notifications, profiles and settings**
- An unread notification increments the nav badge; marking one read decrements it, and mark-all-read clears the badge.
- A non-public profile redirects an anonymous visitor to `/login` and returns 404 to a signed-in stranger.
- An opted-in public profile renders only `public` check-ins to an anonymous visitor.
- Password reset from `/forgot` to `/reset/{token}` changes the password once, expires after 1 hour, and revokes every session.
- The analytics opt-out removes the Umami script even when `UMAMI_URL` is configured.

**Catalog, drops and insights**
- `/scan?gtin=` rejects a bad check digit with 400 and resolves a known GTIN to its drink.
- `/drinks` filters compose in the URL and page 24 rows at a time; the same data is not available from a collection API.
- Discontinuing a wishlisted drink notifies each owner once, even if the status action is retried.
- `/insights` returns a zero-filled series for each day in the selected 7, 30 or 90-day window and is scoped to the signed-in user.

**Home Assistant inbound**
- A valid token creates an idempotent check-in from `gtin` or `slug`; a revoked or unknown token returns the same 401.
- A retry with the same `Idempotency-Key` returns 200 and the original check-in instead of a second row.

**Ops**
- `/healthz` returns 200 with no collector configured; `OTEL_*` unset means no outbound telemetry.
- `OTEL_EXPORTER_OTLP_ENDPOINT` points at the collector profile; traces, metrics, and logs arrive with `service.name=sodapop`.
- Nightly backup produces a restorable file.
- Umami unset means no script tag and no CSP entry.

## 12. Acceptance criteria (v1.2)

- A comment on an invisible check-in returns 404, and a comment thread stops showing rows from a blocked author in either direction.
- Blocking removes the friendship and both follow directions, hides the profile, feed, comments, toasts and notifications, and `/settings/blocked` can undo only the caller's block.
- A venue photo/menu stops being fetchable after an uploader or moderator removes it, and the 6-per-uploader/20-per-venue caps hold.
- A drink gallery upload by a verified member appears without becoming primary; only an admin can promote or delete it.
- `/compare` rejects fewer than 2 or more than 6 drinks, duplicate slugs and unknown/merged drinks without a server error.
- `GET /api/v1/checkins/{id}/comments`, the venue media list and the drink gallery never return a total or an unbounded collection.
- The Android app posts a comment, follows/blocks a profile, opens a list/saved venue/insight/taste/passport/mix/venue screen, and replays an offline check-in with its idempotency key.

## 13. Risks and open questions

| Risk | Mitigation |
|---|---|
| Open Food Facts coverage for soda is patchy (6.2k soft drinks, missing nutrition) | Local catalog is authoritative after first import; create flow is first-class; never block a check-in on OFF |
| iOS barcode performance in low light | Wasm fallback, ROI cropping, torch where supported, manual entry always visible |
| ODbL share-alike on OFF data | Keep OFF-sourced rows in separate tables/columns with attribution; do not blend with proprietary data; document the boundary |
| Catalog vandalism | Provisional entries, report queue, admin merge, revision history, rate limits on create |
| SQLite write contention from badge/stats writes | Single writer, WAL, short transactions; stats from views at read time |
| Self-hosted SMTP deliverability | Clear setup docs, test-email button, outbox visibility, optional relay |

Open: whether to seed a starter catalog (e.g. 500 common sodas) to make first-run useful; whether to default units by locale now that a metric/imperial toggle exists; whether to expose a public read-only API without weakening the friend-scoped `/api/v1` contract.

## 14. Success signals

- First-run: signup → verified → first check-in completes in under 3 minutes.
- ≥ 60% of check-ins start from a scan.
- Barcode local-hit rate ≥ 70% after 30 days of use.
- Median check-in creation < 20 s.
- Zero P1 accessibility failures in axe on the top 10 pages.
