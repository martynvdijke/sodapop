# Sodapop — Flutter Android App (`mobile/`)

The `mobile/` directory contains the native Flutter Android companion app for Sodapop, modeled directly on `board-games/mobile` and `dnd/mobile`. It talks exclusively to a self-hosted Sodapop server over `/api/v1` and `/auth/oidc/*`.

## 1. Architecture & Stack

- **SDK:** Dart `^3.9.0`, Flutter Material 3
- **State management:** `flutter_riverpod: ^3.4.3`
- **Routing:** `go_router: ^18.1.0`
- **HTTP:** `dio: ^5.11.1` with Bearer token, idempotency key header injection, and W3C `traceparent` propagation
- **Storage:** `flutter_secure_storage: ^11.2.0` (server URL + `sd_` Bearer token) and `shared_preferences: ^2.5.4` (offline check-in queue)
- **Photo picker:** `image_picker: ^1.2.4` for one check-in photo; no background media sync
- **OIDC WebView:** `webview_flutter: ^4.14.1` + Kotlin `MethodChannel("xyz.vandijke.sodapop/cookies")` on `MainActivity` to read HttpOnly `sd_session` from Android `CookieManager`
- **Barcode scanner:** `mobile_scanner: ^7.0.0` (Android CameraX + Google ML Kit barcode scanning with hardware torch and zoom controls)
- **Typography & Theme:** Material 3 type scale with Sodapop's fresh lime palette (`#B4E32C` primary fill, `#16241A` forest ink, `#F2F4E6` light cream canvas, `#0C130E` dark canvas, `#E85D26` tangerine rating accent).

## 2. Directory Layout

```
mobile/
├── pubspec.yaml
├── analysis_options.yaml
├── android/
│   └── app/src/main/
│       ├── AndroidManifest.xml          # INTERNET, CAMERA permissions; label "Sodapop"
│       ├── kotlin/.../MainActivity.kt   # CookieManager bridge for sd_session
│       └── res/
│           ├── drawable/                # ic_launcher_foreground.xml, ic_launcher_monochrome.xml, launch_background.xml
│           ├── mipmap-anydpi-v26/       # ic_launcher.xml, ic_launcher_round.xml
│           └── values/                  # colors.xml (#16241A/#B4E32C), styles.xml (API 31+ splash)
├── lib/
│   ├── main.dart
│   └── src/
│       ├── api/
│       │   ├── api_client.dart          # Dio client, base URL guard, Bearer + traceparent
│       │   └── models.dart              # Drink, CheckinDetail, Comment, Venue, UserProfile, badges/lists
│       ├── auth/
│       │   ├── auth_controller.dart     # Riverpod state, OIDC status probe, logout
│       │   └── oidc_webview_screen.dart # Authelia WebView + sd_session → sd_ token exchange
│       ├── offline/
│       │   └── checkin_outbox.dart      # shared_preferences queue with idempotency keys
│       ├── screens/
│       │   ├── onboarding_screen.dart   # Empty server URL → OIDC / token / password sign-in
│       │   ├── feed_screen.dart         # friends/follows feed, toast, offline flush
│       │   ├── scanner_screen.dart      # mobile_scanner viewfinder + torch + manual GTIN entry
│       │   ├── checkin_sheet.dart       # Quarter-star slider, note, AI note polish
│       │   ├── checkin_detail_screen.dart # Check-in, toasts and flat comments
│       │   ├── discover_screen.dart     # Capped catalog search → check-in sheet
│       │   ├── user_profile_screen.dart # Other-user profile, follow and block
│       │   ├── notifications_screen.dart # Friend actions + mark all read
│       │   ├── insights_screen.dart     # 7/30/90-day sugar and caffeine totals
│       │   ├── taste_screen.dart        # Taste twins and recommendations
│       │   ├── passport_screen.dart     # Lifetime styles, venues, countries
│       │   ├── badges_screen.dart       # Full badge list with progress
│       │   ├── lists_screen.dart        # Own lists + wishlist
│       │   ├── list_detail_screen.dart  # One visible list
│       │   ├── mix_screen.dart          # Own mix recipes
│       │   ├── mix_create_screen.dart   # 2–6 drink mix builder
│       │   ├── mix_detail_screen.dart   # One mix
│       │   ├── venues_screen.dart       # Capped venue search
│       │   ├── venue_detail_screen.dart # Venue, recent drinks, photos/menus
│       │   ├── saved_venues_screen.dart # Bookmarked venues
│       │   └── profile_screen.dart      # Account shell and activity grid
│       ├── theme/
│       │   └── soda_theme.dart          # Light/dark ColorScheme and Material 3 type scale
│       ├── utils/                       # URL validation, GTIN normalization
│       └── widgets/                     # List cards, formatting helpers, shared loading/error states
└── test/
    ├── api_client_test.dart
    ├── api_models_test.dart
    ├── checkin_outbox_test.dart
    ├── gtin_test.dart
    ├── member_screens_test.dart
    └── url_test.dart
```

