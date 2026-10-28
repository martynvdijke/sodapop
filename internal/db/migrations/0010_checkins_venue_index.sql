-- Nearby is friend-scoped and pre-filters check-ins by venue bounding box.
-- The venue/created composite lets those lookups scan newest-first without a
-- full checkins scan; soft-deleted rows are excluded from the index.

CREATE INDEX IF NOT EXISTS idx_checkins_venue_created
    ON checkins(venue_id, created_at) WHERE deleted_at IS NULL;
