package analytics

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	names := Names()
	if len(names) != 10 {
		t.Fatalf("Names() returned %d events, want 10", len(names))
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Names() is not sorted: %v", names)
	}
	for _, want := range []string{
		"checkin_created", "barcode_scan_started", "barcode_scan_result",
		"drink_created", "search_used", "venue_viewed", "friend_added",
		"list_created", "badge_earned", "theme_changed",
	} {
		if !Allowed(want) {
			t.Errorf("Allowed(%q) = false, want true", want)
		}
	}
	// Names must return a copy; callers cannot widen the allowlist.
	names[0] = "not_an_event"
	if !sort.StringsAreSorted(Names()) {
		t.Fatal("Names() exposes internal slice")
	}
}

func TestAllowed(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"checkin_created", true},
		{"venue_viewed", true},
		{"", false},
		{"CHECKIN_CREATED", false},
		{"page_view", false},
		{"checkin_created ", false},
		{strings.Repeat("a", 51), false},
	}
	for _, tt := range tests {
		if got := Allowed(tt.name); got != tt.want {
			t.Errorf("Allowed(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSanitizeGating(t *testing.T) {
	tests := []struct {
		name    string
		event   string
		wantOK  bool
		wantLen int
	}{
		{"allowed", "checkin_created", true, 1},
		{"denied name", "page_view", false, 0},
		{"empty name", "", false, 0},
		{"long name", strings.Repeat("x", 51), false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Sanitize(tt.event, map[string]any{"venue_id": "1"})
			if ok != tt.wantOK {
				t.Fatalf("Sanitize(%q) ok = %v, want %v", tt.event, ok, tt.wantOK)
			}
			if got == nil {
				t.Fatal("Sanitize returned a nil map")
			}
			if len(got) != tt.wantLen {
				t.Fatalf("Sanitize(%q) = %v, want empty map", tt.event, got)
			}
		})
	}

	// A nil props map still returns a non-nil result.
	got, ok := Sanitize("search_used", nil)
	if !ok || got == nil {
		t.Fatalf("Sanitize with nil props = %v, %v; want non-nil, true", got, ok)
	}
}

func TestSanitize(t *testing.T) {
	long := strings.Repeat("é", 501)
	tests := []struct {
		name  string
		key   string
		value any
		want  any
		drop  bool
	}{
		{"short string", "surface", "header", "header", false},
		{"string at limit", "surface", strings.Repeat("a", 500), strings.Repeat("a", 500), false},
		{"string truncated by rune", "surface", long, strings.Repeat("é", 500), false},
		{"bool true", "camera", true, true, false},
		{"bool false", "camera", false, false, false},
		{"int", "count", 7, int64(7), false},
		{"int32", "count", int32(7), int64(7), false},
		{"int64", "count", int64(7), int64(7), false},
		{"float rounds down", "rating", 1.23444, 1.2344, false},
		{"float rounds up", "rating", 1.23456, 1.2346, false},
		{"float negative", "rating", -1.23456, -1.2346, false},
		{"float integral", "rating", 3.0, 3.0, false},
		{"NaN", "rating", math.NaN(), nil, true},
		{"+Inf", "rating", math.Inf(1), nil, true},
		{"float32 unsupported", "rating", float32(1.5), nil, true},
		{"uint unsupported", "count", uint(7), nil, true},
		{"slice unsupported", "tags", []string{"a"}, nil, true},
		{"map unsupported", "meta", map[string]string{"a": "b"}, nil, true},
		{"nil value", "venue_id", nil, nil, true},
		{"struct unsupported", "meta", struct{ A int }{1}, nil, true},
		{"denylisted email", "email", "a@example.com", nil, true},
		{"denylisted mixed case", "Email", "a@example.com", nil, true},
		{"denylisted padded handle", "  handle ", "@someone", nil, true},
		{"denylisted username", "USERNAME", "someone", nil, true},
		{"denylisted query", "query", "cola", nil, true},
		{"denylisted drink name", "drink_name", "Cola", nil, true},
		{"denylisted note", "note", "free text", nil, true},
		{"empty key", "", "value", nil, true},
		{"whitespace key", "   ", "value", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Sanitize("checkin_created", map[string]any{tt.key: tt.value})
			if !ok {
				t.Fatal("Sanitize returned ok=false for an allowlisted event")
			}
			outKey := strings.TrimSpace(tt.key)
			value, present := got[outKey]
			if tt.drop {
				if present {
					t.Fatalf("key %q was kept as %#v, want dropped", outKey, value)
				}
				return
			}
			if !present {
				t.Fatalf("key %q missing from %v", outKey, got)
			}
			if !reflect.DeepEqual(value, tt.want) {
				t.Fatalf("key %q = %#v (%T), want %#v (%T)", outKey, value, value, tt.want, tt.want)
			}
		})
	}
}

func TestSanitizeDoesNotMutateInput(t *testing.T) {
	props := map[string]any{"venue_id": "12", "email": "a@example.com", "rating": 2.34567}
	before := map[string]any{}
	for k, v := range props {
		before[k] = v
	}
	got, ok := Sanitize("venue_viewed", props)
	if !ok {
		t.Fatal("Sanitize returned ok=false")
	}
	if !reflect.DeepEqual(props, before) {
		t.Fatalf("Sanitize mutated its input: %v", props)
	}
	if got["venue_id"] != "12" {
		t.Fatalf("venue_id = %v, want 12", got["venue_id"])
	}
	if _, present := got["email"]; present {
		t.Fatal("email was not dropped")
	}
}

func TestSanitizeDeterministic(t *testing.T) {
	props := map[string]any{
		"b": "two", "a": 1, "c": true, "d": 2.00005,
	}
	first, ok := Sanitize("list_created", props)
	if !ok {
		t.Fatal("Sanitize returned ok=false")
	}
	for i := 0; i < 20; i++ {
		next, _ := Sanitize("list_created", props)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("Sanitize is not deterministic: %v != %v", first, next)
		}
	}
}

func TestSanitizeKeysTrimmed(t *testing.T) {
	got, ok := Sanitize("search_used", map[string]any{" venue_id ": "9"})
	if !ok {
		t.Fatal("Sanitize returned ok=false")
	}
	if got["venue_id"] != "9" {
		t.Fatalf("trimmed key missing: %v", got)
	}
	if _, present := got[" venue_id "]; present {
		t.Fatalf("untrimmed key kept: %v", got)
	}
}
