package venues

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sodapop/ent"
	"sodapop/ent/notification"
	"sodapop/ent/venuestockreport"
)

const (
	// shelfWindowDays is how far back evidence counts as "on the shelf". Older
	// reports and check-ins are ignored rather than shown as historical noise.
	shelfWindowDays = 30
	// shelfFreshDays is the upper bound of the fresh band.
	shelfFreshDays = 7
	// shelfEntryCap bounds one venue's rendered shelf.
	shelfEntryCap = 40
	// shelfNoteMaxRunes is the stored note limit.
	shelfNoteMaxRunes = 140
	// shelfMaxPriceCents keeps a typo like 15.00 booked as 1500 cents from
	// turning into an absurd price.
	shelfMaxPriceCents = 100000
	// shelfNearbyCandidateCap bounds the venues considered by NearbyShelf.
	shelfNearbyCandidateCap = 100
)

var (
	// ErrDrinkNotFound is returned when a shelf report names a drink slug that
	// does not resolve to a live drink.
	ErrDrinkNotFound = errors.New("drink not found")
	// ErrSeenAtFuture is returned when a report claims a future sighting.
	ErrSeenAtFuture = errors.New("seen_at is in the future")
	// ErrSeenAtTooOld is returned when a report is older than the shelf window.
	ErrSeenAtTooOld = errors.New("seen_at is too old")
)

// ShelfEntry is one drink known to be available at a venue, merged from member
// stock reports and recent check-ins.
type ShelfEntry struct {
	DrinkID    int     `json:"drink_id"`
	DrinkSlug  string  `json:"drink_slug"`
	DrinkName  string  `json:"drink_name"`
	Brand      string  `json:"brand"`
	Style      string  `json:"style"`
	PriceCents *int    `json:"price_cents"`
	Currency   string  `json:"currency"`
	Note       string  `json:"note"`
	SeenAt     string  `json:"seen_at"`
	AgeDays    int     `json:"age_days"`
	Band       string  `json:"band"`
	Reporters  int     `json:"reporters"`
	Mine       bool    `json:"mine"`
	ReportID   *string `json:"report_id"`
	Source     string  `json:"source"`
}

// ShelfReportInput is a member's report that a drink is on a venue's shelf.
// SeenAt is optional; when empty (and SeenNow is false) the sighting is taken
// as now, which is what the web form submits.
type ShelfReportInput struct {
	DrinkSlug  string
	PriceCents *int
	Currency   string
	Note       string
	SeenNow    bool
	SeenAt     string
}

// NearbyShelf pairs a venue with the fresh stock it currently carries.
type NearbyShelf struct {
	Venue     Venue
	Entries   []ShelfEntry
	DistanceM int
}

// shelfEvidenceRow is one raw report or check-in row before grouping.
type shelfEvidenceRow struct {
	drinkID  int
	slug     string
	name     string
	brand    string
	style    string
	userID   string
	seenAt   time.Time
	price    *int
	currency string
	note     string
	source   string
	reportID string
}

