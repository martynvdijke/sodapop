package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/comment"
	"sodapop/ent/report"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/checkins"
)

// Report reasons Google Play and the moderation queue both understand. Anything
// else is filed as "other" so the queue never fills with free-text categories
// that cannot be filtered.
var reportReasons = map[string]bool{
	"spam": true, "harassment": true, "hate": true, "sexual": true,
	"violence": true, "self-harm": true, "other": true,
}

const (
	reportDetailRunes = 1000
	reportRateLimit   = 20
	reportRateWindow  = time.Hour
)

// createReport files a moderation report against a check-in, a comment or a
// user. The viewer must already be able to see the target, so the endpoint
// cannot be used to confirm that an invisible row exists: anything the viewer
// could not read is reported as not found.
func (s *Server) createReport(w http.ResponseWriter, r *http.Request, tok *auth.APIToken) error {
	var in struct {
		Entity  string `json:"entity"`
		ID      string `json:"id"`
		Handle  string `json:"handle"`
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	reason := strings.TrimSpace(strings.ToLower(in.Reason))
	if !reportReasons[reason] {
		reason = "other"
	}
	details := strings.TrimSpace(in.Details)
	if runes := []rune(details); len(runes) > reportDetailRunes {
		details = string(runes[:reportDetailRunes])
	}

	// Count the attempt before looking anything up: a viewer must not be able to
	// probe for invisible rows — or re-file the same open report — without
	// spending from the same budget as a real report.
	if s.Limiter != nil && !s.Limiter.Allow("report:"+tok.User.ID, reportRateLimit, reportRateWindow) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many reports; try again later")
		return nil
	}

	entity, ref, targetID, err := s.reportTarget(r.Context(), tok.User.ID, in.Entity, in.ID, in.Handle)
	if err != nil {
		return err
	}

	// A second report of the same row while the first is still open would only
	// duplicate the queue entry, so return the existing one instead.
	existing, err := s.Auth.Client.Report.Query().
		Where(
			report.EntityEQ(entity),
			report.EntityRefEQ(ref),
			report.UserIDEQ(tok.User.ID),
			report.StatusEQ(report.StatusOpen),
		).Only(r.Context())
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": reportDTO(existing)})
		return nil
	}
	if !ent.IsNotFound(err) {
		return err
	}

	row, err := s.Auth.Client.Report.Create().
		SetEntity(entity).
		SetEntityID(targetID).
		SetEntityRef(ref).
		SetUserID(tok.User.ID).
		SetReason(reason).
		SetDetails(details).
		Save(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": reportDTO(row)})
	return nil
}

// reportTarget resolves the request's entity to a reportable row, rejecting
// anything the viewer cannot see.
func (s *Server) reportTarget(ctx context.Context, viewerID, entity, id, handle string) (report.Entity, string, int, error) {
	id = strings.TrimSpace(id)
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	switch strings.ToLower(strings.TrimSpace(entity)) {
	case "checkin":
		if id == "" {
			return "", "", 0, bad("id is required for a check-in report")
		}
		if _, err := s.Checkins.Get(ctx, viewerID, id); err != nil {
			return "", "", 0, checkins.ErrNotFound
		}
		return report.EntityCheckin, id, 0, nil
	case "comment":
		if id == "" {
			return "", "", 0, bad("id is required for a comment report")
		}
		c, err := s.Auth.Client.Comment.Query().
			Where(comment.IDEQ(id), comment.DeletedAtIsNil()).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return "", "", 0, checkins.ErrNotFound
			}
			return "", "", 0, err
		}
		if _, err := s.Checkins.Get(ctx, viewerID, c.CheckinID); err != nil {
			return "", "", 0, checkins.ErrNotFound
		}
		return report.EntityComment, id, 0, nil
	case "user":
		if handle == "" {
			return "", "", 0, bad("handle is required for a user report")
		}
		row, err := s.Auth.Client.User.Query().
			Where(user.HandleEQ(handle), user.StatusEQ(user.StatusActive)).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return "", "", 0, checkins.ErrNotFound
			}
			return "", "", 0, err
		}
		if row.ID == viewerID {
			return "", "", 0, bad("you cannot report yourself")
		}
		return report.EntityUser, handle, 0, nil
	default:
		return "", "", 0, bad("entity must be checkin, comment or user")
	}
}

func reportDTO(r *ent.Report) map[string]any {
	return map[string]any{
		"id":         r.ID,
		"entity":     r.Entity,
		"entity_ref": r.EntityRef,
		"reason":     r.Reason,
		"status":     r.Status,
		"created_at": r.CreatedAt,
	}
}
