package web

import (
	"errors"
	"testing"
	"time"

	"sodapop/ent/drink"
)

func TestLimitedUntilForRequiresDateForLimitedAndSeasonal(t *testing.T) {
	for _, status := range []drink.Status{drink.StatusLimited, drink.StatusSeasonal} {
		if _, err := limitedUntilFor(status, ""); !errors.Is(err, errLimitedUntilRequired) {
			t.Fatalf("%s without date: got %v, want errLimitedUntilRequired", status, err)
		}
		if _, err := limitedUntilFor(status, "   "); !errors.Is(err, errLimitedUntilRequired) {
			t.Fatalf("%s blank date: got %v, want errLimitedUntilRequired", status, err)
		}
		if _, err := limitedUntilFor(status, "soon"); !errors.Is(err, errLimitedUntilFormat) {
			t.Fatalf("%s bad date: got %v, want errLimitedUntilFormat", status, err)
		}
		got, err := limitedUntilFor(status, "2026-12-31")
		if err != nil {
			t.Fatalf("%s valid date: %v", status, err)
		}
		if got == nil {
			t.Fatalf("%s valid date: got nil", status)
		}
		want := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		if *got != want {
			t.Fatalf("%s limited_until = %q, want %q", status, *got, want)
		}
	}
}

func TestLimitedUntilForClearsForOtherStatuses(t *testing.T) {
	for _, status := range []drink.Status{drink.StatusActive, drink.StatusDiscontinued} {
		got, err := limitedUntilFor(status, "")
		if err != nil || got != nil {
			t.Fatalf("%s empty: got %v, %v; want nil, nil", status, got, err)
		}
		// A stale value is still parsed, then discarded for non-limited statuses.
		got, err = limitedUntilFor(status, "2026-12-31")
		if err != nil || got != nil {
			t.Fatalf("%s dated: got %v, %v; want nil, nil", status, got, err)
		}
		if _, err := limitedUntilFor(status, "nope"); !errors.Is(err, errLimitedUntilFormat) {
			t.Fatalf("%s invalid date: got %v, want errLimitedUntilFormat", status, err)
		}
	}
}
