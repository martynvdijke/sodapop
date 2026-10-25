# Data Model, Ent ORM, and SQLite Migrations Specification

## Purpose

Defines Sodapop's persistence architecture on SQLite (`modernc.org/sqlite`), combining **Ent ORM (`entgo.io/ent v0.14.6`)** as the canonical entity schema and CRUD layer with a **versioned transactional migration registry (`internal/db/migrations`)** for SQLite-specific constructs (FTS5, views, partial indexes) and schema/data evolution, guarded mechanically by `schema_consistency_test.go`.

## Requirements

### Requirement: Ent ORM as Canonical Entity Schema and CRUD Layer
The system SHALL define all domain entity schemas in `ent/schema/*.go` using `entgo.io/ent v0.14.6` and generate typed client code under `ent/` and `ent/migrate/schema.go` (`migrate.Tables`).

#### Scenario: Entity schemas cover all core domains
- **WHEN** `go generate ./ent` is executed
- **THEN** `ent/migrate/schema.go` exports `Tables` containing definitions for `User`, `UserIdentity`, `Session`, `APIToken`, `EmailVerificationToken`, `PasswordResetToken`, `EmailOutbox`, `UserSetting`, `LoginAttempt`, `Media`, `Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkFlavour`, `DrinkAlias`, `DrinkImage`, `OffProduct`, `CatalogRevision`, `Venue`, `VenueDrink`, `VenueName`, `VenueSave`, `Checkin`, `CheckinLike`, `Follow`, `Friendship`, `Block`, `Activity`, `Notification`, `Badge`, `UserBadge`, `List`, `ListItem`, `Report`, `AdminNote`, `AppSetting`, `AIEndpoint`, and `AIDraftSession`
- **AND** user-owned/high-volume entities use 16-byte random hex `string` IDs while shared catalog/admin entities use autoincrement `int` IDs

#### Scenario: Standard CRUD uses the Ent client
- **WHEN** a domain service creates, reads, updates, or deletes a single entity or traverses entity edges
- **THEN** it executes the operation through `db.Client.<Entity>` sharing the underlying `*sql.DB` pool

### Requirement: SQLite Connection Pool and PRAGMA Configuration
The system SHALL open SQLite using pure-Go `modernc.org/sqlite` with deterministic connection-level and database-level PRAGMAs.

#### Scenario: Database initialization sets WAL and connection pragmas
- **WHEN** `db.Init(dbPath)` opens the SQLite database
- **THEN** every pooled connection applies `_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)&_pragma=foreign_keys(ON)` and `mmap_size`
- **AND** database-level `PRAGMA journal_mode=WAL` and `PRAGMA auto_vacuum=INCREMENTAL` are applied
- **AND** `DB.SetMaxOpenConns(4)` bounds the connection pool and shares `DB` with `entsql.OpenDB(dialect.SQLite, DB)`

### Requirement: Two-Stage Versioned Migration Pipeline
The system SHALL run both Ent schema migration (`Client.Schema.Create`) and ordered versioned migrations (`internal/db/migrations/registry.go`) during database initialization.

#### Scenario: Fresh database boot applies Ent schema and versioned migrations
- **WHEN** `db.Init` runs against an empty SQLite database
- **THEN** `Client.Schema.Create(ctx)` creates all Ent-managed tables and foreign keys
- **AND** `migrations.Migrate(DB)` executes each migration in `migrations.Registry` (`0001` through `0010`) inside its own transaction and records `(version, name, applied_at)` in `schema_migrations`
- **AND** standalone FTS5 virtual table `drinks_fts`, SQL view `drink_ratings`, and partial unique indexes (`idx_drink_images_primary`, `idx_drinks_off`, `idx_outbox_due`, `idx_notifications_unread`, `idx_reports_open`) are created

#### Scenario: Subsequent boots skip already-applied migrations
- **WHEN** `db.Init` runs against a database where `schema_migrations` already contains versions `1..10`
- **THEN** `migrations.Migrate(DB)` skips versions `<= 10` and executes only newly added versions `> 10`

### Requirement: Permitted Raw SQL Scope and Mechanical Schema Consistency Guard
The system SHALL restrict raw `*sql.DB` queries to FTS5 search, multi-table read projections, and `VACUUM INTO` backups, and SHALL enforce schema registration in CI.

#### Scenario: Raw SQL is parameterized and traced
- **WHEN** a repository executes raw SQL on `db.DB` (such as `drinks_fts MATCH ?`, `drink_ratings` projections, or keyset feed joins)
- **THEN** all values are passed as `?` bind parameters, `rows.Close()` is deferred immediately after error checking, and execution is wrapped in `db.TraceQueryEx` for OTel spans and slow-query `EXPLAIN QUERY PLAN` logging

#### Scenario: Unregistered raw DDL fails schema consistency test
- **WHEN** `go test ./internal/db -run TestSchemaConsistency` runs
- **THEN** it initializes `:memory:` and verifies that every table in `sqlite_master` exists in `ent/migrate.Tables` or in the explicit `rawOnlyTables` allowlist (`schema_migrations` and `drinks_fts*`)
- **AND** any unregistered table causes the test to fail with an actionable diff
