-- Venue shelf: per-member alerts that a wishlisted drink is available.
-- Both default off; opting in is explicit.
ALTER TABLE user_settings ADD COLUMN wishlist_friend_alerts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_settings ADD COLUMN wishlist_shelf_alerts INTEGER NOT NULL DEFAULT 0;

-- The shelf is always read for one venue, newest first.
CREATE INDEX IF NOT EXISTS idx_venue_stock_reports_venue_seen ON venue_stock_reports(venue_id, seen_at DESC);
