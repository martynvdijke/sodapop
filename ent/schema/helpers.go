package schema

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// newID returns a 16-byte random hex identifier used by user-owned rows.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// now returns the current UTC time as an RFC3339 string, matching the
// project-wide timestamp convention.
func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
