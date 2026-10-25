# Flutter Android Companion App (`mobile/`) Specification

## Purpose

Defines the native Flutter Android companion app in `mobile/`, including zero-default server onboarding, Authelia OIDC WebView sign-in with native cookie capture, `mobile_scanner` ML Kit barcode scanning, offline check-in queue, lime/forest branding, and GitHub Actions CI + release APK packaging.

## Requirements

### Requirement: Zero-Default Server URL Onboarding and OIDC Discovery
The Flutter Android app SHALL start with an empty server URL field on first launch and require explicit validation before making API calls.

#### Scenario: Fresh launch and OIDC status discovery
- **WHEN** the app launches with no stored credentials in `flutter_secure_storage`
- **THEN** the onboarding screen shows an empty server URL input with placeholder `https://soda.example.com` and makes no network requests until the user submits a valid `http(s)` origin
- **AND WHEN** a valid server URL is submitted
- **THEN** the app calls `GET /api/v1/auth/oidc/status` and, when `enabled: true`, displays **"Sign in with Authelia"** as the primary lime CTA alongside **"Use a personal API token"** (`sd_…`) and optional password fallback

#### Scenario: In-app WebView OIDC login and mobile token exchange
- **WHEN** the user taps **"Sign in with Authelia"**
- **THEN** the app clears stale WebView cookies, opens `{serverUrl}/auth/oidc/login` in an in-app WebView, captures the `sd_session` cookie via the native Android `CookieManager` channel (`xyz.vandijke.sodapop/cookies`) upon redirect back to `{serverUrl}`, exchanges it via `POST /api/v1/auth/mobile-token` for a scoped `sd_` Bearer token, and persists the token in `flutter_secure_storage`

### Requirement: Native ML Kit Barcode Scanning and Feature Parity
The Android app SHALL use `mobile_scanner` (CameraX + ML Kit) for barcode scanning and provide full check-in, feed, discover, venue, AI assistant, and offline outbox support.

#### Scenario: Native camera barcode scan to check-in sheet
- **WHEN** the user taps the center lifted lime Scan button in the 5-slot bottom navigation bar
- **THEN** the camera viewfinder opens with hardware torch and zoom controls, normalizes any detected EAN-13/EAN-8/UPC-A/UPC-E/Code 128 barcode to GTIN-14, resolves it via local cache or `GET /api/v1/barcodes/{gtin}`, and opens the quarter-star check-in sheet (with optional AI tasting-note polish)

### Requirement: Android Branding, CI Workflow, and Release APK Attachment
The repository SHALL include native Android adaptive/monochrome launcher icons in the lime/forest palette, a dedicated CI workflow, and semantic-release APK attachment.

#### Scenario: CI and release workflows build and attach the Android APK
- **WHEN** commits touch `mobile/**` or a semantic release runs on `main`
- **THEN** `.github/workflows/flutter-android.yaml` runs `flutter analyze`, `flutter test`, and `flutter build apk --release`
- **AND** `.releaserc.yaml` updates `mobile/pubspec.yaml` to `${nextRelease.version}` and `.github/workflows/release.yaml` attaches `sodapop-<version>-android.apk` to the GitHub Release without blocking the server Docker release if Android build steps fail
