package capacity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSampleExistingAndMissingChild(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "missing", "child")} {
		s := samplePath(path)
		if !s.Valid || s.TotalBytes <= 0 || s.AvailableBytes < 0 || s.filesystemID == "" {
			t.Fatalf("invalid sample for %s: %+v", path, s)
		}
	}
}

func TestReserveDownloadSameFS(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "assets"), filepath.Join(dir, "tmp"))
	m.SafetyBytes = 0
	snap := m.Snapshot()
	if !snap.Asset.Valid || !snap.Partial.Valid {
		t.Fatal("capacity invalid")
	}
	release, err := m.ReserveDownload(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	after := m.Snapshot()
	if after.ReservedAsset != 1<<20 || after.ReservedPartial != 1<<20 {
		t.Fatalf("same fs reservation should be visible from both views: %+v", after)
	}
	release()
	if got := m.Snapshot().ReservedAsset; got != 0 {
		t.Fatalf("reserved=%d", got)
	}
}

func TestReserveImpossible(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, dir)
	m.SafetyBytes = 0
	_, err := m.ReserveDownload(1 << 62)
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("err=%v", err)
	}
	_ = os.RemoveAll(filepath.Join(dir, "unused"))
}

func TestReserveDownloadResamplesSuddenCapacityDrop(t *testing.T) {
	old := capacitySamplePath
	available := int64(10 << 20)
	capacitySamplePath = func(string) Sample {
		return Sample{AvailableBytes: available, TotalBytes: 20 << 20, Valid: true, filesystemID: "fs"}
	}
	t.Cleanup(func() { capacitySamplePath = old })
	m := NewManager("assets", "partials")
	m.SafetyBytes = 0
	release, err := m.ReserveDownload(6 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	available = 7 << 20
	if _, err := m.ReserveDownload(2 << 20); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("second reservation after capacity drop err=%v", err)
	}
}

func TestReserveDownloadRejectsUnknownFilesystemCapacity(t *testing.T) {
	old := capacitySamplePath
	capacitySamplePath = func(string) Sample { return Sample{} }
	t.Cleanup(func() { capacitySamplePath = old })
	m := NewManager("assets", "partials")
	if _, err := m.ReserveDownload(1); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("unknown capacity err=%v", err)
	}
}
