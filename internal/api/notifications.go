package api

import (
	"net/http"
	"strings"

	"sodapop/ent"
	"sodapop/ent/notification"
	"sodapop/ent/user"
	"sodapop/internal/auth"
)

type notificationActorDTO struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

type notificationDTO struct {
	ID              string                `json:"id"`
	Kind            string                `json:"kind"`
	Text            string                `json:"text"`
	Read            bool                  `json:"read"`
	CreatedAt       string                `json:"created_at"`
	SubjectID       string                `json:"subject_id"`
	Actor           *notificationActorDTO `json:"actor"`
	ActorInitial    string                `json:"actor_initial"`
	Href            string                `json:"href"`
	FriendRequestID string                `json:"friend_request_id,omitempty"`
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	rows, err := s.Social.Notifications(r.Context(), tok.User.ID, 50)
	if err != nil {
		return err
	}

	// Batch-load actors to keep the endpoint at a constant number of queries.
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
		users, err := s.Social.Client.User.Query().
			Where(user.IDIn(actorIDs...)).
			All(r.Context())
		if err != nil {
			return err
		}
		for _, actor := range users {
			actors[actor.ID] = actor
		}
	}

	data := make([]notificationDTO, 0, len(rows))
	pending, err := s.Social.IncomingPendingRequests(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	for _, n := range rows {
		dto := notificationDTO{
			ID:        n.ID,
			Kind:      string(n.Kind),
			Text:      n.Text,
			Read:      n.ReadAt != nil,
			CreatedAt: n.CreatedAt,
			SubjectID: n.SubjectID,
			Href:      notificationHref(string(n.Kind), n.SubjectID),
		}
		if n.ActorID != nil {
			if actor, ok := actors[*n.ActorID]; ok {
				dto.Actor = &notificationActorDTO{Handle: actor.Handle, DisplayName: actor.DisplayName}
				dto.ActorInitial = notificationInitial(actor.DisplayName, actor.Handle)
			}
			if id, ok := pending[*n.ActorID]; ok && string(n.Kind) == "friend_request" {
				dto.FriendRequestID = id
			}
		}
		data = append(data, dto)
	}

	unread, err := s.Social.UnreadCount(r.Context(), tok.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data,
		"meta": map[string]any{"count": len(data), "unread": unread},
	})
	return nil
}

func (s *Server) notificationsRead(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		ID string `json:"id"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &in); err != nil {
			return err
		}
	}

	// Count before marking so the response can report how many rows changed.
	marked := 0
	if in.ID == "" {
		marked, _ = s.Social.UnreadCount(r.Context(), tok.User.ID)
	} else {
		n, err := s.Social.Client.Notification.Query().
			Where(notification.ID(in.ID), notification.UserID(tok.User.ID), notification.ReadAtIsNil()).
			Count(r.Context())
		if err != nil {
			return err
		}
		marked = n
	}
	if err := s.Social.MarkRead(r.Context(), tok.User.ID, in.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"marked": marked}})
	return nil
}

// notificationHref returns a same-origin relative path for the notification's
// subject. It never echoes a URL supplied by another user.
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

func notificationInitial(displayName, handle string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.TrimSpace(handle)
	}
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}
