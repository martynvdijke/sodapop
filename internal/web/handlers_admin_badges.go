package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sodapop/internal/badges"
)

type adminBadgeRule struct {
	Key   string
	Label string
}

// adminBadgeKinds is the fixed badge kind enum shown in the admin form.
var adminBadgeKinds = []string{"core", "local", "special"}

// adminBadgeRules maps each evaluator counter to human copy.
var adminBadgeRules = []adminBadgeRule{
	{Key: "checkins", Label: "Check-ins"},
	{Key: "distinct_styles", Label: "Distinct styles"},
	{Key: "distinct_brands", Label: "Distinct brands"},
	{Key: "distinct_countries", Label: "Distinct countries"},
	{Key: "distinct_venue_kinds", Label: "Distinct venue kinds"},
	{Key: "friends", Label: "Friends"},
	{Key: "streak_current", Label: "Current streak (days)"},
	{Key: "cans", Label: "Cans"},
	{Key: "glass", Label: "Glasses"},
	{Key: "bottle", Label: "Bottles"},
	{Key: "fountain", Label: "Fountain serves"},
	{Key: "sparkling_water", Label: "Sparkling water"},
	{Key: "late_night", Label: "Late-night check-ins"},
}

// adminBadgesPage renders every badge definition with its award count.
func (s *Server) adminBadgesPage(w http.ResponseWriter, r *http.Request) {
	notice, errMsg := adminBadgeQueryMessages(r)
	s.renderAdminBadges(w, r, http.StatusOK, errMsg, notice)
}

func (s *Server) renderAdminBadges(w http.ResponseWriter, r *http.Request, status int, errMsg, notice string) {
	rows, err := s.Badges.AdminList(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, status, "admin_badges", map[string]any{
		"Title":  "Badges",
		"Badges": rows,
		"Kinds":  adminBadgeKinds,
		"Rules":  adminBadgeRules,
		"Notice": notice,
		"Error":  errMsg,
	})
}

func adminBadgeQueryMessages(r *http.Request) (notice, errMsg string) {
	switch r.URL.Query().Get("created") {
	case "1":
		notice = "Badge created."
	}
	switch r.URL.Query().Get("updated") {
	case "1":
		notice = "Badge updated."
	}
	switch r.URL.Query().Get("deleted") {
	case "1":
		notice = "Badge deleted."
	}
	switch r.URL.Query().Get("error") {
	case "awarded":
		errMsg = "That badge has been awarded to users and cannot be deleted."
	case "missing":
		errMsg = "That badge no longer exists."
	case "invalid":
		errMsg = "Check the badge fields and try again."
	}
	return notice, errMsg
}

func (s *Server) adminBadgeCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	_, err := s.Badges.CreateBadge(r.Context(), adminBadgeInputFromForm(r))
	if err != nil {
		if errors.Is(err, badges.ErrBadgeInvalid) {
			s.renderAdminBadges(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/badges?created=1", http.StatusFound)
}

func (s *Server) adminBadgeUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.Badges.UpdateBadge(r.Context(), id, adminBadgeInputFromForm(r)); err != nil {
		switch {
		case errors.Is(err, badges.ErrBadgeInvalid):
			s.renderAdminBadges(w, r, http.StatusBadRequest, err.Error(), "")
		case errors.Is(err, badges.ErrBadgeNotFound):
			http.NotFound(w, r)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/admin/badges?updated=1", http.StatusFound)
}

func (s *Server) adminBadgeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.Badges.DeleteBadge(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, badges.ErrBadgeAwarded):
			http.Redirect(w, r, "/admin/badges?error=awarded", http.StatusFound)
		case errors.Is(err, badges.ErrBadgeNotFound):
			http.Redirect(w, r, "/admin/badges?error=missing", http.StatusFound)
		default:
			s.fail(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/admin/badges?deleted=1", http.StatusFound)
}

func adminBadgeInputFromForm(r *http.Request) badges.BadgeInput {
	rule := strings.TrimSpace(r.FormValue("rule"))
	if need := strings.TrimSpace(r.FormValue("need")); need != "" {
		rule += ">=" + need
	}
	return badges.BadgeInput{
		Slug:        r.FormValue("slug"),
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Kind:        r.FormValue("kind"),
		Rule:        rule,
	}
}
