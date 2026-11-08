-- Admin activity overview pages seven sources newest first. These indexes let
-- each source's created_at ordering scan the index instead of a full table.
CREATE INDEX IF NOT EXISTS idx_drinks_created
    ON drinks(created_at);

CREATE INDEX IF NOT EXISTS idx_users_created
    ON users(created_at);

CREATE INDEX IF NOT EXISTS idx_venues_created
    ON venues(created_at);

CREATE INDEX IF NOT EXISTS idx_checkin_likes_created
    ON checkin_likes(created_at);
