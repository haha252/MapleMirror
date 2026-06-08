package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/master/mirrorsync"
	"mirror-server/internal/storage"
)

func TestAdminHandlerOnlyServesWebPanel(t *testing.T) {
	handler := newAdminHandlerForTest(t)
	for _, path := range []string{
		"/api/admin/v1",
		"/api/admin/v1/",
		"/api/admin/v1/nodes",
		"/api/admin/v1/pairing-codes",
		"/api/admin/v1/projects/p1/reset",
		"/api/admin/v1/sync/scans",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:55000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("raw admin api %s status = %d", path, rec.Code)
		}
	}

	cookie := adminLoginCookie(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/nodes", nil)
	req.RemoteAddr = "127.0.0.1:55000"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("web api status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminHandlerRootEntrypointsRedirectToPanel(t *testing.T) {
	handler := newAdminHandlerForTest(t)
	for _, path := range []string{"/", "/admin"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:55000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/" {
			t.Fatalf("%s redirect = %d %s", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestAdminWebHTTPSEnabledDefaultsToTLS(t *testing.T) {
	if !adminWebHTTPSEnabled(config.Master{}) {
		t.Fatal("管理 Web 默认应启用后端 HTTPS")
	}
	disabled := false
	cfg := config.Master{Admin: config.Administration{Web: config.AdminWeb{HTTPSEnabled: &disabled}}}
	if adminWebHTTPSEnabled(cfg) {
		t.Fatal("https_enabled=false 应关闭后端 HTTPS")
	}
}

func newAdminHandlerForTest(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenMaster(config.Database{
		Path: filepath.Join(dir, "master.db"), BusyTimeout: "5s", WAL: boolPtrMaster(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	usersPath := filepath.Join(dir, "users.yaml")
	if err := os.WriteFile(usersPath, []byte(adminUsersYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Master{
		RequestID: config.RequestID{ResponseHeader: "X-Request-ID", ParentHeader: "X-Request-ID"},
		Admin: config.Administration{
			Web: config.AdminWeb{
				UsersFile:         usersPath,
				SessionSecretFile: filepath.Join(dir, "session.key"),
				SessionTTL:        "12h", LoginFailureWindow: "24h",
				LoginFailureLimit: 3, LoginBanDuration: "168h",
			},
		},
	}
	logger, err := logging.New("test", config.Logging{
		ConsoleLevel: "error", FileLevel: "error", Directory: filepath.Join(dir, "logs"),
		RetentionDays: 1,
	}, time.UTC, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	sync := mirrorsync.Service{Scanner: mirrorsync.Scanner{Store: mirrorsync.Store{DB: db}}}
	handler, err := adminHandler(cfg, mastercontrol.Repository{DB: db}, sync, nil, logger,
		mastercontrol.CertificateSigner{})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func adminLoginCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {"admin"}, "password": {"correct-password"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "127.0.0.1:55000"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "mirror_admin_session" {
			return cookie
		}
	}
	t.Fatal("missing admin session cookie")
	return nil
}

func adminUsersYAML() string {
	salt := []byte("0123456789abcdef")
	key := testPBKDF2([]byte("correct-password"), salt, 100000, sha256.Size)
	hash := "pbkdf2-sha256$100000$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(key)
	return "users:\n  - username: admin\n    password_hash: \"" + hash + "\"\n"
}

func boolPtrMaster(value bool) *bool {
	return &value
}

func testPBKDF2(password, salt []byte, iterations, keyLen int) []byte {
	var out []byte
	var blockIndex uint32 = 1
	for len(out) < keyLen {
		u := testPRF(password, appendTestBlockIndex(salt, blockIndex))
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			u = testPRF(password, u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
		blockIndex++
	}
	return out[:keyLen]
}

func testPRF(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func appendTestBlockIndex(salt []byte, index uint32) []byte {
	out := make([]byte, len(salt)+4)
	copy(out, salt)
	out[len(salt)] = byte(index >> 24)
	out[len(salt)+1] = byte(index >> 16)
	out[len(salt)+2] = byte(index >> 8)
	out[len(salt)+3] = byte(index)
	return out
}
