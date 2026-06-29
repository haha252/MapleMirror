package public

import (
	"database/sql"
	"testing"
)

func assertAuthorizationStateCounters(t *testing.T, db *sql.DB) {
	t.Helper()
	var authCount, webAuthCount, apiAuthCount int
	err := db.QueryRow(`SELECT authorization_count, web_authorization_count,
		api_authorization_count FROM public_stat_totals WHERE id = 'global'`).
		Scan(&authCount, &webAuthCount, &apiAuthCount)
	if err != nil || authCount != 1 || webAuthCount != 0 || apiAuthCount != 1 {
		t.Fatalf("授权状态累计未按 API 来源入账：total=%d web=%d api=%d err=%v",
			authCount, webAuthCount, apiAuthCount, err)
	}
	err = db.QueryRow(`SELECT authorization_count, web_authorization_count,
		api_authorization_count FROM asset_stat_totals WHERE asset_id = 'asset-1'`).
		Scan(&authCount, &webAuthCount, &apiAuthCount)
	if err != nil || authCount != 1 || webAuthCount != 0 || apiAuthCount != 1 {
		t.Fatalf("资源状态累计未按 API 来源入账：total=%d web=%d api=%d err=%v",
			authCount, webAuthCount, apiAuthCount, err)
	}
	err = db.QueryRow(`SELECT authorization_count, web_authorization_count,
		api_authorization_count FROM project_stat_totals WHERE project_id = 'p1'`).
		Scan(&authCount, &webAuthCount, &apiAuthCount)
	if err != nil || authCount != 1 || webAuthCount != 0 || apiAuthCount != 1 {
		t.Fatalf("项目状态累计未按 API 来源入账：total=%d web=%d api=%d err=%v",
			authCount, webAuthCount, apiAuthCount, err)
	}
}
