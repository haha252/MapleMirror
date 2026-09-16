package files

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestReadablePathUsesAuthorizedGenerationWithIdentityIsolatedStorage(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	oldRel := filepath.Join("p1", "latest", ".mirror-assets", "old", "a.zip")
	newRel := filepath.Join("p1", "latest", ".mirror-assets", "new", "a.zip")
	seedRetainedFileAsset(t, db, storageDir, "asset-old", oldRel, "oldold")
	seedRetainedFileAsset(t, db, storageDir, "asset-new", newRel, "newnew")

	for _, tc := range []struct {
		name, assetID, body string
	}{
		{name: "old generation", assetID: "asset-old", body: "oldold"},
		{name: "new generation", assetID: "asset-new", body: "newnew"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := signer.Sign(downloadtoken.Claims{
				AuthorizationID: "auth-" + tc.assetID, AssetID: tc.assetID, NodeID: "node-1",
				ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
				MaxBytes: 12, RangeConcurrencyLimit: 2, RequestID: "req-" + tc.assetID,
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/p1/latest/a.zip", nil)
			req.RemoteAddr = "192.0.2.1:12345"
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
				t.Fatalf("same public path should serve authorized generation code=%d body=%q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestReadablePathRejectsAuthorizedAssetOnDifferentPublicPath(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	rel := filepath.Join("p1", "latest", ".mirror-assets", "old", "a.zip")
	seedRetainedFileAsset(t, db, storageDir, "asset-old", rel, "oldold")
	token, err := signer.Sign(downloadtoken.Claims{
		AuthorizationID: "auth-old", AssetID: "asset-old", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 12, RangeConcurrencyLimit: 2, RequestID: "req-old",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/p1/other/a.zip", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("token must not make a different public path resolve, code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func seedRetainedFileAsset(t *testing.T, db *sql.DB, storageDir, assetID, rel, content string) {
	t.Helper()
	path := filepath.Join(storageDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if _, err := db.Exec(`INSERT INTO local_assets
		(asset_id,relative_path,digest_sha256,size_bytes,verified_at,state)
		VALUES(?,?,?,?,?,'verified')`, assetID, rel, digest, len(content), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}
