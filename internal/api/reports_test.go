package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"sodapop/ent/checkin"
	"sodapop/ent/report"
)

// reportFixture returns bob's public check-in and a comment on it, so alice has
// something she is allowed to see and therefore allowed to report.
func reportFixture(t *testing.T, h *harness) (checkinID, commentID string) {
	t.Helper()
	ctx := context.Background()

	d, err := h.server.DB.Client.Drink.Create().
		SetName("Report Cola").SetSlug("report-cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	c, err := h.server.DB.Client.Checkin.Create().
		SetUserID(h.bob).SetDrinkID(d.ID).
		SetVisibility(checkin.VisibilityPublic).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}
	cm, err := h.server.DB.Client.Comment.Create().
		SetUserID(h.bob).SetCheckinID(c.ID).SetBody("first").Save(ctx)
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	return c.ID, cm.ID
}

func TestReportAPIAcceptsEachEntityKind(t *testing.T) {
	h := newHarness(t)
	checkinID, commentID := reportFixture(t, h)

	cases := []struct {
		name string
		body string
		want report.Entity
		ref  string
	}{
		{
			name: "check-in",
			body: `{"entity":"checkin","id":"` + checkinID + `","reason":"spam"}`,
			want: report.EntityCheckin,
			ref:  checkinID,
		},
		{
			name: "comment",
			body: `{"entity":"comment","id":"` + commentID + `","reason":"harassment","details":"rude"}`,
			want: report.EntityComment,
			ref:  commentID,
		},
		{
			name: "user",
			body: `{"entity":"user","handle":"bob","reason":"other"}`,
			want: report.EntityUser,
			ref:  "bob",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodPost, "/api/v1/reports", tc.body, true)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
			}
			row, err := h.server.DB.Client.Report.Query().
				Where(report.EntityEQ(tc.want), report.EntityRefEQ(tc.ref)).
				Only(context.Background())
			if err != nil {
				t.Fatalf("report row: %v", err)
			}
			if row.UserID == nil || *row.UserID != h.alice {
				t.Errorf("reporter = %v, want alice", *row.UserID)
			}
			if row.Status != report.StatusOpen {
				t.Errorf("status = %q, want open", row.Status)
			}
			if !strings.Contains(rec.Body.String(), `"entity":"`+string(tc.want)+`"`) {
				t.Errorf("body = %s", rec.Body.String())
			}
		})
	}
}

func TestReportAPIIsIdempotentWhileOpen(t *testing.T) {
	h := newHarness(t)
	checkinID, _ := reportFixture(t, h)
	body := `{"entity":"checkin","id":"` + checkinID + `","reason":"spam"}`

	if rec := h.do(t, http.MethodPost, "/api/v1/reports", body, true); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if rec := h.do(t, http.MethodPost, "/api/v1/reports", body, true); rec.Code != http.StatusOK {
		t.Fatalf("second = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	n, err := h.server.DB.Client.Report.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reports = %d, want 1", n)
	}
}

func TestReportAPIRejectsInvisibleAndNonsenseTargets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// bob's private check-in is invisible to alice, so reporting it must look
	// exactly like reporting something that does not exist.
	d, err := h.server.DB.Client.Drink.Create().
		SetName("Private Cola").SetSlug("private-cola").Save(ctx)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	private, err := h.server.DB.Client.Checkin.Create().
		SetUserID(h.bob).SetDrinkID(d.ID).
		SetVisibility(checkin.VisibilityPrivate).Save(ctx)
	if err != nil {
		t.Fatalf("checkin: %v", err)
	}

	cases := []struct {
		name string
		body string
		want int
	}{
		{"private check-in", `{"entity":"checkin","id":"` + private.ID + `","reason":"spam"}`, http.StatusNotFound},
		{"unknown check-in", `{"entity":"checkin","id":"nope","reason":"spam"}`, http.StatusNotFound},
		{"unknown comment", `{"entity":"comment","id":"nope","reason":"spam"}`, http.StatusNotFound},
		{"unknown user", `{"entity":"user","handle":"nobody","reason":"spam"}`, http.StatusNotFound},
		{"self", `{"entity":"user","handle":"alice","reason":"spam"}`, http.StatusBadRequest},
		{"unknown entity", `{"entity":"drink","id":"1","reason":"spam"}`, http.StatusBadRequest},
		{"unknown field", `{"entity":"user","handle":"bob","nope":true}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, http.MethodPost, "/api/v1/reports", tc.body, true)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	if n, _ := h.server.DB.Client.Report.Query().Count(ctx); n != 0 {
		t.Fatalf("reports = %d, want 0", n)
	}
}

func TestReportAPIRequiresAuth(t *testing.T) {
	h := newHarness(t)
	checkinID, _ := reportFixture(t, h)
	rec := h.do(t, http.MethodPost, "/api/v1/reports",
		`{"entity":"checkin","id":"`+checkinID+`","reason":"spam"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestReportAPIForgetsReasonAndCapsDetails(t *testing.T) {
	h := newHarness(t)
	checkinID, _ := reportFixture(t, h)

	// An unrecognised reason is stored as "other" rather than rejected: the
	// report matters more than the label.
	rec := h.do(t, http.MethodPost, "/api/v1/reports",
		`{"entity":"checkin","id":"`+checkinID+`","reason":"because","details":"`+strings.Repeat("x", 1200)+`"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	row, err := h.server.DB.Client.Report.Query().Only(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if row.Reason != "other" {
		t.Errorf("reason = %q, want other", row.Reason)
	}
	if len([]rune(row.Details)) != 1000 {
		t.Errorf("details length = %d, want 1000", len([]rune(row.Details)))
	}
}

func TestReportAPIRateLimits(t *testing.T) {
	h := newHarness(t)
	checkinID, _ := reportFixture(t, h)
	body := `{"entity":"checkin","id":"` + checkinID + `","reason":"spam"}`

	for i := 0; i < 20; i++ {
		if rec := h.do(t, http.MethodPost, "/api/v1/reports", body, true); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("call %d rate limited too early", i+1)
		}
	}
	rec := h.do(t, http.MethodPost, "/api/v1/reports", body, true)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
}