// Shelf merges live stock reports and recent check-ins for one venue into one
// row per drink. Evidence older than the shelf window is excluded from both
// sources. Ordering is fresh first, then reporter count descending, then drink
// name ascending, capped at shelfEntryCap.
func (s *Service) Shelf(ctx context.Context, venueID int, viewerID string, now time.Time) ([]ShelfEntry, error) {
	now = now.UTC()
	cutoff := now.Add(-shelfWindowDays * 24 * time.Hour).Format(time.RFC3339)

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT r.drink_id, d.slug, d.name, COALESCE(b.name,''), COALESCE(st.name,''),
		       r.user_id, r.seen_at, r.price_cents, COALESCE(r.currency,''), COALESCE(r.note,''), 'report', r.id
		FROM venue_stock_reports r
		JOIN drinks d ON d.id = r.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN styles st ON st.id = d.style_id
		WHERE r.venue_id = ? AND r.removed_at IS NULL AND r.seen_at >= ? AND d.status != 'merged'
		UNION ALL
		SELECT c.drink_id, d.slug, d.name, COALESCE(b.name,''), COALESCE(st.name,''),
		       c.user_id, c.created_at, NULL, '', '', 'checkin', NULL
		FROM checkins c
		JOIN drinks d ON d.id = c.drink_id
		LEFT JOIN brands b ON b.id = d.brand_id
		LEFT JOIN styles st ON st.id = d.style_id
		WHERE c.venue_id = ? AND c.deleted_at IS NULL AND c.created_at >= ? AND d.status != 'merged'
		ORDER BY 7 DESC`, venueID, cutoff, venueID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	evidence := []shelfEvidenceRow{}
	for rows.Next() {
		var r shelfEvidenceRow
		var seenAt string
		var price sql.NullInt64
		var reportID sql.NullString
		if err := rows.Scan(&r.drinkID, &r.slug, &r.name, &r.brand, &r.style,
			&r.userID, &seenAt, &price, &r.currency, &r.note, &r.source, &reportID); err != nil {
			return nil, err
		}
		at, err := time.Parse(time.RFC3339, seenAt)
		if err != nil {
			return nil, err
		}
		r.seenAt = at.UTC()
		if price.Valid {
			v := int(price.Int64)
			r.price = &v
		}
		if reportID.Valid {
			r.reportID = reportID.String
		}
		evidence = append(evidence, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return mergeShelf(evidence, viewerID, now), nil
}

// mergeShelf groups already time-descending evidence rows into shelf entries.
func mergeShelf(evidence []shelfEvidenceRow, viewerID string, now time.Time) []ShelfEntry {
	type group struct {
		entry      ShelfEntry
		seenAt     time.Time
		reporters  map[string]struct{}
		hasReport  bool
		hasCheckin bool
		mine       bool
	}
	cutoff := now.Add(-shelfWindowDays * 24 * time.Hour)
	groups := map[int]*group{}
	for _, row := range evidence {
		if row.seenAt.Before(cutoff) {
			continue
		}
		g := groups[row.drinkID]
		if g == nil {
			g = &group{
				entry: ShelfEntry{
					DrinkID:   row.drinkID,
					DrinkSlug: row.slug,
					DrinkName: row.name,
					Brand:     row.brand,
					Style:     row.style,
					SeenAt:    row.seenAt.Format(time.RFC3339),
				},
				seenAt:    row.seenAt,
				reporters: map[string]struct{}{},
			}
			groups[row.drinkID] = g
		}
		g.reporters[row.userID] = struct{}{}
		if row.source == "report" {
			g.hasReport = true
			if g.entry.PriceCents == nil && row.price != nil {
				v := *row.price
				g.entry.PriceCents = &v
				g.entry.Currency = row.currency
			}
			if g.entry.Note == "" && strings.TrimSpace(row.note) != "" {
				g.entry.Note = row.note
			}
			if row.userID == viewerID {
				g.mine = true
				id := row.reportID
				g.entry.ReportID = &id
			}
		} else {
			g.hasCheckin = true
		}
	}

	out := make([]ShelfEntry, 0, len(groups))
	for _, g := range groups {
		e := g.entry
		e.Reporters = len(g.reporters)
		e.Mine = g.mine
		switch {
		case g.hasReport && g.hasCheckin:
			e.Source = "both"
		case g.hasReport:
			e.Source = "report"
		default:
			e.Source = "checkin"
		}
		age := int(now.Sub(g.seenAt).Hours() / 24)
		if age < 0 {
			age = 0
		}
		e.AgeDays = age
		if age <= shelfFreshDays {
			e.Band = "fresh"
		} else {
			e.Band = "stale"
		}
		out = append(out, e)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Band != b.Band {
			return a.Band == "fresh"
		}
		if a.Reporters != b.Reporters {
			return a.Reporters > b.Reporters
		}
		if a.DrinkName != b.DrinkName {
			return a.DrinkName < b.DrinkName
		}
		return a.DrinkID < b.DrinkID
	})
	if len(out) > shelfEntryCap {
		out = out[:shelfEntryCap]
	}
	return out
}

// ReportShelf upserts the caller's report for one drink at one venue and
// returns the merged shelf row it produces.
func (s *Service) ReportShelf(ctx context.Context, userID string, venueID int, in ShelfReportInput) (*ShelfEntry, error) {
	now := time.Now().UTC()

	slug := strings.TrimSpace(in.DrinkSlug)
	drinkID, err := s.liveDrinkID(ctx, slug)
	if err != nil {
		return nil, err
	}

	var hidden int
	var merged sql.NullInt64
	err = s.SQL.QueryRowContext(ctx,
		`SELECT hidden, merged_into_id FROM venues WHERE id = ?`, venueID).
		Scan(&hidden, &merged)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if hidden != 0 || merged.Valid {
		return nil, ErrInvalid
	}

	if in.PriceCents != nil && (*in.PriceCents < 0 || *in.PriceCents > shelfMaxPriceCents) {
		return nil, fmt.Errorf("%w: price must be between 0 and 1000", ErrInvalid)
	}
	currency, err := normalizeShelfCurrency(in.Currency)
	if err != nil {
		return nil, err
	}
	note, err := validateShelfNote(in.Note)
	if err != nil {
		return nil, err
	}

	seenAt, err := shelfSeenAt(in, now)
	if err != nil {
		return nil, err
	}
	seenStr := seenAt.Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	existing, err := s.Client.VenueStockReport.Query().
		Where(
			venuestockreport.VenueIDEQ(venueID),
			venuestockreport.DrinkIDEQ(drinkID),
			venuestockreport.UserIDEQ(userID),
		).
		Only(ctx)
	switch {
	case err == nil:
		upd := s.Client.VenueStockReport.UpdateOneID(existing.ID).
			SetSeenAt(seenStr).
			SetUpdatedAt(nowStr)
		if existing.RemovedAt != nil {
			upd.ClearRemovedAt()
		}
		if in.PriceCents != nil {
			upd.SetPriceCents(*in.PriceCents)
			upd.SetCurrency(currency)
		} else {
			upd.ClearPriceCents()
			upd.SetCurrency("")
		}
		if note != "" {
			upd.SetNote(note)
		} else {
			upd.ClearNote()
		}
		if _, err := upd.Save(ctx); err != nil {
			return nil, err
		}
	case ent.IsNotFound(err):
		create := s.Client.VenueStockReport.Create().
			SetVenueID(venueID).
			SetDrinkID(drinkID).
			SetUserID(userID).
			SetSeenAt(seenStr).
			SetCreatedAt(nowStr).
			SetUpdatedAt(nowStr)
		if in.PriceCents != nil {
			create.SetPriceCents(*in.PriceCents)
			create.SetCurrency(currency)
		}
		if note != "" {
			create.SetNote(note)
		}
		if _, err := create.Save(ctx); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}

	// Availability alerts are in-app only and best effort. They are emitted
	// after the report row is committed so a notification failure can never
	// make the report itself fail.
	s.notifyShelfWatchers(ctx, userID, venueID, drinkID)

	entries, err := s.Shelf(ctx, venueID, userID, now)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].DrinkID == drinkID {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// RetractShelf soft-removes the caller's own live report. It reports false when
// the report does not exist or was already retracted.
func (s *Service) RetractShelf(ctx context.Context, userID, reportID string) (bool, error) {
	reportID = strings.TrimSpace(reportID)
	if reportID == "" {
		return false, nil
	}
	n, err := s.Client.VenueStockReport.Update().
		Where(
			venuestockreport.IDEQ(reportID),
			venuestockreport.UserIDEQ(userID),
			venuestockreport.RemovedAtIsNil(),
		).
		SetRemovedAt(time.Now().UTC().Format(time.RFC3339)).
		SetUpdatedAt(time.Now().UTC().Format(time.RFC3339)).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// NearbyShelf returns nearby venues that currently carry fresh stock, nearest
// first. Coordinates are rounded to three decimals before use and are never
// persisted. Distances are floored to 100 m.
func (s *Service) NearbyShelf(ctx context.Context, viewerID string, lat, lng, radiusKM float64, limit int) ([]NearbyShelf, error) {
	out := []NearbyShelf{}
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 ||
		math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return out, nil
	}
	if math.IsNaN(radiusKM) || math.IsInf(radiusKM, 0) || radiusKM <= 0 {
		return out, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	lat = roundShelfCoord(lat)
	lng = roundShelfCoord(lng)

	box := venueSearchBoxFor(lat, lng, radiusKM)
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT v.id, v.slug, v.name, v.kind, COALESCE(v.address,''), COALESCE(v.city,''),
		       COALESCE(v.region,''), COALESCE(v.country,''), v.lat, v.lng, COALESCE(v.website,'')
		FROM venues v
		WHERE v.merged_into_id IS NULL AND v.hidden = 0
		  AND v.lat BETWEEN ? AND ?
		  AND `+box.lngCondition()+`
		LIMIT ?`,
		box.latMin, box.latMax, box.lngMin, box.lngMax, shelfNearbyCandidateCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		venue    Venue
		distance float64
	}
	venues := []candidate{}
	for rows.Next() {
		var v Venue
		if err := rows.Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Address, &v.City,
			&v.Region, &v.Country, &v.Lat, &v.Lng, &v.Website); err != nil {
			return nil, err
		}
		d := distanceMeters(lat, lng, v.Lat, v.Lng)
		if d > radiusKM*1000 {
			continue
		}
		venues = append(venues, candidate{venue: v, distance: d})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	results := []NearbyShelf{}
	for _, c := range venues {
		entries, err := s.Shelf(ctx, c.venue.ID, viewerID, now)
		if err != nil {
			return nil, err
		}
		fresh := make([]ShelfEntry, 0, len(entries))
		for _, e := range entries {
			if e.Band == "fresh" {
				fresh = append(fresh, e)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		results = append(results, NearbyShelf{
			Venue:     c.venue,
			Entries:   fresh,
			DistanceM: int(math.Floor(c.distance/100.0)) * 100,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].DistanceM != results[j].DistanceM {
			return results[i].DistanceM < results[j].DistanceM
		}
		if results[i].Venue.Name != results[j].Venue.Name {
			return results[i].Venue.Name < results[j].Venue.Name
		}
		return results[i].Venue.ID < results[j].Venue.ID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// shelfAlertWindow is the per-(recipient, venue) cooldown for shelf alerts. The
// underlying drink rides in the text, so one venue ping a week is the useful
// granularity.
const shelfAlertWindow = 7 * 24 * time.Hour

// notifyShelfWatchers tells members who saved this venue — and have the drink
// on their wishlist with shelf alerts enabled — that it is in stock. The @home
// marker is a single shared hidden venue and ReportShelf refuses hidden venues,
// so saved venues are the only possible audience here; there is no per-user
// home venue to match against.
//
// It deliberately writes the notification row directly rather than going
// through social.Notify: these kinds must stay in-app only and must never reach
// the push/delivery callback. Errors are logged and swallowed because the
// report is already committed.
func (s *Service) notifyShelfWatchers(ctx context.Context, reporterID string, venueID, drinkID int) {
	var venueName, drinkName string
	if err := s.SQL.QueryRowContext(ctx, `SELECT name FROM venues WHERE id = ?`, venueID).Scan(&venueName); err != nil {
		slog.ErrorContext(ctx, "shelf alert: venue name", "err", err, "venue_id", venueID)
		return
	}
	if err := s.SQL.QueryRowContext(ctx, `SELECT name FROM drinks WHERE id = ?`, drinkID).Scan(&drinkName); err != nil {
		slog.ErrorContext(ctx, "shelf alert: drink name", "err", err, "drink_id", drinkID)
		return
	}

	rows, err := s.SQL.QueryContext(ctx, `
		SELECT DISTINCT l.user_id
		FROM lists l
		JOIN list_items li ON li.list_id = l.id
		JOIN user_settings us ON us.user_id = l.user_id
		WHERE l.is_system = 1 AND l.slug = 'wishlist'
		  AND li.drink_id = ? AND us.wishlist_shelf_alerts = 1
		  AND l.user_id <> ?
		  AND EXISTS (
		      SELECT 1 FROM venue_saves vs
		      WHERE vs.user_id = l.user_id AND vs.venue_id = ?
		  )`, drinkID, reporterID, venueID)
	if err != nil {
		slog.ErrorContext(ctx, "shelf alert: wishlist query", "err", err, "venue_id", venueID, "drink_id", drinkID)
		return
	}
	recipients := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "shelf alert: scan", "err", err)
			return
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "shelf alert: rows", "err", err)
		return
	}
	if len(recipients) == 0 {
		return
	}

	blocked, err := s.blockedFor(ctx, reporterID)
	if err != nil {
		slog.ErrorContext(ctx, "shelf alert: blocks", "err", err, "user_id", reporterID)
		return
	}

	text := fmt.Sprintf("%s reported %s in stock", venueName, drinkName)
	subject := strconv.Itoa(venueID)
	cutoff := time.Now().UTC().Add(-shelfAlertWindow).Format(time.RFC3339)
	for _, recipient := range recipients {
		if blocked[recipient] {
			continue
		}
		// Plain SELECT-then-INSERT: single process, and a duplicate stock alert
		// is harmless, so this needs no unique index.
		exists, err := s.Client.Notification.Query().
			Where(
				notification.UserID(recipient),
				notification.KindEQ(notification.KindWishlistShelf),
				notification.SubjectID(subject),
				notification.CreatedAtGT(cutoff),
			).
			Exist(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "shelf alert: dedupe", "err", err, "user_id", recipient)
			continue
		}
		if exists {
			continue
		}
		if _, err := s.Client.Notification.Create().
			SetUserID(recipient).
			SetKind(notification.KindWishlistShelf).
			SetActorID(reporterID).
			SetSubjectID(subject).
			SetText(text).
			Save(ctx); err != nil {
			slog.ErrorContext(ctx, "shelf alert: notify", "err", err, "user_id", recipient)
		}
	}
}

