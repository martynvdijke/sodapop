package web

import (
	"errors"
	"net/http"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
)

type notificationView struct {
	ID              string
	Kind            string
	ActorHandle     string
	ActorName       string
	ActorInitial    string
	Text            string
	Href            string
	CreatedAt       string
	Read            bool
	FriendRequestID string
}

func (s *Server) notificationsPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	rows, err := s.Social.Notifications(r.Context(), u.ID, 50)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Batch-load the actors so the page does not issue one query per row.
	actorIDs := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, n := range rows {
		if n.ActorID != nil && *n.ActorID != "" && !seen[*n.ActorID] {
			seen[*n.ActorID] = true
			actorIDs = append(actorIDs, *n.ActorID)
		}
	}
	actors := map[string]*ent.User{}
	if len(actorIDs) > 0 {
		users, err := s.Auth.Client.User.Query().
			Where(user.IDIn(actorIDs...)).
			All(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		for _, actor := range users {
			actors[actor.ID] = actor
		}
	}

	views := make([]notificationView, 0, len(rows))
	pending, err := s.Social.IncomingPendingRequests(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, n := range rows {
		v := notificationView{
			ID:        n.ID,
			Kind:      string(n.Kind),
			Text:      n.Text,
			Href:      notificationHref(string(n.Kind), n.SubjectID),
			CreatedAt: n.CreatedAt,
			Read:      n.ReadAt != nil,
		}
		if n.ActorID != nil {
			if actor, ok := actors[*n.ActorID]; ok {
				v.ActorHandle = actor.Handle
				v.ActorName = actor.DisplayName
				name := actor.DisplayName
				if name == "" {
					name = actor.Handle
				}
				v.ActorInitial = initial(name)
			}
			if id, ok := pending[*n.ActorID]; ok && string(n.Kind) == "friend_request" {
				v.FriendRequestID = id
			}
		}
		views = append(views, v)
	}

	unread, _ := s.Social.UnreadCount(r.Context(), u.ID)
	s.render(w, r, http.StatusOK, "notifications", map[string]any{
		"Title":         "Notifications",
		"Notifications": views,
		"Unread":        unread,
	})
}

func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Social.MarkRead(r.Context(), u.ID, r.FormValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusFound)
}

func (s *Server) checkinDetailPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	viewerID := ""
	if u != nil {
		viewerID = u.ID
	}
	detail, err := s.Checkins.Get(r.Context(), viewerID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, checkins.ErrNotFound) {
			s.render(w, r, http.StatusNotFound, "error", map[string]any{
				"Title": "Not found", "Message": "That checkin is not available.",
			})
			return
		}
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "checkin_detail", map[string]any{
		"Title":         detail.DrinkName,
		"Checkin":       detail,
		"Authenticated": u != nil,
		"CanDelete":     u != nil && u.ID == detail.UserID,
		"CanToast":      u != nil && u.ID != detail.UserID,
	})
}

func (s *Server) checkinDelete(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Checkins.Delete(r.Context(), u.ID, r.PathValue("id")); err != nil {
		if errors.Is(err, checkins.ErrNotFound) || errors.Is(err, checkins.ErrForbidden) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/you", http.StatusFound)
}

func notificationHref(kind, subjectID string) string {
	switch kind {
	case "toast":
		if subjectID != "" {
			return "/checkins/" + subjectID
		}
		return "/"
	case "badge":
		return "/you"
	case "friend_request", "friend_accept":
		return "/friends"
	default:
		return "/"
	}
}
