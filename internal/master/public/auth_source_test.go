package public

import (
	"context"
	"testing"
	"time"
)

func TestIssueAuthorizationAccountsWebSource(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}

	challenge, err := store.CreateChallenge(context.Background(), "altcha",
		"asset-1", "192.0.2.1/32", 4, time.Minute, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	auth, _, err := store.IssueAuthorization(context.Background(), challenge, testTokenLifetime(time.Minute), "req-2")
	if err != nil {
		t.Fatal(err)
	}
	var sourceKind string
	if err := db.QueryRow(`SELECT source_kind FROM download_authorizations
		WHERE id = ?`, auth.Claims.AuthorizationID).Scan(&sourceKind); err != nil {
		t.Fatal(err)
	}
	if sourceKind != "web" {
		t.Fatalf("web challenge should record web source, got %q", sourceKind)
	}
	var total, web, api int
	if err := db.QueryRow(`SELECT authorization_count, web_authorization_count, api_authorization_count
		FROM daily_project_stats WHERE project_id = 'p1'`).Scan(&total, &web, &api); err != nil {
		t.Fatal(err)
	}
	if total != 1 || web != 1 || api != 0 {
		t.Fatalf("web source project stats mismatch: total=%d web=%d api=%d", total, web, api)
	}
}
