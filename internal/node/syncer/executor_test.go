package syncer

import (
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/protocol"
)

func TestRelativeAssetPathUsesProjectVersionAndFileName(t *testing.T) {
	got := relativeAssetPath(protocol.SyncAsset{
		AssetID:   "FoldCraftLauncher:328422826:428255897",
		ProjectID: "fcl",
		Version:   "1.3.0.8",
		FileName:  "FCL-release-1.3.0.8-arm64-v8a.apk",
	})

	if strings.ContainsAny(got, `<>:"|?*`) {
		t.Fatalf("relative path contains Windows-invalid characters: %q", got)
	}
	if !strings.HasSuffix(got, ".apk") {
		t.Fatalf("relative path should preserve file extension, got %q", got)
	}
	if strings.Contains(got, "FoldCraftLauncher:") {
		t.Fatalf("relative path should not include asset id when project/version exist, got %q", got)
	}
	if got != filepath.Join("fcl", "1.3.0.8", "FCL-release-1.3.0.8-arm64-v8a.apk") {
		t.Fatalf("relative path should use project/version/file name, got %q", got)
	}
}

func TestRelativeAssetPathFallsBackForLegacyTask(t *testing.T) {
	got := relativeAssetPath(protocol.SyncAsset{AssetID: "asset:1", FileName: ""})
	if got == "" {
		t.Fatal("relative path returned empty string")
	}
	if strings.ContainsAny(got, `<>:"|?*`) {
		t.Fatalf("relative path contains Windows-invalid characters: %q", got)
	}
}
