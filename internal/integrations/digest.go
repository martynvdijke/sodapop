package integrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/db"
)

type digestPayload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type digestItem struct {
	ID        string
	Handle    string
	DrinkName string
	DrinkSlug string
	Rated     *int
	Toasts    int
}

// WeeklyDigest queues a weekly summary for every user with
// user_settings.email_weekly enabled. The summary highlights the five most
// toasted check-ins from the user's friends over the last seven days.
func (s *Service) WeeklyDigest(ctx context.Context) error {
	rows, err := s.SQL.QueryContext(ctx,
		`SELECT user_id FROM user_settings WHERE email_weekly = 1 ORDER BY user_id`)
	if err != nil {
		return err
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	for _, userID := range userIDs {
		u, err := s.Client.User.Get(ctx, userID)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return err
		}
		if u.DeletedAt != nil || u.Status != user.StatusActive {
			continue
		}
		items, err := s.weeklyItems(ctx, userID, cutoff)
		if err != nil {
			return err
		}
		subject, body := digestMessage(u.Handle, s.baseURL(), items)
		payload, err := json.Marshal(digestPayload{Subject: subject, Body: body})
		if err != nil {
			return err
		}
		if _, err := s.Client.EmailOutbox.Create().
			SetToEmail(u.Email).
			SetTemplate("weekly_digest").
			SetPayloadJSON(string(payload)).
			SetNextAttemptAt(db.Now()).
			Save(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) weeklyItems(ctx context.Context, userID, cutoff string) ([]digestItem, error) {
	friends, err := s.friendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(friends) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(friends)), ",")
	args := make([]any, 0, len(friends)+1)
	for _, id := range friends {
		args = append(args, id)
	}
	args = append(args, cutoff)
	query := `
		SELECT c.id, u.handle, d.name, d.slug, c.rating,
		       (SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id)
		FROM checkins c
		JOIN users u ON u.id = c.user_id
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id IN (` + placeholders + `)
		  AND c.deleted_at IS NULL
		  AND c.visibility IN ('public', 'friends')
		  AND c.created_at >= ?
		ORDER BY (SELECT COUNT(*) FROM checkin_likes l WHERE l.checkin_id = c.id) DESC,
		         c.created_at DESC
		LIMIT 5`
	rows, err := s.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []digestItem{}
	for rows.Next() {
		var (
			item   digestItem
			rating sql.NullInt64
		)
		if err := rows.Scan(&item.ID, &item.Handle, &item.DrinkName, &item.DrinkSlug, &rating, &item.Toasts); err != nil {
			return nil, err
		}
		if rating.Valid {
			v := int(rating.Int64)
			item.Rated = &v
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) friendIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END
		FROM friendships
		WHERE status = 'accepted' AND (user_a = ? OR user_b = ?)`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func digestMessage(handle, baseURL string, items []digestItem) (string, string) {
	subject := "Your Sodapop week"
	var b strings.Builder
	b.WriteString("Hi @" + handle + ",\n\n")
	if len(items) == 0 {
		b.WriteString("Your friends were quiet this week. Be the first to log a soda")
		if baseURL != "" {
			b.WriteString(": " + baseURL + "/new")
		}
		b.WriteString(".\n")
		return subject, b.String()
	}
	b.WriteString("Here is what your friends drank on Sodapop this week:\n\n")
	for _, item := range items {
		b.WriteString("- @" + item.Handle + " drank " + item.DrinkName)
		if item.Rated != nil {
			b.WriteString(" (" + formatRating(*item.Rated) + "/5)")
		}
		if item.Toasts > 0 {
			b.WriteString(" with " + strconv.Itoa(item.Toasts) + " toasts")
		}
		if baseURL != "" {
			b.WriteString(" — " + baseURL + "/drinks/" + item.DrinkSlug)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nOpen Sodapop")
	if baseURL != "" {
		b.WriteString(": " + baseURL)
	}
	b.WriteString("\n")
	return subject, b.String()
}
