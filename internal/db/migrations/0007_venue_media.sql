-- Venue gallery ordering and per-uploader cap counts.
CREATE INDEX IF NOT EXISTS idx_venue_media_venue_created ON venue_media(venue_id, created_at);
CREATE INDEX IF NOT EXISTS idx_venue_media_uploader_venue ON venue_media(uploaded_by, venue_id);
