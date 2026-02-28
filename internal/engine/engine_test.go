package engine

import "testing"

func TestHostname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/path?q=1", "example.com"},
		{"https://sub.domain.org:8080/foo", "sub.domain.org"},
		{"http://localhost", "localhost"},
		{"wss://stream.example.com/feed", "stream.example.com"},
		{"example.com", "example.com"}, // raw hostname (DNS target)
		{"", ""},
	}
	for _, tc := range tests {
		got := hostname(tc.input)
		if got != tc.want {
			t.Errorf("hostname(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
