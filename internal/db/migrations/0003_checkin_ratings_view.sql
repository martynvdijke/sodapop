-- Aggregate drink ratings. Check-ins without a rating never affect averages.
-- A drink's average is only displayed once MIN_RATERS (3) distinct users have
-- rated it; top lists additionally use a Bayesian score in application code.

CREATE VIEW IF NOT EXISTS drink_ratings AS
SELECT
	drink_id,
	COUNT(*)                    AS checkin_count,
	COUNT(DISTINCT user_id)     AS rater_count,
	AVG(rating)                 AS avg_rating
FROM checkins
WHERE rating IS NOT NULL AND deleted_at IS NULL
GROUP BY drink_id;
