package adminui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsersRejectsDuplicateUsernames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.yaml")
	hash := testPasswordHash("correct-password")
	body := strings.Join([]string{
		"users:",
		"  - username: admin",
		"    password_hash: \"" + hash + "\"",
		"  - username: admin",
		"    password_hash: \"" + hash + "\"",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadUsers(path, ""); err == nil || !strings.Contains(err.Error(), "重复用户名") {
		t.Fatalf("duplicate usernames should be rejected, err=%v", err)
	}
}
