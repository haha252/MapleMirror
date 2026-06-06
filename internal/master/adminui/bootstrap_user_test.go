package adminui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsersGeneratesRandomInitialUserWhenFileMissing(t *testing.T) {
	var out bytes.Buffer
	old := bootstrapCredentialConsole
	bootstrapCredentialConsole = &out
	t.Cleanup(func() { bootstrapCredentialConsole = old })

	usersPath := filepath.Join(t.TempDir(), "admin-users.yaml")
	users, err := loadUsers(usersPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1", len(users))
	}
	username := onlyUsername(t, users)
	if len(username) != 8 {
		t.Fatalf("username length = %d, want 8", len(username))
	}
	output := out.String()
	if !strings.Contains(output, "密码：") || !strings.Contains(output, "用户名："+username) {
		t.Fatalf("missing generated credentials output: %s", output)
	}
	password := valueAfterLabel(t, output, "密码：")
	if len(password) != 20 {
		t.Fatalf("password length = %d, want 20", len(password))
	}
	if !verifyPassword(users[username].PasswordHash, password) {
		t.Fatal("generated password does not match persisted hash")
	}
	data, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), password) {
		t.Fatal("generated plaintext password must not be persisted")
	}
}

func TestLoadUsersKeepsBootstrapPasswordEnvBehavior(t *testing.T) {
	var out bytes.Buffer
	old := bootstrapCredentialConsole
	bootstrapCredentialConsole = &out
	t.Cleanup(func() { bootstrapCredentialConsole = old })

	t.Setenv("MIRROR_ADMIN_WEB_PASSWORD", "created-password")
	users, err := loadUsers(filepath.Join(t.TempDir(), "admin-users.yaml"),
		"MIRROR_ADMIN_WEB_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	user, ok := users["admin"]
	if !ok || !verifyPassword(user.PasswordHash, "created-password") {
		t.Fatal("bootstrap password env should create admin user")
	}
	if out.Len() != 0 {
		t.Fatalf("env bootstrap should not print generated password: %s", out.String())
	}
}

func onlyUsername(t *testing.T, users map[string]userRecord) string {
	t.Helper()
	for username := range users {
		return username
	}
	t.Fatal("missing user")
	return ""
}

func valueAfterLabel(t *testing.T, output, label string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, label) {
			return strings.TrimSpace(strings.TrimPrefix(line, label))
		}
	}
	t.Fatalf("missing label %s in %s", label, output)
	return ""
}
