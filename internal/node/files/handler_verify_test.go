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
)

const testDigestABCDEF = "sha256:bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721"

func TestHandlerRefreshesExpiredVerificationBeforeDownload(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	old := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, old)
	if err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "abcdef" {
		t.Fatalf("过期校验通过后应下载成功：code=%d body=%q", rec.Code, rec.Body.String())
	}
	var state, verifiedAt string
	err = db.QueryRow(`SELECT state, verified_at FROM local_assets
		WHERE asset_id = 'asset-1'`).Scan(&state, &verifiedAt)
	if err != nil || state != "verified" || verifiedAt == old {
		t.Fatalf("应刷新本地校验时间：state=%q verified_at=%q err=%v", state, verifiedAt, err)
	}
}

func TestHandlerSkipsFreshVerificationWindow(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	fresh := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, fresh)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "zzzzzz" {
		t.Fatalf("1 分钟内应跳过 hash 重新校验：code=%d body=%q", rec.Code, rec.Body.String())
	}
	var state, verifiedAt string
	err = db.QueryRow(`SELECT state, verified_at FROM local_assets
		WHERE asset_id = 'asset-1'`).Scan(&state, &verifiedAt)
	if err != nil || state != "verified" || verifiedAt != fresh {
		t.Fatalf("跳过 hash 时不应刷新状态：state=%q verified_at=%q err=%v", state, verifiedAt, err)
	}
}

func TestHandlerMarksExpiredMismatchedFile(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF,
		time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("过期后 hash 不一致应拒绝下载：%d", rec.Code)
	}
	assertLocalAssetState(t, db, "mismatch")
}

func TestHandlerMarksExpiredMissingFile(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF,
		time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(storageDir, "p1", "v1", "a.zip")); err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("过期后文件缺失应拒绝下载：%d", rec.Code)
	}
	assertLocalAssetState(t, db, "missing")
}

func serveVerifiedAsset(t *testing.T, db *sql.DB, storageDir string, signer downloadtoken.Signer) *httptest.ResponseRecorder {
	t.Helper()
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-verify", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-verify"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	return rec
}

func assertLocalAssetState(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var state string
	err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'asset-1'`).Scan(&state)
	if err != nil || state != want {
		t.Fatalf("本地资产状态 got=%q want=%q err=%v", state, want, err)
	}
}