## 3. Onboarding & Authentication Flow

1. **Zero hardcoded server URL:** on fresh install, the server URL input is empty (placeholder hint `https://soda.example.com`). Submission validates a non-empty `http://` or `https://` origin, strips trailing slashes, and blocks any API call while unconfigured.
2. **OIDC discovery (`GET /api/v1/auth/oidc/status`):**
   - Once the server URL is validated, the app probes `/api/v1/auth/oidc/status`.
   - When `enabled: true`, **"Sign in with Authelia"** is rendered as the primary lime CTA (`#B4E32C` fill, `#16241A` text).
   - Tapping it clears WebView cookies, opens `{serverUrl}/auth/oidc/login` in `OidcWebViewScreen`, detects redirect back to `{serverUrl}`, reads `sd_session` via `MainActivity`'s native `CookieManager` channel, calls `POST /api/v1/auth/mobile-token` to mint a device-scoped `sd_` API token, and stores it in `flutter_secure_storage`.
3. **Personal API token (`sd_…`) & Password fallback:**
   - Users can always expand **"Use a personal API token"** and paste an `sd_…` token from `/settings/tokens` (verified against `GET /api/v1/me`).
   - When `password_fallback: true`, the email/handle + password form is also available.
4. **Sign out & server switch:** clearing credentials deletes the token from secure storage, clears WebView cookies, and pre-fills the previously saved server URL on the onboarding screen.

## 4. v1.2 screens and limits

- **Check-in detail** shows the photo, rating, note, venue, toast state and a flat comment thread. Comments can be posted and the viewer's own comments deleted; server-side visibility inherits the parent check-in. Photos load through the Bearer `/api/v1/media/{id}` path when requested through `ApiClient.mediaUrl`; video clips show a "watch on the web app" notice.
- **Other-user profiles** show stats, earned badges, top styles and visible lists, with follow/unfollow and block/unblock. Missing, private and blocked accounts render the same "Profile unavailable" state.
- **Notifications** support friend-request accept/decline and mark-all-read alongside toast, comment, badge and system rows.
- **Insights** shows sugar and caffeine totals for 7, 30 or 90 days. **Taste** shows accepted-friend twins and personal recommendations. **Passport** and **Badges** show lifetime styles/venues/countries and the full badge list with progress.
- **Lists, mixes and venues** cover own lists including the wishlist, 2–6 drink mix creation, capped venue search, venue detail with community photos/menus, and saved venues.
- **Offline queue:** check-ins that fail with a network error are stored in `shared_preferences` with an idempotency key and replayed on the next feed load. The queue supports drink, rating, note, venue, serving, visibility and a photo up to 2 MiB; the current sheet supplies drink, rating, note and one photo. Larger photos are dropped with a message and the check-in still queues.

The Android app does not have a separate settings screen; server URL and token management live on the profile shell and onboarding.

## 5. CI & Release Pipeline

- `.github/workflows/flutter-android.yaml` runs `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with SHA-pinned actions and `continue-on-error: true`.
- `.releaserc.yaml` + `.github/workflows/release.yaml` bump `mobile/pubspec.yaml` to `${nextRelease.version}+${runNumber}`, sign the release APK when `ANDROID_KEYSTORE_BASE64` is configured (falling back to debug-signed APK), and attach `sodapop-<version>-android.apk` to the GitHub Release.
