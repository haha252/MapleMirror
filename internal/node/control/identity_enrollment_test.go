package control

import (
	"os"
	"path/filepath"
	"testing"

	"mirror-server/internal/storage"
)

func TestPairingCodeFallbackTrimsWhitespace(t *testing.T) {
	db, err := storage.OpenNode(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(t.TempDir(), "pairing-code")
	if err := os.WriteFile(path, []byte("  abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := IdentityStore{DB: db}
	got, err := store.PairingCode(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc123" {
		t.Fatalf("pairing code=%q", got)
	}
	var persisted string
	if err := db.QueryRow(`SELECT pairing_code FROM node_enrollment_state WHERE id=1`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != "abc123" {
		t.Fatalf("persisted pairing code=%q", persisted)
	}
}
