package syncer

import (
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/protocol"
)

func TestDeleteOlderGenerationLeavesNewerSamePublicPathFile(t *testing.T) {
	db, storageDir, _ := prepareSyncer(t)
	oldRel := filepath.Join("p1", "latest", ".mirror-assets", "old", "a.zip")
	newRel := filepath.Join("p1", "latest", ".mirror-assets", "new", "a.zip")
	oldPath := filepath.Join(storageDir, oldRel)
	newPath := filepath.Join(storageDir, newRel)
	for path, content := range map[string]string{oldPath: "oldold", newPath: "newnew"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO local_assets
		(asset_id,relative_path,digest_sha256,size_bytes,verified_at,state) VALUES
		('asset-old',?,?,6,'old','verified'),
		('asset-new',?,?,6,'new','verified')`, oldRel, digest("oldold"), newRel, digest("newnew")); err != nil {
		t.Fatal(err)
	}

	result := (Executor{DB: db, Storage: storageDir}).delete(protocol.SyncTask{
		TaskID: "delete-old", TaskType: "asset_delete", Asset: protocol.SyncAsset{AssetID: "asset-old"},
	})
	if result.Result != "succeeded" {
		t.Fatalf("delete old generation failed: %+v", result)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old generation file should be removed: %v", err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "newnew" {
		t.Fatalf("newer generation must remain intact data=%q err=%v", data, err)
	}
	var oldState, newState string
	if err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id='asset-old'`).Scan(&oldState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id='asset-new'`).Scan(&newState); err != nil {
		t.Fatal(err)
	}
	if oldState != "removed" || newState != "verified" {
		t.Fatalf("unexpected states old=%q new=%q", oldState, newState)
	}
}
