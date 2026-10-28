# Sodapop — Data model (Ent ORM + SQLite Migrations)

Persistence uses SQLite (`modernc.org/sqlite`, WAL mode) managed by **Ent ORM (`entgo.io/ent v0.14.6`)** plus a **versioned transactional migration registry (`internal/db/migrations.go`)**, following the exact architecture proven in `dnd` (`dnd/docs/adr-data-access.md`, `dnd/db/db.go`, `dnd/db/schema_consistency_test.go`) and `datey`.

## 1. ORM & Migration Architecture

### 1.1 Canonical schema in Ent (`ent/schema/*.go`)
Every domain table is declared as a strongly-typed Ent schema under `ent/schema/` and generated via `go generate ./ent` into `ent/` and `ent/migrate/schema.go` (`migrate.Tables`):

| Domain | Ent Schemas (`ent/schema/`) |
|---|---|
| **Auth & Users** | `User`, `UserIdentity`, `Session`, `APIToken`, `EmailVerificationToken`, `PasswordResetToken`, `EmailOutbox`, `UserSetting`, `LoginAttempt`, `Media` |
| **Catalog** | `Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkFlavour`, `DrinkAlias`, `DrinkImage`, `OffProduct`, `CatalogRevision` |
| **Venues** | `Venue`, `VenueDrink`, `VenueName`, `VenueSave`, `VenueMedia` |
| **Check-ins** | `Checkin`, `CheckinLike`, `CheckinSave`, `Comment` |
| **Social** | `Follow`, `Friendship`, `Block`, `Activity`, `Notification` |
| **Badges & Lists** | `Badge`, `UserBadge`, `List`, `ListItem` |
| **Moderation** | `Report`, `AdminNote` |
| **Integrations** | `Integration`, `PushSubscription`, `InboundToken`, `DeliveryOutbox` |
| **Settings & AI** | `AppSetting`, `RuntimeSetting`, `AIEndpoint`, `AIDraftSession`, `SchedulerState` |

Conventions in `ent/schema`:
- User-owned and high-volume rows (`User`, `APIToken`, `Media`, `Checkin`, `CheckinSave`, `Comment`, `Activity`, `Notification`, `List`, `AIDraftSession`, `VenueMedia`, `InboundToken`) use `field.String("id")` with a default 16-byte `crypto/rand` hex generator. Session and the join/ledger tables (`Session`, `UserIdentity`, `EmailOutbox`, `LoginAttempt`, `CheckinLike`, `Follow`, `Friendship`, `Block`, `VenueSave`, `UserBadge`) use an auto-incrementing integer primary key.
- Shared catalog and administrative rows (`Brand`, `Style`, `Flavour`, `Drink`, `Venue`, `Badge`, `Report`, `AIEndpoint`) use `field.Int("id")` (`INTEGER PRIMARY KEY AUTOINCREMENT`).
- Enums use `field.Enum(...)` (which Ent enforces both in Go types and SQLite `CHECK` constraints).
- Timestamps use `field.Time("created_at").Default(time.Now).Immutable()` and `field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now)` stored as RFC3339 UTC.
- Edges define explicit foreign keys and `entsql.OnDelete(entsql.Cascade)` / `entsql.SetNull` / `entsql.Restrict` annotations.

### 1.2 Versioned Migration Registry (`internal/db/migrations.go` + `internal/db/migrations/`)
Ent's `Client.Schema.Create(ctx)` handles additive schema creation/evolution from `ent/migrate/schema.go`, while the registry in `internal/db/migrations.go` embeds and applies the ordered SQL files in `internal/db/migrations/` as transactional versioned migrations tracked in `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`.

Boot sequence in `internal/db.Init(dbPath)`:
1. Open `*sql.DB` (`modernc.org/sqlite`) with connection-level `_pragma` parameters (`busy_timeout(10000)`, `synchronous(NORMAL)`, `cache_size(-64000)`, `temp_store(MEMORY)`, `mmap_size`, `foreign_keys(ON)`) and DB-level `PRAGMA journal_mode=WAL`, `PRAGMA auto_vacuum=INCREMENTAL`.
2. Attach the Ent driver: `drv := entsql.OpenDB(dialect.SQLite, DB); Client = ent.NewClient(ent.Driver(drv))`.
3. Run pre-Ent data migrations (when a release renames/splits legacy columns before Ent inspects tables).
4. Run `Client.Schema.Create(ctx)` to materialize/upgrade all Ent-managed tables and foreign keys.
5. Run `migrations.Migrate(DB)` in one transaction per version to apply SQLite-specific DDL that Ent cannot express natively (FTS5 virtual tables, SQL views, partial unique indexes, triggers) and versioned data backfills:

