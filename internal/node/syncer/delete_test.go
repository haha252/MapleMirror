package syncer

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/protocol"
)

func TestDeleteRemovesFileAndMarksLocalAssetRemoved(t *testing.T) {
	db, storageDir, _ := prepareSyncer(t)
	rel := filepath.Join("p1", "v1", "a.zip")
	assetPath := filepath.Join(storageDir, rel)
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedLocalAsset(t, db, "asset-1", rel, "verified")

	result := (Executor{DB: db, Storage: storageDir}).delete(deleteTask("asset-1"))
	if result.Result != "succeeded" {
		t.Fatalf("delete should succeed: %+v", result)
	}
	if _, err := os.Stat(assetPath); !os.IsNotExist(err) {
		t.Fatalf("asset file should be removed: %v", err)
	}
	assertLocalAssetState(t, db, "asset-1", "removed")
}

func TestDeleteFailureKeepsLocalAssetVerified(t *testing.T) {
	db, storageDir, _ := prepareSyncer(t)
	seedLocalAsset(t, db, "asset-1", filepath.Join("..", "escape.zip"), "verified")

	result := (Executor{DB: db, Storage: storageDir}).delete(deleteTask("asset-1"))
	if result.Result != "temporary_error" {
		t.Fatalf("unsafe delete should be temporary_error: %+v", result)
	}
	assertLocalAssetState(t, db, "asset-1", "verified")
}

func deleteTask(assetID string) protocol.SyncTask {
	return protocol.SyncTask{
		TaskID:   "delete-1",
		TaskType: "asset_delete",
		Asset: protocol.SyncAsset{
			AssetID: assetID,
		},
	}
}

func seedLocalAsset(t *testing.T, db *sql.DB, assetID, rel, state string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES (?, ?, 'sha256', 6, 'now', ?)`, assetID, rel, state)
	if err != nil {
		t.Fatal(err)
	}
}

func assertLocalAssetState(t *testing.T, db *sql.DB, assetID, want string) {
	t.Helper()
	var got string
	err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = ?`, assetID).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("local asset state=%s want=%s", got, want)
	}
}