// blockedFor returns every account in a block relationship with userID.
func (s *Service) blockedFor(ctx context.Context, userID string) (map[string]bool, error) {
	out := map[string]bool{}
	if userID == "" {
		return out, nil
	}
	rows, err := s.SQL.QueryContext(ctx, `
		SELECT CASE WHEN blocker_id = ? THEN blocked_id ELSE blocker_id END
		FROM blocks WHERE blocker_id = ? OR blocked_id = ?`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// liveDrinkID resolves a slug to a non-merged drink.
func (s *Service) liveDrinkID(ctx context.Context, slug string) (int, error) {
	if slug == "" {
		return 0, ErrDrinkNotFound
	}
	var id int
	err := s.SQL.QueryRowContext(ctx,
		`SELECT id FROM drinks WHERE slug = ? AND status != 'merged'`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrDrinkNotFound
	}
	return id, err
}

// shelfSeenAt chooses and validates the sighting timestamp for a report.
func shelfSeenAt(in ShelfReportInput, now time.Time) (time.Time, error) {
	if in.SeenNow || strings.TrimSpace(in.SeenAt) == "" {
		return now, nil
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(in.SeenAt))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: seen_at must be RFC 3339", ErrInvalid)
	}
	at = at.UTC()
	if at.After(now) {
		return time.Time{}, ErrSeenAtFuture
	}
	if now.Sub(at) > shelfWindowDays*24*time.Hour {
		return time.Time{}, ErrSeenAtTooOld
	}
	return at, nil
}

// validateShelfNote trims and bounds a note.
func validateShelfNote(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if utf8.RuneCountInString(note) > shelfNoteMaxRunes {
		return "", fmt.Errorf("%w: note is longer than %d characters", ErrInvalid, shelfNoteMaxRunes)
	}
	return note, nil
}

// normalizeShelfCurrency upper-cases a currency code and requires the ISO-4217
// shape: empty or exactly three ASCII letters. The check-ins package keeps the
// same rule for check-in prices; it cannot be shared because checkins already
// imports venues.
func normalizeShelfCurrency(raw string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", nil
	}
	if len(code) != 3 {
		return "", fmt.Errorf("%w: currency must be a three-letter ISO-4217 code", ErrInvalid)
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return "", fmt.Errorf("%w: currency must be a three-letter ISO-4217 code", ErrInvalid)
		}
	}
	return code, nil
}

// roundShelfCoord rounds a coordinate to three decimal places.
func roundShelfCoord(v float64) float64 {
	return math.Round(v*1000) / 1000
}
