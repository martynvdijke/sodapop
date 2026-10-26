-- Delivery outbox lookup indexes. The worker polls undelivered rows ordered by
-- next attempt time; users can page their own delivery history newest first.
CREATE INDEX IF NOT EXISTS idx_delivery_outbox_due
    ON delivery_outbox(next_attempt_at)
    WHERE delivered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_delivery_outbox_user_created
    ON delivery_outbox(user_id, created_at);
