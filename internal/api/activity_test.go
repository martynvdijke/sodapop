package api

import (
	"context"
	"net/http"
	"testing"

	"sodapop/ent/activity"
)

// TestMyActivityIsSelfOnly proves the timeline never leaks another user's rows.
func TestMyActivityIsSelfOnly(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.server.DB.Client.Activity.Create().
		SetUserID(h.alice).SetKind(activity.KindCheckin).
		SetSubjectID("alice-checkin").SetVisibility(activity.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("alice activity: %v", err)
	}
	if _, err := h.server.DB.Client.Activity.Create().
		SetUserID(h.bob).SetKind(activity.KindBadge).
		SetSubjectID("bob-badge").SetVisibility(activity.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("bob activity: %v", err)
	}

	rec := h.do(t, http.MethodGet, "/api/v1/me/activity", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("activity: got %d body %s", rec.Code, rec.Body.String())
	}
	rows := dataList(t, rec)
	if len(rows) != 1 {
		t.Fatalf("activity rows = %d, want 1 (%s)", len(rows), rec.Body.String())
	}
	row, _ := rows[0].(map[string]any)
	if got := row["subject_id"]; got != "alice-checkin" {
		t.Fatalf("subject_id = %v, want alice-checkin", got)
	}

	anon := h.do(t, http.MethodGet, "/api/v1/me/activity", "", false)
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous activity: got %d, want 401", anon.Code)
	}
}
