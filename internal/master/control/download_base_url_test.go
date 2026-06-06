package control

import "testing"

func TestNormalizedPublicDownloadBaseURLRejectsUnsafeBase(t *testing.T) {
	for _, raw := range []string{
		"http://node.example.com",
		"https://node.example.com/prefix",
		"https://node.example.com?next=https://evil.example",
		"https://user:pass@node.example.com",
	} {
		if got := normalizedPublicDownloadBaseURL(raw); got != "" {
			t.Fatalf("unsafe base %q should be rejected, got %q", raw, got)
		}
	}
}

func TestNormalizedPublicDownloadBaseURLAllowsLoopbackHTTP(t *testing.T) {
	got := normalizedPublicDownloadBaseURL("http://127.0.0.1:8081/")
	if got != "http://127.0.0.1:8081" {
		t.Fatalf("loopback HTTP base should be normalized, got %q", got)
	}
}
