-- Core badge definitions. Evaluation is idempotent and may be re-run for
-- historical backfills.

INSERT OR IGNORE INTO badges (slug, name, description, kind, rule, created_at) VALUES
	('first-pour',      'First Pour',        'Log your first soda.',                         'core', 'checkins>=1',        datetime('now')),
	('ten-pours',       'Ten Pours',         'Log ten sodas.',                               'core', 'checkins>=10',       datetime('now')),
	('fifty-pours',     'Fifty Pours',       'Log fifty sodas.',                             'core', 'checkins>=50',       datetime('now')),
	('hundred-club',    'Hundred Club',      'Log one hundred sodas.',                       'core', 'checkins>=100',      datetime('now')),
	('style-explorer',  'Style Explorer',    'Try five different soda styles.',              'core', 'distinct_styles>=5', datetime('now')),
	('can-do',          'Can Do',            'Log ten sodas from cans.',                     'core', 'cans>=10',           datetime('now')),
	('glass-act',       'Glass Act',         'Log a soda from a glass bottle.',              'core', 'glass>=1',           datetime('now')),
	('fountain-of-youth','Fountain of Youth', 'Log a fountain soda.',                         'core', 'fountain>=1',        datetime('now')),
	('fizz-whizz',      'Fizz Whizz',        'Rate a sparkling water.',                      'core', 'sparkling_water>=1', datetime('now')),
	('night-owl',       'Night Owl',         'Check in between midnight and 4am.',           'special','late_night>=1',    datetime('now'));

INSERT OR IGNORE INTO app_settings (key, value, updated_at) VALUES
	('ai_enabled', '0', datetime('now')),
	('registration_open', '1', datetime('now'));