| Version | Migration Name | Responsibility |
|---:|---|---|
| `0001` | `0001_auth_indexes.sql` | Case-insensitive unique indexes for `users.handle`/`users.email`, session expiry, API-token owner |
| `0002` | `0002_catalog_fts.sql` | `drinks_fts` FTS5 table and refresh triggers, one-primary-image index, drink status / venue geo / merged-venue indexes, outbox-due, active-check-in, followee, venue-save, like-by-user, unread-notification and open-report partial indexes |
| `0003` | `0003_checkin_ratings_view.sql` | `drink_ratings` SQL view (`checkin_count`, `rater_count`, `avg_rating`) |
| `0004` | `0004_seed_defaults.sql` | Core badge definitions, idempotent seed |
| `0005` | `0005_composite_indexes.sql` | Composite unique indexes for `venue_saves`, `venue_drinks`, `list_items` and `user_badges` |
| `0006` | `0006_comments.sql` | `comments` thread lookup and per-user hourly rate-limit indexes |
| `0007` | `0007_venue_media.sql` | `venue_media` gallery ordering and uploader/venue cap indexes |
| `0008` | `0008_delivery_outbox.sql` | `delivery_outbox` due-lookup and per-user history indexes |
| `0009` | `0009_audit_indexes.sql` | `catalog_revisions(entity, created_at)` and `admin_notes(entity, created_at)` audit indexes |
| `0010` | `0010_checkins_venue_index.sql` | `idx_checkins_venue_created` on `checkins(venue_id, created_at) WHERE deleted_at IS NULL` for the Nearby prefilter |

Migration `0010` is the next append-only step after the audit indexes. The AI tables remain Ent-managed and have no SQL migration; the v1.7 operations slice added no tables and no migrations, because its backups are standalone `.db` files under `$DATA_DIR/backups` outside SQLite. Migrations remain append-only, so any future schema change gets the next number.

### 1.3 Mechanical Schema Guard (`internal/db/db_test.go`)
Just like `dnd/db/schema_consistency_test.go`, `TestSchemaConsistency` initializes an in-memory database (`Init(":memory:")`) and asserts that every table in `sqlite_master` is either present in `ent/migrate.Tables` or listed in the raw-only allowlist (`schema_migrations` and `drinks_fts*`). Any ad-hoc raw `CREATE TABLE` outside Ent + the migration registry fails CI immediately.

---

The SQL DDL below shows the exact resulting SQLite schema produced by `Client.Schema.Create(ctx)` + `migrations.Migrate(DB)`.

## 2. Users and auth

`media` is created first because users and brands reference it; no row is written until every table exists, so the circular `users.avatar_media_id ↔ media.user_id` pair is safe.

