# Sodapop — Flutter Android App (`mobile/`)

The `mobile/` directory contains the native Flutter Android companion app for Sodapop. It talks exclusively to a self-hosted Sodapop server over `/api/v1` and `/auth/oidc/*`, and covers the same member surface as the web app apart from admin tools and public marketing pages. Native push and admin surfaces are not in the app.

## 1. Architecture & Stack

- **SDK:** Dart `^3.9.0`, Flutter Material 3
- **State management:** `flutter_riverpod: ^3.4.3` for the API client, auth state, and theme mode; screens keep local state.
- **Routing:** imperative `Navigator` pushes with a static `open(...)` helper per screen. `go_router` remains in `pubspec.yaml` but is not used.
- **HTTP:** `dio: ^5.11.1` with Bearer token, idempotency key injection, and W3C `traceparent` propagation.
- **Storage:** `flutter_secure_storage: ^11.2.0` (server URL + `sd_` Bearer token) and `shared_preferences: ^2.5.4` (offline check-in queue and offline read cache).
- **Media:** `image_picker: ^1.2.4` for camera/gallery photos and short videos; `video_player: ^2.14.1` for native playback of check-in clips.
- **Location:** `geolocator` for a one-shot Nearby query using coarse or fine permission only. There is no background location and no stored position.
- **Connectivity:** `connectivity_plus: ^7.3.2` triggers offline outbox flush and app-resume refresh.
- **Push (optional):** `firebase_core` + `firebase_messaging`, initialised from `--dart-define` values with no `google-services.json`; omitted defines leave push disabled.
- **OIDC WebView:** `webview_flutter: ^4.14.1` + Kotlin `MethodChannel("xyz.vandijke.sodapop/cookies")` on `MainActivity` to read the HttpOnly `sd_session` from Android `CookieManager`.
- **Barcode scanner:** `mobile_scanner: ^7.0.0` (Android CameraX + Google ML Kit with torch and zoom).
- **Typography & Theme:** Material 3 type scale with Sodapop's fresh lime palette (`#B4E32C` primary fill, `#16241A` forest ink, `#F2F4E6` light cream canvas, `#0C130E` dark canvas, `#E85D26` tangerine rating accent). The saved theme preference (`system`, `light`, `dark`) is applied app-wide.

## 2. Directory Layout

```
mobile/
├── pubspec.yaml
├── analysis_options.yaml
├── android/
│   └── app/src/main/
│       ├── AndroidManifest.xml          # INTERNET, CAMERA, POST_NOTIFICATIONS; label "Sodapop"
│       ├── kotlin/.../MainActivity.kt   # CookieManager bridge for sd_session
│       └── res/                         # adaptive + monochrome launcher, splash
├── lib/
│   ├── main.dart
│   └── src/
│       ├── api/
│       │   ├── api_client.dart          # Dio client, base URL guard, Bearer + traceparent
│       │   └── models.dart              # drinks, check-ins, comments, lists, settings, passport
│       ├── auth/
│       │   ├── auth_controller.dart     # Riverpod state, OIDC status probe, logout
│       │   └── oidc_webview_screen.dart # Authelia WebView + sd_session → sd_ token exchange
│       ├── offline/
│       │   ├── checkin_outbox.dart      # shared_preferences queue with idempotency keys
│       │   └── read_cache.dart          # last feed and last 30 viewed drinks
│       ├── push/
│       │   ├── firebase_push.dart       # FirebaseOptions from dart-defines, background handler
│       │   └── push_service.dart        # config check, token registration lifecycle
│       ├── screens/
│       │   ├── onboarding_screen.dart   # server URL → OIDC / password / token, register, reset
│       │   ├── register_screen.dart     # native registration + verification states
│       │   ├── password_reset_screen.dart
│       │   ├── feed_screen.dart         # friends feed, toast, save, comments link
│       │   ├── scanner_screen.dart      # camera + manual GTIN, AI drafting for unknown codes
│       │   ├── checkin_sheet.dart       # rating, serving, venue, visibility, note, photo/video
│       │   ├── checkin_detail_screen.dart # photo/video, toast, save, paginated comments
│       │   ├── drink_detail_screen.dart # facts, flavours, gallery, compare, log
│       │   ├── compare_screen.dart      # 2–6 drinks side by side
│       │   ├── discover_screen.dart     # capped search, compare, AI draft, Nearby entry
│       │   ├── nearby_screen.dart       # one-shot location, radius chips, distance buckets
│       │   ├── ai_draft_screen.dart     # conversational/vision drink drafting
│       │   ├── friends_screen.dart      # friends, requests, send request
│       │   ├── user_profile_screen.dart # other-user profile, follow and block
│       │   ├── profile_screen.dart      # own stats, recent pours, activity grid, settings
│       │   ├── settings_screen.dart     # profile, appearance, notifications, privacy, security, data
│       │   ├── saved_posts_screen.dart  # bookmarked check-ins
│       │   ├── notifications_screen.dart
│       │   ├── insights_screen.dart     # 7/30/90-day sugar and caffeine totals
│       │   ├── taste_screen.dart        # taste twins and recommendations
│       │   ├── passport_screen.dart     # stamps: styles, venues, countries, recent pours
│       │   ├── badges_screen.dart
│       │   ├── lists_screen.dart        # list CRUD + wishlist
│       │   ├── list_detail_screen.dart  # add/remove sodas
│       │   ├── mix_screen.dart
│       │   ├── mix_create_screen.dart
│       │   ├── mix_detail_screen.dart
│       │   ├── venues_screen.dart
│       │   ├── venue_detail_screen.dart # venue, media upload, recent drinks
│       │   └── saved_venues_screen.dart
│       ├── theme/
│       │   ├── soda_theme.dart          # light/dark ColorScheme and type scale
│       │   └── theme_mode.dart          # saved theme preference provider
│       ├── utils/                       # URL validation, GTIN normalization
│       └── widgets/                     # list cards, formatting, shared states
└── test/                                # data-layer, screen, offline and cache tests
```

