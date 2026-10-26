package catalog

import (
	"context"
	"errors"
	"strings"

	"sodapop/ent"
	"sodapop/ent/drink"
)

var (
	// ErrCompareCount reports a comparison outside the supported 2..6 range.
	ErrCompareCount = errors.New("choose between 2 and 6 drinks to compare")
	// ErrCompareDuplicate reports the same slug appearing more than once.
	ErrCompareDuplicate = errors.New("each drink can be compared only once")
)

const (
	MinCompare = 2
	MaxCompare = 6
)

// Compare loads two to six drinks in the requested order. Duplicate slugs and
// merged or provisional rows are rejected so a comparison never shows drafts
// or the same drink twice.
func (s *Service) Compare(ctx context.Context, slugs []string) ([]*DrinkDetail, error) {
	clean := make([]string, 0, len(slugs))
	seen := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			return nil, ErrCompareDuplicate
		}
		seen[slug] = struct{}{}
		clean = append(clean, slug)
	}
	if len(clean) < MinCompare || len(clean) > MaxCompare {
		return nil, ErrCompareCount
	}

	out := make([]*DrinkDetail, 0, len(clean))
	for _, slug := range clean {
		d, err := s.Client.Drink.Query().
			Where(
				drink.Slug(slug),
				drink.StatusNEQ(drink.StatusMerged),
				drink.StatusNEQ(drink.StatusProvisional),
			).
			WithBrand().
			WithStyle().
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		detail, err := s.detail(ctx, d)
		if err != nil {
			return nil, err
		}
		out = append(out, detail)
	}
	return out, nil
}
