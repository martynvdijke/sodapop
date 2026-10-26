package checkins

import (
	"context"
	"database/sql"
	"strings"
)

// ListSummary is the bounded read model for one user list. ItemCount is the
// number of drinks in the list; it is never a catalog total.
type ListSummary struct {
	ID          string
	Name        string
	Description string
	Visibility  string
	IsSystem    bool
	ItemCount   int
}

// ListItem is one drink saved to a list.
type ListItem struct {
	DrinkID   int
	Slug      string
	Name      string
	Brand     string
	Note      string
	CreatedAt string
}

// ListDetail is a visible list plus its newest items. A caller must treat an
// invisible list exactly like a missing one.
type ListDetail struct {
	ListSummary
	Items []ListItem
}

// listItemCap bounds one list read so an enormous collection cannot be walked
// as a catalog export.
const listItemCap = 200

// ListsForOwner returns every list owned by userID, including the system
// wishlist. It backs GET /api/v1/me/lists and is never used for another user.
func (s *Service) ListsForOwner(ctx context.Context, userID string) ([]ListSummary, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT l.id, l.name, COALESCE(l.description,''), l.visibility, l.is_system,
		       (SELECT COUNT(*) FROM list_items li WHERE li.list_id = l.id)
		FROM lists l
		JOIN users u ON u.id = l.user_id
		WHERE l.user_id = ?
		  AND u.deleted_at IS NULL AND u.status <> 'deleted'
		ORDER BY l.is_system DESC, l.updated_at DESC, l.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ListSummary{}
	for rows.Next() {
		var l ListSummary
		if err := rows.Scan(&l.ID, &l.Name, &l.Description, &l.Visibility, &l.IsSystem, &l.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListForViewer returns one list scoped to what viewerID may read. Public and
// friends lists follow the same visibility ladder as check-ins; system lists
// (the wishlist) are personal plumbing and remain owner-only. A block in either
// direction, a missing row and an invisible row all return ErrNotFound.
func (s *Service) ListForViewer(ctx context.Context, viewerID, listID string) (*ListDetail, error) {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return nil, ErrNotFound
	}
	var (
		owner string
		out   ListDetail
	)
	err := s.SQL.QueryRowContext(ctx, `
		SELECT l.id, l.user_id, l.name, COALESCE(l.description,''), l.visibility, l.is_system,
		       (SELECT COUNT(*) FROM list_items li WHERE li.list_id = l.id)
		FROM lists l
		JOIN users u ON u.id = l.user_id
		WHERE l.id = ? AND u.deleted_at IS NULL AND u.status <> 'deleted'`, listID).
		Scan(&out.ID, &owner, &out.Name, &out.Description, &out.Visibility, &out.IsSystem, &out.ItemCount)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	isOwner := viewerID != "" && viewerID == owner
	if viewerID != "" && !isOwner && s.blocked(ctx, viewerID, owner) {
		return nil, ErrNotFound
	}
	if out.IsSystem {
		if !isOwner {
			return nil, ErrNotFound
		}
	} else if !isOwner {
		switch out.Visibility {
		case "public":
		case "friends":
			if viewerID == "" || s.Social == nil || !s.Social.AreFriends(ctx, viewerID, owner) {
				return nil, ErrNotFound
			}
		default: // private or unknown
			return nil, ErrNotFound
		}
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT li.drink_id, d.slug, d.name, COALESCE(b.name,''), COALESCE(li.note,''), li.added_at
		FROM list_items li
		JOIN drinks d ON d.id = li.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		WHERE li.list_id = ? AND d.status <> 'merged'
		ORDER BY li.added_at DESC, li.id DESC
		LIMIT ?`, listID, listItemCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out.Items = []ListItem{}
	for rows.Next() {
		var it ListItem
		if err := rows.Scan(&it.DrinkID, &it.Slug, &it.Name, &it.Brand, &it.Note, &it.CreatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &out, nil
}
