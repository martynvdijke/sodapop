# Native Android push (FCM)

Sodapop uses Firebase Cloud Messaging (FCM) for the one case a web socket cannot
cover: waking an Android app that is backgrounded or closed. Everything else —
the in-app inbox, the live badge, the web SSE stream and the app's foreground
stream — works without FCM.

FCM is **optional**. With no credentials the server logs `fcm native push
disabled` at startup, the app never initialises Firebase, and nothing else
changes.

## How it fits together

1. The app asks for notification permission from a deliberate action (the
   Settings toggle), obtains an FCM registration token, and sends it to
   `POST /api/v1/me/push/devices`. Sign-out sends `DELETE` and unregisters the
   token at Firebase.
2. When a push-worthy notification is written (`friend_request`,
   `friend_accept`, `toast`, `comment`, `mention`), the outbox queues one `fcm`
   row per registered device.
3. The worker mints a Google OAuth2 token from the service-account key
   (RS256 JWT assertion, cached until shortly before expiry) and posts to
   `https://fcm.googleapis.com/v1/projects/<project>/messages:send`.
4. Tokens Google reports as `UNREGISTERED` or `SENDER_ID_MISMATCH` are deleted
   from `push_devices`.

Payloads carry a `notification` block (title/body rendered by the system tray)
and `data.url`, the same relative href the inbox uses. The app routes a tap:
`/checkins/{id}` opens the check-in, anything else opens the inbox.

## What you need

| Piece | Where it comes from | Goes to |
|---|---|---|
| Firebase project | Firebase console | everything below |
| Android app registration | Firebase console → Project settings → Your apps → Add app → Android, package `xyz.vandijke.sodapop` | enables tokens for that project |
| `apiKey`, `appId`, `messagingSenderId`, `projectId` | Project settings → General → Your apps → SDK setup | app build (dart-defines) |
| Service-account JSON | Google Cloud console → IAM → Service accounts → Keys | server (`FCM_SERVICE_ACCOUNT_JSON`) |

The app and the service account must belong to the **same** Firebase project;
a token issued by one project is not accepted by another.

## 1. Create the Firebase project and Android app

1. <https://console.firebase.google.com> → **Add project**.
2. In the project, **Add app → Android**, package name `xyz.vandijke.sodapop`.
   The nickname and debug signing SHA-1 are optional — FCM does not verify the
   signing certificate; the SHA-1 is only needed for Google Sign-In.
3. Skip downloading `google-services.json`. Sodapop passes
   `FirebaseOptions` explicitly, so the Gradle plugin and that file are not
   used.
4. Note the four values under **Project settings → General → Your apps**:
   `apiKey`, `appId` (`1:...:android:...`), `messagingSenderId` (project
   number) and `projectId`.

## 2. Create the service account

1. <https://console.cloud.google.com> → select the same project.
2. **APIs & Services → Library** → enable **Firebase Cloud Messaging API**.
3. **IAM & Admin → Service accounts → Create service account**, name it
   `sodapop-fcm`. Grant the role **Firebase Cloud Messaging API Admin**
   (`roles/firebasecloudmessaging.admin`). No other role is needed.
4. Open the account → **Keys → Add key → Create new key → JSON**. Download the
   file once; Google will not show it again.

Keep the file out of the repository. Store it on the server with mode `0600`,
for example `/etc/sodapop/fcm-service-account.json`.

## 3. Configure the server

```sh
FCM_SERVICE_ACCOUNT_JSON=/etc/sodapop/fcm-service-account.json
# Optional: defaults to the project_id inside the JSON.
FCM_PROJECT_ID=your-project-id
```

`FCM_SERVICE_ACCOUNT_JSON` accepts either a path to the JSON file or the raw
JSON itself, which is convenient for container secrets. On startup the server
logs one of:

```
fcm native push enabled  detail="project your-project-id"
fcm native push disabled reason="FCM_SERVICE_ACCOUNT_JSON is not set"
```

A malformed or unreadable value is reported as disabled with the reason, so a
typo does not turn into silently missing notifications.

## 4. Build the app with the Firebase values

```sh
cd mobile
flutter build apk --release \
  --dart-define=FCM_API_KEY=... \
  --dart-define=FCM_APP_ID=... \
  --dart-define=FCM_MESSAGING_SENDER_ID=... \
  --dart-define=FCM_PROJECT_ID=... \
  --dart-define=FCM_STORAGE_BUCKET=...   # optional
```

Omitting any of the four required values leaves push disabled in that build. A
value passed as an empty string counts as missing, which is why the CI
workflows can pass `${{ secrets.FCM_* }}` unconditionally.

For GitHub Actions, add repository secrets `FCM_API_KEY`, `FCM_APP_ID`,
`FCM_MESSAGING_SENDER_ID`, `FCM_PROJECT_ID` (and optionally
`FCM_STORAGE_BUCKET`). `flutter-android.yaml` and `release.yaml` already pass
them through, so the released APK and bundle include push once the secrets
exist. With no secrets the builds still succeed and ship a push-less APK.

## 5. Verify

1. Install the build, sign in, open **Settings → Notifications** and turn push
   on. Android 13+ shows the permission prompt at that point, not at launch.
2. Confirm the device registered (any of):
   - the app does not show "push unavailable" in Settings;
   - `sqlite3 data/sodapop.db 'select user_id, platform, last_seen_at from push_devices';`
3. Send a test notification through the configured credentials:

   ```sh
   go run ./cmd/fcmtest -handle <your-handle>
   # or target a specific token
   go run ./cmd/fcmtest -token <device-token> -title "Hi" -body "It works"
   ```

   The command reads the same environment as the server and prints the Google
   error verbatim when delivery fails. It prunes a token Google rejects, just
   like the worker does.
4. Trigger a real event (a friend comments on your check-in) with the app
   backgrounded.

## Troubleshooting

| Symptom | Check |
|---|---|
| `fcm native push disabled` at startup | `FCM_SERVICE_ACCOUNT_JSON` is set and readable inside the container, not just on the host |
| `fcm send failed: ... 401` | the service account lacks `roles/firebasecloudmessaging.admin`, or the FCM API is not enabled |
| `... 404 SENDER_ID_MISMATCH` | the app was built for a different project than the service account |
| `... 404 UNREGISTERED` | the app was reinstalled or the token expired; reopen the app so it registers again |
| Nothing arrives, no error | notification permission is off, or the device is in Doze/force-stopped; check the Settings toggle and battery optimisation |
| Works in debug, not in release | the release build did not receive the dart-defines (check the CI secrets) |

FCM delivery is best effort: Google may delay or drop a message, which is why
the in-app inbox remains the source of truth. Force-stopping the app from
Android settings also stops push until it is opened again.

## Cost, privacy and rotation

- FCM is free for this volume. The only paid dependency would be a very large
  device count, which a self-hosted instance will not reach.
- Payloads contain a title, a body and a relative URL — the same text the inbox
  stores. Nothing else is sent to Google, and no analytics identifiers are
  attached.
- Rotate the service-account key by creating a new key, updating the secret,
  restarting the server, and deleting the old key in the Cloud console. Old
  keys keep working until deleted.
- Web Push (VAPID) remains a separate, independent path configured under
  `/admin/settings`; see `docs/architecture.md`.
