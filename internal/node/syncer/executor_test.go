package syncer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/storage"
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

func TestDownloadReusesExistingTargetFileMarkedRemoved(t *testing.T) {
	db, storageDir, tempDir := prepareSyncer(t)
	rel := filepath.Join("p1", "v1", "a.zip")
	path := filepath.Join(storageDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', ?, ?, 6, 'old', 'removed')`, rel, digest("abcdef"))
	if err != nil {
		t.Fatal(err)
	}

	task := fallbackTask("http://primary.test/asset.zip", "", digest("abcdef"), 6)
	result := (Executor{DB: db, Storage: storageDir, TempDir: tempDir}).download(context.Background(), task)
	if result.Result != "succeeded" || result.LocalDigestSHA256 != digest("abcdef") {
		t.Fatalf("existing target file should be reused: %+v", result)
	}
	var state string
	err = db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'asset-1'`).Scan(&state)
	if err != nil || state != "verified" {
		t.Fatalf("local asset state=%q err=%v", state, err)
	}
}

func TestInventoryReconcileForcesNextFullInventoryReport(t *testing.T) {
	db := openSyncerNodeDB(t)
	defer db.Close()
	now := time.Now().UTC()
	recent := now.Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at)
		VALUES (1, 7, 6, ?)`, recent)
	if err != nil {
		t.Fatal(err)
	}

	exec := Executor{DB: db}
	result := exec.Execute(context.Background(), protocol.SyncTask{
		TaskID:   "task-reconcile",
		TaskType: "inventory_reconcile",
	})
	if result.Result != "succeeded" {
		t.Fatalf("inventory reconcile result = %+v", result)
	}

	var nextRevision, lastAcked uint64
	var updatedAt, forceRequestedAt string
	err = db.QueryRow(`SELECT next_revision, last_acked_revision, updated_at,
			COALESCE(force_report_requested_at, '')
		FROM inventory_report_cursor WHERE id = 1`).
		Scan(&nextRevision, &lastAcked, &updatedAt, &forceRequestedAt)
	if err != nil {
		t.Fatal(err)
	}
	if nextRevision != 7 || lastAcked != 6 {
		t.Fatalf("inventory revision cursor changed next=%d acked=%d",
			nextRevision, lastAcked)
	}
	if updatedAt != recent {
		t.Fatalf("inventory reconcile should keep throttle timestamp, got %q", updatedAt)
	}
	if forceRequestedAt == "" {
		t.Fatal("inventory reconcile should set forced inventory marker")
	}
}

func TestInventoryReconcileKeepsExistingForceRequest(t *testing.T) {
	db := openSyncerNodeDB(t)
	defer db.Close()
	recent := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO inventory_report_cursor
		(id, next_revision, last_acked_revision, updated_at, force_report_requested_at)
		VALUES (1, 7, 6, ?, 'force-existing')`, recent)
	if err != nil {
		t.Fatal(err)
	}

	exec := Executor{DB: db}
	result := exec.Execute(context.Background(), protocol.SyncTask{
		TaskID:   "task-reconcile",
		TaskType: "inventory_reconcile",
	})
	if result.Result != "succeeded" {
		t.Fatalf("inventory reconcile result = %+v", result)
	}

	var forceRequestedAt string
	err = db.QueryRow(`SELECT COALESCE(force_report_requested_at, '')
		FROM inventory_report_cursor WHERE id = 1`).Scan(&forceRequestedAt)
	if err != nil {
		t.Fatal(err)
	}
	if forceRequestedAt != "force-existing" {
		t.Fatalf("inventory reconcile refreshed existing force marker: %q", forceRequestedAt)
	}
}

func openSyncerNodeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}
