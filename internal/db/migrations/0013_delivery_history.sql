-- Admin delivery history pages the outbox newest first, independent of the
-- per-user history index added in 0008.
CREATE INDEX IF NOT EXISTS idx_delivery_outbox_created
    ON delivery_outbox(created_at);
