package integrations

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/integration"
)

// RSSURL ensures the user has a TRMNL feed token and returns its URL.
func (s *Service) RSSURL(ctx context.Context, userID string) (string, error) {
	row, err := s.Client.Integration.Query().
		Where(integration.UserID(userID), integration.KindEQ(integration.KindTrmnl)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		token, tokenErr := randomToken()
		if tokenErr != nil {
			return "", tokenErr
		}
		feedURL := s.feedURL(token)
		if _, err := s.Client.Integration.Create().
			SetUserID(userID).
			SetKind(integration.KindTrmnl).
			SetTarget(feedURL).
			SetSecret(token).
			SetEnabled(true).
			SetCreatedAt(now()).
			SetUpdatedAt(now()).
			Save(ctx); err != nil {
			return "", err
		}
		return feedURL, nil
	case err != nil:
		return "", err
	}

	if row.Secret != "" {
		return s.feedURL(row.Secret), nil
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	feedURL := s.feedURL(token)
	if _, err := row.Update().
		SetSecret(token).
		SetTarget(feedURL).
		SetUpdatedAt(now()).
		Save(ctx); err != nil {
		return "", err
	}
	return feedURL, nil
}

// RSS renders the token owner's last 40 public or friends-visible check-ins as
// RSS 2.0. The token is a capability and must match an enabled TRMNL row.
func (s *Service) RSS(ctx context.Context, token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("feed token is required")
	}
	row, err := s.Client.Integration.Query().
		Where(
			integration.KindEQ(integration.KindTrmnl),
			integration.Secret(token),
			integration.Enabled(true),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("feed not found")
		}
		return nil, err
	}
	handle := ""
	_ = s.SQL.QueryRowContext(ctx,
		`SELECT handle FROM users WHERE id = ? AND deleted_at IS NULL`, row.UserID).Scan(&handle)

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT c.id, c.created_at, d.name, d.slug, c.rating, c.note
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		  AND c.visibility IN ('public', 'friends')
		ORDER BY c.created_at DESC
		LIMIT 40`, row.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items strings.Builder
	for rows.Next() {
		var (
			id, createdAt, drinkName, drinkSlug, note string
			rating                                    sql.NullInt64
		)
		if err := rows.Scan(&id, &createdAt, &drinkName, &drinkSlug, &rating, &note); err != nil {
			return nil, err
		}
		link := s.baseURL() + "/drinks/" + url.PathEscape(drinkSlug)
		title := "@" + handle + " drank " + drinkName
		if rating.Valid {
			title = fmt.Sprintf("@%s rated %s %s/5", handle, drinkName, formatRating(int(rating.Int64)))
		}
		description := note
		if description == "" {
			description = title
		}
		pubDate := createdAt
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			pubDate = t.UTC().Format(time.RFC1123Z)
		}

		items.WriteString("    <item>\n")
		items.WriteString("      <title>" + escapeXML(title) + "</title>\n")
		items.WriteString("      <link>" + escapeXML(link) + "</link>\n")
		items.WriteString("      <guid isPermaLink=\"false\">" + escapeXML(id) + "</guid>\n")
		items.WriteString("      <pubDate>" + escapeXML(pubDate) + "</pubDate>\n")
		items.WriteString("      <description>" + escapeXML(description) + "</description>\n")
		items.WriteString("    </item>\n")
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	channelTitle := "Sodapop — @" + handle
	channelLink := s.baseURL() + "/"
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	out.WriteString(`<rss version="2.0">` + "\n")
	out.WriteString("  <channel>\n")
	out.WriteString("    <title>" + escapeXML(channelTitle) + "</title>\n")
	out.WriteString("    <link>" + escapeXML(channelLink) + "</link>\n")
	out.WriteString("    <description>" + escapeXML("Recent sodas from @"+handle+" on Sodapop") + "</description>\n")
	out.WriteString("    <lastBuildDate>" + escapeXML(time.Now().UTC().Format(time.RFC1123Z)) + "</lastBuildDate>\n")
	out.WriteString(items.String())
	out.WriteString("  </channel>\n")
	out.WriteString("</rss>\n")
	return []byte(out.String()), nil
}

func (s *Service) feedURL(token string) string {
	return s.baseURL() + "/feeds/" + token + ".xml"
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func escapeXML(v string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(v)); err != nil {
		return ""
	}
	return b.String()
}

func formatRating(quarterStars int) string {
	return strconv.FormatFloat(float64(quarterStars)/4, 'f', -1, 64)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
