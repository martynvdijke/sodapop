-- Tea, coffee and 0.0% alcohol support. Drinks gain a beverage type and an ABV
-- percentage (0 is legitimate for NA beer/wine/spirits). Check-ins gain an
-- optional preparation method and a short free-text note.
ALTER TABLE drinks ADD COLUMN beverage_type TEXT NOT NULL DEFAULT 'soda';
ALTER TABLE drinks ADD COLUMN abv_percent REAL NOT NULL DEFAULT 0;

ALTER TABLE checkins ADD COLUMN prep_method TEXT NOT NULL DEFAULT 'none';
ALTER TABLE checkins ADD COLUMN prep_note TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_drinks_beverage_type ON drinks(beverage_type);
