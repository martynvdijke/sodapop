package integrations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"sodapop/ent"
	"sodapop/ent/integration"
)

// TRMNL pushes the latest public or friends-visible check-in to the user's
// TRMNL webhook as a merge_variables payload. A missing or disabled
// integration is a no-op.
func (s *Service) TRMNL(ctx context.Context, userID string) error {
	row, err := s.Client.Integration.Query().
		Where(
			integration.UserID(userID),
			integration.KindEQ(integration.KindTrmnl),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !row.Enabled {
		return nil
	}
	return s.sendTRMNL(ctx, row)
}

type trmnlMergeVariables struct {
	Title     string `json:"title"`
	Text      string `json:"text"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

type trmnlPayload struct {
	MergeVariables trmnlMergeVariables `json:"merge_variables"`
}

func (s *Service) sendTRMNL(ctx context.Context, row *ent.Integration) error {
	target := strings.TrimSpace(row.Target)
	if target == "" {
		return errNoTarget()
	}

	merge := trmnlMergeVariables{
		Title:     "Sodapop",
		Text:      "No pours yet.",
		URL:       s.baseURL(),
		UpdatedAt: now(),
	}

	handle := ""
	_ = s.SQL.QueryRowContext(ctx,
		`SELECT handle FROM users WHERE id = ? AND deleted_at IS NULL`, row.UserID).Scan(&handle)

	var (
		id, createdAt, drinkName, drinkSlug, note string
		rating                                    sql.NullInt64
	)
	err := s.SQL.QueryRowContext(ctx, `
		SELECT c.id, c.created_at, d.name, d.slug, c.rating, c.note
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND c.visibility IN ('public', 'friends')
		ORDER BY c.created_at DESC
		LIMIT 1`, row.UserID).
		Scan(&id, &createdAt, &drinkName, &drinkSlug, &rating, &note)
	switch {
	case err == sql.ErrNoRows:
		// Keep the quiet payload.
	case err != nil:
		return err
	default:
		text := "@" + handle + " drank " + drinkName
		if rating.Valid {
			text = "@" + handle + " rated " + drinkName + " " + formatRating(int(rating.Int64)) + "/5"
		} else if strings.TrimSpace(note) != "" {
			text = "@" + handle + ": " + strings.TrimSpace(note)
		}
		merge.Text = text
		merge.URL = s.baseURL() + "/checkins/" + id
		merge.UpdatedAt = createdAt
	}

	body, err := json.Marshal(trmnlPayload{MergeVariables: merge})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.userAgent())
	return s.do(req)
}

func (s *Service) userAgent() string {
	version := ""
	if s != nil && s.Cfg != nil {
		version = strings.TrimSpace(s.Cfg.Version)
	}
	if version == "" {
		return "sodapop"
	}
	return "sodapop/" + version
}
