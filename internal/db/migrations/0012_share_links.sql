-- Capability links for public passport and streak cards. Tokens are random and
-- revocable; private profiles stay private until a user creates one of these.
CREATE TABLE IF NOT EXISTS share_links (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    token      TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_share_links_user
    ON share_links(user_id, created_at DESC);
