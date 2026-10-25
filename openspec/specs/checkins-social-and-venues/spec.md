# Check-ins, Social Graph, Venues, Badges, and Lists Specification

## Purpose

Defines Sodapop's check-in loop, quarter-star rating aggregation, mutual friendships and one-way follows, chronological activity feed, venues (including `hotel` and `vending` kinds), idempotent badges, and user lists.

## Requirements

### Requirement: Quarter-Star Check-ins and Idempotent Submission
Verified users SHALL log check-ins (`Checkin` Ent entity) with integer ratings `0..20` (`0` = unrated, `1..20` = `0.25..5.00` stars in `0.25` increments), optional serving, volume, price, venue, photo, notes, and visibility (`public|friends|private`).

#### Scenario: Idempotent check-in creation and transactional side effects
- **WHEN** a client submits `POST /checkins` or `POST /api/v1/checkins` with an `idempotency_key`
- **THEN** within a single transaction the service creates the `Checkin`, writes the `Activity` row, updates `venue_drinks.last_seen_at` if a venue is attached, and evaluates `Badge` awards
- **AND** replaying the same `idempotency_key` returns the existing check-in without creating a duplicate row

#### Scenario: Drink rating averages and Bayesian top lists
- **WHEN** `drink_ratings` aggregates ratings for a drink
- **THEN** each user's rated check-ins (`rating > 0`, `deleted_at IS NULL`) are averaged, and the public average is displayed only when `rater_count >= 3` (`MIN_RATERS_FOR_AVG`)
- **AND** top-rated lists rank drinks using a Bayesian score (prior mean `3.5`, weight `5`, minimum `3` distinct raters)

### Requirement: Friends, Follows, Toasts, and Chronological Feed
The system SHALL support symmetric friendships (`user_a < user_b`, `pending|accepted|blocked`), one-way follows, and one-tap toasts (`checkin_likes`).

#### Scenario: Keyset-paginated feed and privacy enforcement
- **WHEN** a user loads the feed (`Following`, `You`, or `Nearby`)
- **THEN** the query returns activities from the user, accepted friends, and followed users ordered by `(created_at DESC, id DESC)`, excluding blocked users and `private` check-ins
- **AND** direct requests for another user's `private` check-in or non-friend `friends`-only check-in return HTTP `404` (never `403`)

### Requirement: Venues Including Hotel and Vending Kinds
The system SHALL model physical places in `Venue` with `kind` in `hotel|bar|restaurant|cafe|shop|festival|vending|other`.

#### Scenario: Venue deduplication and hotel check-in attribution
- **WHEN** a user creates a venue
- **THEN** an exact normalized `name + city` match blocks duplicate creation and links to the existing venue, while a same-kind venue within 150 m prompts confirmation
- **AND** checking in a drink at a venue with `kind='hotel'` lists the drink under that hotel's "Seen here" menu and increments the user's hotel venue badge progress