## 3. Onboarding & Authentication

1. **Zero hardcoded server URL:** on a fresh install the server URL input is empty (hint `https://soda.example.com`). Submission validates a non-empty `http://` or `https://` origin, strips trailing slashes, and blocks API calls while unconfigured.
2. **OIDC discovery (`GET /api/v1/auth/oidc/status`):** once the origin is valid the app probes the endpoint. When enabled, **Sign in with Authelia** is the primary lime CTA. Tapping it clears WebView cookies, opens `{serverUrl}/auth/oidc/login`, detects the redirect back, reads `sd_session` through the native cookie channel, calls `POST /api/v1/auth/mobile-token`, and stores the device-scoped `sd_` token in secure storage.
3. **Password and token fallback:** when `password_fallback` is true, email/handle + password is available. A personal `sd_…` token from `/settings/tokens` can always be pasted and is verified with `GET /api/v1/me`.
4. **Registration and reset:** **New here? Create an account** opens the native registration screen (`POST /api/v1/auth/register`). It supports display name, handle, email, and a 10+ character password, then shows either an inbox/verification state with a 60-second resend cooldown or a created state. **Forgot password?** requests a reset link with an enumeration-safe confirmation. Verification and reset links land on the server's web pages.
5. **Sign out and server switch:** clearing credentials deletes the token, clears WebView cookies, and prefills the saved server URL on onboarding.

## 4. Member surface

- **Feed and posts:** friends feed with toast/untoast, save/unsave, share, and comments. Check-in detail plays photos and MP4/WebM/QuickTime clips natively with authenticated headers, toggles toasts and saves, and shows a paginated flat comment thread with oldest-first pages, "Show earlier comments", `@mention` links, and a Reply action that prefills the composer. Own comments can be deleted.
- **Check-ins:** quarter-star rating (0–20) with a Not rated option, serving (`can`, `bottle`, `glass`, `fountain`), optional venue search, visibility (`private`, `friends`, `public`, default private), a 1,000-character note, one photo from camera/gallery, or a short video from the camera. Video check-ins are online-only. Photos that fail on the network are queued offline with the same idempotency key.
- **Drink detail and catalog:** per-100 ml nutrition, sweetener/carbonation/acid, flavour intensity chips, aliases, lifecycle status, rater count with the three-rater threshold, a community gallery with attribution and moderator controls, compare, and log.
- **Nearby:** opened from Discover. The screen requests a one-shot coarse or fine location from a tap, offers 0.5, 1, 2, 5, 10 and 25 km radius chips, and lists visible pours nearest first with bucketed distance labels and "More nearby pours" pagination. Copy explains that the location is used for the search only; there is no background tracking. Empty, permission-denied and services-off states offer the relevant next step.
- **Compare:** pick 2–6 sodas from search and compare brand, style, container, volume, nutrition, descriptors, rating, and flavours side by side.
- **Friends and profiles:** friends directory with incoming/outgoing requests, accept/decline/cancel, and handle-based requests using neutral anti-enumeration copy. Other-user profiles show stats, badges, styles, and visible lists with follow/block. The own profile shows stats, recent check-ins, and a ten-item activity grid including Settings and Saved posts.
- **Lists, passport, badges, mixes:** create/edit/delete lists and add or remove sodas; the system Wishlist is read-only for its title. Passport shows style, venue-kind, and country stamps plus recent pours. Badges show earned and locked progress. Mix Lab creates 2–6 component recipes.
- **Venues:** capped search, detail with saved state, recent drinks, and photo/menu upload with optional labels and delete for the uploader or a moderator.
- **AI drafting:** start from notes or a scanned barcode, continue a multi-turn conversation, revise, and commit a provisional soda. A 503 renders a clear "not available on this server" state. Check-in notes can also be polished by AI.
- **Notifications:** friend requests, accepts, toasts, comments, mentions, badges, and system messages, with accept/decline and mark-all-read. When FCM defines are supplied, the device token is registered on sign-in and the list refreshes on foreground push; otherwise the app shows notifications when it loads rather than waking the device.
- **Insights and taste:** 7/30/90-day sugar and caffeine totals, caffeine clearance, taste twins, and recommendations.
- **Settings:** profile and bio, theme (applied immediately and persisted), units, email and push preferences, public profile, analytics opt-out, caffeine bedtime, password change, session list/revoke/sign out other sessions, blocked people with unblock, and account counts. The Data section exports the account as JSON and closes it after an exact-handle confirmation; the last-admin refusal is shown as a message. Notification preferences are editable, and native Android push registers automatically when the server build supplies FCM defines.

