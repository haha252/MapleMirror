package public

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestV2AbuseScopeMergesWebAndAPI(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	cfg.Challenge.Exact.ElevatedBurst = 2
	tracker := newAbuseTracker(cfg)
	now := time.Now().UTC()
	prefix := "192.0.2.9/32"

	tracker.record(abuseScope("web", "v2"), prefix, 1, now)
	decision := tracker.recordAndDecide(abuseScope("api", "v2"), prefix, 1, now)
	if decision.Level != abuseLevelElevated || decision.ExactBurst != 2 {
		t.Fatalf("V2 Web/API 应共用 download 统计：decision=%+v", decision)
	}

	v1Tracker := newAbuseTracker(cfg)
	v1Tracker.record(abuseScope("web", "v1"), prefix, 1, now)
	v1Decision := v1Tracker.recordAndDecide(abuseScope("api", "v1"), prefix, 1, now)
	if v1Decision.Level != abuseLevelNormal || v1Decision.ExactBurst != 1 {
		t.Fatalf("V1 来源作用域不应被合并：decision=%+v", v1Decision)
	}
}

func TestV2InvalidAuthorizationUsesDownloadAbuseScope(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	tracker := newAbuseTracker(cfg)
	server := Server{AbuseTracker: tracker}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v2/web/authorizations", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	rec := httptest.NewRecorder()
	server.authorize(rec, req, challengeSubmit{
		SourceKind: "web", ProtocolVersion: "v2", Algorithm: vdfAlgorithm,
		ChallengeID: "missing", AssetID: "asset-1",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("缺失挑战状态=%d，期望 404", rec.Code)
	}
	decision := tracker.decide("download", "192.0.2.10/32", time.Now().UTC())
	if decision.ExactBurst != int64(cfg.Challenge.InvalidSolutionWeight) {
		t.Fatalf("V2 无效授权未计入 download：decision=%+v", decision)
	}
	if split := tracker.decide("web", "192.0.2.10/32", time.Now().UTC()); split.ExactBurst != 0 {
		t.Fatalf("V2 无效授权不应继续计入 web 独立作用域：decision=%+v", split)
	}
}
