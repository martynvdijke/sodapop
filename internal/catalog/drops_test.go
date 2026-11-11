package catalog

import (
	"context"
	"errors"
	"testing"

	"sodapop/ent"
	"sodapop/ent/drink"
)

// recordingDrops stands in for the savedsearch service so the catalog hook can
// be tested without any notification machinery.
type recordingDrops struct {
	calls []int
	err   error
}

func (r *recordingDrops) Evaluate(_ context.Context, d *ent.Drink) (int, error) {
	r.calls = append(r.calls, d.ID)
	return len(r.calls), r.err
}

func TestPublishFiresDropAlerts(t *testing.T) {
	svc, database := newBrowseService(t)
	drops := &recordingDrops{}
	svc.Drops = drops
	ctx := context.Background()

	d := mkDrink(t, database.Client, "Drop Cola", "drop-cola", drink.StatusActive)
	if d.Published {
		t.Fatal("new drinks should start unpublished")
	}

	if err := svc.SetDrinkPublished(ctx, nil, d.ID, true); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(drops.calls) != 1 || drops.calls[0] != d.ID {
		t.Fatalf("evaluate calls = %v, want [%d]", drops.calls, d.ID)
	}

	// Unpublishing is not a drop.
	if err := svc.SetDrinkPublished(ctx, nil, d.ID, false); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if len(drops.calls) != 1 {
		t.Fatalf("unpublish evaluated: %v", drops.calls)
	}
}

func TestStatusChangeFiresOnlyForVisibleStatuses(t *testing.T) {
	tests := []struct {
		name       string
		from       drink.Status
		to         string
		published  bool
		wantNotify bool
	}{
		{"active to limited", drink.StatusActive, "limited", true, true},
		{"active to seasonal", drink.StatusActive, "seasonal", true, true},
		{"active to discontinued", drink.StatusActive, "discontinued", true, false},
		{"unpublished stays quiet", drink.StatusActive, "limited", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, database := newBrowseService(t)
			drops := &recordingDrops{}
			svc.Drops = drops
			ctx := context.Background()

			d := mkDrink(t, database.Client, "Status Cola", "status-cola", tt.from)
			if tt.published {
				if err := database.Client.Drink.UpdateOneID(d.ID).SetPublished(true).Exec(ctx); err != nil {
					t.Fatalf("publish: %v", err)
				}
			}
			if err := svc.SetDrinkStatus(ctx, nil, d.ID, tt.to, ""); err != nil {
				t.Fatalf("status: %v", err)
			}
			if got := len(drops.calls) > 0; got != tt.wantNotify {
				t.Fatalf("notified = %v, want %v", got, tt.wantNotify)
			}
		})
	}
}

func TestActivateDrinkFiresForPublishedDrink(t *testing.T) {
	svc, database := newBrowseService(t)
	drops := &recordingDrops{}
	svc.Drops = drops
	ctx := context.Background()
	u := mkUser(t, database.Client, "activator")

	d := mkDrink(t, database.Client, "Activation Cola", "activation-cola", drink.StatusProvisional)
	if _, err := database.Client.Drink.UpdateOneID(d.ID).SetPublished(true).Save(ctx); err != nil {
		t.Fatalf("publish: %v", err)
	}
	activated, err := svc.ActivateDrink(ctx, &u.ID, d.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.Status != drink.StatusActive {
		t.Fatalf("status = %q", activated.Status)
	}
	if len(drops.calls) != 1 {
		t.Fatalf("evaluate calls = %v", drops.calls)
	}
}

// Alert failures are logged, never returned: an admin action must succeed even
// when the notifier is broken.
func TestDropAlertFailureDoesNotFailTheAction(t *testing.T) {
	svc, database := newBrowseService(t)
	svc.Drops = &recordingDrops{err: errors.New("boom")}
	ctx := context.Background()

	d := mkDrink(t, database.Client, "Resilient Cola", "resilient-cola", drink.StatusActive)
	if err := svc.SetDrinkPublished(ctx, nil, d.ID, true); err != nil {
		t.Fatalf("publish returned the notifier error: %v", err)
	}
	stored, err := database.Client.Drink.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !stored.Published {
		t.Fatal("publish was rolled back by the notifier error")
	}
}

// A service without a notifier behaves exactly as before.
func TestNoNotifierIsHarmless(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Quiet Cola", "quiet-cola", drink.StatusActive)
	if err := svc.SetDrinkPublished(ctx, nil, d.ID, true); err != nil {
		t.Fatalf("publish: %v", err)
	}
}
