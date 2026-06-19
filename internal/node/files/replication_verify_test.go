package files

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandlerReusesFreshReplicationVerification(t *testing.T) {
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
	rec := serveReplicationAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "zzzzzz" {
		t.Fatalf("fresh replication verification should be reused: code=%d body=%q", rec.Code, rec.Body.String())
	}
	assertLocalAssetState(t, db, "verified")
}

func TestHandlerRefreshesExpiredReplicationVerification(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	old := time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, old)
	if err != nil {
		t.Fatal(err)
	}
	rec := serveReplicationAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "abcdef" {
		t.Fatalf("expired valid replication should refresh and serve: code=%d body=%q", rec.Code, rec.Body.String())
	}
	waitForVerifiedAtChange(t, db, old)
}

func TestHandlerRejectsExpiredModifiedReplicationAsset(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	old := time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, old)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.WriteFile(path, []byte("zzzzzz"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := serveReplicationAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expired modified replication asset should be rejected: %d", rec.Code)
	}
	assertLocalAssetState(t, db, "mismatch")
}

func TestHandlerReusesBackdatedFreshReplicationVerification(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	freshAt := time.Now().Add(-10 * time.Second).UTC()
	_, err := db.Exec(`UPDATE local_assets SET digest_sha256 = ?, verified_at = ?
		WHERE asset_id = 'asset-1'`, testDigestABCDEF, freshAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	tamperAssetWithBackdatedMTime(t, storageDir, freshAt)
	rec := serveReplicationAsset(t, db, storageDir, signer)
	if rec.Code != http.StatusOK || rec.Body.String() != "zzzzzz" {
		t.Fatalf("fresh backdated replication should be reused: code=%d body=%q", rec.Code, rec.Body.String())
	}
	assertLocalAssetState(t, db, "verified")
}
