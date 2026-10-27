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
| **Integrations** | `Integration`, `PushSubscription`, `InboundToken` |
| **Settings & AI** | `AppSetting`, `RuntimeSetting`, `AIEndpoint`, `AIDraftSession` |

Conventions in `ent/schema`:
- User-owned and high-volume rows (`User`, `Session`, `APIToken`, `Media`, `Checkin`, `List`, `AIDraftSession`) use `field.String("id")` with a default 16-byte `crypto/rand` hex generator.
- Shared catalog and administrative rows (`Brand`, `Style`, `Flavour`, `Drink`, `Venue`, `Badge`, `Report`, `AIEndpoint`) use `field.Int("id")` (`INTEGER PRIMARY KEY AUTOINCREMENT`).
- Enums use `field.Enum(...)` (which Ent enforces both in Go types and SQLite `CHECK` constraints).
- Timestamps use `field.Time("created_at").Default(time.Now).Immutable()` and `field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now)` stored as RFC3339 UTC.
- Edges define explicit foreign keys and `entsql.OnDelete(entsql.Cascade)` / `entsql.SetNull` / `entsql.Restrict` annotations.

### 1.2 Versioned Migration Registry (`internal/db/migrations/`)
Ent's `Client.Schema.Create(ctx)` handles additive schema creation/evolution from `ent/migrate/schema.go`, while `internal/db/migrations.go` applies ordered, transactional versioned migrations tracked in `schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`.

Boot sequence in `internal/db.Init(dbPath)`:
1. Open `*sql.DB` (`modernc.org/sqlite`) with connection-level `_pragma` parameters (`busy_timeout(10000)`, `synchronous(NORMAL)`, `cache_size(-64000)`, `temp_store(MEMORY)`, `mmap_size`, `foreign_keys(ON)`) and DB-level `PRAGMA journal_mode=WAL`, `PRAGMA auto_vacuum=INCREMENTAL`.
2. Attach the Ent driver: `drv := entsql.OpenDB(dialect.SQLite, DB); Client = ent.NewClient(ent.Driver(drv))`.
3. Run pre-Ent data migrations (when a release renames/splits legacy columns before Ent inspects tables).
4. Run `Client.Schema.Create(ctx)` to materialize/upgrade all Ent-managed tables and foreign keys.
5. Run `migrations.Migrate(DB)` in one transaction per version to apply SQLite-specific DDL that Ent cannot express natively (FTS5 virtual tables, SQL views, partial unique indexes, triggers) and versioned data backfills:

| Version | Migration Name | Responsibility |
|---:|---|---|
| `0001` | `0001_auth_indexes.sql` | Case-insensitive unique indexes for `users.handle`/`users.email`, session expiry, API-token owner |
| `0002` | `0002_catalog_fts.sql` | `drinks_fts` FTS5 table, one-primary-image index, drink status and venue geo indexes, unread-notification and open-report partial indexes |
| `0003` | `0003_checkin_ratings_view.sql` | `drink_ratings` SQL view (`checkin_count`, `rater_count`, `avg_rating`) |
| `0004` | `0004_seed_defaults.sql` | Core badge definitions, idempotent seed |
| `0005` | `0005_composite_indexes.sql` | Composite unique indexes for `venue_saves`, `venue_drinks`, `list_items` and `user_badges` |
| `0006` | `0006_comments.sql` | `comments` thread lookup and per-user hourly rate-limit indexes |
| `0007` | `0007_venue_media.sql` | `venue_media` gallery ordering and uploader/venue cap indexes |
| `0008` | `0008_delivery_outbox.sql` | `delivery_outbox` due-lookup and per-user history indexes |
| `0009` | `0009_audit_indexes.sql` | `catalog_revisions(entity, created_at)` and `admin_notes(entity, created_at)` audit indexes |

There is no `0010` migration: the AI tables are Ent-managed, and no `0010_ai.sql` file exists.

