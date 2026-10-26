package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/internal/db"
)

func TestCheckinPage404sForBlockedViewer(t *testing.T) {
	h := newV12Harness(t)
	h.renderWith(t, "checkin_detail", "unused")

	owner := h.user(t, "owner")
	viewer := h.user(t, "viewer")
	drink, err := h.db.Client.Drink.Create().
		SetName("Bloc Cola").SetSlug("bloc-cola").Save(context.Background())
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	c, err := h.db.Client.Checkin.Create().
		SetUserID(owner.ID).SetDrinkID(drink.ID).
		SetVisibility(checkin.VisibilityPublic).Save(context.Background())
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	if _, err := h.db.Client.Block.Create().
		SetBlockerID(viewer.ID).SetBlockedID(owner.ID).
		SetCreatedAt(db.Now()).Save(context.Background()); err != nil {
		t.Fatalf("block: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/checkins/"+c.ID, nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey{}, viewer))
	req.SetPathValue("id", c.ID)
	rec := httptest.NewRecorder()
	h.srv.renderCheckinDetail(rec, req, http.StatusOK, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("blocked checkin page = %d, want 404: %s", rec.Code, rec.Body.String())
	}

	// The owner still sees their own row after being blocked by someone else.
	req = httptest.NewRequest(http.MethodGet, "/checkins/"+c.ID, nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey{}, owner))
	req.SetPathValue("id", c.ID)
	rec = httptest.NewRecorder()
	h.srv.renderCheckinDetail(rec, req, http.StatusOK, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner checkin page = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
