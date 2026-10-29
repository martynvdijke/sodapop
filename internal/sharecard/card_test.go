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

func TestRenderBothKinds(t *testing.T) {
	for _, kind := range []string{"passport", "streak"} {
		t.Run(kind, func(t *testing.T) {
			raw, err := Render(Data{
				Handle:        "fizzfan",
				DisplayName:   "Fizz Fan",
				Checkins:      128,
				Styles:        9,
				Countries:     4,
				VenueKinds:    3,
				CurrentStreak: 12,
				LongestStreak: 31,
				Kind:          kind,
				GeneratedAt:   time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(raw) == 0 {
				t.Fatal("render returned no bytes")
			}
			w, h := decodeCard(t, raw)
			if w != 1200 || h != 630 {
				t.Fatalf("dimensions = %dx%d, want 1200x630", w, h)
			}
		})
	}
}

func TestRenderEmptyDataDoesNotPanic(t *testing.T) {
	for _, kind := range []string{"", "passport", "streak", "unexpected"} {
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
