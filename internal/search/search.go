// Package search provides a small unified search facade over the catalog.
// It is deliberately capped and non-paginated: results are a convenience for
// discovery, not a way to enumerate the catalog.
package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"sodapop/internal/catalog"
)

const (
	// MaxDrinks, MaxBrands and MaxStyles are the hard per-group caps. MaxTotal
	// is the overall cap; the per-group values sum to it.
	MaxDrinks = 4
	MaxBrands = 3
	MaxStyles = 3
	MaxTotal  = 10

	// visibleStatusesSQL mirrors the browse visibility set: merged and
	// provisional drinks never surface as brands or styles.
	visibleStatusesSQL = `d.status IN ('active','limited','seasonal','discontinued')`
)

// Item is one grouped search result. ID is a string so drinks (integer IDs)
// and catalog rows share a shape.
type Item struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle,omitempty"`
}

// Results groups capped hits by catalog entity. Total is the sum of the
// capped slices and is not exposed as a JSON count.
type Results struct {
	Drinks []Item `json:"drinks"`
	Brands []Item `json:"brands"`
	Styles []Item `json:"styles"`
	Total  int    `json:"-"`
}

// Service is the unified search facade.
type Service struct {
	catalog *catalog.Service
}

// New builds a search service over an existing catalog service.
func New(c *catalog.Service) *Service {
	return &Service{catalog: c}
}

// Search returns drinks, brands and styles matching query. A query shorter
// than catalog.MinSearchLen returns empty results without touching the
// database. limitPerType may lower the per-group cap; values <= 0 use the
// hard caps.
func (s *Service) Search(ctx context.Context, query string, limitPerType int) (Results, error) {
	out := Results{Drinks: []Item{}, Brands: []Item{}, Styles: []Item{}}
	query = strings.TrimSpace(query)
	if len([]rune(query)) < catalog.MinSearchLen {
		return out, nil
	}

	drinks, err := s.catalog.Search(ctx, query, "", capFor(limitPerType, MaxDrinks))
	if err != nil {
		return out, err
	}
	for _, d := range drinks {
		out.Drinks = append(out.Drinks, Item{
			Type:     "drink",
			ID:       strconv.Itoa(d.ID),
			Slug:     d.Slug,
			Name:     d.Name,
			Subtitle: joinSubtitle(d.Brand, d.Style),
		})
	}

	if out.Brands, err = s.brands(ctx, query, capFor(limitPerType, MaxBrands)); err != nil {
		return out, err
	}
	if out.Styles, err = s.styles(ctx, query, capFor(limitPerType, MaxStyles)); err != nil {
		return out, err
	}

	out.Total = len(out.Drinks) + len(out.Brands) + len(out.Styles)
	if out.Total > MaxTotal {
		out.Total = MaxTotal
	}
	return out, nil
}

func (s *Service) brands(ctx context.Context, query string, limit int) ([]Item, error) {
	rows, err := s.catalog.SQL.QueryContext(ctx, `
		SELECT b.id, b.slug, b.name,
		       (SELECT COUNT(*) FROM drinks d
		         WHERE d.brand_id = b.id AND `+visibleStatusesSQL+`)
		FROM brands b
		WHERE lower(b.name) LIKE lower(?) ESCAPE '\'
		ORDER BY CASE WHEN lower(b.name) LIKE lower(?) ESCAPE '\' THEN 0 ELSE 1 END,
		         b.name COLLATE NOCASE ASC, b.id ASC
		LIMIT ?`,
		likeContains(query), likePrefix(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var id, count int
		var slug, name string
		if err := rows.Scan(&id, &slug, &name, &count); err != nil {
			return nil, err
		}
		out = append(out, Item{
			Type:     "brand",
			ID:       strconv.Itoa(id),
			Slug:     slug,
			Name:     name,
			Subtitle: sodaCount(count),
		})
	}
	return out, rows.Err()
}

func (s *Service) styles(ctx context.Context, query string, limit int) ([]Item, error) {
	rows, err := s.catalog.SQL.QueryContext(ctx, `
		SELECT st.id, st.slug, st.name, COALESCE(p.name, ''),
		       (SELECT COUNT(*) FROM drinks d
		         WHERE (d.style_id = st.id OR d.style_id IN (SELECT id FROM styles WHERE parent_id = st.id))
		           AND `+visibleStatusesSQL+`)
		FROM styles st
		LEFT JOIN styles p ON p.id = st.parent_id
		WHERE lower(st.name) LIKE lower(?) ESCAPE '\'
		ORDER BY CASE WHEN lower(st.name) LIKE lower(?) ESCAPE '\' THEN 0 ELSE 1 END,
		         st.name COLLATE NOCASE ASC, st.id ASC
		LIMIT ?`,
		likeContains(query), likePrefix(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var id, count int
		var slug, name, parent string
		if err := rows.Scan(&id, &slug, &name, &parent, &count); err != nil {
			return nil, err
		}
		subtitle := sodaCount(count)
		if parent != "" {
			subtitle = fmt.Sprintf("%s · %s", parent, subtitle)
		}
		out = append(out, Item{
			Type:     "style",
			ID:       strconv.Itoa(id),
			Slug:     slug,
			Name:     name,
			Subtitle: subtitle,
		})
	}
	return out, rows.Err()
}

func capFor(limit, hard int) int {
	if limit <= 0 || limit > hard {
		return hard
	}
	return limit
}

func joinSubtitle(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func sodaCount(n int) string {
	if n == 1 {
		return "1 soda"
	}
	return fmt.Sprintf("%d sodas", n)
}

// likeContains builds a substring LIKE pattern, escaping wildcard characters
// so a query containing % or _ is treated literally.
func likeContains(q string) string { return "%" + escapeLike(q) + "%" }

func likePrefix(q string) string { return escapeLike(q) + "%" }

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
