-- Audit lookup indexes. The audit tables are append-only and only ever read
-- by entity, newest first (admin queue, per-report notes, revision feed).
-- login_attempts already has its (key, attempted_at) index from Ent.

CREATE INDEX IF NOT EXISTS idx_catalog_revisions_entity_created
    ON catalog_revisions(entity, created_at);

CREATE INDEX IF NOT EXISTS idx_admin_notes_entity_created
    ON admin_notes(entity, created_at);
