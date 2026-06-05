package adminui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func TestNewRejectsInvalidWebDuration(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(dir, "master.db"), BusyTimeout: "5s", WAL: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	usersPath := filepath.Join(dir, "users.yaml")
	hash := testPasswordHash("correct-password")
	body := "users:\n  - username: admin\n    password_hash: \"" + hash + "\"\n"
	if err := os.WriteFile(usersPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled := true
	_, err = New(config.Administration{
		AllowedCIDRs: []string{"127.0.0.0/8"},
		Web: config.AdminWeb{
			Enabled: &enabled, UsersFile: usersPath,
			SessionSecretFile: filepath.Join(dir, "session.key"),
			SessionTTL:        "not-a-duration", LoginFailureWindow: "24h",
			LoginFailureLimit: 3, LoginBanDuration: "168h",
		},
	}, mastercontrol.Repository{DB: db}, mirrorsync.Store{DB: db}, Options{})
	if err == nil || !strings.Contains(err.Error(), "admin.web.session_ttl") {
		t.Fatalf("expected session_ttl duration error, got %v", err)
	}
}