```sql
CREATE TABLE media (
  id         TEXT PRIMARY KEY,
  user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
  kind       TEXT NOT NULL CHECK (kind IN ('checkin','avatar','drink','venue','brand','video')),
  path       TEXT NOT NULL,
  thumb_path TEXT NOT NULL DEFAULT '',
  poster_path TEXT NOT NULL DEFAULT '',
  mime       TEXT NOT NULL,
  duration_seconds INTEGER NOT NULL DEFAULT 0,
  width      INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, bytes INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE users (
  id                TEXT PRIMARY KEY,
  handle            TEXT NOT NULL UNIQUE,      -- case-insensitive via idx_users_handle_nocase
  email             TEXT NOT NULL UNIQUE,      -- case-insensitive via idx_users_email_nocase
  email_verified_at TEXT,
  password_hash     TEXT,                    -- NULL for OIDC-only accounts
  display_name      TEXT NOT NULL DEFAULT '',
  bio               TEXT NOT NULL DEFAULT '',
  avatar_media_id   TEXT REFERENCES media(id) ON DELETE SET NULL,
  role              TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user','moderator','admin')),
  status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','deleted')),
  default_visibility TEXT NOT NULL DEFAULT 'public' CHECK (default_visibility IN ('public','friends','private')),
  analytics_opt_out INTEGER NOT NULL DEFAULT 0 CHECK (analytics_opt_out IN (0,1)),
  country           TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  deleted_at        TEXT
);
-- migration 0001: case-insensitive identity lookups
CREATE UNIQUE INDEX idx_users_handle_nocase ON users(handle COLLATE NOCASE);
CREATE UNIQUE INDEX idx_users_email_nocase ON users(email COLLATE NOCASE);

CREATE TABLE user_identities (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  provider    TEXT NOT NULL,                  -- 'oidc', 'github'
  subject     TEXT NOT NULL,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email       TEXT,
  groups_json TEXT NOT NULL DEFAULT '[]',     -- OIDC groups claim snapshot (e.g. ["admins"])
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX useridentity_provider_subject ON user_identities(provider, subject);

CREATE TABLE sessions (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash   TEXT NOT NULL UNIQUE,         -- sha256 hex of cookie value
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token   TEXT NOT NULL,
  ip           TEXT,
  user_agent   TEXT,
  created_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at   TEXT NOT NULL
);
CREATE INDEX session_user_id ON sessions(user_id);
CREATE INDEX session_expires_at ON sessions(expires_at);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);  -- migration 0001

CREATE TABLE api_tokens (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL DEFAULT 'read',  -- comma list: read,write,feed
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at   TEXT
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);   -- migration 0001

CREATE TABLE email_verification_tokens (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash TEXT NOT NULL UNIQUE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE TABLE password_reset_tokens (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash TEXT NOT NULL UNIQUE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE TABLE email_outboxes (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  to_email        TEXT NOT NULL,
  template        TEXT NOT NULL,
  payload_json    TEXT NOT NULL DEFAULT '{}',
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  sent_at         TEXT,
  last_error      TEXT,
  created_at      TEXT NOT NULL
);
CREATE INDEX idx_outbox_due ON email_outboxes(next_attempt_at) WHERE sent_at IS NULL;

CREATE TABLE user_settings (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id           TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  email_friend_req  INTEGER NOT NULL DEFAULT 1 CHECK (email_friend_req IN (0,1)),
  email_weekly      INTEGER NOT NULL DEFAULT 0 CHECK (email_weekly IN (0,1)),
  push_enabled      INTEGER NOT NULL DEFAULT 0 CHECK (push_enabled IN (0,1)),
  theme             TEXT NOT NULL DEFAULT 'system' CHECK (theme IN ('system','light','dark')),
  public_profile    INTEGER NOT NULL DEFAULT 0 CHECK (public_profile IN (0,1)),
  units             TEXT NOT NULL DEFAULT 'metric' CHECK (units IN ('metric','imperial')),
  caffeine_bedtime  TEXT NOT NULL DEFAULT '',   -- 'HH:MM' local clock, '' when unset
  updated_at        TEXT NOT NULL
);

CREATE TABLE inbound_tokens (            -- machine write access, e.g. Home Assistant
  id           TEXT PRIMARY KEY,         -- 32-char hex
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL DEFAULT 'home_assistant' CHECK (kind IN ('home_assistant')),
  label        TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,     -- sha256 hex; plaintext shown once
  last_used_at TEXT,
  created_at   TEXT NOT NULL,
  revoked_at   TEXT
);
CREATE INDEX inboundtoken_user_id ON inbound_tokens(user_id);

CREATE TABLE integrations (              -- outbound delivery targets, one per user+kind
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL DEFAULT 'home_assistant' CHECK (kind IN ('home_assistant','matrix','trmnl')),
  target     TEXT NOT NULL DEFAULT '',   -- webhook URL, homeserver|room, or TRMNL URL
  secret     TEXT NOT NULL DEFAULT '',   -- Matrix access token; stored as supplied
  enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX integration_user_id_kind ON integrations(user_id, kind);

CREATE TABLE push_subscriptions (        -- browser Web Push registrations
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX pushsubscription_user_id ON push_subscriptions(user_id);

CREATE TABLE login_attempts (           -- lightweight throttle aid, pruned hourly
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  key          TEXT NOT NULL,           -- 'ip|identifier' or 'user:<id>'
  attempted_at TEXT NOT NULL
);
CREATE INDEX loginattempt_key_attempted_at ON login_attempts(key, attempted_at);
```

## 3. Catalog

Two-level taxonomy: `styles` (parent_id nullable) for drink families, `flavours` for taste tags with intensity at drink level.

`drinks.published` is the per-row opt-in for the unauthenticated fact sheet at `GET /api/v1/public/drinks/{slug}`. It is independent of `status`; the route requires `published=1` plus a non-merged `active`, `limited` or `seasonal` row. An admin toggles it with `POST /admin/drinks/{id}/publish`, which records a catalog revision.

