package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/list"
	"sodapop/ent/listitem"
	"sodapop/ent/notification"
	"sodapop/internal/catalog"
)

var (
	errLimitedUntilRequired = errors.New("limited_until is required for limited or seasonal drinks")
	errLimitedUntilFormat   = errors.New("limited_until must be YYYY-MM-DD")
)

// limitedUntilFor validates the admin-supplied date. Limited and seasonal
// drinks must carry one; an empty value clears the date for other statuses.
func limitedUntilFor(status drink.Status, raw string) (*string, error) {
	raw = strings.TrimSpace(raw)
	needsDate := status == drink.StatusLimited || status == drink.StatusSeasonal
	if raw == "" {
		if needsDate {
			return nil, errLimitedUntilRequired
		}
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, errLimitedUntilFormat
	}
	if !needsDate {
		return nil, nil
	}
	v := t.UTC().Format(time.RFC3339)
	return &v, nil
}

// adminDrinkStatus moves a drink between the lifecycle statuses an admin can
// choose. Discontinuing a drink notifies the owners of system wishlists that
// contained it; every other failure is non-fatal to the status change.
func (s *Server) adminDrinkStatus(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	status := drink.Status(strings.TrimSpace(r.FormValue("status")))
	switch status {
	case drink.StatusActive, drink.StatusLimited, drink.StatusSeasonal, drink.StatusDiscontinued:
	default:
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	limitedUntil, err := limitedUntilFor(status, r.FormValue("limited_until"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	row, err := s.Auth.Client.Drink.Get(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	previous := row.Status

	actor := UserFromContext(r)
	var actorID *string
	if actor != nil {
		actorID = &actor.ID
	}
	until := ""
	if limitedUntil != nil {
		until = *limitedUntil
	}
	if err := s.Catalog.SetDrinkStatus(r.Context(), actorID, id, string(status), until); err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	if status == drink.StatusDiscontinued && previous != drink.StatusDiscontinued {
		s.notifyWishlistDiscontinued(r, id, row.Name)
	}

	http.Redirect(w, r, "/drinks/"+row.Slug, http.StatusFound)
}

// adminDrinkPublish toggles whether a drink is served by the public catalog
// API. It records the change through the catalog service and returns the admin
// to the drink page they came from.
func (s *Server) adminDrinkPublish(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	var published bool
	switch strings.TrimSpace(r.FormValue("published")) {
	case "1":
		published = true
	case "0":
		published = false
	default:
		http.Error(w, "invalid published value", http.StatusBadRequest)
		return
	}

	row, err := s.Auth.Client.Drink.Get(r.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	actor := UserFromContext(r)
	var actorID *string
	if actor != nil {
		actorID = &actor.ID
	}
	if err := s.Catalog.SetDrinkPublished(r.Context(), actorID, id, published); err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	flag := "0"
	if published {
		flag = "1"
	}
	http.Redirect(w, r, "/drinks/"+row.Slug+"?published="+flag, http.StatusFound)
}

// notifyWishlistDiscontinued tells each distinct owner of a system list that
// held the drink. Notifications are best effort and idempotent per
// user+kind+subject.
func (s *Server) notifyWishlistDiscontinued(r *http.Request, drinkID int, drinkName string) {
	ctx := r.Context()
	admin := UserFromContext(r)

	lists, err := s.Auth.Client.List.Query().
		Where(
			list.IsSystem(true),
			list.HasItemsWith(listitem.DrinkID(drinkID)),
		).
		All(ctx)
	if err != nil {
		return
	}

	subject := strconv.Itoa(drinkID)
	text := "A soda on your wishlist was discontinued: " + drinkName
	seen := map[string]bool{}
	for _, l := range lists {
		owner := l.UserID
		if owner == "" || seen[owner] {
			continue
		}
		seen[owner] = true
		if admin != nil && owner == admin.ID {
			continue
		}
		exists, err := s.Auth.Client.Notification.Query().
			Where(
				notification.UserID(owner),
				notification.KindEQ(notification.KindSystem),
				notification.SubjectID(subject),
			).
			Exist(ctx)
		if err != nil || exists {
			continue
		}
		_ = s.Social.Notify(ctx, owner, "system", "", subject, text)
	}
}
