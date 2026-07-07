package urlsafety

import "testing"

func TestValidateNtfyURL(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		allowPrivate bool
		wantOK       bool
		wantPrivate  bool
	}{
		{name: "empty", url: "", wantOK: true},
		{name: "https_public", url: "https://ntfy.sh/topic", wantOK: true},
		{name: "private_ipv4_blocked", url: "http://127.0.0.1/topic", wantOK: false, wantPrivate: true},
		{name: "private_ipv4_allowed", url: "http://127.0.0.1/topic", allowPrivate: true, wantOK: true, wantPrivate: true},
		{name: "localhost_blocked", url: "http://localhost/topic", wantOK: false, wantPrivate: true},
		{name: "invalid_scheme", url: "ftp://example.com/topic", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateNtfyURL(tc.url, tc.allowPrivate)
			if got.OK != tc.wantOK || got.IsPrivate != tc.wantPrivate {
				t.Fatalf("unexpected validation result: %#v", got)
			}
		})
	}
}
