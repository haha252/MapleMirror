package syncer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanSwarmPartialsRemovesStaleAndKeepsFresh(t *testing.T) {
	db, storage, temp := prepareSyncer(t)
	root := filepath.Join(effectiveTempDir(storage, temp), "swarm-partials")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "old.part")
	freshPath := filepath.Join(root, "fresh.part")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	fresh := now.Format(time.RFC3339Nano)
	for _, item := range []struct{ asset, manifest, path, at string }{{"a-old", "m-old", oldPath, old}, {"a-fresh", "m-fresh", freshPath, fresh}} {
		_, err := db.Exec(`INSERT INTO swarm_partials(asset_id,manifest_id,partial_path,asset_size,piece_size,piece_count,verified_bitmap,created_at,updated_at,last_access_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, item.asset, item.manifest, item.path, 1, 1<<20, 1, []byte{1}, item.at, item.at, item.at)
		if err != nil {
			t.Fatal(err)
		}
	}
	removed, err := CleanSwarmPartials(db, storage, temp, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed=%d want=1", removed)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("stale partial remains: %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh partial removed: %v", err)
	}
}

func TestCleanSwarmPartialsNeverDeletesUnsafeDBPath(t *testing.T) {
	db, storage, temp := prepareSyncer(t)
	outside := filepath.Join(t.TempDir(), "outside.part")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO swarm_partials(asset_id,manifest_id,partial_path,asset_size,piece_size,piece_count,verified_bitmap,created_at,updated_at,last_access_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "a", "m", outside, 1, 1<<20, 1, []byte{1}, old, old, old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CleanSwarmPartials(db, storage, temp, 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("unsafe external path was deleted: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM swarm_partials WHERE asset_id='a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unsafe stale DB row should still be dropped")
	}
}
