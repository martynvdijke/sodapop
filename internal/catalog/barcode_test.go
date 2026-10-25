package catalog

import "testing"

func TestNormalizeGTIN(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "upc padded", in: "5449000000996", want: "05449000000996"},
		{name: "already gtin14", in: "05449000000996", want: "05449000000996"},
		{name: "spaces trimmed", in: " 5449000000996 ", want: "05449000000996"},
		{name: "bad check digit", in: "5449000000997", wantErr: true},
		{name: "too short", in: "123", wantErr: true},
		{name: "letters", in: "54490000abc96", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeGTIN(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Fentimans Curiosity Cola": "fentimans-curiosity-cola",
		"  Cherry & Berry  ":       "cherry-and-berry",
		"Club Soda":                "club-soda",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
