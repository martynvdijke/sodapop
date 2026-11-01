package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sodapop/ent"
	"sodapop/ent/user"
	"sodapop/internal/checkins"
	"sodapop/internal/comments"
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
	s.renderCheckinDetail(w, r, http.StatusOK, commentErrorFromQuery(r.URL.Query().Get("comment_error")))
}

// renderCheckinDetail loads a visible check-in, its comments, and the viewer's
// per-comment capabilities. It backs both the GET page and inline validation
// errors from the comment form.
func (s *Server) renderCheckinDetail(w http.ResponseWriter, r *http.Request, status int, commentError string) {
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

	before := strings.TrimSpace(r.URL.Query().Get("before"))
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil && n > 0 {
			limit = n
		}
	}
	list, total, hasMore, err := s.commentService().ListPage(r.Context(), viewerID, detail.ID, before, limit)
	if err != nil {
		if errors.Is(err, comments.ErrNotFound) {
			s.render(w, r, http.StatusNotFound, "error", map[string]any{
				"Title": "Not found", "Message": "That checkin is not available.",
			})
			return
		}
		if errors.Is(err, comments.ErrInvalid) {
			// A stale or foreign cursor is treated as the newest page rather
			// than an error page.
			before = ""
			list, total, hasMore, err = s.commentService().ListPage(r.Context(), viewerID, detail.ID, "", limit)
		}
		if err != nil {
			if errors.Is(err, comments.ErrNotFound) {
				s.render(w, r, http.StatusNotFound, "error", map[string]any{
					"Title": "Not found", "Message": "That checkin is not available.",
				})
				return
			}
			s.fail(w, r, err)
			return
		}
	}
	admin := u != nil && (u.Role == user.RoleAdmin || u.Role == user.RoleModerator)
	for i := range list {
		list[i].CanDelete = admin || (u != nil && list[i].UserID == u.ID)
		list[i].CanReport = u != nil && u.EmailVerifiedAt != nil && list[i].UserID != u.ID
	}

	beforeCursor := ""
	if hasMore && len(list) > 0 {
		beforeCursor = list[0].ID
	}

	saved := false
	if u != nil {
		saved, _ = s.Saves.IsSaved(r.Context(), u.ID, detail.ID)
	}

	s.render(w, r, status, "checkin_detail", map[string]any{
		"Title":          detail.DrinkName,
		"Checkin":        detail,
		"Authenticated":  u != nil,
		"CanDelete":      u != nil && u.ID == detail.UserID,
		"CanToast":       u != nil && u.ID != detail.UserID,
		"CanReport":      u != nil && u.EmailVerifiedAt != nil && u.ID != detail.UserID,
		"CanSave":        u != nil,
		"Saved":          saved,
		"Comments":       list,
		"CommentCount":   total,
		"HasEarlier":     hasMore,
		"BeforeCursor":   beforeCursor,
		"ShowingEarlier": before != "",
		"CanComment":     u != nil && u.EmailVerifiedAt != nil,
		"CommentError":   commentError,
	})
}

func commentErrorFromQuery(value string) string {
	switch value {
	case "empty":
		return "Write something before posting."
	case "long":
		return "Comments are capped at 1,000 characters."
	case "rate":
		return "You're commenting too quickly. Try again later."
	default:
		return ""
	}
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
	case "comment", "mention":
		if subjectID != "" {
			return "/checkins/" + subjectID + "#comments"
		}
		return "/"
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
