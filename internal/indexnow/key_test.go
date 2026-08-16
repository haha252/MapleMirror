package indexnow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyGeneratesPersistsAndReusesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "indexnow.key")
	first, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || len(first.Value) != 64 || !validKey(first.Value) {
		t.Fatalf("generated key is invalid: %+v", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key permissions=%o, want 600", got)
	}

	second, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.Value != first.Value {
		t.Fatalf("persisted key was not reused: first=%+v second=%+v", first, second)
	}
}

func TestLoadOrCreateKeyRejectsInvalidExistingKeyWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "indexnow.key")
	original := "bad key\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("invalid existing key should fail")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("invalid key was overwritten: %q", data)
	}
}
