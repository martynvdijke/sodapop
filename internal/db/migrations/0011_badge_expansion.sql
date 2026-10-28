-- Badge expansion: multi-level rules for exploration, social and streak badges.
-- Rules list ascending thresholds; a user's level is the number reached.
-- Idempotent so re-running a backfill cannot duplicate definitions.

INSERT OR IGNORE INTO badges (slug, name, description, kind, rule, created_at) VALUES
	('brand-explorer',   'Brand Explorer',   'Try sodas from ten different brands.',          'core',    'distinct_brands>=10,25,50',      datetime('now')),
	('country-explorer', 'Country Explorer', 'Check in from three different countries.',      'core',    'distinct_countries>=3,10,25',    datetime('now')),
	('venue-hopper',     'Venue Hopper',     'Check in at a venue.',                          'core',    'distinct_venue_kinds>=1,2,3',    datetime('now')),
	('social-butterfly', 'Social Butterfly', 'Make your first friend on Sodapop.',            'core',    'friends>=1,10',                  datetime('now')),
	('streak-keeper',    'Streak Keeper',    'Check in seven days in a row.',                 'special', 'streak_current>=7,30,90',        datetime('now'));

UPDATE badges SET rule = 'distinct_styles>=5,15,30' WHERE slug = 'style-explorer';
