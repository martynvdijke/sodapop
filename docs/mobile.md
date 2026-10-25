# Sodapop — Flutter Android App (`mobile/`)

The `mobile/` directory contains the native Flutter Android companion app for Sodapop, modeled directly on `board-games/mobile` and `dnd/mobile`. It talks exclusively to a self-hosted Sodapop server over `/api/v1` and `/auth/oidc/*`.

## 1. Architecture & Stack

- **SDK:** Dart `^3.13.2`, Flutter Material 3
- **State management:** `flutter_riverpod: ^3.4.3`
- **Routing:** `go_router: ^18.1.0`
- **HTTP:** `dio: ^5.11.1` with Bearer token / cookie interceptor, idempotency key header injection, and W3C `traceparent` propagation
- **Storage:** `flutter_secure_storage: ^11.2.0` (server URL + `sd_` Bearer token + session cookie) and `shared_preferences: ^2.5.4` (cached drinks, offline check-in outbox, theme preference)
- **OIDC WebView:** `webview_flutter: ^4.14.1` + Kotlin `MethodChannel("xyz.vandijke.sodapop/cookies")` on `MainActivity` to read HttpOnly `sd_session` from Android `CookieManager`
- **Barcode scanner:** `mobile_scanner: ^7.0.0` (Android CameraX + Google ML Kit barcode scanning with hardware torch and zoom controls)
- **Typography & Theme:** bundled `BricolageGrotesque` variable font (`mobile/assets/fonts/`) and Sodapop's fresh lime palette (`#B4E32C` primary fill, `#16241A` forest ink, `#F2F4E6` light cream canvas, `#0C130E` dark canvas, `#E85D26` tangerine rating accent)

## 2. Directory Layout

```
mobile/
├── pubspec.yaml
├── analysis_options.yaml
├── assets/
│   └── fonts/BricolageGrotesque-Variable.woff2 (or .ttf)
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
│       │   └── models.dart              # Drink, Checkin, Venue, User, Badge, AIDraft
│       ├── auth/
│       │   ├── auth_controller.dart     # Riverpod state, OIDC status probe, logout
│       │   └── oidc_webview_screen.dart # Authelia WebView + sd_session → sd_ token exchange
│       ├── offline/
│       │   └── checkin_outbox.dart      # Persistent queue with UUID idempotency keys
│       ├── screens/
│       │   ├── onboarding_screen.dart   # Empty server URL → OIDC / token / password sign-in
│       │   ├── feed_screen.dart         # Following / You / Nearby tabs + one-tap toast
│       │   ├── scanner_screen.dart      # mobile_scanner viewfinder + torch + manual GTIN entry
│       │   ├── checkin_sheet.dart       # Quarter-star slider, photo, venue, AI note polish
│       │   ├── discover_screen.dart     # FTS5 search, style/flavour filters, Sommelier card
│       │   ├── drink_detail_screen.dart # Ratings, flavours, nutrition, venues, AI draft edit
│       │   ├── venues_screen.dart       # Nearby/city venues (incl. hotel, vending)
│       │   ├── profile_screen.dart      # Stats, badges grid, lists/wishlist, friends
│       │   └── settings_screen.dart     # Server URL, theme, analytics opt-out, sign out
│       ├── telemetry/
│       │   ├── otel_tracer.dart         # Lightweight OTLP/HTTP span exporter + traceparent
│       │   └── umami_client.dart        # Opt-out-aware /api/send allowlisted event helper
│       ├── theme/
│       │   └── sodapop_theme.dart       # Light/dark ColorScheme & BricolageGrotesque text theme
│       └── widgets/                     # DrinkCard, QuarterStarSlider, FlavourChip, BadgeTile
└── test/
    ├── url_validation_test.dart
    ├── gtin_normalization_test.dart
    ├── onboarding_screen_test.dart
    └── checkin_outbox_test.dart
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

## 4. CI & Release Pipeline

- `.github/workflows/flutter-android.yaml` runs `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with SHA-pinned actions and `continue-on-error: true`.
- `.releaserc.yaml` + `.github/workflows/release.yaml` bump `mobile/pubspec.yaml` to `${nextRelease.version}+${runNumber}`, sign the release APK when `ANDROID_KEYSTORE_BASE64` is configured (falling back to debug-signed APK), and attach `sodapop-<version>-android.apk` to the GitHub Release.