```sql
CREATE TABLE brands (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  slug        TEXT NOT NULL UNIQUE,
  country     TEXT,
  website     TEXT,
  description TEXT NOT NULL DEFAULT '',
  logo_media_id TEXT REFERENCES media(id) ON DELETE SET NULL,
  source      TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local','off','user')),
  verified    INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0,1)),
  created_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE styles (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id   INTEGER REFERENCES styles(id) ON DELETE SET NULL,
  name        TEXT NOT NULL UNIQUE,
  slug        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE flavours (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  name     TEXT NOT NULL UNIQUE,   -- sweet, sour, bitter, fruity, herbal, creamy, spicy, floral
  slug     TEXT NOT NULL UNIQUE,
  category TEXT NOT NULL DEFAULT 'general'
);

CREATE TABLE drinks (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  brand_id        INTEGER REFERENCES brands(id) ON DELETE SET NULL,
  style_id        INTEGER REFERENCES styles(id) ON DELETE SET NULL,
  name            TEXT NOT NULL,
  slug            TEXT NOT NULL UNIQUE,
  description     TEXT NOT NULL DEFAULT '',
  image_url       TEXT,
  container       TEXT NOT NULL DEFAULT 'can' CHECK (container IN ('can','glass','pet','fountain','carton','other')),
  volume_ml       INTEGER NOT NULL DEFAULT 355,
  caffeine_mg_per_100ml INTEGER NOT NULL DEFAULT 0,
  sugar_g_per_100ml     REAL NOT NULL DEFAULT 0,
  calories_kcal_per_100ml INTEGER NOT NULL DEFAULT 0,
  sweetener       TEXT NOT NULL DEFAULT 'unknown' CHECK (sweetener IN ('sugar','hfcs','aspartame','sucralose','acesulfame_k','saccharin','stevia','monk_fruit','allulose','erythritol','mixed','none','unknown')),
  carbonation     TEXT NOT NULL DEFAULT 'unknown' CHECK (carbonation IN ('still','light','standard','extra','unknown')),
  acid            TEXT NOT NULL DEFAULT 'unknown' CHECK (acid IN ('phosphoric','citric','malic','tartaric','none','unknown')),
  kind            TEXT NOT NULL DEFAULT 'packaged' CHECK (kind IN ('packaged','mix')),
  recipe_json     TEXT NOT NULL DEFAULT '{}',  -- Mix Lab components for kind = 'mix'
  source          TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local','off','user')),
  status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('provisional','active','merged','limited','seasonal','discontinued')),
  published       INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0,1)),  -- opt-in public fact sheet
  discontinued_at TEXT,                        -- RFC 3339, set with status = 'discontinued'
  limited_until   TEXT,                        -- RFC 3339, only for limited/seasonal
  created_by      TEXT REFERENCES users(id) ON DELETE SET NULL,
  merged_into_id  INTEGER REFERENCES drinks(id) ON DELETE SET NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);
CREATE INDEX drink_name ON drinks(name);
CREATE INDEX drink_brand_id ON drinks(brand_id);
CREATE INDEX drink_style_id ON drinks(style_id);
CREATE INDEX idx_drinks_status ON drinks(status);  -- migration 0002

CREATE TABLE drink_barcodes (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id    INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  gtin14      TEXT NOT NULL UNIQUE,          -- canonical 14-digit GTIN
  raw_code    TEXT NOT NULL DEFAULT '',      -- what the scanner read
  symbology   TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('scan','off','user','brand')),
  created_at  TEXT NOT NULL
);

CREATE TABLE drink_flavours (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id   INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  flavour_id INTEGER NOT NULL REFERENCES flavours(id) ON DELETE CASCADE,
  intensity  INTEGER NOT NULL DEFAULT 3 CHECK (intensity BETWEEN 0 AND 5)
);
CREATE UNIQUE INDEX drinkflavour_drink_id_flavour_id ON drink_flavours(drink_id, flavour_id);

CREATE TABLE drink_alias (              -- alternate names, localisations, "Coke" → Coca-Cola
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  alias    TEXT NOT NULL
);

CREATE TABLE drink_images (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id    INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,
  is_primary  INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0,1)),
  attribution TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_drink_images_primary ON drink_images(drink_id) WHERE is_primary = 1;

-- OFF rows stay isolated: never joined into user-authored edits, never mixed into drinks columns.
-- Attribution and ODbL notice are rendered wherever this data is shown (see architecture.md §6).
CREATE TABLE off_products (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  gtin14          TEXT NOT NULL UNIQUE,
  payload_json    TEXT NOT NULL DEFAULT '{}',
  product_name    TEXT NOT NULL DEFAULT '',
  brands          TEXT NOT NULL DEFAULT '',
  quantity        TEXT NOT NULL DEFAULT '',
  nutriments_json TEXT NOT NULL DEFAULT '{}',
  image_front_url TEXT,
  license         TEXT NOT NULL DEFAULT 'ODbL-1.0',
  fetched_at      TEXT NOT NULL
);

CREATE TABLE catalog_revisions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  entity      TEXT NOT NULL DEFAULT 'drink' CHECK (entity IN ('drink','brand','style','venue')),
  entity_id   INTEGER NOT NULL,
  user_id     TEXT REFERENCES users(id) ON DELETE SET NULL,
  action      TEXT NOT NULL DEFAULT 'update',
  diff_json   TEXT NOT NULL DEFAULT '{}',
  created_at  TEXT NOT NULL
);
CREATE INDEX idx_catalog_revisions_entity_created ON catalog_revisions(entity, created_at);  -- migration 0009

-- Search: standalone FTS5, rowid mirrors drinks.id, refreshed in the same tx as writes
CREATE VIRTUAL TABLE drinks_fts USING fts5(
  name, brand, style, aliases,
  tokenize = 'unicode61 remove_diacritics 2'
);
```

