// Package clientip derives a client address from an HTTP request without
// trusting client-supplied forwarding headers from untrusted peers.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// FromRequest returns the client IP for r. X-Forwarded-For is honoured only
// when the direct peer is trusted; the header is then walked from the right so
// a client cannot prepend a spoofed hop. When the peer is empty it returns
// "unknown".
func FromRequest(r *http.Request, trusted []string) string {
	if r == nil {
		return "unknown"
	}
	peer := normalize(r.RemoteAddr)
	if peer == "" {
		peer = strings.TrimSpace(r.RemoteAddr)
	}
	if peer == "" {
		return "unknown"
	}
	if !Trusted(peer, trusted) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := normalize(hops[i])
		if hop == "" {
			continue
		}
		if !Trusted(hop, trusted) {
			return hop
		}
	}
	return peer
}

// Trusted reports whether ip matches an exact entry or a CIDR in trusted.
// Entries are parsed on each call; the list is expected to be tiny.
func Trusted(ipStr string, trusted []string) bool {
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(ipStr), "[]"))
	if ip == nil {
		return false
	}
	for _, entry := range trusted {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err == nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if allowed := net.ParseIP(strings.Trim(entry, "[]")); allowed != nil && allowed.Equal(ip) {
			return true
		}
	}
	return false
}

// normalize accepts bare IPs and bracketed IPv6, optionally with a port, and
// returns a canonical string. It returns "" for malformed values.
func normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else {
		value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
