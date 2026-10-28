package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		trusted []string
		xff     string
		want    string
	}{
		{
			name:   "untrusted peer ignores spoofed header",
			remote: "192.0.2.10:4567", trusted: nil, xff: "203.0.113.1",
			want: "192.0.2.10",
		},
		{
			name:   "trusted peer uses rightmost untrusted hop",
			remote: "10.0.0.5:4321", trusted: []string{"10.0.0.0/8"}, xff: "203.0.113.7",
			want: "203.0.113.7",
		},
		{
			name:   "trusted chain picks rightmost untrusted",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.0/8", "192.0.2.0/24"},
			xff: "203.0.113.9, 192.0.2.50, 10.0.0.7", want: "203.0.113.9",
		},
		{
			name:   "all hops trusted falls back to peer",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.0/8", "192.0.2.0/24"},
			xff: "192.0.2.50, 10.0.0.7", want: "10.0.0.5",
		},
		{
			name:   "trusted exact IP entry",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.5"}, xff: "203.0.113.8",
			want: "203.0.113.8",
		},
		{
			name:   "cidr entry matches peer",
			remote: "10.20.30.40:1", trusted: []string{"10.0.0.0/8"}, xff: "203.0.113.8",
			want: "203.0.113.8",
		},
		{
			name:   "malformed xff falls back to peer",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.0/8"}, xff: "not-an-ip, , garbage",
			want: "10.0.0.5",
		},
		{
			name:   "malformed hops skipped to find untrusted client",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.0/8"},
			xff: "203.0.113.9, garbage, 10.0.0.7", want: "203.0.113.9",
		},
		{
			name:   "trusted peer without header falls back",
			remote: "10.0.0.5:1", trusted: []string{"10.0.0.5"}, xff: "",
			want: "10.0.0.5",
		},
		{
			name:   "bracketed ipv6 and ports normalize",
			remote: "[::1]:1234", trusted: []string{"::1"}, xff: "[2001:db8::2]:555",
			want: "2001:db8::2",
		},
		{
			name:   "empty remote is unknown",
			remote: "", trusted: nil, want: "unknown",
		},
		{
			name:   "bare ipv6 peer normalizes",
			remote: "2001:db8::1", trusted: nil, want: "2001:db8::1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := FromRequest(req, tt.trusted); got != tt.want {
				t.Fatalf("FromRequest = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromRequestNilRequest(t *testing.T) {
	if got := FromRequest(nil, nil); got != "unknown" {
		t.Fatalf("FromRequest(nil) = %q, want unknown", got)
	}
}

func TestTrusted(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		trusted []string
		want    bool
	}{
		{name: "exact ipv4", ip: "10.0.0.5", trusted: []string{"10.0.0.5"}, want: true},
		{name: "exact mismatch", ip: "10.0.0.6", trusted: []string{"10.0.0.5"}, want: false},
		{name: "cidr ipv4", ip: "10.1.2.3", trusted: []string{"10.0.0.0/8"}, want: true},
		{name: "cidr mismatch", ip: "11.1.2.3", trusted: []string{"10.0.0.0/8"}, want: false},
		{name: "exact ipv6 bracketed", ip: "[::1]", trusted: []string{"::1"}, want: true},
		{name: "cidr ipv6", ip: "2001:db8::5", trusted: []string{"2001:db8::/32"}, want: true},
		{name: "malformed ip", ip: "nope", trusted: []string{"10.0.0.0/8"}, want: false},
		{name: "empty entries ignored", ip: "10.0.0.5", trusted: []string{"", " "}, want: false},
		{name: "malformed cidr ignored", ip: "10.0.0.5", trusted: []string{"10.0.0.0/bad"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Trusted(tt.ip, tt.trusted); got != tt.want {
				t.Fatalf("Trusted(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}
