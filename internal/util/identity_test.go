package util

import "testing"

func TestNormalizeServer(t *testing.T) {
	same := []string{"https://pb.example.com", "https://pb.example.com/", "HTTPS://PB.example.com", "https://pb.example.com:443/", " https://pb.example.com "}
	for _, s := range same {
		if got := normalizeServer(s); got != "https://pb.example.com" {
			t.Errorf("normalizeServer(%q) = %q", s, got)
		}
	}
	if got := normalizeServer("https://pb.example.com:8443/sub/"); got != "https://pb.example.com:8443/sub" {
		t.Errorf("port/path: %q", got)
	}
}
