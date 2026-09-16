package syncer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMoveAssetFilePreservesTargetOnENOSPC(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source-enospc.tmp")
	dst := filepath.Join(dir, "asset-enospc.zip")
	if err := os.WriteFile(src, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRename, oldCopy := assetRename, assetCopy
	assetRename = func(from, to string) error {
		if from == src && to == dst {
			return errors.New("force copy fallback")
		}
		return oldRename(from, to)
	}
	assetCopy = func(io.Writer, io.Reader) (int64, error) { return 0, syscall.ENOSPC }
	t.Cleanup(func() { assetRename, assetCopy = oldRename, oldCopy })

	if err := moveAssetFile(src, dst); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("err=%v want ENOSPC", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "stale" {
		t.Fatalf("old target changed after ENOSPC data=%q err=%v", data, err)
	}
	data, err = os.ReadFile(src)
	if err != nil || string(data) != "fresh" {
		t.Fatalf("source should remain retryable data=%q err=%v", data, err)
	}
}