## 5. Offline behaviour

- **Check-in outbox:** network failures store the check-in in `shared_preferences` with an idempotency key. The queue supports drink, rating, note, venue, serving, visibility, and a photo up to 2 MiB. Replay happens on feed load, when connectivity returns, and when the app resumes. A photo over the limit is dropped with a message while the check-in still queues.
- **Read cache:** the last successful friends feed (40 rows) and the last 30 viewed drinks with galleries are stored locally. When a request fails with a likely network error, the screen shows the cached copy with a "Showing saved… Pull to refresh when you are online" banner. Logging, uploads, and moderation require a connection.
- **Video:** check-in clips are not queued; the sheet keeps the user informed and asks them to retry online.

## 7. Android push (optional)

Push uses Firebase Cloud Messaging and is disabled unless every required value is supplied at build time. No `google-services.json` or Google Services Gradle plugin is used; `Firebase.initializeApp` receives explicit `FirebaseOptions`.

```sh
flutter build apk --release \
  --dart-define=FCM_API_KEY=... \
  --dart-define=FCM_APP_ID=... \
  --dart-define=FCM_MESSAGING_SENDER_ID=... \
  --dart-define=FCM_PROJECT_ID=... \
  --dart-define=FCM_STORAGE_BUCKET=...   # optional
```

- Omitting any of `FCM_API_KEY`, `FCM_APP_ID`, `FCM_MESSAGING_SENDER_ID` or `FCM_PROJECT_ID` (or leaving it empty) skips Firebase entirely; the app starts and behaves as before.
- On sign-in and on a restored session the app requests notification permission, reads the FCM token, and calls `POST /api/v1/me/push/devices` with `{"token": "...", "platform": "android"}`. Token refreshes re-register.
- Sign-out calls `DELETE /api/v1/me/push/devices` with the last token in the body, best effort, while the Bearer token is still valid, then invalidates the registration at Firebase (`unregister()`) even if the request failed, so a lost request cannot leave the device receiving another account's notifications.
- The server sends `notification.title/body`, `data.url`, high Android priority, `collapse_key=sodapop-notification`, and channel `sodapop_default`. Foreground messages refresh the notifications screen; the background handler is registered so data messages are delivered.

## 8. CI & Release Pipeline

- `.github/workflows/flutter-android.yaml` runs `flutter analyze`, `flutter test`, and `flutter build apk --release` in `mobile/` with SHA-pinned actions.
- `.releaserc.yaml` + `.github/workflows/release.yaml` bump `mobile/pubspec.yaml` to `${nextRelease.version}+${runNumber}`, sign the release builds when `ANDROID_KEYSTORE_BASE64` is configured, and attach `sodapop-<version>-android.apk` to the GitHub Release. The App Bundle is uploaded as the `app-release-aab` workflow artifact (not a release asset: it is a submission format, not something users install) with the same version code as the APK (`git rev-list --count HEAD`, guarded so it must exceed the highest code already on Play), so Play never sees a decreasing code. Both builds refuse to run without a signing config rather than shipping a debug-signed artifact.
- `.github/workflows/play-store.yaml` reuses that artifact (no rebuild) and publishes it to the track in `GOOGLE_PLAY_TRACK` when `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON` is set; without the secret the publish job is skipped. The service account needs the Google Play Android Developer API enabled and Play Console access to the package, and the app must already exist in Play Console because the API cannot create it.
