package integrations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/ent/usersetting"
	"sodapop/internal/db"
)

type exportData struct {
	ExportedAt string          `json:"exported_at"`
	Profile    exportProfile   `json:"profile"`
	Checkins   []exportCheckin `json:"checkins"`
	Lists      []exportList    `json:"lists"`
	Badges     []exportBadge   `json:"badges"`
	Settings   *exportSettings `json:"settings,omitempty"`
}

type exportProfile struct {
	ID                string  `json:"id"`
	Handle            string  `json:"handle"`
	Email             string  `json:"email"`
	EmailVerifiedAt   *string `json:"email_verified_at,omitempty"`
	DisplayName       string  `json:"display_name"`
	Bio               string  `json:"bio"`
	Role              string  `json:"role"`
	Status            string  `json:"status"`
	DefaultVisibility string  `json:"default_visibility"`
	AnalyticsOptOut   bool    `json:"analytics_opt_out"`
	Country           *string `json:"country,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type exportCheckin struct {
	ID             string  `json:"id"`
	DrinkID        int     `json:"drink_id"`
	DrinkSlug      string  `json:"drink_slug"`
	DrinkName      string  `json:"drink_name"`
	VenueID        *int    `json:"venue_id,omitempty"`
	Rating         *int    `json:"rating,omitempty"`
	Serving        string  `json:"serving"`
	Note           string  `json:"note,omitempty"`
	Visibility     string  `json:"visibility"`
	PhotoMediaID   *string `json:"photo_media_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
}

