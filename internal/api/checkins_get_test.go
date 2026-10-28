package api

import (
	"context"
	"net/http"
	"testing"
)

func TestCheckinDetailSavedFlag(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	drink := newDrink(t, h, "Saved Cola", "saved-cola", nil)
	c := checkinFor(t, h, mustUser(t, h, h.alice), drink.ID, "public")

	rec := h.do(t, http.MethodGet, "/api/v1/checkins/"+c.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	if saved := dataMap(t, rec)["saved"]; saved != false {
		t.Fatalf("saved = %v, want false", saved)
	}

	if err := h.server.Saves.Save(ctx, h.alice, c.ID); err != nil {
		t.Fatalf("save: %v", err)
	}
	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+c.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after save: %d %s", rec.Code, rec.Body.String())
	}
	if saved := dataMap(t, rec)["saved"]; saved != true {
		t.Fatalf("saved = %v, want true", saved)
	}

	// Another viewer's bookmark state is independent of Alice's.
	bobToken := h.tokenFor(t, h.bob)
	rec = h.doWith(t, bobToken, http.MethodGet, "/api/v1/checkins/"+c.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("bob get: %d %s", rec.Code, rec.Body.String())
	}
	if saved := dataMap(t, rec)["saved"]; saved != false {
		t.Fatalf("bob saved = %v, want false", saved)
	}

	// Bob's own check-in is likewise unsaved for Alice.
	bobCheckin := checkinFor(t, h, mustUser(t, h, h.bob), drink.ID, "public")
	rec = h.do(t, http.MethodGet, "/api/v1/checkins/"+bobCheckin.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice get bob checkin: %d %s", rec.Code, rec.Body.String())
	}
	if saved := dataMap(t, rec)["saved"]; saved != false {
		t.Fatalf("alice saved on bob checkin = %v, want false", saved)
	}
}
