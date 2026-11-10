-- A recap share card describes one calendar year. 0 means the card is not
-- year-scoped (passport, streak, flavour fingerprint).
ALTER TABLE share_links ADD COLUMN year INTEGER NOT NULL DEFAULT 0;