type exportList struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Slug        string           `json:"slug"`
	Description string           `json:"description"`
	Visibility  string           `json:"visibility"`
	IsSystem    bool             `json:"is_system"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
	Items       []exportListItem `json:"items"`
}

type exportListItem struct {
	DrinkID   int    `json:"drink_id"`
	DrinkSlug string `json:"drink_slug"`
	DrinkName string `json:"drink_name"`
	Note      string `json:"note,omitempty"`
	AddedAt   string `json:"added_at"`
}

type exportBadge struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Level       int    `json:"level"`
	AwardedAt   string `json:"awarded_at"`
}

type exportSettings struct {
	EmailFriendReq bool   `json:"email_friend_req"`
	EmailWeekly    bool   `json:"email_weekly"`
	PushEnabled    bool   `json:"push_enabled"`
	Theme          string `json:"theme"`
	UpdatedAt      string `json:"updated_at"`
}

// ExportJSON returns only the user's own profile, check-ins, lists, badges and
// settings. Password hashes, sessions, tokens and other users' data are never
// included.
func (s *Service) ExportJSON(ctx context.Context, userID string) ([]byte, error) {
	u, err := s.Client.User.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	data := exportData{
		ExportedAt: db.Now(),
		Profile: exportProfile{
			ID:                u.ID,
			Handle:            u.Handle,
			Email:             u.Email,
			EmailVerifiedAt:   u.EmailVerifiedAt,
			DisplayName:       u.DisplayName,
			Bio:               u.Bio,
			Role:              string(u.Role),
			Status:            string(u.Status),
			DefaultVisibility: string(u.DefaultVisibility),
			AnalyticsOptOut:   u.AnalyticsOptOut,
			Country:           u.Country,
			CreatedAt:         u.CreatedAt,
			UpdatedAt:         u.UpdatedAt,
		},
		Checkins: []exportCheckin{},
		Lists:    []exportList{},
		Badges:   []exportBadge{},
	}
	if data.Checkins, err = s.exportCheckins(ctx, userID); err != nil {
		return nil, err
	}
	if data.Lists, err = s.exportLists(ctx, userID); err != nil {
		return nil, err
	}
	if data.Badges, err = s.exportBadges(ctx, userID); err != nil {
		return nil, err
	}
	settings, err := s.Client.UserSetting.Query().
		Where(usersetting.UserID(userID)).
		Only(ctx)
	switch {
	case err == nil:
		data.Settings = &exportSettings{
			EmailFriendReq: settings.EmailFriendReq,
			EmailWeekly:    settings.EmailWeekly,
			PushEnabled:    settings.PushEnabled,
			Theme:          string(settings.Theme),
			UpdatedAt:      settings.UpdatedAt,
		}
	case ent.IsNotFound(err):
		// No settings row; leave it out.
	default:
		return nil, err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Service) exportCheckins(ctx context.Context, userID string) ([]exportCheckin, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT c.id, c.drink_id, d.slug, d.name, c.venue_id, c.rating, c.serving,
		       c.note, c.visibility, c.photo_media_id, c.idempotency_key,
		       c.created_at, c.updated_at
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		WHERE c.user_id = ? AND c.deleted_at IS NULL
		ORDER BY c.created_at DESC, c.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []exportCheckin{}
	for rows.Next() {
		var (
			item                 exportCheckin
			venueID, rating      sql.NullInt64
			photoID, idempotency sql.NullString
		)
		if err := rows.Scan(&item.ID, &item.DrinkID, &item.DrinkSlug, &item.DrinkName,
			&venueID, &rating, &item.Serving, &item.Note, &item.Visibility,
			&photoID, &idempotency, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if venueID.Valid {
			v := int(venueID.Int64)
			item.VenueID = &v
		}
		if rating.Valid {
			v := int(rating.Int64)
			item.Rating = &v
		}
		if photoID.Valid {
			v := photoID.String
			item.PhotoMediaID = &v
		}
		if idempotency.Valid {
			v := idempotency.String
			item.IdempotencyKey = &v
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) exportLists(ctx context.Context, userID string) ([]exportList, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT l.id, l.name, l.slug, l.description, l.visibility, l.is_system,
		       l.created_at, l.updated_at,
		       li.drink_id, COALESCE(d.slug, ''), COALESCE(d.name, ''),
		       COALESCE(li.note, ''), COALESCE(li.added_at, '')
		FROM lists l
		LEFT JOIN list_items li ON li.list_id = l.id
		LEFT JOIN drinks d ON d.id = li.drink_id
		WHERE l.user_id = ?
		ORDER BY l.created_at, l.id, li.added_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []exportList{}
	index := map[string]int{}
	for rows.Next() {
		var (
			list                  exportList
			itemDrinkID           sql.NullInt64
			itemSlug, itemName    string
			itemNote, itemAddedAt string
		)
		if err := rows.Scan(&list.ID, &list.Name, &list.Slug, &list.Description,
			&list.Visibility, &list.IsSystem, &list.CreatedAt, &list.UpdatedAt,
			&itemDrinkID, &itemSlug, &itemName, &itemNote, &itemAddedAt); err != nil {
			return nil, err
		}
		pos, exists := index[list.ID]
		if !exists {
			list.Items = []exportListItem{}
			out = append(out, list)
			pos = len(out) - 1
			index[list.ID] = pos
		}
		if itemDrinkID.Valid {
			out[pos].Items = append(out[pos].Items, exportListItem{
				DrinkID:   int(itemDrinkID.Int64),
				DrinkSlug: itemSlug,
				DrinkName: itemName,
				Note:      itemNote,
				AddedAt:   itemAddedAt,
			})
		}
	}
	return out, rows.Err()
}

func (s *Service) exportBadges(ctx context.Context, userID string) ([]exportBadge, error) {
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT b.slug, b.name, b.description, b.kind, ub.level, ub.awarded_at
		FROM user_badges ub
		JOIN badges b ON b.id = ub.badge_id
		WHERE ub.user_id = ?
		ORDER BY ub.awarded_at, b.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []exportBadge{}
	for rows.Next() {
		var b exportBadge
		if err := rows.Scan(&b.Slug, &b.Name, &b.Description, &b.Kind, &b.Level, &b.AwardedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ImportCSV inserts check-ins from a CSV with the columns drink_slug, rating,
// serving, note and created_at. Ratings are decimal stars (0-5) and are stored
// as quarter-star integers. Repeated imports are idempotent.
func (s *Service) ImportCSV(ctx context.Context, userID string, r io.Reader) (int, error) {
	u, err := s.Client.User.Get(ctx, userID)
	if err != nil {
		return 0, err
	}
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("read csv header: %w", err)
	}
	cols := map[string]int{}
	for i, name := range header {
		if i == 0 {
			name = strings.TrimPrefix(name, "\ufeff")
		}
		cols[strings.ToLower(strings.TrimSpace(name))] = i
	}
	if _, ok := cols["drink_slug"]; !ok {
		return 0, fmt.Errorf("csv is missing the drink_slug column")
	}
	get := func(row []string, name string) string {
		i, ok := cols[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	inserted := 0
	line := 1
	for {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		line++
		if readErr != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: %w", line, readErr)
		}

		slug := get(row, "drink_slug")
		if slug == "" {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: drink_slug is required", line)
		}
		d, err := tx.Drink.Query().
			Where(drink.Slug(slug), drink.StatusNEQ(drink.StatusMerged)).
			Only(ctx)
		if err != nil {
			_ = tx.Rollback()
			if ent.IsNotFound(err) {
				return 0, fmt.Errorf("row %d: drink %q not found", line, slug)
			}
			return 0, fmt.Errorf("row %d: %w", line, err)
		}
		rating, err := parseImportRating(get(row, "rating"))
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: %w", line, err)
		}
		serving, err := parseImportServing(get(row, "serving"))
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: %w", line, err)
		}
		note := get(row, "note")
		if len(note) > 1000 {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: note is too long", line)
		}
		createdAt, err := parseImportTime(get(row, "created_at"))
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: %w", line, err)
		}

		// Hash the raw created_at value so importing a file without timestamps is
		// still idempotent (the resolved timestamp would change every run).
		key := importKey(userID, d.ID, rating, string(serving), note, get(row, "created_at"))
		exists, err := tx.Checkin.Query().
			Where(checkin.IdempotencyKey(key)).
			Exist(ctx)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		if exists {
			continue
		}

		builder := tx.Checkin.Create().
			SetUserID(userID).
			SetDrinkID(d.ID).
			SetServing(serving).
			SetNote(note).
			SetVisibility(defaultVisibility(u)).
			SetIdempotencyKey(key).
			SetCreatedAt(createdAt).
			SetUpdatedAt(createdAt)
		if rating != nil {
			builder.SetRating(*rating)
		}
		if _, err := builder.Save(ctx); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("row %d: %w", line, err)
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func defaultVisibility(u *ent.User) checkin.Visibility {
	switch string(u.DefaultVisibility) {
	case "friends":
		return checkin.VisibilityFriends
	case "private":
		return checkin.VisibilityPrivate
	default:
		return checkin.VisibilityPublic
	}
}

func parseImportRating(raw string) (*int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, fmt.Errorf("rating %q is not a number", raw)
	}
	if value < 0 || value > 5 {
		return nil, fmt.Errorf("rating %q must be between 0 and 5", raw)
	}
	quarter := int(math.Round(value * 4))
	if quarter < 0 {
		quarter = 0
	}
	if quarter > 20 {
		quarter = 20
	}
	return &quarter, nil
}

func parseImportServing(raw string) (checkin.Serving, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "can":
		return checkin.ServingCan, nil
	case "bottle":
		return checkin.ServingBottle, nil
	case "glass":
		return checkin.ServingGlass, nil
	case "fountain":
		return checkin.ServingFountain, nil
	case "other":
		return checkin.ServingOther, nil
	default:
		return "", fmt.Errorf("unknown serving %q", raw)
	}
}

func parseImportTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return db.Now(), nil
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("created_at %q is not RFC 3339", raw)
}

func importKey(userID string, drinkID int, rating *int, serving, note, createdAt string) string {
	ratingPart := ""
	if rating != nil {
		ratingPart = strconv.Itoa(*rating)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		userID,
		strconv.Itoa(drinkID),
		ratingPart,
		serving,
		note,
		createdAt,
	}, "\x00")))
	return "import-" + hex.EncodeToString(sum[:16])
}
