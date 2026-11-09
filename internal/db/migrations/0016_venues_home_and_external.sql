-- Venue support for the shared @home pseudo-venue and external (Photon)
-- nearby results. A hidden venue is never listed by discovery, saved lists,
-- recent lists, or venue counts; the @home row is the single shared marker so a
-- check-in can say "at home" without storing a home coordinate.
ALTER TABLE venues ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
ALTER TABLE venues ADD COLUMN external_provider TEXT;
ALTER TABLE venues ADD COLUMN external_id TEXT;

-- External venues are unique per provider identifier. Partial so the many
-- self-authored rows with NULL provider/id never collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_venues_external
    ON venues(external_provider, external_id)
    WHERE external_provider IS NOT NULL AND external_id IS NOT NULL;

-- One shared hidden @home marker. Idempotent so a re-run or a race with
-- EnsureHome cannot create a second row.
INSERT INTO venues (name, slug, kind, address, city, region, country, lat, lng, hidden, created_at, updated_at)
SELECT '@home', 'at-home', 'other', '', '', '', '', 0, 0, 1,
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
       strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE NOT EXISTS (SELECT 1 FROM venues WHERE slug = 'at-home');

UPDATE venues SET hidden = 1 WHERE slug = 'at-home';
