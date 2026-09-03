package public

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestAuthorizationStatusRequiresBearerToken(t *testing.T) {
	server, authID, _ := prepareAuthorizationStatus(t)
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/"+authID, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	server.authorization(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"DOWNLOAD_TOKEN_INVALID"`) {
		t.Fatalf("expected token error, body=%s", rec.Body.String())
	}
}

func TestAuthorizationStatusRejectsDifferentClientPrefix(t *testing.T) {
	server, authID, token := prepareAuthorizationStatus(t)
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/"+authID, nil)
	req.RemoteAddr = "198.51.100.9:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.authorization(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"DOWNLOAD_TOKEN_INVALID"`) {
		t.Fatalf("expected token error, body=%s", rec.Body.String())
	}
}

func TestAuthorizationStatusAllowsMatchingClientPrefix(t *testing.T) {
	server, authID, token := prepareAuthorizationStatus(t)
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/"+authID, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.authorization(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthorizationStatusReturnsNodeNameWithLegacyNodeIDAlias(t *testing.T) {
	server, authID, token := prepareAuthorizationStatus(t)
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/"+authID, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.authorization(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"node_name":"节点一"`) ||
		!strings.Contains(rec.Body.String(), `"node_id":"节点一"`) {
		t.Fatalf("expected node_name plus legacy node_id alias, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "node-1") {
		t.Fatalf("raw node id should not be exposed, body=%s", rec.Body.String())
	}
}

func TestAuthorizationStatusUsesReservationSettledBytes(t *testing.T) {
	server, authID, token := prepareAuthorizationStatus(t)
	if _, err := server.Store.DB.Exec(`UPDATE traffic_reservations
		SET settled_bytes = 64 WHERE authorization_id = ?`, authID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/public/v1/authorizations/"+authID, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.authorization(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"sent_bytes":64`) {
		t.Fatalf("expected settled bytes in response, body=%s", rec.Body.String())
	}
}

func prepareAuthorizationStatus(t *testing.T) (Server, string, string) {
	t.Helper()
	db := openMaster(t)
	seedRoutableAsset(t, db)
	signer := testDownloadTokenSigner(t)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 0, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(auth.Claims)
	if err != nil {
		t.Fatal(err)
	}
	return Server{Store: store, Signer: signer}, auth.Claims.AuthorizationID, token
}

func testDownloadTokenSigner(t *testing.T) downloadtoken.Signer {
	t.Helper()
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "token.key")
	publicPath := filepath.Join(dir, "token.pub")
	if err := downloadtoken.GenerateKeyFiles(privatePath, publicPath); err != nil {
		t.Fatal(err)
	}
	signer, err := downloadtoken.NewSignerFromPrivateFile(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
