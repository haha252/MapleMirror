package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
)

func TestAPIV2ChallengeAuthorizationFlow(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	server := Server{Store: store, VDFTTL: time.Minute, TokenLifetime: testTokenLifetime(time.Minute)}
	handler := server.Handler()
	challengeRequest := httptest.NewRequest(http.MethodPost, "/api/public/v2/api/challenges",
		strings.NewReader(`{"asset_id":"asset-1"}`))
	challengeRequest.RemoteAddr = "192.0.2.44:1234"
	challengeResponse := httptest.NewRecorder()
	handler.ServeHTTP(challengeResponse, challengeRequest)
	if challengeResponse.Code != http.StatusCreated {
		t.Fatalf("challenge=%d %s", challengeResponse.Code, challengeResponse.Body.String())
	}
	var envelope struct {
		Data struct {
			ChallengeID string `json:"challenge_id"`
			Algorithm   string `json:"algorithm"`
			Modulus     string `json:"modulus"`
			Base        string `json:"base"`
			Iterations  uint64 `json:"iterations"`
			Encoding    string `json:"encoding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(challengeResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope.Data
	if data.Algorithm != vdfAlgorithm || data.Encoding != vdfEncoding || len(data.Modulus) != 512 || len(data.Base) != 512 {
		t.Fatalf("challenge contract=%+v", data)
	}
	base, _, _ := decodeVDFInteger(data.Base)
	modulus, _, _ := decodeVDFInteger(data.Modulus)
	solution, _ := encodeVDFInteger(sequentialVDFSolution(base, modulus, data.Iterations))
	delivered := deliverNextAuthorization(t, db, "node-1")
	authorizationRequest := httptest.NewRequest(http.MethodPost, "/api/public/v2/api/authorizations",
		strings.NewReader(`{"challenge_id":"`+data.ChallengeID+`","asset_id":"asset-1","solution":"`+solution+`"}`))
	authorizationRequest.RemoteAddr = "192.0.2.44:1234"
	authorizationResponse := httptest.NewRecorder()
	handler.ServeHTTP(authorizationResponse, authorizationRequest)
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
	if authorizationResponse.Code != http.StatusCreated ||
		!strings.Contains(authorizationResponse.Body.String(), `"download_token":"`) {
		t.Fatalf("authorization=%d %s", authorizationResponse.Code, authorizationResponse.Body.String())
	}
	var authorizationEnvelope struct {
		Data struct {
			AuthorizationID string `json:"authorization_id"`
			DownloadToken   string `json:"download_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(authorizationResponse.Body.Bytes(), &authorizationEnvelope); err != nil {
		t.Fatal(err)
	}
	statusRequest := httptest.NewRequest(http.MethodGet,
		"/api/public/v2/authorizations/"+authorizationEnvelope.Data.AuthorizationID, nil)
	statusRequest.RemoteAddr = "192.0.2.44:1234"
	statusRequest.Header.Set("Authorization", "Bearer "+authorizationEnvelope.Data.DownloadToken)
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("V2 status=%d %s", statusResponse.Code, statusResponse.Body.String())
	}
	if _, err := store.LoadChallenge(authorizationRequest.Context(), data.ChallengeID); err == nil {
		t.Fatal("successful authorization did not consume challenge")
	}
}

func TestV2ChallengeRejectsCrossSourceVersionAndAsset(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	challenge, err := store.CreateVDFChallenge(t.Context(), "api", "asset-1",
		"192.0.2.55/32", abuseLevelNormal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := Server{Store: store}
	for _, submit := range []challengeSubmit{
		{SourceKind: "web", ProtocolVersion: "v2", Algorithm: vdfAlgorithm, AssetID: "asset-1"},
		{SourceKind: "api", ProtocolVersion: "v1", Algorithm: "sha256", AssetID: "asset-1"},
		{SourceKind: "api", ProtocolVersion: "v2", Algorithm: vdfAlgorithm, AssetID: "asset-other"},
	} {
		submit.ChallengeID = challenge.ID
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "192.0.2.55:1234"
		rec := httptest.NewRecorder()
		server.authorize(rec, req, submit)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("cross-boundary submit=%+v status=%d", submit, rec.Code)
		}
	}
	if _, err := store.LoadChallenge(t.Context(), challenge.ID); err != nil {
		t.Fatalf("rejected cross-boundary submit consumed challenge: %v", err)
	}
}

func TestV2WebAndAPIChallengeShareAbuseLevel(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := testVDFStore(t, db, config.ChallengeLimits{BucketCapacity: 30,
		BucketFullRefill: "10m", MaxOutstandingExact: 4, MaxOutstandingTotal: 100})
	cfg := testAbuseControl("enforce", true)
	cfg.Challenge.Exact.ElevatedBurst = 2
	server := Server{Store: store, VDFTTL: time.Minute, AbuseTracker: newAbuseTracker(cfg)}
	handler := server.Handler()

	request := func(path string) uint64 {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"asset_id":"asset-1"}`))
		req.RemoteAddr = "192.0.2.66:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var envelope struct {
			Data struct {
				Iterations uint64 `json:"iterations"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.Iterations
	}

	if got := request("/api/public/v2/web/challenges"); got != 8 {
		t.Fatalf("首个 Web V2 挑战 iterations=%d，期望 8", got)
	}
	if got := request("/api/public/v2/api/challenges"); got != 16 {
		t.Fatalf("第二个 API V2 挑战应继承 Web 统计并升级：iterations=%d，期望 16", got)
	}
}
