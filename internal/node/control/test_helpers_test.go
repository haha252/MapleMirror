package control

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mirror-server/internal/storage"
)

func openNodeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}
