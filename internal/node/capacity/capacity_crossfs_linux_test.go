//go:build linux

package capacity

import (
	"os"
	"testing"
)

func TestReserveDownloadDifferentFilesystems(t *testing.T) {
	asset := t.TempDir()
	partial, err := os.MkdirTemp("/dev/shm", "mirror-capacity-")
	if err != nil {
		t.Skipf("/dev/shm unavailable: %v", err)
	}
	defer os.RemoveAll(partial)
	as, ps := samplePath(asset), samplePath(partial)
	if !as.Valid || !ps.Valid || as.filesystemID == ps.filesystemID {
		t.Skip("test host does not expose two distinct filesystems")
	}
	m := NewManager(asset, partial)
	m.SafetyBytes = 0
	release, err := m.ReserveDownload(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	if len(m.reservedByFS) != 2 || m.reservedByFS[as.filesystemID] != 1<<20 || m.reservedByFS[ps.filesystemID] != 1<<20 {
		t.Fatalf("cross-fs reservation=%v", m.reservedByFS)
	}
	m.mu.Unlock()
	release()
}
