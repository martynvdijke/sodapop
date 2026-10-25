-- Case-insensitive uniqueness for handles and email addresses. Ent's column
-- uniqueness stays in place; these indexes enforce the user-facing contract.

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_handle_nocase ON users(handle COLLATE NOCASE);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nocase ON users(email COLLATE NOCASE);

CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
