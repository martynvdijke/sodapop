package catalog

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// NormalizeGTIN validates a scanned barcode and returns its canonical 14-digit
// GTIN representation. UPC-A, EAN-8 and EAN-13 values are left-padded with
// zeroes, so the same package always resolves to the same row.
func NormalizeGTIN(raw string) (string, error) {
	var digits strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '\t':
			continue
		default:
			return "", ErrInvalidBarcode
		}
	}
	code := digits.String()
	switch len(code) {
	case 8, 12, 13:
		code = strings.Repeat("0", 14-len(code)) + code
	case 14:
	default:
		return "", ErrInvalidBarcode
	}
	if !validGTIN(code) {
		return "", ErrInvalidBarcode
	}
	return code, nil
}

func validGTIN(code string) bool {
	if len(code) != 14 {
		return false
	}
	sum := 0
	for i, r := range code[:13] {
		d := int(r - '0')
		if i%2 == 0 {
			sum += d * 3
		} else {
			sum += d
		}
	}
	check := (10 - (sum % 10)) % 10
	return check == int(code[13]-'0')
}

// RandomSuffix returns four hex characters for collision-safe slugs.
func RandomSuffix() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%10000)
	}
	return hex.EncodeToString(b)
}
