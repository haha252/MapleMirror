package public

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/downloadtoken"
)

func TestAuthorizationFailureKeepsChallengeRetryable(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db, Quota: newQuotaPolicy(config.Quota{
		RequestBuckets: config.RequestBuckets{
			IPv432: config.Bucket{Capacity: 1, FullRefill: "48h"},
			IPv424: config.Bucket{Capacity: 10, FullRefill: "48h"},
		},
	})}
	mustExec(t, db, `UPDATE projects SET download_multiplier = 3 WHERE id = 'p1'`)
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if !errors.Is(err, errRequestQuota) {
		t.Fatalf("quota failure should be returned, got %v", err)
	}
	assertChallengeLoadable(t, store, challenge.ID)
	assertPublicTableCount(t, db, "download_authorizations", 0)
	assertPublicTableCount(t, db, "traffic_reservations", 0)
}

func TestSignedAuthorizationFailureKeepsChallengeRetryable(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	signErr := errors.New("sign failed")

	_, _, _, err = store.IssueSignedAuthorization(context.Background(), challenge,
		testTokenLifetime(time.Minute), "req-2", func(downloadtoken.Claims) (string, error) {
			return "", signErr
		})
	if !errors.Is(err, signErr) {
		t.Fatalf("sign failure should be returned, got %v", err)
	}
	assertChallengeLoadable(t, store, challenge.ID)
	assertPublicTableCount(t, db, "download_authorizations", 0)
	assertPublicTableCount(t, db, "traffic_reservations", 0)
}

func TestHTTPAuthorizationReturnsShortOpaqueToken(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Store: store, TokenLifetime: testTokenLifetime(time.Minute)}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/authorizations", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	delivered := deliverNextAuthorization(t, db, "node-1")

	server.authorize(rec, req, challengeSubmit{
		Kind:        "api_pow",
		ChallengeID: challenge.ID,
		AssetID:     "asset-1",
		Solution:    solveNonce(challenge),
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("authorization status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"download_token":"`) {
		t.Fatalf("response should include download token: %s", body)
	}
	token := body[strings.Index(body, `"download_token":"`)+len(`"download_token":"`):]
	token = token[:strings.Index(token, `"`)]
	if len(token) != 43 {
		t.Fatalf("opaque token length=%d want 43: %s", len(token), token)
	}
	var hash string
	if err := db.QueryRow(`SELECT token_hash FROM download_authorizations
		WHERE asset_id = 'asset-1'`).Scan(&hash); err != nil || hash == "" || strings.Contains(hash, token) {
		t.Fatalf("authorization should store only token hash: hash=%q err=%v", hash, err)
	}
}

func TestHTTPAuthorizationWaitsForNodeDeliveryBeforeResponding(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Store: store, TokenLifetime: testTokenLifetime(time.Minute)}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/authorizations", nil)
	req.RemoteAddr = "192.0.2.1:12345"
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.authorize(rec, req, challengeSubmit{
			Kind:        "api_pow",
			ChallengeID: challenge.ID,
			AssetID:     "asset-1",
			Solution:    solveNonce(challenge),
		})
	}()

	select {
	case <-done:
		t.Fatalf("authorization responded before node delivery: status=%d body=%s",
			rec.Code, rec.Body.String())
	case <-time.After(authorizationDeliveryPoll + 50*time.Millisecond):
	}
	var id string
	deadline := time.After(2 * time.Second)
	for id == "" {
		err := db.QueryRow(`SELECT id FROM download_authorizations
			WHERE asset_id = 'asset-1'`).Scan(&id)
		if err == nil {
			break
		}
		if err != sql.ErrNoRows {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for authorization insert")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	mustExec(t, db, `UPDATE download_authorizations
		SET delivered_at = '2026-01-01T00:00:03Z' WHERE id = '`+id+`'`)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authorization did not respond after node delivery")
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("authorization status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"download_url":"https://node-1.example.com/p1/v1/a.zip"`) {
		t.Fatalf("response should include real download url after delivery: %s", rec.Body.String())
	}
}

func TestHTTPAuthorizationRejectsDifferentClientPrefix(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	challenge, err := store.CreateChallenge(context.Background(), "api_pow",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Store: store, Signer: testDownloadTokenSigner(t),
		TokenLifetime: testTokenLifetime(time.Minute)}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/authorizations", nil)
	req.RemoteAddr = "198.51.100.9:12345"
	rec := httptest.NewRecorder()

	server.authorize(rec, req, challengeSubmit{
		Kind:        "api_pow",
		ChallengeID: challenge.ID,
		AssetID:     "asset-1",
		Solution:    solveNonce(challenge),
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"CHALLENGE_FAILED"`) {
		t.Fatalf("expected challenge failure, body=%s", rec.Body.String())
	}
	assertChallengeLoadable(t, store, challenge.ID)
	assertPublicTableCount(t, db, "download_authorizations", 0)
	assertPublicTableCount(t, db, "traffic_reservations", 0)
	assertPublicTableCount(t, db, "quota_buckets", 0)
}
