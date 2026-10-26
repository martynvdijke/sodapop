-- Comment thread lookups and per-user hourly rate-limit counts.
CREATE INDEX IF NOT EXISTS idx_comments_checkin_created ON comments(checkin_id, created_at);
CREATE INDEX IF NOT EXISTS idx_comments_user_created ON comments(user_id, created_at);
