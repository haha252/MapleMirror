package downloadurl

import "testing"

func TestNormalizeBaseAcceptsTrustedOrigins(t *testing.T) {
	cases := map[string]string{
		"https://Node-1.Example.Com/":   "https://node-1.example.com",
		"https://node.example.com:8443": "https://node.example.com:8443",
		"http://127.0.0.1:8081":         "http://127.0.0.1:8081",
		"http://localhost:8081/":        "http://localhost:8081",
	}
	for raw, want := range cases {
		got, ok := NormalizeBase(raw)
		if !ok || got != want {
			t.Fatalf("NormalizeBase(%q) = %q/%v, want %q/true", raw, got, ok, want)
		}
	}
}

func TestNormalizeBaseRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{
		"http://node.example.com",
		"https://user:pass@node.example.com",
		"https://node.example.com/prefix",
		"https://node.example.com?next=https://evil.example",
		"https://node.example.com/#frag",
		"ftp://node.example.com",
		"not-a-url",
	} {
		if got, ok := NormalizeBase(raw); ok {
			t.Fatalf("NormalizeBase(%q) unexpectedly accepted as %q", raw, got)
		}
	}
}

func TestJoinRejectsUnsafeBase(t *testing.T) {
	if _, err := Join("https://node.example.com/prefix", "/p1/v1/a.zip"); err == nil {
		t.Fatal("Join should reject unsafe base URL")
	}
	got, err := Join("https://node.example.com", "/p1/v1/a%20b.zip")
	if err != nil || got != "https://node.example.com/p1/v1/a%20b.zip" {
		t.Fatalf("Join result mismatch: %q err=%v", got, err)
	}
	if _, err := Join("https://node.example.com", "p1/v1/a.zip"); err == nil {
		t.Fatal("Join should reject relative paths")
	}
}
