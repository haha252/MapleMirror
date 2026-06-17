package syncer

import (
	"context"
	"database/sql"
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
	var updatedAt string
	err = db.QueryRow(`SELECT next_revision, last_acked_revision, updated_at
		FROM inventory_report_cursor WHERE id = 1`).
		Scan(&nextRevision, &lastAcked, &updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if nextRevision != 7 || lastAcked != 6 {
		t.Fatalf("inventory revision cursor changed next=%d acked=%d",
			nextRevision, lastAcked)
	}
	if updatedAt != "" {
		t.Fatalf("inventory reconcile should clear throttle timestamp, got %q", updatedAt)
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