### 1.3 Mechanical Schema Guard (`internal/db/schema_consistency_test.go`)
Just like `dnd/db/schema_consistency_test.go`, `TestSchemaConsistency` initializes an in-memory database (`Init(":memory:")`) and asserts that every table in `sqlite_master` is either present in `ent/migrate.Tables` or listed in `rawOnlyTables` (`schema_migrations` and `drinks_fts*`). Any ad-hoc raw `CREATE TABLE` outside Ent + the migration registry fails CI immediately.

---

The SQL DDL below shows the exact resulting SQLite schema produced by `Client.Schema.Create(ctx)` + `migrations.Migrate(DB)`.

## 2. Users and auth

`media` is created first because users, brands, drinks and venues all reference it; no row is written until every table exists, so the circular `users.avatar_media_id ↔ media.user_id` pair is safe.

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
  width      INTEGER, height INTEGER, bytes INTEGER,
  created_at TEXT NOT NULL
);

CREATE TABLE users (
  id                TEXT PRIMARY KEY,
  handle            TEXT NOT NULL UNIQUE COLLATE NOCASE,
  email             TEXT NOT NULL UNIQUE COLLATE NOCASE,
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
CREATE INDEX idx_users_status ON users(status);

CREATE TABLE user_identities (
  provider    TEXT NOT NULL,                  -- 'oidc', 'github'
  subject     TEXT NOT NULL,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email       TEXT,
  groups_json TEXT NOT NULL DEFAULT '[]',     -- OIDC groups claim snapshot (e.g. ["admins"])
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  PRIMARY KEY (provider, subject)
);

CREATE TABLE sessions (
  token_hash   TEXT PRIMARY KEY,             -- sha256 hex of cookie value
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token   TEXT NOT NULL,
  ip           TEXT,
  user_agent   TEXT,
  created_at   TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at   TEXT NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE api_tokens (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL,                -- comma list: read,write,feed
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at   TEXT
);

CREATE TABLE email_verification_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE TABLE password_reset_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT
);

CREATE TABLE email_outbox (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  to_email        TEXT NOT NULL,
  template        TEXT NOT NULL,
  payload_json    TEXT NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  sent_at         TEXT,
  last_error      TEXT,
  created_at      TEXT NOT NULL
);
CREATE INDEX idx_outbox_due ON email_outbox(next_attempt_at) WHERE sent_at IS NULL;

CREATE TABLE user_settings (
  user_id           TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
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
CREATE INDEX idx_inbound_tokens_user ON inbound_tokens(user_id);

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
CREATE UNIQUE INDEX idx_integrations_user_kind ON integrations(user_id, kind);

CREATE TABLE push_subscriptions (        -- browser Web Push registrations
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_push_subscriptions_user ON push_subscriptions(user_id);

CREATE TABLE login_attempts (           -- lightweight throttle aid, pruned hourly
  key         TEXT NOT NULL,            -- 'ip|identifier' or 'user:<id>'
  attempted_at TEXT NOT NULL
);
CREATE INDEX idx_login_attempts_key ON login_attempts(key, attempted_at);
```

## 3. Catalog

Two-level taxonomy: `styles` (parent_id nullable) for drink families, `flavours` for taste tags with intensity at drink level.

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
CREATE UNIQUE INDEX idx_brands_name_country ON brands(name COLLATE NOCASE, COALESCE(country,''));

CREATE TABLE styles (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id   INTEGER REFERENCES styles(id) ON DELETE SET NULL,
  name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
  slug        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  sort_order  INTEGER NOT NULL DEFAULT 100
);

CREATE TABLE flavours (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,   -- sweet, sour, bitter, fruity, herbal, creamy, spicy, floral
  slug TEXT NOT NULL UNIQUE
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
  discontinued_at TEXT,                        -- RFC 3339, set with status = 'discontinued'
  limited_until   TEXT,                        -- RFC 3339, only for limited/seasonal
  created_by      TEXT REFERENCES users(id) ON DELETE SET NULL,
  merged_into_id  INTEGER REFERENCES drinks(id) ON DELETE SET NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);
CREATE INDEX idx_drinks_brand ON drinks(brand_id);
CREATE INDEX idx_drinks_style ON drinks(style_id);
CREATE INDEX idx_drinks_status ON drinks(status);
CREATE INDEX idx_drinks_merged ON drinks(merged_into_id) WHERE merged_into_id IS NOT NULL;

CREATE TABLE drink_barcodes (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id    INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  gtin14      TEXT NOT NULL UNIQUE,          -- canonical 14-digit GTIN
  raw_code    TEXT NOT NULL DEFAULT '',      -- what the scanner read
  symbology   TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('scan','off','user','brand')),
  created_at  TEXT NOT NULL
);
CREATE INDEX idx_barcodes_drink ON drink_barcodes(drink_id);

CREATE TABLE drink_flavours (
  drink_id   INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  flavour_id INTEGER NOT NULL REFERENCES flavours(id) ON DELETE CASCADE,
  intensity  INTEGER NOT NULL DEFAULT 2 CHECK (intensity BETWEEN 1 AND 5),
  PRIMARY KEY (drink_id, flavour_id)
);

CREATE TABLE drink_aliases (           -- alternate names, localisations, "Coke" → Coca-Cola
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  alias    TEXT NOT NULL
);

CREATE TABLE drink_images (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  drink_id   INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  media_id   TEXT NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0,1)),
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_drink_images_primary ON drink_images(drink_id) WHERE is_primary = 1;

-- OFF rows stay isolated: never joined into user-authored edits, never mixed into drinks columns.
-- Attribution and ODbL notice are rendered wherever this data is shown (see architecture.md §6).
CREATE TABLE off_products (
  barcode         TEXT PRIMARY KEY,
  payload_json    TEXT NOT NULL,
  product_name    TEXT,
  brands          TEXT,
  quantity        TEXT,
  nutriments_json TEXT,
  image_front_url TEXT,
  license         TEXT NOT NULL DEFAULT 'ODbL-1.0',
  last_synced_at  TEXT NOT NULL
);

CREATE TABLE catalog_revisions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  entity      TEXT NOT NULL CHECK (entity IN ('drink','brand','barcode','venue')),
  entity_id   TEXT NOT NULL,
  action      TEXT NOT NULL CHECK (action IN ('create','update','merge','verify','delete')),
  diff_json   TEXT NOT NULL,
  actor_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL
);
CREATE INDEX idx_revisions_entity ON catalog_revisions(entity, entity_id, created_at);

-- Search: standalone FTS5, rowid mirrors drinks.id, refreshed in the same tx as writes
CREATE VIRTUAL TABLE drinks_fts USING fts5(
  name, brand, style, aliases,
  tokenize = 'unicode61 remove_diacritics 2'
);
```

`drink_aliases` is the alias table used by FTS indexing and lookup; there is no separate alias relation. Alias writes refresh the FTS row in the same transaction.

Seed data (in `cmd/seed`, not a migration): styles such as cola, diet cola, lemon-lime, root beer, cream soda, ginger ale, ginger beer, orange, grape, pineapple, fruit punch, tonic, seltzer/sparkling water, energy, iced tea, kombucha, malta, other; flavours as listed; a handful of drinks for e2e only.

## 4. Venues

```sql
CREATE TABLE venues (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  slug        TEXT NOT NULL UNIQUE,
  kind        TEXT NOT NULL CHECK (kind IN ('hotel','bar','restaurant','cafe','shop','festival','vending','other')),
  description TEXT NOT NULL DEFAULT '',
  address_line1 TEXT, address_line2 TEXT, city TEXT, region TEXT, postal_code TEXT,
  country     TEXT,
  latitude    REAL, longitude REAL,
  website     TEXT,
  photo_media_id TEXT REFERENCES media(id) ON DELETE SET NULL,
  source      TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user','admin','osm')),
  verified    INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0,1)),
  merged_into_id INTEGER REFERENCES venues(id) ON DELETE SET NULL,
  created_by  TEXT REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX idx_venues_kind_city ON venues(kind, city);
CREATE INDEX idx_venues_geo ON venues(latitude, longitude);
CREATE INDEX idx_venues_merged ON venues(merged_into_id) WHERE merged_into_id IS NOT NULL;

CREATE TABLE venue_drinks (
  venue_id     INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
  drink_id     INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'seen' CHECK (status IN ('on_offer','seen','removed')),
  last_seen_at TEXT NOT NULL,
  added_by     TEXT REFERENCES users(id) ON DELETE SET NULL,
  PRIMARY KEY (venue_id, drink_id)
);

CREATE TABLE venue_names (              -- alternate/legacy names for dedupe
  venue_id INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
  name     TEXT NOT NULL COLLATE NOCASE,
  PRIMARY KEY (venue_id, name)
);

CREATE TABLE venue_saves (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  venue_id   INTEGER NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY (user_id, venue_id)
);

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

Rating stored as integer quarter-steps 0–20 (0.00–5.00) to avoid float drift; `0` means "no rating".

```sql
CREATE TABLE checkins (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  drink_id     INTEGER NOT NULL REFERENCES drinks(id) ON DELETE RESTRICT,
  venue_id     INTEGER REFERENCES venues(id) ON DELETE SET NULL,
  rating       INTEGER NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 20),
  serving      TEXT CHECK (serving IN ('can','glass_bottle','plastic_bottle','fountain','carton','other')),
  volume_ml    INTEGER,
  price_cents  INTEGER,
  currency     TEXT,
  notes        TEXT NOT NULL DEFAULT '',
  photo_media_id TEXT REFERENCES media(id) ON DELETE SET NULL,
  visibility   TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','friends','private')),
  source       TEXT NOT NULL DEFAULT 'web' CHECK (source IN ('web','android','api','import')),
  idempotency_key TEXT UNIQUE,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  deleted_at   TEXT
);
CREATE INDEX idx_checkins_user_time ON checkins(user_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_checkins_drink_time ON checkins(drink_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_checkins_venue_time ON checkins(venue_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE VIEW drink_ratings AS
SELECT drink_id,
       COUNT(*)                                   AS checkin_count,
       COUNT(DISTINCT user_id)                    AS rater_count,
       ROUND(AVG(NULLIF(rating,0)) / 4.0, 2)      AS avg_rating
FROM checkins
WHERE deleted_at IS NULL AND rating > 0
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
  follower_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL,
  PRIMARY KEY (follower_id, followee_id),
  CHECK (follower_id <> followee_id)
);

CREATE TABLE friendships (
  user_a      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_b      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      TEXT NOT NULL CHECK (status IN ('pending','accepted','declined')),
  requested_by TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  PRIMARY KEY (user_a, user_b),
  CHECK (user_a < user_b)
);

CREATE TABLE blocks (
  blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY (blocker_id, blocked_id)
);

CREATE TABLE checkin_likes (
  checkin_id TEXT NOT NULL REFERENCES checkins(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY (checkin_id, user_id)
);
CREATE INDEX idx_checkin_likes_checkin ON checkin_likes(checkin_id);

CREATE TABLE activities (  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL CHECK (kind IN ('checkin','badge','friend','venue_checkin','list','drink_added')),
  subject_type TEXT NOT NULL,
  subject_id   TEXT NOT NULL,
  visibility   TEXT NOT NULL CHECK (visibility IN ('public','friends','private')),
  created_at   TEXT NOT NULL
);
CREATE INDEX idx_activities_user_time ON activities(user_id, created_at DESC, id DESC);
CREATE INDEX idx_activities_time ON activities(created_at DESC, id DESC);

CREATE TABLE notifications (
  id         TEXT PRIMARY KEY,       -- 32-char hex
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,          -- friend_request, friend_accept, toast, badge, comment, mention, system
  actor_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
  subject_id TEXT NOT NULL DEFAULT '',
  text       TEXT NOT NULL DEFAULT '',
  read_at    TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_notifications_unread ON notifications(user_id, created_at DESC) WHERE read_at IS NULL;
```

Feed read is `activities JOIN follows ON followee_id = activities.user_id WHERE follower_id = ?` union own rows, filtered by visibility and blocks, ordered `(created_at DESC, id DESC)`, keyset paginated. Blocking is checked on read and on write.

## 7. Badges and lists

```sql
CREATE TABLE badges (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  code        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('count','style','brand','country','venue','streak','social','special')),
  threshold   INTEGER,
  style_id    INTEGER REFERENCES styles(id) ON DELETE SET NULL,
  country     TEXT,
  active      INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0,1))
);

CREATE TABLE user_badges (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  badge_id   INTEGER NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
  level      INTEGER NOT NULL DEFAULT 1,
  earned_at  TEXT NOT NULL,
  checkin_id TEXT REFERENCES checkins(id) ON DELETE SET NULL,
  PRIMARY KEY (user_id, badge_id)
);

CREATE TABLE lists (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  slug        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  visibility  TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public','friends','private')),
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  UNIQUE (user_id, slug)
);

CREATE TABLE list_items (
  list_id  TEXT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  drink_id INTEGER NOT NULL REFERENCES drinks(id) ON DELETE CASCADE,
  note     TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  PRIMARY KEY (list_id, drink_id)
);
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
CREATE INDEX idx_reports_open ON reports(status, created_at) WHERE status = 'open';

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
  type              TEXT NOT NULL CHECK (type IN ('text','image','vision')),
  base_url          TEXT NOT NULL,          -- normalized via NormalizeAIBaseURL (e.g. https://opencode.ai/zen/go/v1)
  encrypted_api_key TEXT NOT NULL,          -- AES-GCM encrypted using SHA-256('sodapop-ai-key:' || SESSION_SECRET)
  model             TEXT NOT NULL,
  tags              TEXT NOT NULL DEFAULT '[]', -- JSON array of strings, e.g. ["draft","tasting_note","recommend","default"]
  enabled           INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  temperature       REAL,
  max_tokens        INTEGER,
  image_size        TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
CREATE INDEX idx_ai_endpoints_type_enabled ON ai_endpoints(type, enabled);

CREATE TABLE ai_draft_sessions (
  id            TEXT PRIMARY KEY,           -- 32-char hex; reused as x-opencode-session header
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL CHECK (kind IN ('drink','tasting_note','venue')),
  messages_json TEXT NOT NULL DEFAULT '[]', -- conversation turns
  draft_json    TEXT,                       -- latest structured DrinkDraft / VenueDraft JSON
  status        TEXT NOT NULL DEFAULT 'chatting' CHECK (status IN ('chatting','ready','committed')),
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX idx_ai_draft_sessions_user ON ai_draft_sessions(user_id, updated_at DESC);
```

## 11. Integrity rules that live in code, not DDL

- Barcode GTIN checksum validated before insert; normalize UPC-A to EAN-13 by zero-prefixing, then store as GTIN-14 with leading zeros where the source is 12/13 digits.
- One primary image per drink enforced by `idx_drink_images_primary`; changing the primary is a single transaction that clears the old flag before setting the new one.
- `drink_aliases` feeds the FTS `aliases` column; alias writes refresh the FTS row in the same transaction.
- `drinks.merged_into_id` redirects reads; all writes to a merged drink are rejected with 409 and the canonical ID.
- Deleting a user: `status='deleted'`, anonymize handle/email/avatar, keep check-ins with `user_id` intact but display "deleted user"; purge after 30 days.
- Free-text notes capped at 2000 chars, `details` at 1000; all rendered escaped (no HTML).
- Every catalog write also inserts `catalog_revisions` in the same transaction.