`drink_alias` is the alias table used by FTS indexing and lookup; there is no separate alias relation. Alias writes refresh the FTS row in the same transaction.

Seed data (in `cmd/seed`, not a migration): styles such as cola, diet cola, lemon-lime, root beer, cream soda, ginger ale, ginger beer, orange, grape, pineapple, fruit punch, tonic, seltzer/sparkling water, energy, iced tea, kombucha, malta, other; flavours as listed; a handful of drinks for e2e only.

## 4. Venues

```sql
CREATE TABLE venues (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  slug        TEXT NOT NULL UNIQUE,
  kind        TEXT NOT NULL DEFAULT 'other' CHECK (kind IN ('bar','restaurant','cafe','shop','hotel','festival','vending','other')),
  address     TEXT NOT NULL DEFAULT '',
  city        TEXT NOT NULL DEFAULT '',
  region      TEXT NOT NULL DEFAULT '',
  country     TEXT NOT NULL DEFAULT '',
  lat         REAL NOT NULL DEFAULT 0,
  lng         REAL NOT NULL DEFAULT 0,
  website     TEXT,
  created_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
  merged_into_id INTEGER REFERENCES venues(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX idx_venues_geo ON venues(lat, lng);                                    -- migration 0002
CREATE INDEX idx_venues_merged ON venues(merged_into_id) WHERE merged_into_id IS NOT NULL;  -- migration 0002

CREATE TABLE venue_drinks (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  venue_id     INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
  drink_id     INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  last_seen_at TEXT NOT NULL,
  on_offer     INTEGER NOT NULL DEFAULT 0 CHECK (on_offer IN (0,1))
);
CREATE UNIQUE INDEX idx_venue_drinks_unique ON venue_drinks(venue_id, drink_id);   -- migration 0005

CREATE TABLE venue_names (              -- alternate/legacy names for dedupe
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  name     TEXT NOT NULL,
  venue_id INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE
);

CREATE TABLE venue_saves (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  venue_id   INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_venue_saves_unique ON venue_saves(user_id, venue_id);       -- migration 0005
CREATE INDEX idx_venue_saves_user ON venue_saves(user_id);                          -- migration 0002

CREATE TABLE venue_media (               -- user-contributed photo or menu image
  id          TEXT PRIMARY KEY,          -- 32-char hex
  venue_id    INTEGER NOT NULL,
  media_id    TEXT NOT NULL,
  kind        TEXT NOT NULL DEFAULT 'photo' CHECK (kind IN ('photo','menu')),
  label       TEXT NOT NULL DEFAULT '',  -- optional caption, <= 80 runes
  uploaded_by TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  deleted_at  TEXT
);
CREATE INDEX idx_venue_media_venue_created ON venue_media(venue_id, created_at);
CREATE INDEX idx_venue_media_uploader_venue ON venue_media(uploaded_by, venue_id);
```

`venue_media` is the attachment row; `media` owns the bytes. Active rows are public and follow the venue; removal is a soft delete that revokes media access while keeping the file. Caps are 6 active rows per uploader per venue and 20 per venue, enforced in the service. A deleted or merged venue stops serving its media.

