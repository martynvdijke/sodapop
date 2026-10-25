# Authentication, OIDC, and Email Verification Specification

## Purpose

Defines Sodapop's hybrid authentication system combining first-class OpenID Connect (Authelia PKCE S256) with local email+password accounts, mandatory email verification for write actions, DB-backed sessions, CSRF protection, and personal/mobile API tokens (`sd_`).

## Requirements

### Requirement: OIDC Authentication with PKCE and JWKS Verification
When `OIDC_ENABLED=true`, the system SHALL authenticate users via OpenID Connect Authorization Code flow with PKCE (`S256`) against `OIDC_ISSUER_URL`.

#### Scenario: Discovery and login redirect
- **WHEN** the server starts with `OIDC_ENABLED=true` and a user requests `GET /auth/oidc/login`
- **THEN** the server uses endpoints discovered from `{OIDC_ISSUER_URL}/.well-known/openid-configuration`
- **AND** sets an HMAC-signed `HttpOnly` cookie containing `state`, `nonce`, and PKCE `code_verifier`
- **AND** redirects the browser to the authorization endpoint with `scope=openid email profile groups`, `code_challenge`, and `code_challenge_method=S256`

#### Scenario: Valid OIDC callback provisions or links verified user and syncs admin group
- **WHEN** the provider redirects to `GET /auth/oidc/callback` with a valid `code` and matching `state`
- **THEN** the server exchanges the code using the PKCE `code_verifier`, verifies the ID token's RS256 signature against cached JWKS keys, validates `iss`, `aud`, `exp`, `nonce`, and requires `email_verified=true`
- **AND** enforces `ALLOWED_EMAILS` and `ALLOWED_DOMAINS` if configured
- **AND** links or creates the `User` and `UserIdentity(provider='oidc', subject=sub)` with `email_verified_at` stamped
- **AND** promotes the user's `role` to `admin` if it is the first user in the database or if the `groups` claim contains `admins`
- **AND** issues an `sd_session` cookie

#### Scenario: Public OIDC discovery endpoint for Web and Flutter Android
- **WHEN** an unauthenticated client requests `GET /api/v1/auth/oidc/status`
- **THEN** the server returns `200 OK` with `{"enabled": <bool>, "issuer": "<url>", "password_fallback": <bool>}`

### Requirement: Local Signup and Mandatory Email Verification Gate
When local accounts are enabled (`OIDC_ENABLED=false` or `ALLOW_PASSWORD_FALLBACK=true`), the system SHALL allow email+handle+password signup with `bcrypt` hashing and restrict write actions until the email address is verified.

#### Scenario: Unverified user is read-only
- **WHEN** a newly registered local user has `email_verified_at IS NULL`
- **THEN** the user can browse, search, and scan barcodes
- **AND** any attempt to create a check-in, follow, friend request, drink, venue, list, photo upload, or report returns HTTP `403` with code `email_not_verified`
- **AND** the web UI displays a persistent banner with a throttled (3/hour) resend button

#### Scenario: Verification token consumption
- **WHEN** the user visits `GET /verify?token=<raw>` within 24 hours of issuance
- **THEN** the server matches `SHA-256(raw)` in `email_verification_tokens`, stamps `users.email_verified_at` and `used_at`, invalidates older verification tokens for that user, and unlocks write actions

### Requirement: DB-Backed Sessions, CSRF, and Scoped API Tokens
The system SHALL store opaque sessions and personal/mobile API tokens hashed with SHA-256 in SQLite.

#### Scenario: Session cookie and CSRF validation
- **WHEN** a browser mutates state via `POST`, `PUT`, `PATCH`, or `DELETE` using an `sd_session` cookie
- **THEN** `RequireCSRF` verifies `X-CSRF-Token` or form `csrf_token` in constant time against `sessions.csrf_token` and checks `Origin` against `BASE_URL`

#### Scenario: Scoped personal and mobile API tokens
- **WHEN** a user creates a token in `/settings/tokens` or completes mobile OIDC token exchange (`POST /api/v1/auth/mobile-token`)
- **THEN** the server generates a `sd_`-prefixed 32-byte secret, returns the plaintext once, stores only its SHA-256 hash with scopes (`read`, `write`, `feed`), and accepts `Authorization: Bearer sd_...` on `/api/v1/*` without CSRF
