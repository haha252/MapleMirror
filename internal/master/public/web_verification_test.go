package public

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

func TestWebVerificationEnforceBlocksAndConsumesToken(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := newWebVerificationTestServer(db, "enforce", nil)
	handler := srv.Handler()
	body, token := requestWebVerificationPage(t, handler, "192.0.2.9:1234")
	assertNeutralVerificationMarkup(t, body)

	rec := submitWebVerification(t, handler, token, "asset-1", "192.0.2.9:1234")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"CLIENT_BLOCKED"`) {
		t.Fatalf("valid verification submission should block: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var reason, source string
	if err := db.QueryRow(`SELECT reason, source FROM client_blocks
		WHERE client_prefix_key = '192.0.2.9/32'`).Scan(&reason, &source); err != nil {
		t.Fatal(err)
	}
	if reason != "automated_access" || source != "local_auto_ban" {
		t.Fatalf("block reason/source=%q/%q", reason, source)
	}
	repeat := submitWebVerification(t, handler, token, "asset-1", "192.0.2.9:1234")
	if repeat.Code != http.StatusNotFound {
		t.Fatalf("repeated token should return 404, got %d: %s", repeat.Code, repeat.Body.String())
	}

	blocked := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	handler.ServeHTTP(blocked, req)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("subsequent download should be blocked, got %d: %s", blocked.Code, blocked.Body.String())
	}
}

func TestWebVerificationRejectsInvalidAndCrossClientTokens(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := newWebVerificationTestServer(db, "enforce", nil)
	handler := srv.Handler()
	_, token := requestWebVerificationPage(t, handler, "192.0.2.9:1234")

	invalid := submitWebVerification(t, handler, "invalid", "asset-1", "192.0.2.9:1234")
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid token should return 404, got %d", invalid.Code)
	}
	crossClient := submitWebVerification(t, handler, token, "asset-1", "198.51.100.8:1234")
	if crossClient.Code != http.StatusNotFound {
		t.Fatalf("cross-client token should return 404, got %d", crossClient.Code)
	}
	valid := submitWebVerification(t, handler, token, "asset-1", "192.0.2.9:1234")
	if valid.Code != http.StatusForbidden {
		t.Fatalf("cross-client attempt must not consume the token, got %d", valid.Code)
	}
}

func TestWebVerificationModesAndExemptions(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		db := openMaster(t)
		seedRoutableAsset(t, db)
		srv := newWebVerificationTestServer(db, "off", nil)
		handler := srv.Handler()
		body := requestPageBody(t, handler, "192.0.2.9:1234")
		if strings.Contains(body, `action="/api/public/v1/web/verifications"`) {
			t.Fatalf("off mode should not render verification options: %s", body)
		}
		rec := submitWebVerification(t, handler, "invalid", "asset-1", "192.0.2.9:1234")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("off mode endpoint should return 404, got %d", rec.Code)
		}
	})

	t.Run("monitor", func(t *testing.T) {
		db := openMaster(t)
		seedRoutableAsset(t, db)
		srv := newWebVerificationTestServer(db, "monitor", nil)
		handler := srv.Handler()
		_, token := requestWebVerificationPage(t, handler, "192.0.2.9:1234")
		rec := submitWebVerification(t, handler, token, "asset-1", "192.0.2.9:1234")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("monitor mode should return 204, got %d: %s", rec.Code, rec.Body.String())
		}
		assertNoClientBlocks(t, db)
	})

	t.Run("exempt", func(t *testing.T) {
		db := openMaster(t)
		seedRoutableAsset(t, db)
		srv := newWebVerificationTestServer(db, "enforce", []string{"192.0.2.9/32"})
		handler := srv.Handler()
		_, token := requestWebVerificationPage(t, handler, "192.0.2.9:1234")
		rec := submitWebVerification(t, handler, token, "asset-1", "192.0.2.9:1234")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("exempt source should return 204, got %d: %s", rec.Code, rec.Body.String())
		}
		assertNoClientBlocks(t, db)
	})
}

func newWebVerificationTestServer(db *sql.DB, mode string, exemptions []string) Server {
	abuse := testAbuseControl(mode, false)
	quota := config.Quota{AbuseControl: abuse, Exemptions: exemptions}
	return Server{
		Store:            Store{DB: db, Quota: newQuotaPolicy(quota)},
		AbuseTracker:     newAbuseTracker(abuse),
		Blocklist:        newBlocklistPolicy(quota, nil),
		WebVerifications: newWebVerificationTokenStore(webVerificationTokenCapacity, webVerificationTokenTTL),
	}
}

var verificationTokenPattern = regexp.MustCompile(`name="verification_token" value="([^"]+)"`)

func requestWebVerificationPage(t *testing.T, handler http.Handler, remoteAddr string) (string, string) {
	t.Helper()
	body := requestPageBody(t, handler, remoteAddr)
	match := verificationTokenPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("verification token not found in page: %s", body)
	}
	return body, match[1]
}

func requestPageBody(t *testing.T, handler http.Handler, remoteAddr string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/download/asset-1", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page request code=%d body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func submitWebVerification(t *testing.T, handler http.Handler, token, assetID, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	values := url.Values{
		"verification_token": {token},
		"asset_id":           {assetID},
		"intent":             {"candidate"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/web/verifications",
		strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertNeutralVerificationMarkup(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, forbidden := range []string{"honeypot", "trap", "decoy", "bot", "automation", "punishment"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("rendered page should not contain %q: %s", forbidden, body)
		}
	}
	form := strings.Index(body, `class="download-pow__verification-options"`)
	card := strings.Index(body, `class="download-pow panel-card"`)
	if form < 0 || card < 0 || form >= card {
		t.Fatalf("verification options should precede the visible card: %s", body)
	}
	end := strings.Index(body[form:], "</form>")
	if end < 0 {
		t.Fatalf("verification form is incomplete: %s", body)
	}
	formMarkup := body[form : form+end]
	for _, want := range []string{
		`download-pow__compatibility-warning`,
		`如果你能看到下方这几个按钮，说明你的浏览器大概率不支持本站人机验证。`,
		`请考虑使用最新版 Microsoft Edge 或 Firefox 浏览器。`,
	} {
		if !strings.Contains(formMarkup, want) {
			t.Fatalf("verification form should contain %q: %s", want, formMarkup)
		}
	}
	for _, forbidden := range []string{" hidden", "display:none", "visibility:hidden", " inert"} {
		if strings.Contains(strings.ToLower(formMarkup), forbidden) {
			t.Fatalf("verification form should not contain %q: %s", forbidden, formMarkup)
		}
	}
	if strings.Count(formMarkup, `type="submit"`) != 3 {
		t.Fatalf("verification form should contain three submit buttons: %s", formMarkup)
	}
}

func assertNoClientBlocks(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM client_blocks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("client_blocks count=%d want 0", count)
	}
}
