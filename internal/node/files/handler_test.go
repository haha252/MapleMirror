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
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
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
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename=a.zip" {
		t.Fatalf("Content-Disposition 不符合预期：%q", got)
	}
	var bytes int64
	var masterReq string
	err = db.QueryRow(`SELECT sent_bytes, master_request_id FROM pending_traffic_events
		WHERE authorization_id = 'auth-1'`).Scan(&bytes, &masterReq)
	if err != nil || bytes != 3 || masterReq != "req-1" {
		t.Fatalf("真实流量事件未正确记录：bytes=%d req=%q err=%v", bytes, masterReq, err)
	}
}

func TestHandlerServesReadableAssetPathWithQueryToken(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip?token="+token, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Range", "bytes=1-3")
	rec := httptest.NewRecorder()
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "bcd" {
		t.Fatalf("可读路径 Range 下载响应不符合预期：code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsHEADWithoutActivatingAuthorization(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-head", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-head"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodHead, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler := &Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD should be rejected: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_authorizations
		WHERE authorization_id = 'auth-head'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("HEAD should not activate local authorization: rows=%d err=%v", rows, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pending_traffic_events
		WHERE authorization_id = 'auth-head'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("HEAD should not record traffic: rows=%d err=%v", rows, err)
	}
}

func TestHandlerReadablePathUsesTokenAssetIDWhenPathHasOldRows(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-old', ?, 'sha256:old', 6, ?, 'verified')`,
		filepath.Join("p1", "v1", "a.zip"), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, err := signer.Sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/p1/v1/a.zip?token="+token, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("可读路径应按令牌资产定位，code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAllowsLargeRequestedRangeWhenActualBytesFit(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`INSERT INTO pending_traffic_events
		(event_sequence, authorization_id, node_request_id, master_request_id,
		sent_bytes, created_at, asset_id, status)
		VALUES (1, 'auth-1', 'node-req-1', 'master-req-1', 5, 'now', 'asset-1', 'completed')`)
	if err != nil {
		t.Fatal(err)
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 6, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=5-")
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "f" {
		t.Fatalf("实际发送字节未超限时应允许：code=%d body=%q", rec.Code, rec.Body.String())
	}
	var bytes int64
	err = db.QueryRow(`SELECT COALESCE(SUM(sent_bytes), 0)
		FROM pending_traffic_events WHERE authorization_id = 'auth-1'`).Scan(&bytes)
	if err != nil || bytes != 6 {
		t.Fatalf("应按真实发送字节累计授权用量：bytes=%d err=%v", bytes, err)
	}
}

func TestHandlerRejectsAuthorizationWhenActualBytesAlreadyExhausted(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	_, err := db.Exec(`INSERT INTO pending_traffic_events
		(event_sequence, authorization_id, node_request_id, master_request_id,
		sent_bytes, created_at, asset_id, status)
		VALUES (1, 'auth-1', 'node-req-1', 'master-req-1', 6, 'now', 'asset-1', 'completed')`)
	if err != nil {
		t.Fatal(err)
	}
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 6, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=5-")
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("真实发送字节已达上限时应拒绝：%d", rec.Code)
	}
}

func TestHandlerRejectsCrossAssetToken(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
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

func TestHandlerAllowsDifferentClientPrefix(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer,
		TrustedCIDRs: []string{"127.0.0.0/8"}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("下载节点不应要求请求来源前缀与令牌一致：%d", rec.Code)
	}
}

func TestHandlerUsesForwardedHeaderFromTrustedRemote(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: "auth-1", AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
		MaxBytes: 10, RangeConcurrencyLimit: 2, RequestID: "req-1"}
	token, _ := signer.Sign(claims)
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1", Signer: signer,
		TrustedCIDRs: []string{"127.0.0.0/8"}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("可信代理头应通过客户端前缀校验：%d", rec.Code)
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
	assetPath := filepath.Join(storageDir, "p1", "v1", "a.zip")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO local_assets
		(asset_id, relative_path, digest_sha256, size_bytes, verified_at, state)
		VALUES ('asset-1', ?, 'sha256:bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721', 6, ?, 'verified')`,
		filepath.Join("p1", "v1", "a.zip"), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(dir, "token.key")
	publicPath := filepath.Join(dir, "token.pub")
	if err := downloadtoken.GenerateKeyFiles(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	signer, err := downloadtoken.NewSignerFromPrivateFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	return db, storageDir, signer
}
