package files

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestHandlerRejectsFirstConnectionAfterTokenWindow(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := testTimedClaims("auth-first", time.Now().Add(-30*time.Second), 5*time.Minute)
	token, _ := signer.Sign(claims)
	rec := serveDownload(t, db, storageDir, signer, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("首连超时应拒绝：code=%d body=%s", rec.Code, rec.Body.String())
	}
	assertLocalAuthorizationStatus(t, db, "auth-first", authorizationStatusExpiredFirstConnection)
}

func TestHandlerPersistsFirstConnectionState(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	claims := testTimedClaims("auth-active", time.Now().Add(-time.Second), time.Minute)
	token, _ := signer.Sign(claims)
	rec := serveDownload(t, db, storageDir, signer, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("首连窗口内应允许：code=%d body=%s", rec.Code, rec.Body.String())
	}
	assertLocalAuthorizationStatus(t, db, "auth-active", authorizationStatusActive)
}

func TestHandlerRejectsIdleAuthorizationAfterRestart(t *testing.T) {
	db, storageDir, signer := prepareNodeFile(t)
	issued := time.Now().Add(-3 * time.Minute).UTC()
	claims := testTimedClaims("auth-idle", issued, 5*time.Minute)
	_, err := db.Exec(`INSERT INTO local_authorizations
		(authorization_id, asset_id, node_id, issued_at, expires_at, first_seen_at,
		 last_activity_at, status, reason, created_at, updated_at)
		VALUES ('auth-idle', 'asset-1', 'node-1', ?, ?, ?, ?, 'active', '', ?, ?)`,
		issued.Format(time.RFC3339Nano), issued.Add(5*time.Minute).Format(time.RFC3339Nano),
		issued.Add(time.Second).Format(time.RFC3339Nano),
		issued.Add(time.Second).Format(time.RFC3339Nano),
		issued.Format(time.RFC3339Nano), issued.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	token, _ := signer.Sign(claims)
	rec := serveDownload(t, db, storageDir, signer, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("重启后空闲超时应拒绝：code=%d body=%s", rec.Code, rec.Body.String())
	}
	assertLocalAuthorizationStatus(t, db, "auth-idle", authorizationStatusExpiredIdle)
}

func TestLimitWriterExpiresAtMaxDuration(t *testing.T) {
	db, _, _ := prepareNodeFile(t)
	handler := &Handler{DB: db}
	claims := testTimedClaims("auth-max", time.Now().Add(-time.Minute), 30*time.Second)
	writer := &limitCountingWriter{ResponseWriter: httptest.NewRecorder(),
		handler: handler, authorizationID: claims.AuthorizationID, claims: claims,
		limit: 10, timing: authorizationTiming{MaxDeadline: time.Now().Add(-time.Second)}}
	n, err := writer.Write([]byte("abc"))
	if err == nil || n != 0 {
		t.Fatalf("超过最大时长应停止写入：n=%d err=%v", n, err)
	}
	assertLocalAuthorizationStatus(t, db, "auth-max", authorizationStatusExpiredMaxDuration)
}

func TestLimitWriterExpiresAfterIdleWriteWindow(t *testing.T) {
	db, _, _ := prepareNodeFile(t)
	handler := &Handler{DB: db}
	claims := testTimedClaims("auth-write-idle", time.Now(), time.Minute)
	writer := &limitCountingWriter{ResponseWriter: httptest.NewRecorder(),
		handler: handler, authorizationID: claims.AuthorizationID, claims: claims,
		limit: 10, timing: authorizationTiming{MaxDeadline: time.Now().Add(time.Minute),
			IdleTimeout: time.Second, LastWriteAt: time.Now().Add(-2 * time.Second)}}
	n, err := writer.Write([]byte("abc"))
	if err == nil || n != 0 {
		t.Fatalf("写入空闲超时应停止：n=%d err=%v", n, err)
	}
	assertLocalAuthorizationStatus(t, db, "auth-write-idle", authorizationStatusExpiredIdle)
}

func testTimedClaims(id string, issued time.Time, max time.Duration) downloadtoken.Claims {
	return downloadtoken.Claims{TokenVersion: downloadtoken.Version,
		AuthorizationID: id, AssetID: "asset-1", NodeID: "node-1",
		ClientPrefix: "192.0.2.1/32", IssuedAt: issued.UTC().Format(time.RFC3339Nano),
		ExpiresAt:              issued.UTC().Add(max).Format(time.RFC3339Nano),
		FirstConnectionSeconds: 20, IdleTimeoutSeconds: 120,
		MaxDurationSeconds: int(max / time.Second), MaxBytes: 10,
		RangeConcurrencyLimit: 2, RequestID: "req-1"}
}

func serveDownload(t *testing.T, db *sql.DB, storageDir string,
	signer downloadtoken.Signer, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/downloads/asset-1", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	(&Handler{DB: db, Storage: storageDir, NodeID: "node-1",
		Signer: signer}).ServeHTTP(rec, req)
	return rec
}

func assertLocalAuthorizationStatus(t *testing.T, db *sql.DB, id, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM local_authorizations
		WHERE authorization_id = ?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("authorization %s status=%q want %q", id, got, want)
	}
}
