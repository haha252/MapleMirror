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
	old := time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, old)
	if err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "abcdef" {
		t.Fatalf("过期校验通过后应下载成功：code=%d body=%q", rec.Code, rec.Body.String())
	}
	waitForVerifiedAtChange(t, db, old)
}

func TestHandlerReusesFreshVerificationForModifiedFile(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	fresh := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339Nano)
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
		t.Fatalf("fresh window 内应复用校验结果：code=%d body=%q", rec.Code, rec.Body.String())
	}
	assertLocalAssetState(t, db, "verified")
}

func TestHandlerReusesFreshVerificationWithBackdatedMTime(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	freshAt := time.Now().Add(-10 * time.Second).UTC()
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, freshAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	tamperAssetWithBackdatedMTime(t, storageDir, freshAt)

	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "zzzzzz" {
		t.Fatalf("fresh backdated file should reuse cached verification: code=%d body=%q", rec.Code, rec.Body.String())
	}
	assertLocalAssetState(t, db, "verified")
}

func TestHandlerMarksExpiredMismatchedFile(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF,
		time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := serveVerifiedAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "zzzzzz" {
		t.Fatalf("过期但后台校验未完成前不应阻塞下载：code=%d body=%q", rec.Code, rec.Body.String())
	}
	waitForLocalAssetState(t, db, "mismatch")
	assertInventoryForceRequested(t, db)
}

func TestHandlerMarksExpiredMissingFile(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF,
		time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano))
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
	waitForLocalAssetState(t, db, "missing")
	assertInventoryForceRequested(t, db)
}

func tamperAssetWithBackdatedMTime(t *testing.T, storageDir string, verifiedAt time.Time) {
	t.Helper()
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	backdated := verifiedAt.Add(-time.Second)
	if err := os.Chtimes(path, backdated, backdated); err != nil {
		t.Fatal(err)
	}
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
	state := localAssetState(t, db)
	if state != want {
		t.Fatalf("本地资产状态 got=%q want=%q", state, want)
	}
}

func assertInventoryForceRequested(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var force string
		err := db.QueryRow(`SELECT COALESCE(force_report_requested_at, '')
			FROM inventory_report_cursor WHERE id = 1`).Scan(&force)
		if err == nil && force != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("资产校验失败后应强制下一次完整库存上报")
}

func waitForLocalAssetState(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if localAssetState(t, db) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("本地资产状态 got=%q want=%q", localAssetState(t, db), want)
}

func localAssetState(t *testing.T, db *sql.DB) string {
	t.Helper()
	var state string
	err := db.QueryRow(`SELECT state FROM local_assets WHERE asset_id = 'asset-1'`).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func waitForVerifiedAtChange(t *testing.T, db *sql.DB, old string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var state, verifiedAt string
		err := db.QueryRow(`SELECT state, verified_at FROM local_assets
			WHERE asset_id = 'asset-1'`).Scan(&state, &verifiedAt)
		if err != nil {
			t.Fatal(err)
		}
		if state == "verified" && verifiedAt != old {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("后台校验未刷新本地校验时间")
}
