package syncer

import (
	"strings"
	"testing"
)

func TestSafeNameWindowsCompatible(t *testing.T) {
	got := safeName("FoldCraftLauncher:328422826:428255897", "FCL-release-1.3.0.8-arm64-v8a.apk")

	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("safeName produced Windows-invalid characters: %q", got)
	}
	if !strings.HasSuffix(got, ".apk") {
		t.Fatalf("safeName should preserve file extension, got %q", got)
	}
	if strings.Contains(got, "FoldCraftLauncher:") {
		t.Fatalf("safeName should sanitize asset id, got %q", got)
	}
}

func TestSafeNameFallsBackForEmptyName(t *testing.T) {
	got := safeName("asset:1", "")
	if got == "" {
		t.Fatal("safeName returned empty string")
	}
	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("safeName produced Windows-invalid characters: %q", got)
	}
}
