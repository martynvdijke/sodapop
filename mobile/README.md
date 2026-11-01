# Sodapop mobile

Flutter Android companion app. It talks only to your own Sodapop server
(`/api/v1`) and never contains a default server address.

## Run

```sh
cd mobile
flutter pub get
flutter run
```

Enter the server origin (for example `https://soda.example.com`) on first
launch. Sign in with Authelia when the server advertises OIDC, paste a personal
`sd_` token, or use email + password when password fallback is enabled.

## v1.2 surfaces

- **Check-in detail and comments** (`CheckinDetailScreen`): one check-in with
  photo, rating, note, venue, toasts and a flat comment thread. Comments can be
  posted and the viewer's own comments deleted; the server enforces visibility
  inheritance from the parent check-in. The client exposes an authenticated media
  helper (`ApiClient.mediaUrl` → `/api/v1/media/{id}`); video clips show a "watch on
  the web app" notice.
- **Other-user profile** (`UserProfileScreen`): stats, earned badges, top styles
  and visible lists, with follow/unfollow and block/unblock. Missing, private and
  blocked profiles all show the same neutral "Profile unavailable" state.
- **Notifications** (`NotificationsScreen`): friend requests, toasts, comments,
  badges and system messages; friend requests can be accepted or declined and
  the list can be marked all read.
- **Lists** (`ListsScreen`, `ListDetailScreen`): the member's own lists including
  the system wishlist, with item counts and a read-only item view.
- **Saved venues** (`SavedVenuesScreen`): bookmarked venues, newest first.
- **Venues** (`VenuesScreen`, `VenueDetailScreen`): capped venue search, recent
  drinks, community photos and menus, and save/unsave.
- **Insights** (`InsightsScreen`): sugar and caffeine totals for 7, 30 or 90 days.
- **Taste** (`TasteScreen`): accepted-friend taste twins and personal
  recommendations.
- **Passport and badges** (`PassportScreen`, `BadgesScreen`): lifetime styles,
  venue kinds and countries, earned badges, plus the full badge list with progress.
- **Mix** (`MixScreen`, `MixCreateScreen`, `MixDetailScreen`): the member's own
  recipes and a 2–6 drink mix builder.
- Feed, native scan, discover/search and the quarter-star check-in sheet remain
  the core app surfaces. The check-in sheet can attach one photo and the offline
  queue carries it when it is small enough.

## Auth and tokens

- The server URL and `sd_` token live in `flutter_secure_storage`; no default URL
  is compiled in.
- OIDC onboarding opens `/auth/oidc/login` in a WebView, captures the `sd_session`
  cookie through the native `xyz.vandijke.sodapop/cookies` channel, and exchanges
  it for a device token at `POST /api/v1/auth/mobile-token`.
- A pasted personal token is validated with `GET /api/v1/me` before it is stored.
- All API calls send `Authorization: Bearer <sd_ token>` and a W3C `traceparent`.
- The app exposes a Bearer media URL through `ApiClient.mediaUrl` (`/api/v1/media/{id}`) for authenticated media access.

## Offline queue

Check-ins that fail with a network error are stored in `shared_preferences` with a
client-generated idempotency key and replayed on the next feed load. The queue
supports drink, rating, note, venue, serving, visibility and a photo up to 2 MiB
(base64 in the queue); the current check-in sheet supplies the drink, rating,
note and one photo. A larger photo is dropped with a one-time message and the
check-in still queues. There is no background sync; replay happens when the feed
refreshes.

## Build

```sh
flutter analyze
flutter test
flutter build apk --release
```

## Scanner emulator test

`mobile/integration_test/scanner_test.dart` runs the scanner on an Android
emulator. It decodes a generated EAN-13 asset through the same native
`analyzeImage` path used by the camera, normalizes it to GTIN-14, then resolves
it against a live seeded server (`10.0.2.2` is the host from the emulator).

```sh
# terminal 1: seeded Sodapop server on the host
DATABASE_PATH=/tmp/sodapop-emulator.db SEED_DEMO=1 go run ./cmd/seed
DATABASE_PATH=/tmp/sodapop-emulator.db PORT=8097 \
  BASE_URL=http://127.0.0.1:8097 \
  SESSION_SECRET=android-emulator-secret-android-emulator \
  ALLOW_PASSWORD_FALLBACK=true ENV=development go run ./cmd/server

# terminal 2: with an emulator already booted
task mobile-emulator-test
# or: cd mobile && flutter test integration_test/scanner_test.dart \
#       -d emulator-5554 \
#       --dart-define=E2E_BASE_URL=http://10.0.2.2:8097
```

Debug builds allow cleartext HTTP to the local test server; release builds keep
cleartext disabled. CI runs the same flow in
`.github/workflows/android-emulator.yaml` with `reactivecircus/android-emulator-runner`.

Release signing reads `android/key.properties`; without it `flutter build appbundle`
fails in CI rather than shipping a bundle Play would reject. CI attaches the
signed APK (`sodapop-<version>-android.apk`) to GitHub Releases and keeps the
matching App Bundle as the `app-release-aab` workflow artifact — it is a
submission format, so it is uploaded to the Play track named in
`GOOGLE_PLAY_TRACK` rather than offered for download.