Venue dedupe: exact normalized name + city wins; otherwise within ~150 m and same kind prompts "is this the same place?" on the create flow. A venue is never deleted once check-ins reference it — only merged via `venues.merged_into_id`, which redirects reads and returns 409 + canonical ID on write.

## 5. Check-ins

Rating stored as integer quarter-steps 0–20 (0.00–5.00) to avoid float drift; a NULL rating means "not rated".

```sql
CREATE TABLE checkins (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE NO ACTION,
  drink_id     INTEGER NOT NULL REFERENCES drinks(id) ON DELETE NO ACTION,
  venue_id     INTEGER REFERENCES venues(id) ON DELETE SET NULL,
  rating       INTEGER CHECK (rating BETWEEN 0 AND 20),
  serving      TEXT NOT NULL DEFAULT 'can' CHECK (serving IN ('can','bottle','glass','fountain','other')),
  note         TEXT NOT NULL DEFAULT '',
  photo_media_id TEXT REFERENCES media(id) ON DELETE SET NULL,
  visibility   TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','friends','private')),
  idempotency_key TEXT,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  deleted_at   TEXT
);
CREATE INDEX checkin_user_id_created_at ON checkins(user_id, created_at);
CREATE INDEX checkin_drink_id ON checkins(drink_id);
CREATE UNIQUE INDEX checkin_idempotency_key ON checkins(idempotency_key);
CREATE INDEX idx_checkins_active ON checkins(user_id, created_at) WHERE deleted_at IS NULL;  -- migration 0002
CREATE INDEX idx_checkins_venue_created ON checkins(venue_id, created_at) WHERE deleted_at IS NULL; -- migration 0010, Nearby

CREATE VIEW drink_ratings AS
SELECT drink_id,
       COUNT(*)                                   AS checkin_count,
       COUNT(DISTINCT user_id)                    AS rater_count,
       AVG(rating)                                AS avg_rating
FROM checkins
WHERE rating IS NOT NULL AND deleted_at IS NULL
GROUP BY drink_id;
```

Rules enforced in the service layer: a check-in photo must belong to the uploader; private check-ins never join feed queries; deleting a check-in is soft-delete and removes its activity row in the same tx.

### Comments

```sql
CREATE TABLE comments (                  -- flat, user-owned, soft-deleted
  id         TEXT PRIMARY KEY,           -- 32-char hex
  checkin_id TEXT NOT NULL,
  user_id    TEXT NOT NULL,
  body       TEXT NOT NULL,              -- 1..1000 runes, enforced in the service
  created_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE INDEX idx_comments_checkin_created ON comments(checkin_id, created_at);
CREATE INDEX idx_comments_user_created ON comments(user_id, created_at);
```

Comments have no threading columns and no Ent edges, so the service enforces the parent check-in and author lookups. Reads call `checkins.Get` first, which makes comment visibility identical to the parent check-in; rows from deleted users and blocked pairs are excluded in SQL. Reads are keyset-paginated oldest-first (`before` cursor, 50 default, 100 maximum). Writes are capped at 30 per user per rolling hour; there is no per-thread row cap, so older comments remain readable across pages. `@handle` mentions link to `/u/{handle}` and produce a best-effort `mention` notification unless the target is the commenter, the check-in owner, blocked with the commenter, or unable to see the parent check-in. Deleting a comment sets `deleted_at`; the row is retained for moderation history.

### Check-in saves (bookmarks)

```sql
CREATE TABLE checkin_saves (              -- private bookmark of one check-in
  id         TEXT PRIMARY KEY,            -- 32-char hex
  checkin_id TEXT NOT NULL,
  user_id    TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX checkinsave_checkin_id_user_id
  ON checkin_saves(checkin_id, user_id);
CREATE INDEX checkinsave_user_id_created_at
  ON checkin_saves(user_id, created_at);
```

A save is owned by one user and is idempotent on `(checkin_id, user_id)`; saving your own check-in is allowed. `checkins.Get` guards every save, and the saved list re-checks visibility per row, so a bookmark never widens access or keeps a deleted/private check-in visible. The `/saved` page and `GET /api/v1/me/saved` page by `(created_at DESC, id DESC)`.

## 6. Social

