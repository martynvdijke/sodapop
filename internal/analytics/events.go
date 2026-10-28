// Package analytics defines the closed set of product analytics events Sodapop
// is allowed to emit. The browser helper in web/src/analytics.ts mirrors these
// rules: event names must be allowlisted and at most 50 characters, string
// property values at most 500 characters, numbers rounded to four decimal
// places, and free-form or identifying keys are dropped.
package analytics

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaxNameLen is the maximum accepted event name length, in runes.
const MaxNameLen = 50

// MaxStringLen is the maximum accepted string property length, in runes.
const MaxStringLen = 500

// allowed lists every event Sodapop may send. Keep it in sync with
// web/src/analytics.ts (which receives it from the page as JSON) and with
// docs/architecture.md §10. Additions are deliberate product decisions; never
// accept ad-hoc names from callers.
var allowed = []string{
	"badge_earned",
	"barcode_scan_result",
	"barcode_scan_started",
	"checkin_created",
	"drink_created",
	"friend_added",
	"list_created",
	"search_used",
	"theme_changed",
	"venue_viewed",
}

var allowedSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		m[name] = struct{}{}
	}
	return m
}()

// deniedKeys are property names that could carry free text or identifying
// data. Matching is case-insensitive after trimming surrounding space.
var deniedKeys = map[string]struct{}{
	"body":       {},
	"comment":    {},
	"drink":      {},
	"drink_name": {},
	"email":      {},
	"handle":     {},
	"note":       {},
	"password":   {},
	"query":      {},
	"search":     {},
	"text":       {},
	"token":      {},
	"username":   {},
}

// Names returns a sorted copy of the event allowlist.
func Names() []string {
	out := make([]string, len(allowed))
	copy(out, allowed)
	sort.Strings(out)
	return out
}

// Allowed reports whether name is in the event allowlist. It does not check
// the length limit; use Sanitize for the full gate.
func Allowed(name string) bool {
	_, ok := allowedSet[name]
	return ok
}

// Sanitize gates an event name and cleans its properties. It returns ok=false
// when the name is not allowlisted or is longer than MaxNameLen runes. The
// returned map is always non-nil, even when every property was dropped.
//
// Accepted values: strings (truncated to MaxStringLen runes), float64
// (rounded to four decimal places, non-finite values dropped), int, int32 and
// int64 (normalised to int64), and bool. Everything else is dropped.
func Sanitize(name string, props map[string]any) (map[string]any, bool) {
	if !Allowed(name) || utf8.RuneCountInString(name) > MaxNameLen {
		return map[string]any{}, false
	}
	out := make(map[string]any, len(props))
	for key, value := range props {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		if _, denied := deniedKeys[strings.ToLower(k)]; denied {
			continue
		}
		switch v := value.(type) {
		case string:
			out[k] = truncate(v, MaxStringLen)
		case bool:
			out[k] = v
		case int:
			out[k] = int64(v)
		case int32:
			out[k] = int64(v)
		case int64:
			out[k] = v
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			out[k] = math.Round(v*1e4) / 1e4
		default:
			// Unsupported types are dropped rather than coerced.
		}
	}
	return out, true
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
