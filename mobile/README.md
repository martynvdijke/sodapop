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

## Build

```sh
flutter analyze
flutter test
flutter build apk --release
```

Release signing reads `android/key.properties`; without it the build falls back
to the debug key. CI attaches a signed (or debug-signed) APK to GitHub Releases
as `sodapop-<version>-android.apk`.
