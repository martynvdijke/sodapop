package checkins

import "context"

// TopStyle is one drink style ranked by how many visible check-ins a profile
// has for it. It carries only display data, never the underlying check-ins.
type TopStyle struct {
	Name  string
	Slug  string
	Count int
}

// ProfileTopStyles ranks the styles on targetID's profile using the same
// access rules as ProfileCheckins: the viewer sees only the visibility set
// their relationship allows, and deleted check-ins and deleted accounts are
// excluded in SQL. limit defaults to 3 and is capped at 5 so a profile shelf
// stays small; the returned slice is always non-nil.
func (s *Service) ProfileTopStyles(ctx context.Context, viewerID, targetID string, limit int) ([]TopStyle, error) {
	if limit <= 0 {
		limit = 3
	}
	if limit > 5 {
		limit = 5
	}
	out := []TopStyle{}
	if targetID == "" {
		return out, nil
	}
	visit := s.ProfileVisitFor(ctx, viewerID, targetID)
	vis := profileVisibilitySet(visit)

	q := `
		SELECT st.name, st.slug, COUNT(*)
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		JOIN styles st ON st.id = d.style_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		  AND d.style_id IS NOT NULL
		  AND c.visibility IN (` + placeholders(len(vis)) + `)
		GROUP BY st.id, st.name, st.slug
		ORDER BY COUNT(*) DESC, st.name ASC
		LIMIT ?`
	args := []any{targetID}
	args = append(args, visibilityArgs(vis)...)
	args = append(args, limit)

	rows, err := s.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st TopStyle
		if err := rows.Scan(&st.Name, &st.Slug, &st.Count); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
