package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sodapop/ent/activity"
)

// TestYouPageShowsStreaksAndActivity renders the real /you template and checks
// the streak cells plus the self-only activity list.
func TestYouPageShowsStreaksAndActivity(t *testing.T) {
	h := newV12Harness(t)
	u := h.user(t, "streaker")
	ctx := context.Background()

	if _, err := h.db.Client.Activity.Create().
		SetUserID(u.ID).SetKind(activity.KindBadge).
		SetSubjectID("early-bird").SetVisibility(activity.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("badge activity: %v", err)
	}
	if _, err := h.db.Client.Activity.Create().
		SetUserID(u.ID).SetKind(activity.KindCheckin).
		SetSubjectID("abc123").SetVisibility(activity.VisibilityPublic).
		Save(ctx); err != nil {
		t.Fatalf("checkin activity: %v", err)
	}

	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	h.srv.Render = r

	rec := httptest.NewRecorder()
	h.srv.youPage(rec, h.request(http.MethodGet, "/you", u, h.form()))
	if rec.Code != http.StatusOK {
		t.Fatalf("you: got %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"current streak", "longest", "Activity", "Earned early-bird", "Checked in"} {
		if !strings.Contains(body, want) {
			t.Errorf("you page missing %q", want)
		}
	}
}
