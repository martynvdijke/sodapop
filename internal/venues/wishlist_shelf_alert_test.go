package venues

import (
	"errors"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drink"
	"sodapop/ent/notification"
)

// wishlistFan creates a user whose system wishlist holds drinkID and whose
// shelf-alert preference is prefOn.
func (h *venueHarness) wishlistFan(t *testing.T, handle string, drinkID int, prefOn bool) *ent.User {
	t.Helper()
	u := h.addUser(t, handle)
	list, err := h.db.Client.List.Create().
		SetUserID(u.ID).
		SetName("Wishlist").
		SetSlug("wishlist").
		SetIsSystem(true).
		Save(h.ctx)
	if err != nil {
		t.Fatalf("wishlist list: %v", err)
	}
	if _, err := h.db.Client.ListItem.Create().
		SetListID(list.ID).
		SetDrinkID(drinkID).
		Save(h.ctx); err != nil {
		t.Fatalf("wishlist item: %v", err)
	}
	if _, err := h.db.Client.UserSetting.Create().
		SetUserID(u.ID).
		SetWishlistShelfAlerts(prefOn).
		Save(h.ctx); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return u
}

func (h *venueHarness) shelfAlertCount(t *testing.T, userID string) int {
	t.Helper()
	n, err := h.db.Client.Notification.Query().
		Where(
			notification.UserID(userID),
			notification.KindEQ(notification.KindWishlistShelf),
		).
		Count(h.ctx)
	if err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

func TestShelfReportAlertsSavedVenueWatchers(t *testing.T) {
	cases := []struct {
		name     string
		prefOn   bool
		saved    bool
		reporter bool
		second   bool
		want     int
	}{
		{name: "saved venue and pref on", prefOn: true, saved: true, want: 1},
		{name: "pref off", prefOn: false, saved: true, want: 0},
		{name: "venue not saved", prefOn: true, saved: false, want: 0},
		{name: "reporter is excluded", prefOn: true, saved: true, reporter: true, want: 0},
		{name: "second report inside window", prefOn: true, saved: true, second: true, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newVenueHarness(t)
			v := h.create(t, "Café Zomer", "Bristol", 0, 0)
			drinkID := h.addDrink(t, "Pomegranate Fizz", "pomegranate-fizz", drink.StatusActive)

			handle := "fan"
			if tc.reporter {
				handle = "reporter"
			}
			watcher := h.wishlistFan(t, handle, drinkID, tc.prefOn)
			if tc.saved {
				if err := h.svc.Save(h.ctx, watcher.ID, v.ID); err != nil {
					t.Fatalf("save venue: %v", err)
				}
			}

			reporterID := h.user.ID
			if tc.reporter {
				reporterID = watcher.ID
			}
			for i := 0; i < 1+boolInt(tc.second); i++ {
				if _, err := h.svc.ReportShelf(h.ctx, reporterID, v.ID, ShelfReportInput{
					DrinkSlug: "pomegranate-fizz", SeenNow: true,
				}); err != nil {
					t.Fatalf("report: %v", err)
				}
			}

			if got := h.shelfAlertCount(t, watcher.ID); got != tc.want {
				t.Fatalf("notifications = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestShelfAlertTextNamesVenueAndDrink(t *testing.T) {
	h := newVenueHarness(t)
	v := h.create(t, "Café Zomer", "Bristol", 0, 0)
	drinkID := h.addDrink(t, "Pomegranate Fizz", "pomegranate-fizz", drink.StatusActive)
	fan := h.wishlistFan(t, "fan", drinkID, true)
	if err := h.svc.Save(h.ctx, fan.ID, v.ID); err != nil {
		t.Fatalf("save venue: %v", err)
	}
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, v.ID, ShelfReportInput{
		DrinkSlug: "pomegranate-fizz", SeenNow: true,
	}); err != nil {
		t.Fatalf("report: %v", err)
	}
	row, err := h.db.Client.Notification.Query().
		Where(notification.UserID(fan.ID), notification.KindEQ(notification.KindWishlistShelf)).
		Only(h.ctx)
	if err != nil {
		t.Fatalf("notification: %v", err)
	}
	want := "Café Zomer reported Pomegranate Fizz in stock"
	if row.Text != want {
		t.Fatalf("text = %q, want %q", row.Text, want)
	}
}

// The @home marker is one shared hidden venue and ReportShelf refuses hidden
// venues, so it can never be a shelf-alert target. This pins that assumption:
// no "home venue" audience exists to notify.
func TestReportShelfRejectsHomeVenue(t *testing.T) {
	h := newVenueHarness(t)
	homeID, err := h.svc.EnsureHome(h.ctx)
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	drinkID := h.addDrink(t, "Pomegranate Fizz", "pomegranate-fizz", drink.StatusActive)
	fan := h.wishlistFan(t, "fan", drinkID, true)
	if err := h.svc.Save(h.ctx, fan.ID, homeID); err == nil {
		t.Fatal("saving the hidden home venue should fail")
	}
	if _, err := h.svc.ReportShelf(h.ctx, h.user.ID, homeID, ShelfReportInput{
		DrinkSlug: "pomegranate-fizz", SeenNow: true,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ReportShelf on home = %v, want ErrInvalid", err)
	}
	if got := h.shelfAlertCount(t, fan.ID); got != 0 {
		t.Fatalf("notifications = %d, want 0", got)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
