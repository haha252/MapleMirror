package files

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
	"mirror-server/internal/storage"
)

func TestHandlerServesVerifiedAssetRange(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: "download.v1",
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=1-3")
	rec := httptest.NewRecorder()
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "bcd" {
		t.Fatalf("Range 下载响应不符合预期：code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsCrossAssetToken(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: "download.v1",
		AuthorizationID: "auth-1", AssetID: "other", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("跨资产令牌应被拒绝：%d", rec.Code)
	}
}

func prepareNodeFile(t *testing.T) (*sql.DB, string, downloadtoken.Signer) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenNode(filepath.Join(dir, "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	storageDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storageDir, "asset.bin"), []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', 'asset.bin', 'sha256:aa', 6, 'now', 'verified')`)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, []byte("12345678901234567890123456789012"), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := downloadtoken.NewFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return db, storageDir, signer
}