```sql
CREATE TABLE follows (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  follower_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX follow_follower_id_followee_id ON follows(follower_id, followee_id);
CREATE INDEX idx_follows_followee ON follows(followee_id);  -- migration 0002

CREATE TABLE friendships (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_a       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_b       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
  requested_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE UNIQUE INDEX friendship_user_a_user_b ON friendships(user_a, user_b);

CREATE TABLE blocks (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX block_blocker_id_blocked_id ON blocks(blocker_id, blocked_id);

CREATE TABLE checkin_likes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  checkin_id TEXT NOT NULL REFERENCES checkins(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX checkinlike_checkin_id_user_id ON checkin_likes(checkin_id, user_id);
CREATE INDEX idx_checkin_likes_user ON checkin_likes(user_id);  -- migration 0002

CREATE TABLE activities (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL DEFAULT 'checkin' CHECK (kind IN ('checkin','badge','list','venue','friend')),
  subject_id   TEXT NOT NULL DEFAULT '',
  visibility   TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','friends','private')),
  created_at   TEXT NOT NULL
);
CREATE INDEX activity_user_id_created_at ON activities(user_id, created_at);
CREATE INDEX activity_created_at ON activities(created_at);

CREATE TABLE notifications (
  id         TEXT PRIMARY KEY,       -- 32-char hex
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL DEFAULT 'system' CHECK (kind IN ('friend_request','friend_accept','toast','comment','mention','badge','system')),
  actor_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
  subject_id TEXT NOT NULL DEFAULT '',
  text       TEXT NOT NULL DEFAULT '',
  read_at    TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX notification_user_id_created_at ON notifications(user_id, created_at);
CREATE INDEX idx_notifications_unread ON notifications(user_id, created_at DESC) WHERE read_at IS NULL;  -- migration 0002
```

Feed read is `activities JOIN follows ON followee_id = activities.user_id WHERE follower_id = ?` union own rows, filtered by visibility and blocks, ordered `(created_at DESC, id DESC)`, keyset paginated. Blocking is checked on read and on write.

## 7. Badges and lists

```sql
CREATE TABLE badges (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  slug        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind        TEXT NOT NULL DEFAULT 'core' CHECK (kind IN ('core','local','special')),
  rule        TEXT NOT NULL DEFAULT '',   -- evaluator counter + threshold, e.g. 'checkins>=10'
  created_at  TEXT NOT NULL
);

CREATE TABLE user_badges (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  badge_id   INTEGER NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
  level      INTEGER NOT NULL DEFAULT 1,
  awarded_at TEXT NOT NULL
);
CREATE UNIQUE INDEX userbadge_user_id_badge_id ON user_badges(user_id, badge_id);
CREATE UNIQUE INDEX idx_user_badges_unique ON user_badges(user_id, badge_id);  -- migration 0005

CREATE TABLE lists (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  slug        TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  visibility  TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','friends','private')),
  is_system   INTEGER NOT NULL DEFAULT 0 CHECK (is_system IN (0,1)),
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE list_items (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  list_id  TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  drink_id INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  note     TEXT NOT NULL DEFAULT '',
  added_at TEXT NOT NULL
);
CREATE UNIQUE INDEX listitem_list_id_drink_id ON list_items(list_id, drink_id);
CREATE UNIQUE INDEX idx_list_items_unique ON list_items(list_id, drink_id);  -- migration 0005
```

Badge evaluation runs after each check-in inside the same transaction: count thresholds, first-of-style, distinct-brand/country counts, venue-kind counts, streaks by local date. Awards insert `user_badges` + an `activities` row idempotently (`INSERT OR IGNORE`).

## 8. Moderation

```sql
CREATE TABLE reports (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  entity     TEXT NOT NULL CHECK (entity IN ('drink','brand','venue','checkin','user','comment','venue_media','drink_image')),
  entity_id  INTEGER NOT NULL,              -- integer target id (0 for text-keyed rows)
  entity_ref TEXT NOT NULL DEFAULT '',      -- string target id for comments/venue_media
  user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
  reason     TEXT NOT NULL DEFAULT 'other',
  details    TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','dismissed')),
  created_at TEXT NOT NULL,
  resolved_at TEXT
);
CREATE INDEX report_status_created_at ON reports(status, created_at);
CREATE INDEX idx_reports_open ON reports(status, created_at) WHERE status = 'open';  -- migration 0002

CREATE TABLE admin_notes (              -- internal notes and runtime-setting audit
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  author_id  TEXT NOT NULL,
  entity     TEXT NOT NULL DEFAULT 'system' CHECK (entity IN ('drink','brand','venue','checkin','user','system')),
  entity_id  INTEGER NOT NULL DEFAULT 0,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_admin_notes_entity_created ON admin_notes(entity, created_at);
```

