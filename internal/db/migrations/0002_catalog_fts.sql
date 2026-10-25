-- Raw SQLite constructs that Ent cannot model: FTS5 search index, partial
-- indexes and the schema bookkeeping table.

CREATE VIRTUAL TABLE IF NOT EXISTS drinks_fts USING fts5(
	name,
	brand,
	style,
	aliases,
	tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS drinks_fts_ai AFTER INSERT ON drinks BEGIN
	INSERT INTO drinks_fts(rowid, name, brand, style, aliases)
	VALUES (
		new.id,
		new.name,
		COALESCE((SELECT name FROM brands WHERE id = new.brand_id), ''),
		COALESCE((SELECT name FROM styles WHERE id = new.style_id), ''),
		COALESCE((SELECT group_concat(alias, ' ') FROM drink_alias WHERE drink_id = new.id), '')
	);
END;

CREATE TRIGGER IF NOT EXISTS drinks_fts_ad AFTER DELETE ON drinks BEGIN
	DELETE FROM drinks_fts WHERE rowid = old.id;
END;

CREATE TRIGGER IF NOT EXISTS drinks_fts_au AFTER UPDATE OF name, brand_id, style_id ON drinks BEGIN
	UPDATE drinks_fts SET
		name = new.name,
		brand = COALESCE((SELECT name FROM brands WHERE id = new.brand_id), ''),
		style = COALESCE((SELECT name FROM styles WHERE id = new.style_id), '')
	WHERE rowid = new.id;
END;

CREATE TRIGGER IF NOT EXISTS drink_alias_fts_ai AFTER INSERT ON drink_alias BEGIN
	UPDATE drinks_fts SET aliases = COALESCE((SELECT group_concat(alias, ' ') FROM drink_alias WHERE drink_id = new.drink_id), '')
	WHERE rowid = new.drink_id;
END;

CREATE TRIGGER IF NOT EXISTS drink_alias_fts_ad AFTER DELETE ON drink_alias BEGIN
	UPDATE drinks_fts SET aliases = COALESCE((SELECT group_concat(alias, ' ') FROM drink_alias WHERE drink_id = old.drink_id), '')
	WHERE rowid = old.drink_id;
END;

CREATE UNIQUE INDEX IF NOT EXISTS idx_drink_images_primary
	ON drink_images(drink_id) WHERE is_primary = 1;

CREATE INDEX IF NOT EXISTS idx_drinks_status ON drinks(status);
CREATE INDEX IF NOT EXISTS idx_venues_merged ON venues(merged_into_id) WHERE merged_into_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_venues_geo ON venues(lat, lng);
CREATE INDEX IF NOT EXISTS idx_outbox_due ON email_outboxes(next_attempt_at) WHERE sent_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(user_id, created_at) WHERE read_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_reports_open ON reports(status, created_at) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_checkins_active ON checkins(user_id, created_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_follows_followee ON follows(followee_id);
CREATE INDEX IF NOT EXISTS idx_venue_saves_user ON venue_saves(user_id);
CREATE INDEX IF NOT EXISTS idx_checkin_likes_user ON checkin_likes(user_id);
