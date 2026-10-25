-- Uniqueness constraints that Ent cannot express as partial/composite indexes.

CREATE UNIQUE INDEX IF NOT EXISTS idx_venue_saves_unique ON venue_saves(user_id, venue_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_venue_drinks_unique ON venue_drinks(venue_id, drink_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_list_items_unique ON list_items(list_id, drink_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_badges_unique ON user_badges(user_id, badge_id);
