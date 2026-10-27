package web

import "testing"

func TestNotificationHrefCommentAndMentionDeepLink(t *testing.T) {
	cases := map[string]string{
		"comment":        "/checkins/abc#comments",
		"mention":        "/checkins/abc#comments",
		"toast":          "/checkins/abc",
		"badge":          "/you",
		"friend_request": "/friends",
		"friend_accept":  "/friends",
	}
	for kind, want := range cases {
		if got := notificationHref(kind, "abc"); got != want {
			t.Fatalf("notificationHref(%q) = %q, want %q", kind, got, want)
		}
	}
	if got := notificationHref("comment", ""); got != "/" {
		t.Fatalf("empty comment subject = %q, want /", got)
	}
}
