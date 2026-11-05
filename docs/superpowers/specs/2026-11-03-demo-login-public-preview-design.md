# Demo login and public preview

Date: 2026-11-03
Status: approved

## Problem

The hosted instance at `https://sodapop.vandijke.xyz` currently starts empty and
requires a normal registration before anyone can see or do anything. We want a
visitor to be able to look around first, and we want a known account that can be
used to try posting immediately.

## Goals

1. The hosted instance ships a demo account (`demo` / `demo`) that is verified
   and can post.
2. Logged-out visitors can browse a global feed of **public** check-ins and a
   directory of **opted-in public** profiles on both the web app and the Android
   app.
3. Logged-out visitors cannot post, toast, save, comment, report, follow, or
   mutate anything.
4. The Android app always defaults to `https://sodapop.vandijke.xyz`.
5. `friends` and `private` check-ins, private profiles, and all write routes
   remain closed to logged-out visitors.

## Non-goals

- Public access to the catalog, venues, search, or settings.
- Public write access of any kind.
- Replacing the existing friend-scoped authenticated feed.

## Demo account

- New `internal/demo` package with `Ensure(ctx, client, cfg, checkins)`.
- Config:
  - `DEMO_USER_ENABLED` — defaults to `true` when `BASE_URL`'s host is
    `sodapop.vandijke.xyz`, otherwise `false`.
  - `DEMO_USER_HANDLE` — default `demo`.
  - `DEMO_USER_PASSWORD` — default `demo`.
  - `DEMO_USER_EMAIL` — default `demo@sodapop.local`.
- Provisioning is idempotent. It creates or repairs an active, verified,
  non-admin user with a public profile, default visibility `public`, display
  name `Demo Soda`, and a short bio. The password hash is refreshed if the
  configured password no longer matches.
- When the demo user has no check-ins, seed six public check-ins across three
  drinks. Use `SEED_CATALOG` rows when present; otherwise create minimal demo
  drinks. Use stable `idempotency_key` values so restarts do not duplicate.
- Web `/login` shows a "Use the demo account" button when demo login is enabled.
- `GET /api/v1/auth/config` returns
  `demo: {enabled, handle, password}` so the app can offer the same button.
- Startup logs the handle only; never log the password.

## Public preview read model

New `internal/checkins/public.go`:

- `PublicFeed(ctx, PublicFeedFilter{ViewerID, Limit, Before}) ([]ProfileItem, error)`
  - `c.visibility = 'public'`
  - `c.deleted_at IS NULL`
  - author `u.deleted_at IS NULL AND u.status = 'active'`
  - newest first, `created_at DESC, id DESC`, limit capped at 50.
  - A viewer ID is optional and only affects the `toasted` flag.
- `PublicPeople(ctx, PublicPeopleFilter{Limit, Before}) ([]PublicPerson, error)`
  - join `user_settings` where `public_profile = 1`
  - user is active and not deleted
  - ordered by last public check-in descending, then handle.
- `PublicProfile(ctx, handle)` returns the opted-in public profile and stats.
- `PublicUserCheckins` reuses `ProfileCheckins` with an empty viewer.
- `PublicCheckin(ctx, id)` reuses the existing viewer-aware `Get`; only public
  rows resolve for an empty viewer.

## Web

- Anonymous `/` keeps the hero and adds **Public pours** (20 newest, `before`
  paging) and **Public people** (8 profiles, link to `/people`).
- `GET /people` is a public directory page with `before` paging.
- Existing `/u/{handle}` and `/checkins/{id}` public behaviour is reused.
- Logged-in `/` keeps the existing friend feed and stats unchanged.
- Every public card carries a "Sign in to post" action to `/login?next=...`.

## API

Unauthenticated, IP-limited (30/min), `Cache-Control: public, max-age=60`:

- `GET /api/v1/public/feed?limit&before`
- `GET /api/v1/public/people?limit&before`
- `GET /api/v1/public/users/{handle}`
- `GET /api/v1/public/users/{handle}/checkins?limit&before`
- `GET /api/v1/public/checkins/{id}`
- `GET /api/v1/public/media/{id}`
- `GET /api/v1/public/media/{id}/thumb`

No public write routes. All other `/api/v1` routes stay Bearer-only and
friend-scoped.

## Mobile

- `defaultServerUrl` stays `https://sodapop.vandijke.xyz` and onboarding always
  prefills it; a saved server still wins for users who explicitly changed it.
- Onboarding adds "Browse without signing in" and, when the server reports a
  demo account, "Try the demo".
- A read-only `PublicShell` shows **Public** and **People** tabs with a
  persistent "Sign in to post" action.
- Tapping a pour or person opens read-only screens. No toast, save, comment,
  report, or compose affordances.
- `ApiClient` gains the public methods and a public media URL helper.

## Rule change

This feature deliberately adds a global public read surface. The existing
"never add a global feed" rule is amended to:

> Authenticated feeds remain friend-scoped. The only public collection surface
> is the read-only preview, limited to `public` check-ins and opted-in public
> profiles. `friends`, `private`, and all write routes stay closed.

`AGENTS.md`, `docs/architecture.md`, `README.md`, `docs/mobile.md`, and
`internal/api/openapi.json` must reflect this.

## Testing

- Go: demo provisioning idempotency; public feed/directory scoping; media
  visibility; rate-limit and cache headers.
- Web: anonymous `/` contains public only; authenticated `/` unchanged;
  anonymous mutations redirect to login.
- API: public routes work without a token and 404 on non-public rows.
- Flutter: preview shell, read-only affordances, demo button, default server.
- Playwright: anonymous preview, demo login, post, logout.
