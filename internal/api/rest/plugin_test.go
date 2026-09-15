package rest

import "testing"

func TestValidRepoURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://github.com/o/r/repo.json", true},
		{"http://example.com/repo.json", true},
		{"https://user:pass@example.com/repo.json", false}, // userinfo
		{"ftp://example.com/repo.json", false},             // non-http scheme
		{"https:///repo.json", false},                      // empty host
		{"not-a-url", false},
	}
	for _, c := range cases {
		if got := validRepoURL(c.raw); got != c.want {
			t.Errorf("validRepoURL(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
