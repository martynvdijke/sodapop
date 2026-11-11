package checkins_test

import (
	"errors"
	"testing"
	"time"

	"sodapop/internal/checkins"
)

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func TestUpdateWithinWindow(t *testing.T) {
	e := newTestEnv(t)
	c := e.createCheckin(t, e.alice, "public")

	price := 250
	ok, err := e.svc.Update(e.ctx, e.alice.ID, c.ID, checkins.UpdateInput{
		Rating:        intPtr(12),
		Serving:       strPtr("bottle"),
		PrepMethod:    strPtr("cold_brew"),
		PrepNote:      strPtr(" 3 min steep "),
		Note:          strPtr("bright and sharp"),
		Visibility:    strPtr("friends"),
		PriceCents:    &price,
		PriceCurrency: strPtr("eur"),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !ok {
		t.Fatal("update reported no write")
	}

	detail, err := e.svc.Get(e.ctx, e.alice.ID, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Rating == nil || *detail.Rating != 12 {
		t.Fatalf("rating = %v", detail.Rating)
	}
	if detail.Serving != "bottle" || detail.PrepMethod != "cold_brew" {
		t.Fatalf("serving/prep = %q/%q", detail.Serving, detail.PrepMethod)
	}
	if detail.PrepNote != "3 min steep" {
		t.Fatalf("prep note = %q", detail.PrepNote)
	}
	if detail.Note != "bright and sharp" || detail.Visibility != "friends" {
		t.Fatalf("note/visibility = %q/%q", detail.Note, detail.Visibility)
	}
	if detail.PriceCents == nil || *detail.PriceCents != 250 || detail.PriceCurrency != "EUR" {
		t.Fatalf("price = %v %q", detail.PriceCents, detail.PriceCurrency)
	}
	if detail.EditedAt == "" {
		t.Fatal("edited_at was not set")
	}
}

func TestUpdateClearsNillableFields(t *testing.T) {
	e := newTestEnv(t)
	price := 199
	c, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, Rating: intPtr(8), PriceCents: &price, PriceCurrency: "GBP",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := e.svc.Update(e.ctx, e.alice.ID, c.ID, checkins.UpdateInput{
		ClearRating: true, ClearPrice: true,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	detail, err := e.svc.Get(e.ctx, e.alice.ID, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.Rating != nil {
		t.Fatalf("rating was not cleared: %v", *detail.Rating)
	}
	if detail.PriceCents != nil || detail.PriceCurrency != "" {
		t.Fatalf("price was not cleared: %v %q", detail.PriceCents, detail.PriceCurrency)
	}
}

func TestUpdateRefusals(t *testing.T) {
	t.Run("after window", func(t *testing.T) {
		e := newTestEnv(t)
		c := e.createCheckin(t, e.alice, "public")
		old := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
		if _, err := e.db.Client.Checkin.UpdateOneID(c.ID).SetCreatedAt(old).Save(e.ctx); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		_, err := e.svc.Update(e.ctx, e.alice.ID, c.ID, checkins.UpdateInput{Note: strPtr("late")})
		if !errors.Is(err, checkins.ErrEditWindowClosed) {
			t.Fatalf("err = %v, want ErrEditWindowClosed", err)
		}
	})

	t.Run("non-owner is not found", func(t *testing.T) {
		e := newTestEnv(t)
		c := e.createCheckin(t, e.alice, "public")
		_, err := e.svc.Update(e.ctx, e.bob.ID, c.ID, checkins.UpdateInput{Note: strPtr("hijack")})
		if !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("deleted is not editable", func(t *testing.T) {
		e := newTestEnv(t)
		c := e.createCheckin(t, e.alice, "public")
		if err := e.svc.Delete(e.ctx, e.alice.ID, c.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		_, err := e.svc.Update(e.ctx, e.alice.ID, c.ID, checkins.UpdateInput{Note: strPtr("ghost")})
		if !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("missing id is not found", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.svc.Update(e.ctx, e.alice.ID, "", checkins.UpdateInput{})
		if !errors.Is(err, checkins.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestUpdateValidation(t *testing.T) {
	tests := []struct {
		name string
		in   checkins.UpdateInput
		want error
	}{
		{"rating too high", checkins.UpdateInput{Rating: intPtr(21)}, checkins.ErrInvalidRating},
		{"rating negative", checkins.UpdateInput{Rating: intPtr(-1)}, checkins.ErrInvalidRating},
		{"price too high", checkins.UpdateInput{PriceCents: intPtr(100001)}, checkins.ErrInvalidPrice},
		{"price negative", checkins.UpdateInput{PriceCents: intPtr(-5)}, checkins.ErrInvalidPrice},
		{"currency too long", checkins.UpdateInput{PriceCurrency: strPtr("EURO")}, checkins.ErrInvalidCurrency},
		{"currency not letters", checkins.UpdateInput{PriceCurrency: strPtr("E1R")}, checkins.ErrInvalidCurrency},
		{"unknown visibility", checkins.UpdateInput{Visibility: strPtr("everyone")}, checkins.ErrInvalidInput},
		{"unknown serving", checkins.UpdateInput{Serving: strPtr("keg")}, checkins.ErrInvalidInput},
		{"unknown prep method", checkins.UpdateInput{PrepMethod: strPtr("sun_tea")}, checkins.ErrInvalidInput},
		{"venue does not exist", checkins.UpdateInput{VenueID: intPtr(99999)}, checkins.ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			c := e.createCheckin(t, e.alice, "public")
			if _, err := e.svc.Update(e.ctx, e.alice.ID, c.ID, tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNormalizeCurrency(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"eur", "EUR", false},
		{" gbp ", "GBP", false},
		{"EURO", "", true},
		{"EU", "", true},
		{"E1R", "", true},
		{"€", "", true},
	}
	for _, tt := range tests {
		got, err := checkins.NormalizeCurrency(tt.in)
		if tt.wantErr {
			if !errors.Is(err, checkins.ErrInvalidCurrency) {
				t.Fatalf("NormalizeCurrency(%q) err = %v", tt.in, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("NormalizeCurrency(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestCreatePersistsPrice(t *testing.T) {
	e := newTestEnv(t)
	price := 349
	c, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, PriceCents: &price, PriceCurrency: "usd",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.PriceCents == nil || *c.PriceCents != 349 || c.PriceCurrency != "USD" {
		t.Fatalf("price = %v %q", c.PriceCents, c.PriceCurrency)
	}

	if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
		DrinkID: e.drink.ID, PriceCurrency: "dollars",
	}); !errors.Is(err, checkins.ErrInvalidCurrency) {
		t.Fatalf("err = %v, want ErrInvalidCurrency", err)
	}
}

func TestCanEditWindow(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		createdAt string
		want      bool
	}{
		{"just now", now.Format(time.RFC3339), true},
		{"23 hours", now.Add(-23 * time.Hour).Format(time.RFC3339), true},
		{"25 hours", now.Add(-25 * time.Hour).Format(time.RFC3339), false},
		{"garbage", "not a time", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkins.CanEdit(tt.createdAt, now); got != tt.want {
				t.Fatalf("CanEdit = %v, want %v", got, tt.want)
			}
		})
	}
}
