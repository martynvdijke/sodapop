package sharecard

import (
	"bytes"
	"image/png"
	"testing"
	"time"
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func decodeCard(t *testing.T, raw []byte) (int, int) {
	t.Helper()
	if !bytes.HasPrefix(raw, pngMagic) {
		t.Fatalf("card does not start with PNG magic: % x", raw[:min(8, len(raw))])
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy()
}

// renderChecked renders data and asserts the invariants every card shares: a
// decodable PNG at exactly 1200x630 that is more than a blank panel.
func renderChecked(t *testing.T, data Data) []byte {
	t.Helper()
	raw, err := Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("render returned no bytes")
	}
	// A flat-colour 1200x630 PNG compresses to a few hundred bytes; anything
	// at or below that means the copy and the chart never made it to the card.
	if len(raw) < 4096 {
		t.Fatalf("card is only %d bytes; expected real content", len(raw))
	}
	w, h := decodeCard(t, raw)
	if w != 1200 || h != 630 {
		t.Fatalf("dimensions = %dx%d, want 1200x630", w, h)
	}
	return raw
}

func tenAxes(n int) []Axis {
	all := []Axis{
		{Label: "Sweetness", Score: 78},
		{Label: "Sourness", Score: 34},
		{Label: "Fizz", Score: 91},
		{Label: "Refreshment", Score: 62},
		{Label: "Bitterness", Score: 18},
	}
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

func TestRenderAllKinds(t *testing.T) {
	for _, kind := range []string{"passport", "streak", "recap", "fingerprint"} {
		t.Run(kind, func(t *testing.T) {
			renderChecked(t, Data{
				Handle:        "fizzfan",
				DisplayName:   "Fizz Fan",
				Checkins:      128,
				UniqueDrinks:  96,
				Styles:        9,
				Countries:     4,
				VenueKinds:    3,
				Badges:        7,
				CurrentStreak: 12,
				LongestStreak: 31,
				Year:          2025,
				TopVenue:      "Corner Fizz",
				TopStyle:      "Cola",
				Axes:          tenAxes(5),
				Kind:          kind,
				GeneratedAt:   time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
			})
		})
	}
}

func TestRenderFingerprintWithFiveAxes(t *testing.T) {
	raw := renderChecked(t, Data{
		Handle:      "fizzfan",
		DisplayName: "Fizz Fan",
		Axes:        tenAxes(5),
		Kind:        "fingerprint",
		GeneratedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	})
	// The radar pass must actually change pixels: a card with the chart drawn
	// differs from the same card without axes.
	plain := renderChecked(t, Data{
		Handle:      "fizzfan",
		DisplayName: "Fizz Fan",
		Kind:        "fingerprint",
		GeneratedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	})
	if bytes.Equal(raw, plain) {
		t.Fatal("radar axes did not change the rendered card")
	}
}

func TestRenderFingerprintDegradesBelowThreeAxes(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		t.Run(string(rune('0'+n))+" axes", func(t *testing.T) {
			renderChecked(t, Data{
				Handle:      "fizzfan",
				DisplayName: "Fizz Fan",
				Axes:        tenAxes(n),
				Kind:        "fingerprint",
				GeneratedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
			})
		})
	}
}

func TestRenderFingerprintClampsOddScores(t *testing.T) {
	renderChecked(t, Data{
		Axes: []Axis{
			{Label: "Sweetness", Score: 400},
			{Label: "Sourness", Score: -50},
			{Label: "", Score: 33},
		},
		Kind:        "fingerprint",
		GeneratedAt: time.Now(),
	})
}

func TestRenderRecapWithoutData(t *testing.T) {
	renderChecked(t, Data{Kind: "recap", Year: 2024, GeneratedAt: time.Now()})
}

func TestRenderEmptyDataDoesNotPanic(t *testing.T) {
	for _, kind := range []string{"", "passport", "streak", "recap", "fingerprint", "unexpected"} {
		t.Run(kind, func(t *testing.T) {
			raw, err := Render(Data{Kind: kind})
			if err != nil {
				t.Fatalf("render empty: %v", err)
			}
			w, h := decodeCard(t, raw)
			if w != 1200 || h != 630 {
				t.Fatalf("dimensions = %dx%d, want 1200x630", w, h)
			}
		})
	}
}

func TestRenderLongTextIsTrimmed(t *testing.T) {
	long := "A very long display name that should never overflow the card even when a member uses every character allowed"
	raw, err := Render(Data{
		Handle:      "very_long_handle_that_keeps_going",
		DisplayName: long,
		Checkins:    1000000,
		Kind:        "passport",
		GeneratedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("render long text: %v", err)
	}
	w, h := decodeCard(t, raw)
	if w != 1200 || h != 630 {
		t.Fatalf("dimensions = %dx%d, want 1200x630", w, h)
	}
}

func TestRecapSummaryOmitsEmptyParts(t *testing.T) {
	cases := []struct {
		name string
		data Data
		want string
	}{
		{"empty", Data{}, ""},
		{"all parts", Data{UniqueDrinks: 96, Styles: 9, Badges: 4}, "96 drinks · 9 styles · 4 badges"},
		{"singular", Data{UniqueDrinks: 1, Styles: 1, Badges: 1}, "1 drink · 1 style · 1 badge"},
		{"top style and venue", Data{TopStyle: "Cola", TopVenue: "Corner Fizz"}, "favourite Cola at Corner Fizz"},
		{"only venue", Data{TopVenue: "Corner Fizz"}, "most visits at Corner Fizz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recapSummary(tc.data); got != tc.want {
				t.Fatalf("recapSummary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeKindFallsBackToPassport(t *testing.T) {
	cases := map[string]string{
		"":            "passport",
		"passport":    "passport",
		" STREAK ":    "streak",
		"Recap":       "recap",
		"fingerprint": "fingerprint",
		"bogus":       "passport",
	}
	for input, want := range cases {
		if got := normalizeKind(input); got != want {
			t.Errorf("normalizeKind(%q) = %q, want %q", input, got, want)
		}
	}
}
