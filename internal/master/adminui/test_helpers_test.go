package adminui

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mirror-server/internal/config"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(dir, "master.db"), BusyTimeout: "5s", WAL: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	usersPath := filepath.Join(dir, "users.yaml")
	body := "users:\n  - username: admin\n    password_hash: \"" +
		testPasswordHash("correct-password") + "\"\n"
	if err := os.WriteFile(usersPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(config.Administration{
		Web: config.AdminWeb{
			UsersFile: usersPath, SessionSecretFile: filepath.Join(dir, "session.key"),
			SessionTTL: "12h", LoginFailureWindow: "24h",
			LoginFailureLimit: 3, LoginBanDuration: "168h",
		},
	}, mastercontrol.Repository{DB: db}, mirrorsync.Store{DB: db}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return server, db
}

func withAdminUser(req *http.Request) *http.Request {
	return withAdminUsername(req, "admin")
}

func withAdminUsername(req *http.Request, username string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), usernameKey{}, username))
}

func mustExecAdminUI(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func loginRequest(username, password string) *http.Request {
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func sessionFrom(cookies []*http.Cookie) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	return nil
}

func testPasswordHash(password string) string {
	salt := []byte("0123456789abcdef")
	key := pbkdf2SHA256([]byte(password), salt, 100000, 32)
	return "pbkdf2-sha256$100000$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key)
}
