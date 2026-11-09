# Sodapop TRMNL plugins

Polling plugins for a [TRMNL](https://usetrmnl.com) e-ink display. Each directory is one plugin, and each polls one ready-to-render JSON endpoint under `/api/v1/trmnl/`; there is no transform code to maintain.

| Directory | Name | Endpoint | Refresh |
| --- | --- | --- | --- |
| `checkins/` | Sodapop - Recent Pours | `/api/v1/trmnl/checkins?limit=10&handle=` | 15 s |
| `new-drinks/` | Sodapop - New on the Shelf | `/api/v1/trmnl/new-drinks?scope=drinks` | 60 s |
| `admin/` | Sodapop - Mod Desk | `/api/v1/trmnl/admin` | 30 s |
| `me/` | Sodapop - My Stats | `/api/v1/trmnl/me` | 30 s |

Each plugin has `.trmnlp.yml`, `src/settings.yml` (custom fields and polling URL) and the four `src/*.liquid` views TRMNL renders.

## Install one

1. In TRMNL, open **Plugins → Add a plugin → Private plugin**, pick a strategy, and create it.
2. Fill in the custom fields:

   | Plugin | Fields |
   | --- | --- |
   | `checkins` | `url`; optional `handle` (blank polls the whole community feed) |
   | `new-drinks` | `url`, `api_token` |
   | `admin` | `url`, `api_token` |
   | `me` | `url`, `api_token` |

   `url` is the instance base URL including the protocol, e.g. `https://sodapop.vandijke.xyz`.

3. Save. The plugin polls on its own `refresh_interval`; the view fills in with live data.

The `new-drinks` plugin is shipped with `?scope=drinks`. To show venues or new members instead, edit `polling_url` in `src/settings.yml` and push: `scope=venues`, `scope=users` (counted over 7 days) or `scope=all` (recent activity). `scope_label` and `window_label` on the screen come from the endpoint, so no view changes are needed.

## Tokens

Create a token in Sodapop under **Settings → API tokens** (`/settings/tokens`). It needs the `read` scope, and the value goes in the plugin's `api_token` password field.

- `me` works with any member's token.
- `admin` and `new-drinks` need a token minted by an admin or moderator; a member token gets a 404 from both endpoints.
- `checkins` needs no token. A `handle` only works when that member has opted into a public profile.

The check-ins feed stays public, but the three authenticated plugins send `Authorization: Bearer <token>` as configured in `polling_headers`.

## Local preview

```sh
gem install trmnl_preview -v 0.11.0
trmnlp serve --dir trmnl/checkins
```

`serve` renders the plugin in a browser using the values in `.trmnlp.yml`; edit `src/*.liquid` and it reloads. Point `url` at a reachable instance to see live data, and add a `read` token to `api_token` for the authenticated plugins.

## Lint and push

```sh
task trmnl-lint                  # trmnlp lint --dir each plugin
trmnlp push --force --dir trmnl/checkins
```

`trmnlp push` needs `TRMNL_API_KEY` in the environment (`TRML_TOKEN` in CI). `.github/workflows/trml.yaml` lints on pull requests touching `trmnl/**`, and the release workflow calls it to push on `main`.

Pushing a plugin whose `src/settings.yml` has no `id:` creates a **new** TRMNL plugin. The push job scrapes the new ID from the output and fails on purpose: add `id: <n>` to that `settings.yml`, commit, and the next push updates the existing plugin instead of duplicating it.
