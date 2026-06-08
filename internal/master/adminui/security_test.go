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

func TestLoadOrCreateSecretRejectsUnreadableExistingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.key")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateSecret(path); err == nil ||
		!strings.Contains(err.Error(), "读取管理面板会话密钥失败") {
		t.Fatalf("existing unreadable secret path should fail, err=%v", err)
	}
}