Report notes reuse `admin_notes` with `entity='system'` and the report id as `entity_id`; they are admin-only and removed with a hard delete. `catalog_revisions` remains the audit trail for catalog writes, and migration `0009_audit_indexes.sql` adds the `(entity, created_at)` indexes used by the admin queue and revision feed.

## 9. Runtime settings

`runtime_settings` overlays audited operational values on env config. Precedence is built-in default < env < database. The row stores only allowlisted keys; env-only keys (`ENV`, `PORT`, `DATABASE_PATH`, `DATA_DIR`, `SESSION_SECRET`, `OTEL_*`, `EMAIL_DEV_DIR`, `SEED_DEMO`) are rejected by the settings service before insert.

```sql
CREATE TABLE runtime_settings (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  key             TEXT NOT NULL UNIQUE,
  value           TEXT NOT NULL DEFAULT '',
  encrypted_value TEXT NOT NULL DEFAULT '', -- AES-256-GCM ciphertext for secrets
  is_secret       INTEGER NOT NULL DEFAULT 0,
  updated_by      TEXT,
  updated_at      TEXT NOT NULL
);
```

Non-secret values live in `value`; secrets (currently the SMTP password) live only as `enc:v1:` ciphertext in `encrypted_value` under a key derived from `SESSION_SECRET`. A blank secret field preserves the stored value and an explicit clear removes it. Every write appends an `admin_notes` audit row with key names only. The table is read once at boot into an immutable snapshot, so request paths never query it.

## 10. OpenCode ("opencode2") AI

Matches `dnd` (`ai_endpoints`, `app_settings`, and conversational drafting sessions). These tables are Ent-managed; no SQL migration file is involved:

```sql
CREATE TABLE app_settings (
  key        TEXT PRIMARY KEY,              -- e.g. 'ai_enabled' ('1'|'0')
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE ai_endpoints (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  name              TEXT NOT NULL UNIQUE,
  type              TEXT NOT NULL DEFAULT 'text' CHECK (type IN ('text','image','vision')),
  base_url          TEXT NOT NULL,          -- normalized via NormalizeAIBaseURL (e.g. https://opencode.ai/zen/go/v1)
  encrypted_api_key TEXT NOT NULL DEFAULT '', -- AES-GCM encrypted using SHA-256('sodapop-ai-key:' || SESSION_SECRET)
  model             TEXT NOT NULL,
  tags              TEXT NOT NULL DEFAULT '[]', -- JSON array of strings, e.g. ["draft","tasting_note","recommend","default"]
  enabled           INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  temperature       REAL,
  max_tokens        INTEGER,
  image_size        TEXT NOT NULL DEFAULT '',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);

CREATE TABLE ai_draft_sessions (
  id            TEXT PRIMARY KEY,           -- 32-char hex; reused as x-opencode-session header
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL DEFAULT 'drink' CHECK (kind IN ('drink','tasting_note','venue')),
  messages_json TEXT NOT NULL DEFAULT '[]', -- conversation turns
  draft_json    TEXT NOT NULL DEFAULT '{}', -- latest structured DrinkDraft / VenueDraft JSON
  status        TEXT NOT NULL DEFAULT 'chatting' CHECK (status IN ('chatting','ready','committed')),
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
```

## 11. Integrity rules that live in code, not DDL

- Barcode GTIN checksum validated before insert; normalize UPC-A to EAN-13 by zero-prefixing, then store as GTIN-14 with leading zeros where the source is 12/13 digits.
- One primary image per drink enforced by `idx_drink_images_primary`; changing the primary is a single transaction that clears the old flag before setting the new one.
- `drink_alias` feeds the FTS `aliases` column; alias writes refresh the FTS row in the same transaction.
- `drinks.merged_into_id` redirects reads; all writes to a merged drink are rejected with 409 and the canonical ID.
- `drinks.published` defaults to false and gates only the public fact-sheet route. A published row still needs a non-merged active, limited or seasonal status to be visible there.
- Deleting a user: `status='deleted'`, `deleted_at` stamped, sessions removed, API and inbound tokens revoked in one transaction; check-ins stay with `user_id` intact and display "deleted user". The last live admin is refused. There is no purge job and the handle remains reserved.
- Free-text notes capped at 2000 chars, `details` at 1000; all rendered escaped (no HTML).
- Every catalog write also inserts `catalog_revisions` in the same transaction.
