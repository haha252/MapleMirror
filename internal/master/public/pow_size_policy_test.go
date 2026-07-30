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

func TestPoWSizePolicyBoundaries(t *testing.T) {
	policy, err := newPoWSizePolicy(config.DefaultPoWSizeTiers(), 28)
	if err != nil {
		t.Fatal(err)
	}
	mib := int64(1 << 20)
	tests := []struct {
		size int64
		want int
	}{
		{size: 0, want: 20},
		{size: 5*mib - 1, want: 20},
		{size: 5 * mib, want: 22},
		{size: 50*mib - 1, want: 22},
		{size: 50 * mib, want: 23},
		{size: 150*mib - 1, want: 23},
		{size: 150 * mib, want: 24},
		{size: 300*mib - 1, want: 24},
		{size: 300 * mib, want: 25},
		{size: 500*mib - 1, want: 25},
		{size: 500 * mib, want: 26},
		{size: 8 << 30, want: 26},
	}
	for _, tc := range tests {
		if got := policy.difficulty(tc.size, 0); got != tc.want {
			t.Fatalf("size=%d difficulty=%d，期望 %d", tc.size, got, tc.want)
		}
	}
}

func TestPoWSizePolicyAdaptiveDifficultyCapsAtNormalMaximum(t *testing.T) {
	policy, err := newPoWSizePolicy(config.DefaultPoWSizeTiers(), 28)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(500 << 20)
	for _, tc := range []struct {
		additional int
		want       int
	}{{0, 26}, {2, 28}, {4, 28}} {
		if got := policy.difficulty(size, tc.additional); got != tc.want {
			t.Fatalf("additional=%d difficulty=%d，期望 %d", tc.additional, got, tc.want)
		}
	}
}

func TestPoWSizePolicyRejectsTierAboveNormalMaximum(t *testing.T) {
	_, err := newPoWSizePolicy([]config.PoWSizeTier{
		{MinSize: "0 B", Difficulty: 29},
	}, 28)
	if err == nil || !strings.Contains(err.Error(), "超过普通 PoW 上限") {
		t.Fatalf("应拒绝超过普通上限的分档：%v", err)
	}
}

func TestWebAndAPIChallengesUseSameAssetSizeTier(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	size := int64(160 << 20)
	mustExec(t, db, `UPDATE assets SET size_bytes = ? WHERE id = 'asset-1'`, size)
	mustExec(t, db, `UPDATE node_inventory SET size_bytes = ? WHERE asset_id = 'asset-1'`, size)
	policy, err := newPoWSizePolicy(config.DefaultPoWSizeTiers(), 28)
	if err != nil {
		t.Fatal(err)
	}
	server := Server{
		Store:     Store{DB: db, PoWDifficulty: policy},
		ALTCHATTL: time.Minute,
		APITTL:    time.Minute,
	}
	for _, tc := range []struct {
		path       string
		handler    func(http.ResponseWriter, *http.Request)
		difficulty string
	}{
		{"/api/public/v1/web/challenges", server.webChallenge, "difficulty"},
		{"/api/public/v1/api/challenges", server.apiChallenge, "leading_zero_bits"},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path,
			strings.NewReader(`{"asset_id":"asset-1"}`))
		req.RemoteAddr = "192.0.2.9:1234"
		rec := httptest.NewRecorder()
		tc.handler(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s 创建挑战失败：%d %s", tc.path, rec.Code, rec.Body.String())
		}
		var body struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var difficulty int
		if err := json.Unmarshal(body.Data[tc.difficulty], &difficulty); err != nil {
			t.Fatal(err)
		}
		if difficulty != 24 {
			t.Fatalf("%s difficulty=%d，期望 24", tc.path, difficulty)
		}
	}
}

func TestAPIChallengeAddsAdaptiveDifficultyToSizeTier(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	size := int64(50 << 20)
	mustExec(t, db, `UPDATE assets SET size_bytes = ? WHERE id = 'asset-1'`, size)
	mustExec(t, db, `UPDATE node_inventory SET size_bytes = ? WHERE asset_id = 'asset-1'`, size)
	cfg := testAbuseControl("enforce", true)
	tracker := newAbuseTracker(cfg)
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		tracker.record("api", "192.0.2.9/32", 1, now)
	}
	policy, err := newPoWSizePolicy(config.DefaultPoWSizeTiers(), cfg.Challenge.MaxBits)
	if err != nil {
		t.Fatal(err)
	}
	server := Server{
		Store: Store{DB: db, PoWDifficulty: policy}, APITTL: time.Minute,
		Blocklist: newBlocklistPolicy(config.Quota{}, nil), AbuseTracker: tracker,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/api/challenges",
		strings.NewReader(`{"asset_id":"asset-1"}`))
	req.RemoteAddr = "192.0.2.9:1234"
	rec := httptest.NewRecorder()
	server.apiChallenge(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建挑战失败：%d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Bits int `json:"leading_zero_bits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Bits != 25 {
		t.Fatalf("动态难度=%d，期望 25", body.Data.Bits)
	}
}

func TestPunishmentDifficultyIgnoresNormalPoWPolicyMaximum(t *testing.T) {
	cfg := testAbuseControl("enforce", true)
	server := Server{AbuseTracker: newAbuseTracker(cfg)}
	if got := server.punishmentDifficulty(); got != 128 {
		t.Fatalf("惩罚 PoW difficulty=%d，期望独立保持 128", got)
	}
}
